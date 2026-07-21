package aws

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type ACMAdapter struct {
	mu               sync.Mutex
	certificates     map[string]acmCertificate
	requestTokens    map[string]string
	nextID           int
	certificateQuota int
	authorizer       authz.Authorizer
	auditSink        func(authz.Decision)
}

type ACMOption func(*ACMAdapter)

type acmCertificate struct {
	Arn    string
	Domain string
	Tags   map[string]string
}

func NewACMAdapter(options ...ACMOption) *ACMAdapter {
	adapter := &ACMAdapter{
		certificates:  map[string]acmCertificate{},
		requestTokens: map[string]string{},
		authorizer:    authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithACMAuthorizer(authorizer authz.Authorizer) ACMOption {
	return func(adapter *ACMAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithACMAuditSink(sink func(authz.Decision)) ACMOption {
	return func(adapter *ACMAdapter) {
		adapter.auditSink = sink
	}
}

func WithACMCertificateQuota(maxCertificates int) ACMOption {
	return func(adapter *ACMAdapter) {
		adapter.certificateQuota = maxCertificates
	}
}

func (ACMAdapter) Provider() string { return "aws" }
func (ACMAdapter) Service() string  { return "acm" }
func (ACMAdapter) Routes() []string { return []string{"POST /compat/aws/acm"} }
func (ACMAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_ACM":    "http://homeport:8080/api/v1/compat/aws/acm",
		"HOMEPORT_COMPAT_BACKEND": "traefik-acme",
	}
}
func (ACMAdapter) ConformanceChecks() []string {
	return []string{"request-certificate", "describe-certificate", "list-certificates", "delete-certificate", "list-tags-for-certificate", "add-tags-to-certificate", "remove-tags-from-certificate"}
}

func (a *ACMAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeACMError(w, "ValidationException", err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "RequestCertificate":
		domain := stringValue(body["DomainName"])
		if !acmDomainNameValid(domain) {
			writeACMError(w, "ValidationException", "DomainName is invalid")
			return
		}
		if !a.authorized(w, r, "RequestCertificate", body) {
			return
		}
		if token := stringValue(body["IdempotencyToken"]); token != "" {
			if arn := a.requestTokens[token]; arn != "" {
				writeJSON(w, http.StatusOK, map[string]string{"CertificateArn": arn})
				return
			}
		}
		if a.certificateQuota > 0 && len(a.certificates) >= a.certificateQuota {
			writeACMError(w, "LimitExceededException", "certificate quota exceeded")
			return
		}
		a.nextID++
		cert := acmCertificate{
			Arn:    "arn:aws:acm:us-east-1:000000000000:certificate/homeport-" + strconv.Itoa(a.nextID),
			Domain: domain,
			Tags:   acmTags(body["Tags"]),
		}
		a.certificates[cert.Arn] = cert
		if token := stringValue(body["IdempotencyToken"]); token != "" {
			a.requestTokens[token] = cert.Arn
		}
		writeJSON(w, http.StatusOK, map[string]string{"CertificateArn": cert.Arn})
	case "DescribeCertificate":
		if !a.authorized(w, r, "DescribeCertificate", body) {
			return
		}
		cert, ok := a.certificates[stringValue(body["CertificateArn"])]
		if !ok {
			writeACMError(w, "ResourceNotFoundException", "certificate not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Certificate": acmCertificateShape(cert)})
	case "ListCertificates":
		if !a.authorized(w, r, "ListCertificates", body) {
			return
		}
		arns := make([]string, 0, len(a.certificates))
		for arn := range a.certificates {
			arns = append(arns, arn)
		}
		sort.Strings(arns)
		start, ok := acmPageStart(stringValue(body["NextToken"]), len(arns))
		if !ok {
			writeACMError(w, "ValidationException", "NextToken is invalid")
			return
		}
		limit, ok := cloudWatchLogsLimit(body, 1000, 1000, "MaxItems")
		if !ok {
			writeACMError(w, "ValidationException", "MaxItems must be between 1 and 1000")
			return
		}
		end := start + limit
		if end > len(arns) {
			end = len(arns)
		}
		summaries := make([]map[string]string, 0, end-start)
		for _, arn := range arns[start:end] {
			cert := a.certificates[arn]
			summaries = append(summaries, map[string]string{
				"CertificateArn": cert.Arn,
				"DomainName":     cert.Domain,
			})
		}
		response := map[string]any{"CertificateSummaryList": summaries}
		if end < len(arns) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "DeleteCertificate":
		arn := stringValue(body["CertificateArn"])
		if !a.authorized(w, r, "DeleteCertificate", body) {
			return
		}
		if _, ok := a.certificates[arn]; !ok {
			writeACMError(w, "ResourceNotFoundException", "certificate not found")
			return
		}
		delete(a.certificates, arn)
		for token, certificateARN := range a.requestTokens {
			if certificateARN == arn {
				delete(a.requestTokens, token)
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ListTagsForCertificate":
		if !a.authorized(w, r, "ListTagsForCertificate", body) {
			return
		}
		cert, ok := a.certificates[stringValue(body["CertificateArn"])]
		if !ok {
			writeACMError(w, "ResourceNotFoundException", "certificate not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Tags": acmTagsJSON(cert.Tags)})
	case "AddTagsToCertificate":
		if !a.authorized(w, r, "AddTagsToCertificate", body) {
			return
		}
		cert, ok := a.certificates[stringValue(body["CertificateArn"])]
		if !ok {
			writeACMError(w, "ResourceNotFoundException", "certificate not found")
			return
		}
		if cert.Tags == nil {
			cert.Tags = map[string]string{}
		}
		mergeStringMap(cert.Tags, acmTags(body["Tags"]))
		a.certificates[cert.Arn] = cert
		writeJSON(w, http.StatusOK, map[string]string{})
	case "RemoveTagsFromCertificate":
		if !a.authorized(w, r, "RemoveTagsFromCertificate", body) {
			return
		}
		cert, ok := a.certificates[stringValue(body["CertificateArn"])]
		if !ok {
			writeACMError(w, "ResourceNotFoundException", "certificate not found")
			return
		}
		for _, key := range acmTagKeys(body["Tags"]) {
			delete(cert.Tags, key)
		}
		a.certificates[cert.Arn] = cert
		writeJSON(w, http.StatusOK, map[string]string{})
	default:
		writeACMError(w, "UnsupportedOperation", "ACM action is not implemented")
	}
}

func acmDomainNameValid(domain string) bool {
	domain = strings.TrimPrefix(domain, "*.")
	if domain == "" || len(domain) > 253 || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if r != '-' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
				return false
			}
		}
	}
	return true
}

func acmPageStart(token string, count int) (int, bool) {
	if token == "" {
		return 0, true
	}
	start, err := strconv.Atoi(token)
	return start, err == nil && start >= 0 && start < count
}

func (a *ACMAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	resource := stringValue(body["CertificateArn"])
	if resource == "" {
		resource = "arn:aws:acm:us-east-1:000000000000:certificate/" + stringValue(body["DomainName"])
	}
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "acm:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "acm",
			"method":       r.Method,
			"request_id":   "homeport",
			"source_ip":    sourceIP(r),
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
		writeACMErrorStatus(w, http.StatusInternalServerError, "InternalFailure", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"__type": "AccessDenied", "message": decision.Reason})
		return false
	}
	return true
}

func acmCertificateShape(cert acmCertificate) map[string]string {
	return map[string]string{
		"CertificateArn": cert.Arn,
		"DomainName":     cert.Domain,
		"Status":         "ISSUED",
	}
}

func acmTags(raw any) map[string]string {
	values, _ := raw.([]any)
	tags := map[string]string{}
	for _, value := range values {
		tag, _ := value.(map[string]any)
		key := stringValue(tag["Key"])
		if key != "" {
			tags[key] = stringValue(tag["Value"])
		}
	}
	return tags
}

func acmTagsJSON(tags map[string]string) []map[string]string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, map[string]string{"Key": key, "Value": tags[key]})
	}
	return out
}

func acmTagKeys(raw any) []string {
	values, _ := raw.([]any)
	keys := make([]string, 0, len(values))
	for _, value := range values {
		if key := stringValue(value); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func writeACMError(w http.ResponseWriter, code, message string) {
	writeACMErrorStatus(w, http.StatusBadRequest, code, message)
}

func writeACMErrorStatus(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"__type": code, "message": message})
}
