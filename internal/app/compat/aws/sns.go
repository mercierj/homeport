package aws

import (
	"bytes"
	"fmt"
	"html"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type SNSAdapter struct {
	mu         sync.Mutex
	topics     map[string]*snsTopic
	nextID     int
	topicQuota int
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type SNSOption func(*SNSAdapter)

type snsSubscription struct {
	ARN      string
	TopicARN string
	Protocol string
	Endpoint string
}

type snsTopic struct {
	ARN           string
	Attributes    map[string]string
	Tags          map[string]string
	Subscriptions []snsSubscription
	Dedup         map[string]string
}

func NewSNSAdapter(options ...SNSOption) *SNSAdapter {
	adapter := &SNSAdapter{
		topics:     make(map[string]*snsTopic),
		authorizer: authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithSNSAuthorizer(authorizer authz.Authorizer) SNSOption {
	return func(adapter *SNSAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithSNSAuditSink(sink func(authz.Decision)) SNSOption {
	return func(adapter *SNSAdapter) {
		adapter.auditSink = sink
	}
}

func WithSNSTopicQuota(maxTopics int) SNSOption {
	return func(adapter *SNSAdapter) {
		adapter.topicQuota = maxTopics
	}
}

func (SNSAdapter) Provider() string { return "aws" }
func (SNSAdapter) Service() string  { return "sns" }
func (SNSAdapter) Routes() []string { return []string{"POST /compat/aws/sns"} }
func (SNSAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_SNS":    "http://homeport:8080/api/v1/compat/aws/sns",
		"HOMEPORT_COMPAT_BACKEND": "nats",
	}
}
func (SNSAdapter) ConformanceChecks() []string {
	return []string{"create-topic", "set-topic-attributes", "get-topic-attributes", "list-topics", "delete-topic", "list-tags-for-resource", "tag-resource", "untag-resource", "subscribe", "list-subscriptions", "list-subscriptions-by-topic", "unsubscribe", "publish"}
}

func (a *SNSAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeQueryError(w, err.Error())
		return
	}
	if !a.authorized(w, r, action, body) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateTopic":
		name := stringValue(body["Name"])
		if !snsTopicNameValid(name) {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameter", "topic name is invalid")
			return
		}
		arn := "arn:aws:sns:us-east-1:000000000000:" + name
		if _, ok := a.topics[arn]; !ok {
			if a.topicQuota > 0 && len(a.topics) >= a.topicQuota {
				writeQueryErrorCode(w, http.StatusTooManyRequests, "Throttled", "topic quota exceeded")
				return
			}
			a.topics[arn] = &snsTopic{
				ARN: arn,
				Attributes: map[string]string{
					"TopicArn": arn,
					"Owner":    "000000000000",
					"Policy":   `{"Version":"2012-10-17","Statement":[]}`,
				},
				Tags:  snsTags(body),
				Dedup: map[string]string{},
			}
		}
		writeQueryResult(w, "CreateTopic", "<TopicArn>"+xmlEscape(arn)+"</TopicArn>")
	case "SetTopicAttributes":
		topic := a.topics[stringValue(body["TopicArn"])]
		if topic == nil {
			writeQueryErrorCode(w, http.StatusBadRequest, "NotFound", "topic not found")
			return
		}
		topic.Attributes[stringValue(body["AttributeName"])] = stringValue(body["AttributeValue"])
		writeQueryResult(w, "SetTopicAttributes", "")
	case "GetTopicAttributes":
		topic := a.topics[stringValue(body["TopicArn"])]
		if topic == nil {
			writeQueryErrorCode(w, http.StatusBadRequest, "NotFound", "topic not found")
			return
		}
		writeQueryResult(w, "GetTopicAttributes", snsAttributesXML(topic.Attributes))
	case "ListTopics":
		start, ok := snsPageStart(stringValue(body["NextToken"]))
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameter", "invalid next token")
			return
		}
		writeQueryResult(w, "ListTopics", snsTopicsXML(a.topics, start))
	case "DeleteTopic":
		topicARN := stringValue(body["TopicArn"])
		delete(a.topics, topicARN)
		writeQueryResult(w, "DeleteTopic", "")
	case "ListTagsForResource":
		topic := a.topics[stringValue(body["ResourceArn"])]
		if topic == nil {
			writeQueryErrorCode(w, http.StatusBadRequest, "NotFound", "topic not found")
			return
		}
		writeQueryResult(w, "ListTagsForResource", snsTagsXML(topic.Tags))
	case "TagResource":
		topic := a.topics[stringValue(body["ResourceArn"])]
		if topic == nil {
			writeQueryErrorCode(w, http.StatusBadRequest, "NotFound", "topic not found")
			return
		}
		mergeStringMap(topic.Tags, snsTags(body))
		writeQueryResult(w, "TagResource", "")
	case "UntagResource":
		topic := a.topics[stringValue(body["ResourceArn"])]
		if topic == nil {
			writeQueryErrorCode(w, http.StatusBadRequest, "NotFound", "topic not found")
			return
		}
		for _, key := range snsTagKeys(body) {
			delete(topic.Tags, key)
		}
		writeQueryResult(w, "UntagResource", "")
	case "Subscribe":
		topicARN := stringValue(body["TopicArn"])
		topic := a.topics[topicARN]
		if topic == nil {
			writeQueryErrorCode(w, http.StatusBadRequest, "NotFound", "topic not found")
			return
		}
		protocol := stringValue(body["Protocol"])
		endpoint := stringValue(body["Endpoint"])
		if protocol == "" || endpoint == "" || !snsProtocolValid(protocol) {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameter", "protocol and endpoint are required")
			return
		}
		for _, sub := range topic.Subscriptions {
			if sub.Protocol == protocol && sub.Endpoint == endpoint {
				writeQueryResult(w, "Subscribe", "<SubscriptionArn>"+xmlEscape(sub.ARN)+"</SubscriptionArn>")
				return
			}
		}
		a.nextID++
		sub := snsSubscription{
			ARN:      fmt.Sprintf("%s:%d", topicARN, a.nextID),
			TopicARN: topicARN,
			Protocol: protocol,
			Endpoint: endpoint,
		}
		topic.Subscriptions = append(topic.Subscriptions, sub)
		writeQueryResult(w, "Subscribe", "<SubscriptionArn>"+xmlEscape(sub.ARN)+"</SubscriptionArn>")
	case "ListSubscriptions":
		start, ok := snsPageStart(stringValue(body["NextToken"]))
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameter", "invalid next token")
			return
		}
		writeQueryResult(w, "ListSubscriptions", snsSubscriptionsXML("", snsAllSubscriptions(a.topics), start))
	case "ListSubscriptionsByTopic":
		topic := a.topics[stringValue(body["TopicArn"])]
		if topic == nil {
			writeQueryErrorCode(w, http.StatusBadRequest, "NotFound", "topic not found")
			return
		}
		start, ok := snsPageStart(stringValue(body["NextToken"]))
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameter", "invalid next token")
			return
		}
		writeQueryResult(w, "ListSubscriptionsByTopic", snsSubscriptionsXML(topic.ARN, topic.Subscriptions, start))
	case "Unsubscribe":
		subscriptionARN := stringValue(body["SubscriptionArn"])
		for _, topic := range a.topics {
			for i, sub := range topic.Subscriptions {
				if sub.ARN == subscriptionARN {
					topic.Subscriptions = append(topic.Subscriptions[:i], topic.Subscriptions[i+1:]...)
					writeQueryResult(w, "Unsubscribe", "")
					return
				}
			}
		}
		writeQueryErrorCode(w, http.StatusBadRequest, "NotFound", "subscription not found")
	case "Publish":
		topic := a.topics[stringValue(body["TopicArn"])]
		if topic == nil {
			writeQueryErrorCode(w, http.StatusBadRequest, "NotFound", "topic not found")
			return
		}
		dedupID := stringValue(body["MessageDeduplicationId"])
		if dedupID != "" {
			if messageID := topic.Dedup[dedupID]; messageID != "" {
				writeQueryResult(w, "Publish", "<MessageId>"+xmlEscape(messageID)+"</MessageId>")
				return
			}
		}
		a.deliverHTTP(topic, stringValue(body["Message"]))
		a.nextID++
		messageID := fmt.Sprintf("msg-%d", a.nextID)
		if dedupID != "" {
			topic.Dedup[dedupID] = messageID
		}
		writeQueryResult(w, "Publish", "<MessageId>"+xmlEscape(messageID)+"</MessageId>")
	default:
		writeQueryError(w, "unsupported SNS action: "+action)
	}
}

func snsProtocolValid(protocol string) bool {
	switch protocol {
	case "application", "email", "email-json", "firehose", "http", "https", "lambda", "sms", "sqs":
		return true
	}
	return false
}

func snsTopicNameValid(name string) bool {
	return len(name) >= 1 && len(name) <= 256 && sqsQueueNameCharsValid(strings.TrimSuffix(name, ".fifo"))
}

func (a *SNSAdapter) deliverHTTP(topic *snsTopic, message string) {
	for _, sub := range topic.Subscriptions {
		if sub.Protocol != "http" {
			continue
		}
		req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewBufferString(message))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "text/plain")
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}
}

func (a *SNSAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "sns:" + action,
		Resource:            snsARN(body),
		Context: map[string]string{
			"provider":     "aws",
			"service":      "sns",
			"method":       r.Method,
			"request_id":   "homeport",
			"source_ip":    snsSourceIP(r),
			"current_time": time.Now().UTC().Format(time.RFC3339),
			"user_agent":   r.UserAgent(),
		},
		Claims: awsClaims(r),
	}
	if value := r.Header.Get("X-Homeport-Credential-Age"); value != "" {
		req.Context["credential_age"] = value
	}
	if value := r.Header.Get("X-Homeport-Credential-Expired"); value != "" {
		req.Context["credential_expired"] = value
	}
	decision, err := a.authorizer.Authorize(r.Context(), req)
	if err != nil {
		writeQueryError(w, err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeQueryAccessDenied(w, decision.Reason)
		return false
	}
	return true
}

func writeQueryResult(w http.ResponseWriter, action, result string) {
	w.Header().Set("Content-Type", "text/xml")
	_, _ = fmt.Fprintf(w, `<%sResponse xmlns="https://sns.amazonaws.com/doc/2010-03-31/"><%sResult>%s</%sResult><ResponseMetadata><RequestId>homeport</RequestId></ResponseMetadata></%sResponse>`, action, action, result, action, action)
}

func writeQueryError(w http.ResponseWriter, message string) {
	writeQueryErrorCode(w, http.StatusBadRequest, "InvalidAction", message)
}

func writeQueryErrorCode(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<ErrorResponse><Error><Code>%s</Code><Message>%s</Message></Error><RequestId>homeport</RequestId></ErrorResponse>`, xmlEscape(code), xmlEscape(message))
}

func writeQueryAccessDenied(w http.ResponseWriter, message string) {
	writeQueryErrorCode(w, http.StatusForbidden, "AccessDenied", message)
}

func xmlEscape(value string) string {
	return html.EscapeString(value)
}

func snsAttributesXML(attributes map[string]string) string {
	out := "<Attributes>"
	for key, value := range attributes {
		out += "<entry><key>" + xmlEscape(key) + "</key><value>" + xmlEscape(value) + "</value></entry>"
	}
	return out + "</Attributes>"
}

func snsTagsXML(tags map[string]string) string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := "<Tags>"
	for _, key := range keys {
		out += "<member><Key>" + xmlEscape(key) + "</Key><Value>" + xmlEscape(tags[key]) + "</Value></member>"
	}
	return out + "</Tags>"
}

func snsTopicsXML(topics map[string]*snsTopic, start int) string {
	arns := make([]string, 0, len(topics))
	for arn := range topics {
		arns = append(arns, arn)
	}
	sort.Strings(arns)
	if start > len(arns) {
		start = len(arns)
	}
	end := start + 100
	if end > len(arns) {
		end = len(arns)
	}

	out := "<Topics>"
	for _, arn := range arns[start:end] {
		out += "<member><TopicArn>" + xmlEscape(arn) + "</TopicArn></member>"
	}
	out += "</Topics>"
	if end < len(arns) {
		out += fmt.Sprintf("<NextToken>%d</NextToken>", end)
	}
	return out
}

func snsSubscriptionsXML(topicARN string, subscriptions []snsSubscription, start int) string {
	if start > len(subscriptions) {
		start = len(subscriptions)
	}
	end := start + 100
	if end > len(subscriptions) {
		end = len(subscriptions)
	}

	out := "<Subscriptions>"
	for _, sub := range subscriptions[start:end] {
		subTopicARN := topicARN
		if subTopicARN == "" {
			subTopicARN = sub.TopicARN
		}
		out += "<member>"
		out += "<SubscriptionArn>" + xmlEscape(sub.ARN) + "</SubscriptionArn>"
		out += "<Owner>000000000000</Owner>"
		out += "<Protocol>" + xmlEscape(sub.Protocol) + "</Protocol>"
		out += "<Endpoint>" + xmlEscape(sub.Endpoint) + "</Endpoint>"
		out += "<TopicArn>" + xmlEscape(subTopicARN) + "</TopicArn>"
		out += "</member>"
	}
	out += "</Subscriptions>"
	if end < len(subscriptions) {
		out += fmt.Sprintf("<NextToken>%d</NextToken>", end)
	}
	return out
}

func snsAllSubscriptions(topics map[string]*snsTopic) []snsSubscription {
	subscriptions := []snsSubscription{}
	for _, topic := range topics {
		subscriptions = append(subscriptions, topic.Subscriptions...)
	}
	sort.Slice(subscriptions, func(i, j int) bool {
		return subscriptions[i].ARN < subscriptions[j].ARN
	})
	return subscriptions
}

func snsPageStart(token string) (int, bool) {
	if token == "" {
		return 0, true
	}
	start, err := strconv.Atoi(token)
	return start, err == nil && start >= 0
}

func snsARN(body map[string]any) string {
	if arn := stringValue(body["ResourceArn"]); arn != "" {
		return arn
	}
	if arn := stringValue(body["TopicArn"]); arn != "" {
		return arn
	}
	if arn := stringValue(body["SubscriptionArn"]); arn != "" {
		return arn
	}
	name := stringValue(body["Name"])
	if name == "" {
		name = "unknown"
	}
	return "arn:aws:sns:us-east-1:000000000000:" + name
}

func snsTags(body map[string]any) map[string]string {
	tags := map[string]string{}
	for i := 1; ; i++ {
		key := stringValue(body["Tags.member."+strconv.Itoa(i)+".Key"])
		if key == "" {
			break
		}
		tags[key] = stringValue(body["Tags.member."+strconv.Itoa(i)+".Value"])
	}
	return tags
}

func snsTagKeys(body map[string]any) []string {
	keys := []string{}
	for i := 1; ; i++ {
		key := stringValue(body["TagKeys.member."+strconv.Itoa(i)])
		if key == "" {
			break
		}
		keys = append(keys, key)
	}
	return keys
}

func snsSourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
