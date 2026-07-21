package aws

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/homeport/homeport/internal/domain/authz"
)

type SQSAdapter struct {
	mu           sync.Mutex
	queues       map[string]*sqsQueue
	idempotency  map[string]string
	queueQuota   int
	messageQuota int
	authorizer   authz.Authorizer
	auditSink    func(authz.Decision)
}

type SQSOption func(*SQSAdapter)

type sqsQueue struct {
	Name       string
	URL        string
	Messages   []sqsMessage
	Inflight   map[string]sqsMessage
	Attributes map[string]string
	Tags       map[string]string
	Dedup      map[string]time.Time
}

type sqsMessage struct {
	ID                     string
	ReceiptHandle          string
	Body                   string
	MessageAttributes      map[string]sqsMessageAttribute
	MessageGroupID         string
	MessageDeduplicationID string
	SequenceNumber         string
	AWSTraceHeader         string
	VisibleAt              time.Time
	CreatedAt              time.Time
	FirstReceivedAt        time.Time
	ReceiveCount           int
}

type sqsMessageAttribute struct {
	DataType    string
	StringValue string
	BinaryValue []byte
}

var sqsNumberAttributePattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func NewSQSAdapter(options ...SQSOption) *SQSAdapter {
	adapter := &SQSAdapter{
		queues:      make(map[string]*sqsQueue),
		idempotency: map[string]string{},
		authorizer:  authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithSQSAuthorizer(authorizer authz.Authorizer) SQSOption {
	return func(adapter *SQSAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithSQSAuditSink(sink func(authz.Decision)) SQSOption {
	return func(adapter *SQSAdapter) {
		adapter.auditSink = sink
	}
}

func WithSQSQueueQuota(maxQueues int) SQSOption {
	return func(adapter *SQSAdapter) {
		adapter.queueQuota = maxQueues
	}
}

func WithSQSMessageQuota(maxMessages int) SQSOption {
	return func(adapter *SQSAdapter) {
		adapter.messageQuota = maxMessages
	}
}

func (SQSAdapter) Provider() string { return "aws" }
func (SQSAdapter) Service() string  { return "sqs" }
func (SQSAdapter) Routes() []string { return []string{"POST /compat/aws/sqs"} }
func (SQSAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_SQS":    "http://homeport:8080/api/v1/compat/aws/sqs",
		"HOMEPORT_COMPAT_BACKEND": "rabbitmq",
	}
}
func (SQSAdapter) ConformanceChecks() []string {
	return []string{"create-queue", "send-message", "receive-message", "delete-message"}
}
func (a *SQSAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if !a.authorized(w, r, action, body) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateQueue":
		queueName := stringValue(body["QueueName"])
		if queueName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "QueueName is required"})
			return
		}
		attributes := mapValue(body["Attributes"])
		if !sqsAttributeNamesValid(attributes) {
			writeSQSInvalidAttributeName(w)
			return
		}
		if !sqsAttributesValid(attributes) {
			writeSQSInvalidAttributeValue(w)
			return
		}
		if !sqsQueueNameValid(queueName, attributes["FifoQueue"] == "true") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "invalid QueueName"})
			return
		}
		queueURL := queueURL(r, queueName)
		q := a.queueByURL(queueURL)
		if q == nil {
			if a.queueQuota > 0 && len(a.queues) >= a.queueQuota {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{
					"__type":  "RequestThrottled",
					"message": "queue quota exceeded",
				})
				return
			}
			q = &sqsQueue{
				Name:     queueName,
				URL:      queueURL,
				Inflight: make(map[string]sqsMessage),
				Tags:     map[string]string{},
				Dedup:    map[string]time.Time{},
				Attributes: map[string]string{
					"VisibilityTimeout":      "30",
					"DelaySeconds":           "0",
					"MessageRetentionPeriod": "345600",
				},
			}
			a.queues[queueURL] = q
			mergeStringMap(q.Attributes, attributes)
			mergeStringMap(q.Tags, sqsTags(body))
			writeJSON(w, http.StatusOK, map[string]string{"QueueUrl": queueURL})
			return
		}
		if !sqsAttributesMatch(q.Attributes, attributes) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"__type":  "QueueNameExists",
				"message": "queue already exists with different attributes",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"QueueUrl": queueURL})
	case "GetQueueUrl":
		queueName := stringValue(body["QueueName"])
		queueURL := queueURL(r, queueName)
		if a.queueByURL(queueURL) == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"QueueUrl": queueURL})
	case "ListQueues":
		a.writeListQueues(w, body)
	case "SetQueueAttributes":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		attributes := mapValue(body["Attributes"])
		if !sqsAttributeNamesValid(attributes) {
			writeSQSInvalidAttributeName(w)
			return
		}
		if !sqsAttributesValid(attributes) {
			writeSQSInvalidAttributeValue(w)
			return
		}
		mergeStringMap(q.Attributes, attributes)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "GetQueueAttributes":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			if strings.Contains(r.UserAgent(), "Terraform") {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"__type":  "AWS.SimpleQueueService.NonExistentQueue",
					"message": "queue not found",
				})
				return
			}
			writeSQSQueueDoesNotExist(w)
			return
		}
		attributes, ok := sqsSelectedAttributes(q.Attributes, body["AttributeNames"])
		if !ok {
			writeSQSInvalidAttributeName(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]map[string]string{"Attributes": attributes})
	case "ListQueueTags":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]map[string]string{"Tags": q.Tags})
	case "TagQueue":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		mergeStringMap(q.Tags, sqsTags(body))
		writeJSON(w, http.StatusOK, map[string]string{})
	case "UntagQueue":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		for _, key := range sqsTagKeys(body) {
			delete(q.Tags, key)
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "SendMessage":
		queueURL := stringValue(body["QueueUrl"])
		q := a.queueByURL(queueURL)
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		messageBody := stringValue(body["MessageBody"])
		if !sqsMessageBodyValid(messageBody) {
			writeSQSInvalidMessageContents(w)
			return
		}
		if len(messageBody) > secondsAttr(q.Attributes, "MaximumMessageSize", 1048576) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "MessageBody exceeds MaximumMessageSize"})
			return
		}
		if q.Attributes["FifoQueue"] == "true" {
			messageGroupID := stringValue(body["MessageGroupId"])
			if messageGroupID == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "MissingParameter", "message": "The request must contain the parameter MessageGroupId."})
				return
			}
			if len(messageGroupID) > 128 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "MessageGroupId is too long"})
				return
			}
			messageDeduplicationID := stringValue(body["MessageDeduplicationId"])
			if messageDeduplicationID == "" && q.Attributes["ContentBasedDeduplication"] != "true" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "MessageDeduplicationId is required for FIFO queues without content-based deduplication"})
				return
			}
			if len(messageDeduplicationID) > 128 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "MessageDeduplicationId is too long"})
				return
			}
			if _, ok := body["DelaySeconds"]; ok {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "DelaySeconds is not valid for FIFO queues"})
				return
			}
		}
		delay := secondsValue(body["DelaySeconds"], secondsAttr(q.Attributes, "DelaySeconds", 0))
		if delay < 0 || delay > 900 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "invalid DelaySeconds"})
			return
		}
		if !sqsMessageAttributesValid(body["MessageAttributes"]) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "message attribute is invalid"})
			return
		}
		if !sqsMessageWithAttributesFitsLimit(messageBody, body["MessageAttributes"], secondsAttr(q.Attributes, "MaximumMessageSize", 1048576)) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "message and attributes exceed MaximumMessageSize"})
			return
		}
		if !sqsMessageWithAttributesSizeValid(messageBody, body["MessageAttributes"]) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "message and attributes exceed 1048576 bytes"})
			return
		}
		idempotencyKey := sqsIdempotencyKey(r, action, queueURL)
		if idempotencyKey != "" {
			if id, ok := a.idempotency[idempotencyKey]; ok {
				writeJSON(w, http.StatusOK, map[string]string{"MessageId": id})
				return
			}
		}
		id := fmt.Sprintf("msg-%d", len(q.Messages)+len(q.Inflight)+1)
		now := time.Now()
		q.purgeExpired(now)
		sequenceNumber := ""
		if q.Attributes["FifoQueue"] == "true" {
			sequenceNumber = sqsSequenceNumber(now, len(q.Messages)+len(q.Inflight)+1)
		}
		messageAttributes := sqsStringMessageAttributes(body["MessageAttributes"])
		messageAttributeMD5 := sqsMessageAttributesMD5(messageAttributes)
		systemAttributeMD5 := sqsTraceHeaderMD5(body["MessageSystemAttributes"])
		if a.messageQuota > 0 && q.messageCount() >= a.messageQuota {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"__type":  "RequestThrottled",
				"message": "message quota exceeded",
			})
			return
		}
		dedupID := ""
		if q.Attributes["FifoQueue"] == "true" {
			dedupID = sqsDeduplicationID(q, messageBody, body)
			if dedupID != "" {
				if q.duplicate(dedupID, now) {
					response := map[string]string{"MessageId": id, "MD5OfMessageBody": sqsBodyMD5(messageBody), "SequenceNumber": sequenceNumber}
					if messageAttributeMD5 != "" {
						response["MD5OfMessageAttributes"] = messageAttributeMD5
					}
					if systemAttributeMD5 != "" {
						response["MD5OfMessageSystemAttributes"] = systemAttributeMD5
					}
					writeJSON(w, http.StatusOK, response)
					return
				}
				q.Dedup[dedupID] = now.Add(5 * time.Minute)
			}
		}
		msg := sqsMessage{
			ID:                     id,
			Body:                   messageBody,
			MessageAttributes:      messageAttributes,
			MessageGroupID:         stringValue(body["MessageGroupId"]),
			MessageDeduplicationID: dedupID,
			SequenceNumber:         sequenceNumber,
			AWSTraceHeader:         sqsAWSTraceHeader(body["MessageSystemAttributes"]),
			VisibleAt:              now.Add(time.Duration(delay) * time.Second),
			CreatedAt:              now,
		}
		q.Messages = append(q.Messages, msg)
		if idempotencyKey != "" {
			a.idempotency[idempotencyKey] = id
		}
		response := map[string]string{"MessageId": id, "MD5OfMessageBody": sqsBodyMD5(messageBody)}
		if sequenceNumber != "" {
			response["SequenceNumber"] = sequenceNumber
		}
		if messageAttributeMD5 != "" {
			response["MD5OfMessageAttributes"] = messageAttributeMD5
		}
		if systemAttributeMD5 != "" {
			response["MD5OfMessageSystemAttributes"] = systemAttributeMD5
		}
		writeJSON(w, http.StatusOK, response)
	case "SendMessageBatch":
		queueURL := stringValue(body["QueueUrl"])
		q := a.queueByURL(queueURL)
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		entries, _ := body["Entries"].([]any)
		if len(entries) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "EmptyBatchRequest", "message": "batch request is empty"})
			return
		}
		if len(entries) > 10 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "TooManyEntriesInBatchRequest", "message": "batch request has too many entries"})
			return
		}
		if !sqsBatchEntryIDsValid(entries) {
			writeSQSInvalidBatchEntryID(w)
			return
		}
		if !sqsBatchEntryIDsDistinct(entries) {
			writeSQSBatchEntryIDsNotDistinct(w)
			return
		}
		if sqsBatchMessageBytes(entries) > 1048576 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "BatchRequestTooLong", "message": "batch request is too long"})
			return
		}
		now := time.Now()
		q.purgeExpired(now)
		successful := []map[string]string{}
		failed := []map[string]any{}
		for _, raw := range entries {
			entry, _ := raw.(map[string]any)
			entryID := stringValue(entry["Id"])
			messageBody := stringValue(entry["MessageBody"])
			delay := secondsValue(entry["DelaySeconds"], secondsAttr(q.Attributes, "DelaySeconds", 0))
			if !sqsMessageBodyValid(messageBody) {
				failed = append(failed, map[string]any{"Id": entryID, "Code": "InvalidMessageContents", "Message": "message contents are invalid", "SenderFault": true})
				continue
			}
			if q.Attributes["FifoQueue"] == "true" {
				if message := sqsFIFOEntryInvalidMessage(q, entry); message != "" {
					failed = append(failed, map[string]any{"Id": entryID, "Code": "InvalidParameterValue", "Message": message, "SenderFault": true})
					continue
				}
			}
			if len(messageBody) > secondsAttr(q.Attributes, "MaximumMessageSize", 1048576) || delay < 0 || delay > 900 {
				failed = append(failed, map[string]any{"Id": entryID, "Code": "InvalidParameterValue", "Message": "invalid batch entry", "SenderFault": true})
				continue
			}
			if !sqsMessageAttributesValid(entry["MessageAttributes"]) {
				failed = append(failed, map[string]any{"Id": entryID, "Code": "InvalidParameterValue", "Message": "message attribute is invalid", "SenderFault": true})
				continue
			}
			if !sqsMessageWithAttributesFitsLimit(messageBody, entry["MessageAttributes"], secondsAttr(q.Attributes, "MaximumMessageSize", 1048576)) {
				failed = append(failed, map[string]any{"Id": entryID, "Code": "InvalidParameterValue", "Message": "message and attributes exceed MaximumMessageSize", "SenderFault": true})
				continue
			}
			if !sqsMessageWithAttributesSizeValid(messageBody, entry["MessageAttributes"]) {
				failed = append(failed, map[string]any{"Id": entryID, "Code": "InvalidParameterValue", "Message": "message and attributes exceed 1048576 bytes", "SenderFault": true})
				continue
			}
			if a.messageQuota > 0 && q.messageCount() >= a.messageQuota {
				failed = append(failed, map[string]any{"Id": entryID, "Code": "RequestThrottled", "Message": "message quota exceeded", "SenderFault": false})
				continue
			}
			id := fmt.Sprintf("msg-%d", len(q.Messages)+len(q.Inflight)+1)
			messageAttributes := sqsStringMessageAttributes(entry["MessageAttributes"])
			success := map[string]string{"Id": entryID, "MessageId": id, "MD5OfMessageBody": sqsBodyMD5(messageBody)}
			if messageAttributeMD5 := sqsMessageAttributesMD5(messageAttributes); messageAttributeMD5 != "" {
				success["MD5OfMessageAttributes"] = messageAttributeMD5
			}
			if systemAttributeMD5 := sqsTraceHeaderMD5(entry["MessageSystemAttributes"]); systemAttributeMD5 != "" {
				success["MD5OfMessageSystemAttributes"] = systemAttributeMD5
			}
			dedupID := ""
			sequenceNumber := ""
			if q.Attributes["FifoQueue"] == "true" {
				sequenceNumber = sqsSequenceNumber(now, len(successful)+1)
				success["SequenceNumber"] = sequenceNumber
				dedupID = sqsDeduplicationID(q, messageBody, entry)
				if dedupID != "" {
					if q.duplicate(dedupID, now) {
						successful = append(successful, success)
						continue
					}
					q.Dedup[dedupID] = now.Add(5 * time.Minute)
				}
			}
			q.Messages = append(q.Messages, sqsMessage{
				ID:                     id,
				Body:                   messageBody,
				MessageAttributes:      messageAttributes,
				MessageGroupID:         stringValue(entry["MessageGroupId"]),
				MessageDeduplicationID: dedupID,
				SequenceNumber:         sequenceNumber,
				AWSTraceHeader:         sqsAWSTraceHeader(entry["MessageSystemAttributes"]),
				VisibleAt:              now.Add(time.Duration(delay) * time.Second),
				CreatedAt:              now,
			})
			successful = append(successful, success)
		}
		writeJSON(w, http.StatusOK, map[string]any{"Successful": successful, "Failed": failed})
	case "ReceiveMessage":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		maxMessages := secondsValue(body["MaxNumberOfMessages"], 1)
		if maxMessages < 1 || maxMessages > 10 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "invalid MaxNumberOfMessages"})
			return
		}
		waitTime := secondsValue(body["WaitTimeSeconds"], 0)
		if waitTime < 0 || waitTime > 20 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "invalid WaitTimeSeconds"})
			return
		}
		visibility := secondsValue(body["VisibilityTimeout"], secondsAttr(q.Attributes, "VisibilityTimeout", 30))
		if visibility < 0 || visibility > 43200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "invalid VisibilityTimeout"})
			return
		}
		now := time.Now()
		a.requeueExpired(q, now)
		q.purgeExpired(now)
		blockedGroups := q.blockedMessageGroups()
		messages := []map[string]any{}
		for i := 0; i < len(q.Messages) && len(messages) < maxMessages; {
			msg := q.Messages[i]
			if msg.VisibleAt.After(now) {
				i++
				continue
			}
			if q.Attributes["FifoQueue"] == "true" && msg.MessageGroupID != "" {
				if _, blocked := blockedGroups[msg.MessageGroupID]; blocked {
					i++
					continue
				}
				blockedGroups[msg.MessageGroupID] = struct{}{}
			}
			receipt := fmt.Sprintf("%s-rh-%d", msg.ID, now.UnixNano())
			msg.ReceiptHandle = receipt
			msg.ReceiveCount++
			if msg.FirstReceivedAt.IsZero() {
				msg.FirstReceivedAt = now
			}
			msg.VisibleAt = now.Add(time.Duration(visibility) * time.Second)
			q.Inflight[receipt] = msg
			q.Messages = append(q.Messages[:i], q.Messages[i+1:]...)
			message := map[string]any{
				"MessageId":     msg.ID,
				"ReceiptHandle": receipt,
				"Body":          msg.Body,
				"MD5OfBody":     sqsBodyMD5(msg.Body),
			}
			if attrs := sqsMessageSystemAttributes(msg, body["MessageSystemAttributeNames"], body["AttributeNames"]); len(attrs) > 0 {
				message["Attributes"] = attrs
			}
			if attrs := sqsMessageAttributes(msg.MessageAttributes, body["MessageAttributeNames"]); len(attrs) > 0 {
				message["MessageAttributes"] = attrs
				message["MD5OfMessageAttributes"] = sqsMessageAttributesMD5(msg.MessageAttributes)
			}
			messages = append(messages, message)
		}
		writeJSON(w, http.StatusOK, map[string]any{"Messages": messages})
	case "DeleteMessage":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		receipt := stringValue(body["ReceiptHandle"])
		if _, ok := q.Inflight[receipt]; !ok {
			writeSQSReceiptHandleIsInvalid(w)
			return
		}
		delete(q.Inflight, receipt)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DeleteMessageBatch":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		entries, _ := body["Entries"].([]any)
		if len(entries) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "EmptyBatchRequest", "message": "batch request is empty"})
			return
		}
		if len(entries) > 10 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "TooManyEntriesInBatchRequest", "message": "batch request has too many entries"})
			return
		}
		if !sqsBatchEntryIDsValid(entries) {
			writeSQSInvalidBatchEntryID(w)
			return
		}
		if !sqsBatchEntryIDsDistinct(entries) {
			writeSQSBatchEntryIDsNotDistinct(w)
			return
		}
		successful := []map[string]string{}
		failed := []map[string]any{}
		for _, raw := range entries {
			entry, _ := raw.(map[string]any)
			entryID := stringValue(entry["Id"])
			receipt := stringValue(entry["ReceiptHandle"])
			if _, ok := q.Inflight[receipt]; !ok {
				failed = append(failed, map[string]any{"Id": entryID, "Code": "ReceiptHandleIsInvalid", "Message": "receipt handle is invalid", "SenderFault": true})
				continue
			}
			delete(q.Inflight, receipt)
			successful = append(successful, map[string]string{"Id": entryID})
		}
		writeJSON(w, http.StatusOK, map[string]any{"Successful": successful, "Failed": failed})
	case "DeleteQueue":
		queueURL := stringValue(body["QueueUrl"])
		if a.queueByURL(queueURL) == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		delete(a.queues, queueURL)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "PurgeQueue":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		q.Messages = nil
		q.Inflight = make(map[string]sqsMessage)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ChangeMessageVisibility":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		receipt := stringValue(body["ReceiptHandle"])
		msg, ok := q.Inflight[receipt]
		if !ok {
			writeSQSReceiptHandleIsInvalid(w)
			return
		}
		visibility := secondsValue(body["VisibilityTimeout"], secondsAttr(q.Attributes, "VisibilityTimeout", 30))
		if visibility < 0 || visibility > 43200 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValue", "message": "invalid VisibilityTimeout"})
			return
		}
		delete(q.Inflight, receipt)
		msg.VisibleAt = time.Now().Add(time.Duration(visibility) * time.Second)
		msg.ReceiptHandle = ""
		q.Messages = append(q.Messages, msg)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ChangeMessageVisibilityBatch":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeSQSQueueDoesNotExist(w)
			return
		}
		entries, _ := body["Entries"].([]any)
		if len(entries) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "EmptyBatchRequest", "message": "batch request is empty"})
			return
		}
		if len(entries) > 10 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "TooManyEntriesInBatchRequest", "message": "batch request has too many entries"})
			return
		}
		if !sqsBatchEntryIDsValid(entries) {
			writeSQSInvalidBatchEntryID(w)
			return
		}
		if !sqsBatchEntryIDsDistinct(entries) {
			writeSQSBatchEntryIDsNotDistinct(w)
			return
		}
		successful := []map[string]string{}
		failed := []map[string]any{}
		now := time.Now()
		for _, raw := range entries {
			entry, _ := raw.(map[string]any)
			entryID := stringValue(entry["Id"])
			receipt := stringValue(entry["ReceiptHandle"])
			msg, ok := q.Inflight[receipt]
			if !ok {
				failed = append(failed, map[string]any{"Id": entryID, "Code": "ReceiptHandleIsInvalid", "Message": "receipt handle is invalid", "SenderFault": true})
				continue
			}
			visibility := secondsValue(entry["VisibilityTimeout"], secondsAttr(q.Attributes, "VisibilityTimeout", 30))
			if visibility < 0 || visibility > 43200 {
				failed = append(failed, map[string]any{"Id": entryID, "Code": "InvalidParameterValue", "Message": "invalid VisibilityTimeout", "SenderFault": true})
				continue
			}
			delete(q.Inflight, receipt)
			msg.VisibleAt = now.Add(time.Duration(visibility) * time.Second)
			msg.ReceiptHandle = ""
			q.Messages = append(q.Messages, msg)
			successful = append(successful, map[string]string{"Id": entryID})
		}
		writeJSON(w, http.StatusOK, map[string]any{"Successful": successful, "Failed": failed})
	default:
		writeSQSUnsupportedOperation(w, action)
	}
}

func (a *SQSAdapter) writeListQueues(w http.ResponseWriter, body map[string]any) {
	prefix := stringValue(body["QueueNamePrefix"])
	queueURLs := make([]string, 0, len(a.queues))
	for _, q := range a.queues {
		if prefix == "" || strings.HasPrefix(q.Name, prefix) {
			queueURLs = append(queueURLs, q.URL)
		}
	}
	sort.Strings(queueURLs)

	start := 0
	if token := stringValue(body["NextToken"]); token != "" {
		parsed, err := strconv.Atoi(token)
		if err != nil || parsed < 0 || parsed > len(queueURLs) {
			writeSQSInvalidAttributeValue(w)
			return
		}
		start = parsed
	}
	maxResults := 1000
	if _, ok := body["MaxResults"]; ok {
		maxResults = secondsValue(body["MaxResults"], 0)
		if maxResults < 1 || maxResults > 1000 {
			writeSQSInvalidAttributeValue(w)
			return
		}
	}
	end := start + maxResults
	if end > len(queueURLs) {
		end = len(queueURLs)
	}
	out := map[string]any{"QueueUrls": queueURLs[start:end]}
	if end < len(queueURLs) {
		out["NextToken"] = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, out)
}

func writeSQSQueueDoesNotExist(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "QueueDoesNotExist",
		"message": "queue not found",
	})
}

func writeSQSReceiptHandleIsInvalid(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "ReceiptHandleIsInvalid",
		"message": "receipt handle is invalid",
	})
}

func writeSQSInvalidAttributeValue(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "InvalidAttributeValue",
		"message": "attribute value is invalid",
	})
}

func writeSQSInvalidAttributeName(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "InvalidAttributeName",
		"message": "attribute name is invalid",
	})
}

func writeSQSInvalidMessageContents(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "InvalidMessageContents",
		"message": "message contents are invalid",
	})
}

func writeSQSUnsupportedOperation(w http.ResponseWriter, action string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "UnsupportedOperation",
		"message": "unsupported SQS action: " + action,
	})
}

func writeSQSBatchEntryIDsNotDistinct(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "BatchEntryIdsNotDistinct",
		"message": "batch entry ids are not distinct",
	})
}

func writeSQSInvalidBatchEntryID(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "InvalidBatchEntryId",
		"message": "batch entry id is invalid",
	})
}

func sqsAttributesValid(attrs map[string]string) bool {
	return sqsIntAttrInRange(attrs, "VisibilityTimeout", 0, 43200) &&
		sqsIntAttrInRange(attrs, "DelaySeconds", 0, 900) &&
		sqsIntAttrInRange(attrs, "MessageRetentionPeriod", 60, 1209600) &&
		sqsIntAttrInRange(attrs, "MaximumMessageSize", 1024, 1048576) &&
		sqsIntAttrInRange(attrs, "ReceiveMessageWaitTimeSeconds", 0, 20) &&
		sqsIntAttrInRange(attrs, "KmsDataKeyReusePeriodSeconds", 60, 86400)
}

func sqsAttributeNamesValid(attrs map[string]string) bool {
	for name := range attrs {
		if _, ok := sqsKnownAttributeNames[name]; !ok {
			return false
		}
	}
	return true
}

func sqsQueueNameValid(name string, fifo bool) bool {
	if name == "" || len(name) > 80 {
		return false
	}
	if fifo {
		return strings.HasSuffix(name, ".fifo") && sqsQueueNameCharsValid(strings.TrimSuffix(name, ".fifo"))
	}
	return sqsQueueNameCharsValid(name)
}

func sqsQueueNameCharsValid(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func sqsFIFOEntryInvalidMessage(q *sqsQueue, entry map[string]any) string {
	messageGroupID := stringValue(entry["MessageGroupId"])
	if messageGroupID == "" {
		return "MessageGroupId is required for FIFO queues"
	}
	if len(messageGroupID) > 128 {
		return "MessageGroupId is too long"
	}
	messageDeduplicationID := stringValue(entry["MessageDeduplicationId"])
	if messageDeduplicationID == "" && q.Attributes["ContentBasedDeduplication"] != "true" {
		return "MessageDeduplicationId is required for FIFO queues without content-based deduplication"
	}
	if len(messageDeduplicationID) > 128 {
		return "MessageDeduplicationId is too long"
	}
	if _, ok := entry["DelaySeconds"]; ok {
		return "DelaySeconds is not valid for FIFO queues"
	}
	return ""
}

func sqsDeduplicationID(q *sqsQueue, body string, request map[string]any) string {
	if id := stringValue(request["MessageDeduplicationId"]); id != "" {
		return id
	}
	if q.Attributes["ContentBasedDeduplication"] != "true" {
		return ""
	}
	sum := sha256.Sum256([]byte(body))
	return fmt.Sprintf("%x", sum[:])
}

func sqsSequenceNumber(now time.Time, offset int) string {
	return fmt.Sprintf("%039d", now.UnixNano()+int64(offset))
}

func sqsBodyMD5(body string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(body)))
}

func sqsMessageAttributesMD5(attrs map[string]sqsMessageAttribute) string {
	if len(attrs) == 0 {
		return ""
	}
	var buf bytes.Buffer
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		attr := attrs[name]
		sqsWriteMD5String(&buf, name)
		sqsWriteMD5String(&buf, attr.DataType)
		if strings.HasPrefix(attr.DataType, "Binary") {
			buf.WriteByte(2)
			_ = binary.Write(&buf, binary.BigEndian, uint32(len(attr.BinaryValue)))
			buf.Write(attr.BinaryValue)
			continue
		}
		buf.WriteByte(1)
		sqsWriteMD5String(&buf, attr.StringValue)
	}
	return fmt.Sprintf("%x", md5.Sum(buf.Bytes()))
}

func sqsTraceHeaderMD5(raw any) string {
	traceHeader := sqsAWSTraceHeader(raw)
	if traceHeader == "" {
		return ""
	}
	var buf bytes.Buffer
	sqsWriteMD5String(&buf, "AWSTraceHeader")
	sqsWriteMD5String(&buf, "String")
	buf.WriteByte(1)
	sqsWriteMD5String(&buf, traceHeader)
	return fmt.Sprintf("%x", md5.Sum(buf.Bytes()))
}

func sqsWriteMD5String(buf *bytes.Buffer, value string) {
	_ = binary.Write(buf, binary.BigEndian, uint32(len(value)))
	buf.WriteString(value)
}

func sqsStringMessageAttributes(raw any) map[string]sqsMessageAttribute {
	items, _ := raw.(map[string]any)
	attrs := map[string]sqsMessageAttribute{}
	for name, rawAttr := range items {
		attr, _ := rawAttr.(map[string]any)
		dataType := stringValue(attr["DataType"])
		if dataType == "" {
			continue
		}
		messageAttribute := sqsMessageAttribute{DataType: dataType, StringValue: stringValue(attr["StringValue"])}
		if strings.HasPrefix(dataType, "Number") {
			messageAttribute.StringValue = sqsNormalizeNumberAttribute(messageAttribute.StringValue)
		}
		if rawBinary := stringValue(attr["BinaryValue"]); rawBinary != "" {
			messageAttribute.BinaryValue, _ = base64.StdEncoding.DecodeString(rawBinary)
		}
		attrs[name] = messageAttribute
	}
	return attrs
}

func sqsNormalizeNumberAttribute(value string) string {
	sign := ""
	if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") {
		sign, value = value[:1], value[1:]
	}
	exponent := ""
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		exponent, value = value[index:], value[:index]
	}
	whole, fraction, hasFraction := strings.Cut(value, ".")
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	if hasFraction {
		fraction = strings.TrimRight(fraction, "0")
		if fraction != "" {
			return sign + whole + "." + fraction + exponent
		}
	}
	if whole == "0" {
		return "0"
	}
	return sign + whole + exponent
}

func sqsMessageAttributesValid(raw any) bool {
	items, _ := raw.(map[string]any)
	if len(items) > 10 {
		return false
	}
	for name, rawAttr := range items {
		if !sqsMessageAttributeNameValid(name) {
			return false
		}
		attr, _ := rawAttr.(map[string]any)
		dataType := stringValue(attr["DataType"])
		if !sqsMessageAttributeTypeValid(dataType) {
			return false
		}
		if sqsListValuePresent(attr["StringListValues"]) || sqsListValuePresent(attr["BinaryListValues"]) {
			return false
		}
		if strings.HasPrefix(dataType, "Binary") {
			if stringValue(attr["BinaryValue"]) == "" {
				return false
			}
			continue
		}
		if stringValue(attr["StringValue"]) == "" {
			return false
		}
		if strings.HasPrefix(dataType, "Number") && !sqsNumberAttributeValueValid(stringValue(attr["StringValue"])) {
			return false
		}
	}
	return true
}

func sqsMessageWithAttributesSizeValid(messageBody string, raw any) bool {
	return sqsMessageWithAttributesFitsLimit(messageBody, raw, 1048576)
}

func sqsMessageWithAttributesFitsLimit(messageBody string, raw any, limit int) bool {
	size, ok := sqsMessageWithAttributesBytes(messageBody, raw)
	return ok && size <= limit
}

func sqsMessageWithAttributesBytes(messageBody string, raw any) (int, bool) {
	size := len(messageBody)
	items, _ := raw.(map[string]any)
	for name, rawAttr := range items {
		attribute, _ := rawAttr.(map[string]any)
		dataType := stringValue(attribute["DataType"])
		size += len(name) + len(dataType)
		if strings.HasPrefix(dataType, "Binary") {
			binaryValue, err := base64.StdEncoding.DecodeString(stringValue(attribute["BinaryValue"]))
			if err != nil {
				return 0, false
			}
			size += len(binaryValue)
			continue
		}
		size += len(stringValue(attribute["StringValue"]))
	}
	return size, true
}

func sqsNumberAttributeValueValid(value string) bool {
	if !sqsNumberAttributePattern.MatchString(value) {
		return false
	}

	mantissa := value
	if exponentIndex := strings.IndexAny(mantissa, "eE"); exponentIndex >= 0 {
		mantissa = mantissa[:exponentIndex]
	}
	mantissa = strings.TrimPrefix(strings.TrimPrefix(mantissa, "+"), "-")
	whole, fraction, hasFraction := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole, "0")
	if digits == "" && hasFraction {
		digits = strings.TrimRight(strings.TrimLeft(fraction, "0"), "0")
	} else if hasFraction {
		digits += strings.TrimRight(fraction, "0")
	}
	if len(digits) > 38 {
		return false
	}

	number, ok := new(big.Rat).SetString(value)
	if !ok || number.Sign() == 0 {
		return ok
	}
	number.Abs(number)
	minimum, _ := new(big.Rat).SetString("1e-128")
	maximum, _ := new(big.Rat).SetString("1e126")
	return number.Cmp(minimum) >= 0 && number.Cmp(maximum) <= 0
}

func sqsListValuePresent(raw any) bool {
	values, _ := raw.([]any)
	return len(values) > 0
}

func sqsMessageAttributeNameValid(name string) bool {
	if name == "" || len(name) > 256 || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "aws.") || strings.HasPrefix(lower, "amazon.") {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func sqsMessageAttributeTypeValid(dataType string) bool {
	if utf8.RuneCountInString(dataType) > 256 {
		return false
	}
	for _, logicalType := range []string{"String", "Number", "Binary"} {
		if dataType == logicalType {
			return true
		}
		if label, ok := strings.CutPrefix(dataType, logicalType+"."); ok {
			return sqsMessageBodyValid(label)
		}
	}
	return false
}

func sqsMessageAttributes(attrs map[string]sqsMessageAttribute, rawRequested any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, requested := range sqsStringList(rawRequested) {
		for name, attr := range attrs {
			if requested == "All" || requested == ".*" || requested == name || (strings.HasSuffix(requested, ".*") && strings.HasPrefix(name, strings.TrimSuffix(requested, "*"))) {
				out[name] = map[string]any{"DataType": attr.DataType}
				if strings.HasPrefix(attr.DataType, "Binary") {
					out[name]["BinaryValue"] = attr.BinaryValue
				} else {
					out[name]["StringValue"] = attr.StringValue
				}
			}
		}
	}
	return out
}

func sqsMessageSystemAttributes(msg sqsMessage, requested ...any) map[string]string {
	attrs := map[string]string{}
	for _, raw := range requested {
		for _, name := range sqsStringList(raw) {
			if name == "All" || name == "MessageGroupId" {
				if msg.MessageGroupID != "" {
					attrs["MessageGroupId"] = msg.MessageGroupID
				}
			}
			if name == "All" || name == "MessageDeduplicationId" {
				if msg.MessageDeduplicationID != "" {
					attrs["MessageDeduplicationId"] = msg.MessageDeduplicationID
				}
			}
			if name == "All" || name == "SequenceNumber" {
				if msg.SequenceNumber != "" {
					attrs["SequenceNumber"] = msg.SequenceNumber
				}
			}
			if name == "All" || name == "ApproximateReceiveCount" {
				attrs["ApproximateReceiveCount"] = fmt.Sprintf("%d", msg.ReceiveCount)
			}
			if name == "All" || name == "SentTimestamp" {
				if !msg.CreatedAt.IsZero() {
					attrs["SentTimestamp"] = fmt.Sprintf("%d", msg.CreatedAt.UnixMilli())
				}
			}
			if name == "All" || name == "ApproximateFirstReceiveTimestamp" {
				if !msg.FirstReceivedAt.IsZero() {
					attrs["ApproximateFirstReceiveTimestamp"] = fmt.Sprintf("%d", msg.FirstReceivedAt.UnixMilli())
				}
			}
			if name == "All" || name == "SenderId" {
				attrs["SenderId"] = "homeport"
			}
			if name == "All" || name == "AWSTraceHeader" {
				if msg.AWSTraceHeader != "" {
					attrs["AWSTraceHeader"] = msg.AWSTraceHeader
				}
			}
		}
	}
	return attrs
}

func sqsAWSTraceHeader(raw any) string {
	attrs, _ := raw.(map[string]any)
	trace, _ := attrs["AWSTraceHeader"].(map[string]any)
	return stringValue(trace["StringValue"])
}

func sqsStringList(raw any) []string {
	values, _ := raw.([]any)
	out := []string{}
	for _, value := range values {
		if s := stringValue(value); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sqsBatchEntryIDsValid(entries []any) bool {
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		if !sqsBatchEntryIDValid(stringValue(entry["Id"])) {
			return false
		}
	}
	return true
}

func sqsBatchEntryIDValid(id string) bool {
	if id == "" || len(id) > 80 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func sqsBatchMessageBytes(entries []any) int {
	total := 0
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		size, ok := sqsMessageWithAttributesBytes(stringValue(entry["MessageBody"]), entry["MessageAttributes"])
		if !ok {
			return 1048577
		}
		total += size
	}
	return total
}

func sqsBatchEntryIDsDistinct(entries []any) bool {
	seen := map[string]struct{}{}
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		id := stringValue(entry["Id"])
		if _, ok := seen[id]; ok {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

var sqsKnownAttributeNames = map[string]struct{}{
	"All":                                   {},
	"Policy":                                {},
	"VisibilityTimeout":                     {},
	"MaximumMessageSize":                    {},
	"MessageRetentionPeriod":                {},
	"ApproximateNumberOfMessages":           {},
	"ApproximateNumberOfMessagesNotVisible": {},
	"CreatedTimestamp":                      {},
	"LastModifiedTimestamp":                 {},
	"QueueArn":                              {},
	"ApproximateNumberOfMessagesDelayed":    {},
	"DelaySeconds":                          {},
	"ReceiveMessageWaitTimeSeconds":         {},
	"RedrivePolicy":                         {},
	"FifoQueue":                             {},
	"ContentBasedDeduplication":             {},
	"KmsMasterKeyId":                        {},
	"KmsDataKeyReusePeriodSeconds":          {},
	"DeduplicationScope":                    {},
	"FifoThroughputLimit":                   {},
	"RedriveAllowPolicy":                    {},
	"SqsManagedSseEnabled":                  {},
}

func sqsIntAttrInRange(attrs map[string]string, name string, min, max int) bool {
	value, ok := attrs[name]
	if !ok {
		return true
	}
	parsed, err := strconv.Atoi(value)
	return err == nil && parsed >= min && parsed <= max
}

func sqsSelectedAttributes(attrs map[string]string, names any) (map[string]string, bool) {
	values, ok := names.([]any)
	if !ok || len(values) == 0 {
		return map[string]string{}, true
	}
	out := map[string]string{}
	for _, value := range values {
		name := stringValue(value)
		if name == "All" {
			mergeStringMap(out, attrs)
			return out, true
		}
		if attr, ok := attrs[name]; ok {
			out[name] = attr
			continue
		}
		return nil, false
	}
	return out, true
}

func sqsMessageBodyValid(body string) bool {
	if body == "" || len(body) > 1048576 {
		return false
	}
	for _, r := range body {
		if r == '\t' || r == '\n' || r == '\r' ||
			(r >= 0x20 && r <= 0xD7FF) ||
			(r >= 0xE000 && r <= 0xFFFD) ||
			(r >= 0x10000 && r <= 0x10FFFF) {
			continue
		}
		return false
	}
	return true
}

func sqsAttributesMatch(existing, requested map[string]string) bool {
	for key, value := range requested {
		if existing[key] != value {
			return false
		}
	}
	return true
}

func (a *SQSAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	context := map[string]string{
		"provider":     "aws",
		"service":      "sqs",
		"method":       r.Method,
		"request_id":   "homeport",
		"source_ip":    sourceIP(r),
		"current_time": time.Now().UTC().Format(time.RFC3339),
		"user_agent":   r.UserAgent(),
	}
	if value := r.Header.Get("X-Homeport-Credential-Age"); value != "" {
		context["credential_age"] = value
	}
	if value := r.Header.Get("X-Homeport-Credential-Expired"); value != "" {
		context["credential_expired"] = value
	}
	for key, value := range a.authzTags(body) {
		context["tag:"+key] = value
	}
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "sqs:" + action,
		Resource:            sqsARN(r, body),
		Context:             context,
		Claims:              awsClaims(r),
	}
	decision, err := a.authorizer.Authorize(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"__type": "InternalFailure", "message": err.Error()})
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"__type":  "AccessDenied",
			"message": decision.Reason,
		})
		return false
	}
	return true
}

func (a *SQSAdapter) authzTags(body map[string]any) map[string]string {
	tags := sqsTags(body)
	queueURL := stringValue(body["QueueUrl"])
	if queueURL == "" {
		return tags
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if q := a.queueByURL(queueURL); q != nil {
		mergeStringMap(tags, q.Tags)
	}
	return tags
}

func (a *SQSAdapter) queueByURL(queueURL string) *sqsQueue {
	return a.queues[queueURL]
}

func (a *SQSAdapter) requeueExpired(q *sqsQueue, now time.Time) {
	for receipt, msg := range q.Inflight {
		if msg.VisibleAt.After(now) {
			continue
		}
		delete(q.Inflight, receipt)
		if a.redrive(q, msg) {
			continue
		}
		msg.ReceiptHandle = ""
		q.Messages = append(q.Messages, msg)
	}
}

func (a *SQSAdapter) redrive(q *sqsQueue, msg sqsMessage) bool {
	var policy struct {
		DeadLetterTargetArn string `json:"deadLetterTargetArn"`
		MaxReceiveCount     string `json:"maxReceiveCount"`
	}
	if err := json.Unmarshal([]byte(q.Attributes["RedrivePolicy"]), &policy); err != nil {
		return false
	}
	maxReceive := secondsValue(policy.MaxReceiveCount, 0)
	if maxReceive == 0 || msg.ReceiveCount < maxReceive {
		return false
	}
	for _, candidate := range a.queues {
		if strings.HasSuffix(policy.DeadLetterTargetArn, ":"+candidate.Name) {
			candidate.Messages = append(candidate.Messages, msg)
			return true
		}
	}
	return false
}

func (q *sqsQueue) purgeExpired(now time.Time) {
	retention := secondsAttr(q.Attributes, "MessageRetentionPeriod", 345600)
	cutoff := now.Add(-time.Duration(retention) * time.Second)
	kept := q.Messages[:0]
	for _, msg := range q.Messages {
		if msg.CreatedAt.IsZero() || msg.CreatedAt.After(cutoff) {
			kept = append(kept, msg)
		}
	}
	q.Messages = kept
	for receipt, msg := range q.Inflight {
		if !msg.CreatedAt.IsZero() && msg.CreatedAt.Before(cutoff) {
			delete(q.Inflight, receipt)
		}
	}
}

func (q *sqsQueue) messageCount() int {
	return len(q.Messages) + len(q.Inflight)
}

func (q *sqsQueue) blockedMessageGroups() map[string]struct{} {
	groups := map[string]struct{}{}
	for _, msg := range q.Inflight {
		if msg.MessageGroupID != "" {
			groups[msg.MessageGroupID] = struct{}{}
		}
	}
	return groups
}

func (q *sqsQueue) duplicate(dedupID string, now time.Time) bool {
	if q.Dedup == nil {
		q.Dedup = map[string]time.Time{}
	}
	for id, expiresAt := range q.Dedup {
		if !expiresAt.After(now) {
			delete(q.Dedup, id)
		}
	}
	expiresAt, ok := q.Dedup[dedupID]
	return ok && expiresAt.After(now)
}

func secondsAttr(attrs map[string]string, name string, fallback int) int {
	return secondsValue(attrs[name], fallback)
}

func secondsValue(value any, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func queueURL(r *http.Request, name string) string {
	return "http://" + r.Host + "/123456789012/" + strings.TrimPrefix(url.PathEscape(name), "/")
}

func sqsIdempotencyKey(r *http.Request, action, queueURL string) string {
	key := r.Header.Get("X-Idempotency-Key")
	if key == "" {
		return ""
	}
	return action + ":" + queueURL + ":" + key
}

func sqsTags(body map[string]any) map[string]string {
	tags := mapValue(body["Tags"])
	mergeStringMap(tags, mapValue(body["tags"]))
	return tags
}

func sqsTagKeys(body map[string]any) []string {
	values, ok := body["TagKeys"].([]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(values))
	for _, value := range values {
		if key := stringValue(value); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func sqsARN(r *http.Request, body map[string]any) string {
	name := stringValue(body["QueueName"])
	if name == "" {
		name = strings.TrimPrefix(stringValue(body["QueueUrl"]), "http://"+r.Host+"/")
		if before, after, ok := strings.Cut(name, "/"); ok && before == "123456789012" {
			name = after
		}
	}
	if name == "" {
		name = "unknown"
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	return "arn:aws:sqs:us-east-1:homeport:" + name
}

func awsPrincipal(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if i := strings.Index(auth, "Credential="); i >= 0 {
		credential := auth[i+len("Credential="):]
		if j := strings.IndexAny(credential, "/, "); j >= 0 {
			credential = credential[:j]
		}
		if credential != "" {
			return credential
		}
	}
	return "anonymous"
}

func awsClaims(r *http.Request) map[string]string {
	claims := map[string]string{}
	const prefix = "x-homeport-claim-"
	for key, values := range r.Header {
		name, ok := strings.CutPrefix(strings.ToLower(key), prefix)
		if ok && len(values) > 0 && values[0] != "" {
			claims[name] = values[0]
		}
	}
	return claims
}

func awsPrincipalAttributes(r *http.Request) map[string]string {
	attributes := map[string]string{}
	const prefix = "x-homeport-principal-attribute-"
	for key, values := range r.Header {
		name, ok := strings.CutPrefix(strings.ToLower(key), prefix)
		if ok && len(values) > 0 && values[0] != "" {
			attributes[name] = values[0]
		}
	}
	return attributes
}

func sourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
