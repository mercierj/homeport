package aws

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SQSAdapter struct {
	mu     sync.Mutex
	queues map[string]*sqsQueue
}

type sqsQueue struct {
	Name       string
	URL        string
	Messages   []sqsMessage
	Inflight   map[string]sqsMessage
	Attributes map[string]string
}

type sqsMessage struct {
	ID            string
	ReceiptHandle string
	Body          string
	VisibleAt     time.Time
	CreatedAt     time.Time
	ReceiveCount  int
}

func NewSQSAdapter() *SQSAdapter {
	return &SQSAdapter{queues: make(map[string]*sqsQueue)}
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

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateQueue":
		queueName := stringValue(body["QueueName"])
		if queueName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "QueueName is required"})
			return
		}
		queueURL := queueURL(r, queueName)
		q := a.queueByURL(queueURL)
		if q == nil {
			q = &sqsQueue{
				Name:     queueName,
				URL:      queueURL,
				Inflight: make(map[string]sqsMessage),
				Attributes: map[string]string{
					"VisibilityTimeout":      "30",
					"DelaySeconds":           "0",
					"MessageRetentionPeriod": "345600",
				},
			}
			a.queues[queueURL] = q
		}
		mergeStringMap(q.Attributes, mapValue(body["Attributes"]))
		writeJSON(w, http.StatusOK, map[string]string{"QueueUrl": queueURL})
	case "GetQueueUrl":
		queueName := stringValue(body["QueueName"])
		queueURL := queueURL(r, queueName)
		if a.queueByURL(queueURL) == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "queue not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"QueueUrl": queueURL})
	case "SetQueueAttributes":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "queue not found"})
			return
		}
		mergeStringMap(q.Attributes, mapValue(body["Attributes"]))
		writeJSON(w, http.StatusOK, map[string]string{})
	case "GetQueueAttributes":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "queue not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]map[string]string{"Attributes": q.Attributes})
	case "SendMessage":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "queue not found"})
			return
		}
		id := fmt.Sprintf("msg-%d", len(q.Messages)+len(q.Inflight)+1)
		now := time.Now()
		q.purgeExpired(now)
		delay := secondsValue(body["DelaySeconds"], secondsAttr(q.Attributes, "DelaySeconds", 0))
		msg := sqsMessage{
			ID:        id,
			Body:      stringValue(body["MessageBody"]),
			VisibleAt: now.Add(time.Duration(delay) * time.Second),
			CreatedAt: now,
		}
		q.Messages = append(q.Messages, msg)
		writeJSON(w, http.StatusOK, map[string]string{"MessageId": id})
	case "ReceiveMessage":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "queue not found"})
			return
		}
		now := time.Now()
		a.requeueExpired(q, now)
		q.purgeExpired(now)
		for i, msg := range q.Messages {
			if msg.VisibleAt.After(now) {
				continue
			}
			receipt := fmt.Sprintf("%s-rh-%d", msg.ID, now.UnixNano())
			msg.ReceiptHandle = receipt
			msg.ReceiveCount++
			visibility := secondsValue(body["VisibilityTimeout"], secondsAttr(q.Attributes, "VisibilityTimeout", 30))
			msg.VisibleAt = now.Add(time.Duration(visibility) * time.Second)
			q.Inflight[receipt] = msg
			q.Messages = append(q.Messages[:i], q.Messages[i+1:]...)
			writeJSON(w, http.StatusOK, map[string][]map[string]string{
				"Messages": {
					{
						"MessageId":     msg.ID,
						"ReceiptHandle": receipt,
						"Body":          msg.Body,
					},
				},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string][]map[string]string{"Messages": {}})
	case "DeleteMessage":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "queue not found"})
			return
		}
		delete(q.Inflight, stringValue(body["ReceiptHandle"]))
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ChangeMessageVisibility":
		q := a.queueByURL(stringValue(body["QueueUrl"]))
		if q == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "queue not found"})
			return
		}
		receipt := stringValue(body["ReceiptHandle"])
		msg, ok := q.Inflight[receipt]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "receipt not found"})
			return
		}
		visibility := secondsValue(body["VisibilityTimeout"], secondsAttr(q.Attributes, "VisibilityTimeout", 30))
		delete(q.Inflight, receipt)
		msg.VisibleAt = time.Now().Add(time.Duration(visibility) * time.Second)
		msg.ReceiptHandle = ""
		q.Messages = append(q.Messages, msg)
		writeJSON(w, http.StatusOK, map[string]string{})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported SQS action: " + action})
	}
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
	return "http://" + r.Host + "/" + strings.TrimPrefix(url.PathEscape(name), "/")
}
