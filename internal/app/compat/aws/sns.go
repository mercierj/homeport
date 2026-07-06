package aws

import (
	"fmt"
	"html"
	"net/http"
	"sync"
)

type SNSAdapter struct {
	mu     sync.Mutex
	topics map[string][]snsSubscription
	nextID int
}

type snsSubscription struct {
	ARN      string
	Protocol string
	Endpoint string
}

func NewSNSAdapter() *SNSAdapter {
	return &SNSAdapter{topics: make(map[string][]snsSubscription)}
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
	return []string{"create-topic", "subscribe", "publish"}
}

func (a *SNSAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeQueryError(w, err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateTopic":
		name := stringValue(body["Name"])
		arn := "arn:aws:sns:us-east-1:000000000000:" + name
		if _, ok := a.topics[arn]; !ok {
			a.topics[arn] = nil
		}
		writeQueryResult(w, "CreateTopic", "<TopicArn>"+xmlEscape(arn)+"</TopicArn>")
	case "Subscribe":
		topicARN := stringValue(body["TopicArn"])
		a.nextID++
		sub := snsSubscription{
			ARN:      fmt.Sprintf("%s:%d", topicARN, a.nextID),
			Protocol: stringValue(body["Protocol"]),
			Endpoint: stringValue(body["Endpoint"]),
		}
		a.topics[topicARN] = append(a.topics[topicARN], sub)
		writeQueryResult(w, "Subscribe", "<SubscriptionArn>"+xmlEscape(sub.ARN)+"</SubscriptionArn>")
	case "Publish":
		a.nextID++
		writeQueryResult(w, "Publish", fmt.Sprintf("<MessageId>msg-%d</MessageId>", a.nextID))
	default:
		writeQueryError(w, "unsupported SNS action: "+action)
	}
}

func writeQueryResult(w http.ResponseWriter, action, result string) {
	w.Header().Set("Content-Type", "text/xml")
	_, _ = fmt.Fprintf(w, `<%sResponse xmlns="https://sns.amazonaws.com/doc/2010-03-31/"><%sResult>%s</%sResult><ResponseMetadata><RequestId>homeport</RequestId></ResponseMetadata></%sResponse>`, action, action, result, action, action)
}

func writeQueryError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = fmt.Fprintf(w, `<ErrorResponse><Error><Code>InvalidAction</Code><Message>%s</Message></Error><RequestId>homeport</RequestId></ErrorResponse>`, xmlEscape(message))
}

func xmlEscape(value string) string {
	return html.EscapeString(value)
}
