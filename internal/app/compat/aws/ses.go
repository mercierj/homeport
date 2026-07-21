package aws

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type SESAdapter struct {
	mu         sync.Mutex
	identities map[string]sesIdentity
	templates  map[string]sesTemplate
	nextID     int
	nextMsgID  int
	quota      int
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type SESOption func(*SESAdapter)

type sesIdentity struct {
	Name     string
	Token    string
	Policies map[string]string
	DKIM     []string
	Tags     map[string]string
}

type sesTemplate struct {
	Name    string
	HTML    string
	Subject string
	Text    string
}

func NewSESAdapter(options ...SESOption) *SESAdapter {
	adapter := &SESAdapter{
		identities: map[string]sesIdentity{},
		templates:  map[string]sesTemplate{},
		authorizer: authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithSESAuthorizer(authorizer authz.Authorizer) SESOption {
	return func(adapter *SESAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithSESAuditSink(sink func(authz.Decision)) SESOption {
	return func(adapter *SESAdapter) {
		adapter.auditSink = sink
	}
}

func WithSESIdentityQuota(maxIdentities int) SESOption {
	return func(adapter *SESAdapter) {
		adapter.quota = maxIdentities
	}
}

func (SESAdapter) Provider() string { return "aws" }
func (SESAdapter) Service() string  { return "ses" }
func (SESAdapter) Routes() []string { return []string{"POST /compat/aws/ses"} }
func (SESAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_SES":    "http://homeport:8080/api/v1/compat/aws/ses",
		"AWS_ENDPOINT_URL_SESV2":  "http://homeport:8080/api/v1/compat/aws/ses",
		"HOMEPORT_COMPAT_BACKEND": "postal",
	}
}
func (SESAdapter) ConformanceChecks() []string {
	return []string{"verify-domain-identity", "get-identity-verification-attributes", "list-identities", "delete-identity", "put-identity-policy", "list-identity-policies", "get-identity-policies", "delete-identity-policy", "verify-domain-dkim", "get-identity-dkim-attributes", "send-email", "send-raw-email", "create-template", "get-template", "list-templates", "update-template", "delete-template", "test-render-template", "send-templated-email", "send-bulk-templated-email", "create-email-identity", "get-email-identity", "list-email-identities", "delete-email-identity", "tag-resource", "list-tags-for-resource", "untag-resource"}
}

func (a *SESAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/v2/email/identities") || r.URL.Path == "/v2/email/tags" {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.serveSESV2(w, r)
		return
	}

	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "VerifyDomainIdentity":
		domain := stringValue(body["Domain"])
		if domain == "" {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "Domain is required")
			return
		}
		if !a.authorized(w, r, "VerifyDomainIdentity", body) {
			return
		}
		identity := a.identities[domain]
		if identity.Token == "" {
			if a.quota > 0 && len(a.identities) >= a.quota {
				writeQueryErrorCode(w, http.StatusBadRequest, "LimitExceeded", "identity quota exceeded")
				return
			}
			a.nextID++
			identity.Name = domain
			identity.Token = "homeport-ses-token-" + strconv.Itoa(a.nextID)
			if identity.Policies == nil {
				identity.Policies = map[string]string{}
			}
			a.identities[domain] = identity
		}
		writeSESResult(w, "VerifyDomainIdentity", "<VerificationToken>"+xmlEscape(identity.Token)+"</VerificationToken>")
	case "GetIdentityVerificationAttributes":
		if !a.authorizedIdentities(w, r, "GetIdentityVerificationAttributes", sesMembers(body, "Identities.member."), body) {
			return
		}
		writeSESResult(w, "GetIdentityVerificationAttributes", "<VerificationAttributes>"+a.verificationAttributesXML(sesMembers(body, "Identities.member."))+"</VerificationAttributes>")
	case "ListIdentities":
		if !a.authorized(w, r, "ListIdentities", body) {
			return
		}
		start, ok := sesPageStart(stringValue(body["NextToken"]))
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "invalid NextToken")
			return
		}
		maxItems, ok := sesMaxItems(body)
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "invalid MaxItems")
			return
		}
		writeSESResult(w, "ListIdentities", a.identitiesXML(stringValue(body["IdentityType"]), start, maxItems))
	case "DeleteIdentity":
		if !a.authorized(w, r, "DeleteIdentity", body) {
			return
		}
		delete(a.identities, sesIdentityName(stringValue(body["Identity"])))
		writeSESResult(w, "DeleteIdentity", "")
	case "VerifyDomainDkim":
		domain := stringValue(body["Domain"])
		if domain == "" {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "Domain is required")
			return
		}
		if !a.authorized(w, r, "VerifyDomainDkim", body) {
			return
		}
		identity := a.ensureIdentity(domain)
		writeSESResult(w, "VerifyDomainDkim", "<DkimTokens>"+sesStringListXML(identity.DKIM)+"</DkimTokens>")
	case "GetIdentityDkimAttributes":
		if !a.authorizedIdentities(w, r, "GetIdentityDkimAttributes", sesMembers(body, "Identities.member."), body) {
			return
		}
		writeSESResult(w, "GetIdentityDkimAttributes", "<DkimAttributes>"+a.dkimAttributesXML(sesMembers(body, "Identities.member."))+"</DkimAttributes>")
	case "SendEmail":
		if !a.authorized(w, r, "SendEmail", body) {
			return
		}
		if !a.sourceVerified(stringValue(body["Source"])) {
			writeQueryErrorCode(w, http.StatusBadRequest, "MessageRejected", "Email address is not verified")
			return
		}
		if !sesHasRecipient(body, "Destination.") {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "Destination must include at least one recipient")
			return
		}
		a.nextMsgID++
		writeSESResult(w, "SendEmail", "<MessageId>homeport-ses-message-"+strconv.Itoa(a.nextMsgID)+"</MessageId>")
	case "SendRawEmail":
		if !a.authorized(w, r, "SendRawEmail", body) {
			return
		}
		if !a.sourceVerified(stringValue(body["Source"])) {
			writeQueryErrorCode(w, http.StatusBadRequest, "MessageRejected", "Email address is not verified")
			return
		}
		a.nextMsgID++
		writeSESResult(w, "SendRawEmail", "<MessageId>homeport-ses-message-"+strconv.Itoa(a.nextMsgID)+"</MessageId>")
	case "CreateTemplate":
		if !a.authorized(w, r, "CreateTemplate", body) {
			return
		}
		name := sesTemplateName(body)
		if name == "" {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "TemplateName is required")
			return
		}
		if _, ok := a.templates[name]; ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "AlreadyExists", "template already exists")
			return
		}
		a.templates[name] = sesTemplateFromBody(name, body)
		writeSESResult(w, "CreateTemplate", "")
	case "GetTemplate":
		if !a.authorized(w, r, "GetTemplate", body) {
			return
		}
		template, ok := a.templates[stringValue(body["TemplateName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "TemplateDoesNotExist", "template does not exist")
			return
		}
		writeSESResult(w, "GetTemplate", "<Template>"+template.xml()+"</Template>")
	case "ListTemplates":
		if !a.authorized(w, r, "ListTemplates", body) {
			return
		}
		start, ok := sesPageStart(stringValue(body["NextToken"]))
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "invalid NextToken")
			return
		}
		maxItems, ok := sesTemplateMaxItems(body)
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "invalid MaxItems")
			return
		}
		writeSESResult(w, "ListTemplates", a.templatesXML(start, maxItems))
	case "UpdateTemplate":
		if !a.authorized(w, r, "UpdateTemplate", body) {
			return
		}
		name := sesTemplateName(body)
		if _, ok := a.templates[name]; !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "TemplateDoesNotExist", "template does not exist")
			return
		}
		a.templates[name] = sesTemplateFromBody(name, body)
		writeSESResult(w, "UpdateTemplate", "")
	case "DeleteTemplate":
		if !a.authorized(w, r, "DeleteTemplate", body) {
			return
		}
		delete(a.templates, stringValue(body["TemplateName"]))
		writeSESResult(w, "DeleteTemplate", "")
	case "TestRenderTemplate":
		if !a.authorized(w, r, "TestRenderTemplate", body) {
			return
		}
		template, ok := a.templates[stringValue(body["TemplateName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "TemplateDoesNotExist", "template does not exist")
			return
		}
		rendered, errCode := template.render(stringValue(body["TemplateData"]))
		if errCode != "" {
			writeQueryErrorCode(w, http.StatusBadRequest, errCode, "invalid template data")
			return
		}
		writeSESResult(w, "TestRenderTemplate", "<RenderedTemplate>"+xmlEscape(rendered)+"</RenderedTemplate>")
	case "SendTemplatedEmail":
		if !a.authorized(w, r, "SendTemplatedEmail", body) {
			return
		}
		if !a.sourceVerified(stringValue(body["Source"])) {
			writeQueryErrorCode(w, http.StatusBadRequest, "MessageRejected", "Email address is not verified")
			return
		}
		if !sesHasRecipient(body, "Destination.") {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "Destination must include at least one recipient")
			return
		}
		if _, ok := a.templates[stringValue(body["Template"])]; !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "TemplateDoesNotExist", "template does not exist")
			return
		}
		a.nextMsgID++
		writeSESResult(w, "SendTemplatedEmail", "<MessageId>homeport-ses-message-"+strconv.Itoa(a.nextMsgID)+"</MessageId>")
	case "SendBulkTemplatedEmail":
		if !a.authorized(w, r, "SendBulkTemplatedEmail", body) {
			return
		}
		if !a.sourceVerified(stringValue(body["Source"])) {
			writeQueryErrorCode(w, http.StatusBadRequest, "MessageRejected", "Email address is not verified")
			return
		}
		if _, ok := a.templates[stringValue(body["Template"])]; !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "TemplateDoesNotExist", "template does not exist")
			return
		}
		count := sesMemberCount(body, "Destinations.member.")
		if count == 0 {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "Destinations must include at least one recipient")
			return
		}
		for i := 1; i <= count; i++ {
			if !sesHasRecipient(body, fmt.Sprintf("Destinations.member.%d.Destination.", i)) {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "each destination must include at least one recipient")
				return
			}
		}
		a.nextMsgID += count
		writeSESResult(w, "SendBulkTemplatedEmail", sesBulkStatusesXML(a.nextMsgID-count+1, count))
	case "PutIdentityPolicy":
		if !a.authorized(w, r, "PutIdentityPolicy", body) {
			return
		}
		identityName := sesIdentityName(stringValue(body["Identity"]))
		policyName := stringValue(body["PolicyName"])
		policy := stringValue(body["Policy"])
		identity, ok := a.identities[identityName]
		if !ok {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "identity does not exist")
			return
		}
		if policyName == "" || policy == "" {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidParameterValue", "PolicyName and Policy are required")
			return
		}
		if identity.Policies == nil {
			identity.Policies = map[string]string{}
		}
		identity.Policies[policyName] = policy
		a.identities[identityName] = identity
		writeSESResult(w, "PutIdentityPolicy", "")
	case "ListIdentityPolicies":
		if !a.authorized(w, r, "ListIdentityPolicies", body) {
			return
		}
		identity := a.identities[sesIdentityName(stringValue(body["Identity"]))]
		writeSESResult(w, "ListIdentityPolicies", "<PolicyNames>"+sesPolicyNamesXML(identity.Policies)+"</PolicyNames>")
	case "GetIdentityPolicies":
		if !a.authorized(w, r, "GetIdentityPolicies", body) {
			return
		}
		identity := a.identities[sesIdentityName(stringValue(body["Identity"]))]
		writeSESResult(w, "GetIdentityPolicies", "<Policies>"+sesPoliciesXML(identity.Policies, sesMembers(body, "PolicyNames.member."))+"</Policies>")
	case "DeleteIdentityPolicy":
		if !a.authorized(w, r, "DeleteIdentityPolicy", body) {
			return
		}
		identityName := sesIdentityName(stringValue(body["Identity"]))
		if identity, ok := a.identities[identityName]; ok {
			delete(identity.Policies, stringValue(body["PolicyName"]))
			a.identities[identityName] = identity
		}
		writeSESResult(w, "DeleteIdentityPolicy", "")
	default:
		writeQueryErrorCode(w, http.StatusBadRequest, "InvalidAction", "unsupported SES action")
	}
}

func (a *SESAdapter) serveSESV2(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v2/email/tags":
		a.serveSESV2Tags(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v2/email/identities":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeSESV2Error(w, http.StatusBadRequest, "BadRequestException", "invalid JSON request body")
			return
		}
		name := stringValue(body["EmailIdentity"])
		if name == "" {
			writeSESV2Error(w, http.StatusBadRequest, "BadRequestException", "EmailIdentity is required")
			return
		}
		if !a.authorizedResourceSESV2(w, r, "CreateEmailIdentity", sesIdentityARN(name)) {
			return
		}
		if _, ok := a.identities[name]; ok {
			writeSESV2Error(w, http.StatusBadRequest, "AlreadyExistsException", "identity already exists")
			return
		}
		identity := a.ensureIdentity(name)
		identity.Tags = sesV2Tags(body["Tags"])
		a.identities[name] = identity
		writeSESV2JSON(w, http.StatusOK, map[string]any{
			"IdentityType":             sesV2IdentityType(name),
			"VerifiedForSendingStatus": false,
			"DkimAttributes":           map[string]any{},
		})
	case r.Method == http.MethodGet && r.URL.Path == "/v2/email/identities":
		if !a.authorizedResourceSESV2(w, r, "ListEmailIdentities", "*") {
			return
		}
		identities := a.sesV2IdentityList()
		start, ok := sesPageStart(r.URL.Query().Get("NextToken"))
		if !ok || start > len(identities) {
			writeSESV2Error(w, http.StatusBadRequest, "BadRequestException", "invalid NextToken")
			return
		}
		pageSize := 1000
		if value := r.URL.Query().Get("PageSize"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed < 1 || parsed > 1000 {
				writeSESV2Error(w, http.StatusBadRequest, "BadRequestException", "invalid PageSize")
				return
			}
			pageSize = parsed
		}
		end := start + pageSize
		if end > len(identities) {
			end = len(identities)
		}
		response := map[string]any{"EmailIdentities": identities[start:end]}
		if end < len(identities) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeSESV2JSON(w, http.StatusOK, response)
	case strings.HasPrefix(r.URL.Path, "/v2/email/identities/"):
		name, err := url.PathUnescape(strings.TrimPrefix(r.URL.Path, "/v2/email/identities/"))
		if err != nil || name == "" {
			writeSESV2Error(w, http.StatusBadRequest, "BadRequestException", "invalid identity")
			return
		}
		if r.Method == http.MethodGet {
			if !a.authorizedResourceSESV2(w, r, "GetEmailIdentity", sesIdentityARN(name)) {
				return
			}
			identity, ok := a.identities[name]
			if !ok {
				writeSESV2Error(w, http.StatusNotFound, "NotFoundException", "identity not found")
				return
			}
			writeSESV2JSON(w, http.StatusOK, sesV2IdentityJSON(identity))
			return
		}
		if r.Method == http.MethodDelete {
			if !a.authorizedResourceSESV2(w, r, "DeleteEmailIdentity", sesIdentityARN(name)) {
				return
			}
			if _, ok := a.identities[name]; !ok {
				writeSESV2Error(w, http.StatusNotFound, "NotFoundException", "identity not found")
				return
			}
			delete(a.identities, name)
			writeSESV2JSON(w, http.StatusOK, map[string]any{})
			return
		}
		fallthrough
	default:
		writeSESV2Error(w, http.StatusBadRequest, "BadRequestException", "unsupported SESv2 action")
	}
}

func (a *SESAdapter) serveSESV2Tags(w http.ResponseWriter, r *http.Request) {
	resourceARN := r.URL.Query().Get("ResourceArn")
	var body map[string]any
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&body)
		resourceARN = stringValue(body["ResourceArn"])
	}
	identityName := sesIdentityName(resourceARN)
	if identityName == "" {
		writeSESV2Error(w, http.StatusBadRequest, "BadRequestException", "ResourceArn is required")
		return
	}
	var action string
	switch r.Method {
	case http.MethodPost:
		action = "TagResource"
	case http.MethodGet:
		action = "ListTagsForResource"
	case http.MethodDelete:
		action = "UntagResource"
	default:
		writeSESV2Error(w, http.StatusBadRequest, "BadRequestException", "unsupported SESv2 tag action")
		return
	}
	if !a.authorizedResourceSESV2(w, r, action, resourceARN) {
		return
	}
	identity, ok := a.identities[identityName]
	if !ok {
		writeSESV2Error(w, http.StatusNotFound, "NotFoundException", "identity not found")
		return
	}

	switch r.Method {
	case http.MethodPost:
		if identity.Tags == nil {
			identity.Tags = map[string]string{}
		}
		for key, value := range sesV2Tags(body["Tags"]) {
			identity.Tags[key] = value
		}
		a.identities[identityName] = identity
		writeSESV2JSON(w, http.StatusOK, map[string]any{})
	case http.MethodGet:
		writeSESV2JSON(w, http.StatusOK, map[string]any{"Tags": sesV2TagsJSON(identity.Tags)})
	case http.MethodDelete:
		for _, key := range r.URL.Query()["TagKeys"] {
			delete(identity.Tags, key)
		}
		a.identities[identityName] = identity
		writeSESV2JSON(w, http.StatusOK, map[string]any{})
	default:
		writeSESV2Error(w, http.StatusBadRequest, "BadRequestException", "unsupported SESv2 tag action")
	}
}

func (a *SESAdapter) ensureIdentity(name string) sesIdentity {
	identity := a.identities[name]
	if identity.Name == "" {
		identity.Name = name
	}
	if identity.Policies == nil {
		identity.Policies = map[string]string{}
	}
	if identity.Tags == nil {
		identity.Tags = map[string]string{}
	}
	if len(identity.DKIM) == 0 {
		identity.DKIM = []string{"homeport-dkim-1-" + name, "homeport-dkim-2-" + name, "homeport-dkim-3-" + name}
	}
	a.identities[name] = identity
	return identity
}

func (a *SESAdapter) sourceVerified(source string) bool {
	if identity, ok := a.identities[source]; ok && identity.Token != "" {
		return true
	}
	if _, domain, ok := strings.Cut(source, "@"); ok {
		identity, ok := a.identities[domain]
		return ok && identity.Token != ""
	}
	return false
}

func (a *SESAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	return a.authorizedResource(w, r, action, body, sesResource(body))
}

func (a *SESAdapter) authorizedIdentities(w http.ResponseWriter, r *http.Request, action string, identities []string, body map[string]any) bool {
	if len(identities) == 0 {
		return a.authorized(w, r, action, body)
	}
	for _, identity := range identities {
		if !a.authorizedResource(w, r, action, body, sesIdentityARN(identity)) {
			return false
		}
	}
	return true
}

func (a *SESAdapter) authorizedResource(w http.ResponseWriter, r *http.Request, action string, body map[string]any, resource string) bool {
	decision, err := a.authorizeSES(r, action, resource)
	if err != nil {
		writeQueryError(w, err.Error())
		return false
	}
	if !decision.Allowed {
		writeQueryAccessDenied(w, decision.Reason)
		return false
	}
	return true
}

func (a *SESAdapter) authorizedResourceSESV2(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	decision, err := a.authorizeSES(r, action, resource)
	if err != nil {
		writeSESV2Error(w, http.StatusInternalServerError, "InternalFailure", err.Error())
		return false
	}
	if !decision.Allowed {
		writeSESV2Error(w, http.StatusForbidden, "AccessDenied", decision.Reason)
		return false
	}
	return true
}

func (a *SESAdapter) authorizeSES(r *http.Request, action, resource string) (authz.Decision, error) {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "ses:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "ses",
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
		return decision, err
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	return decision, nil
}

func sesResource(body map[string]any) string {
	identity := stringValue(body["Identity"])
	if identity == "" {
		identity = stringValue(body["Domain"])
	}
	if identity == "" {
		identity = stringValue(body["SourceArn"])
	}
	if identity == "" {
		identity = stringValue(body["Source"])
	}
	if identity == "" {
		identity = stringValue(body["Identities.member.1"])
	}
	if identity == "" {
		return "*"
	}
	return sesIdentityARN(identity)
}

func sesIdentityARN(identity string) string {
	if strings.HasPrefix(identity, "arn:aws:ses:") {
		return identity
	}
	return "arn:aws:ses:us-east-1:000000000000:identity/" + identity
}

func sesIdentityName(identity string) string {
	const marker = ":identity/"
	if before, after, ok := strings.Cut(identity, marker); ok && strings.HasPrefix(before, "arn:aws:ses:") {
		return after
	}
	return identity
}

func sesTemplateName(body map[string]any) string {
	if name := stringValue(body["Template.TemplateName"]); name != "" {
		return name
	}
	return stringValue(body["TemplateName"])
}

func sesTemplateFromBody(name string, body map[string]any) sesTemplate {
	return sesTemplate{
		Name:    name,
		HTML:    stringValue(body["Template.HtmlPart"]),
		Subject: stringValue(body["Template.SubjectPart"]),
		Text:    stringValue(body["Template.TextPart"]),
	}
}

func (t sesTemplate) xml() string {
	return "<TemplateName>" + xmlEscape(t.Name) + "</TemplateName><HtmlPart>" + xmlEscape(t.HTML) + "</HtmlPart><SubjectPart>" + xmlEscape(t.Subject) + "</SubjectPart><TextPart>" + xmlEscape(t.Text) + "</TextPart>"
}

func (t sesTemplate) render(data string) (string, string) {
	values := map[string]any{}
	if err := json.Unmarshal([]byte(data), &values); err != nil {
		return "", "InvalidRenderingParameter"
	}
	rendered := []string{
		sesRenderPart(t.Subject, values),
		sesRenderPart(t.HTML, values),
		sesRenderPart(t.Text, values),
	}
	for _, part := range rendered {
		if strings.Contains(part, "{{") && strings.Contains(part, "}}") {
			return "", "MissingRenderingAttribute"
		}
	}
	return strings.Join(rendered, "\n"), ""
}

func sesRenderPart(part string, values map[string]any) string {
	for key, value := range values {
		part = strings.ReplaceAll(part, "{{"+key+"}}", fmt.Sprint(value))
	}
	return part
}

func (a *SESAdapter) verificationAttributesXML(names []string) string {
	var out strings.Builder
	for _, name := range names {
		identity, ok := a.identities[name]
		if !ok {
			continue
		}
		out.WriteString("<entry><key>" + xmlEscape(name) + "</key><value><VerificationStatus>Pending</VerificationStatus><VerificationToken>" + xmlEscape(identity.Token) + "</VerificationToken></value></entry>")
	}
	return out.String()
}

func (a *SESAdapter) dkimAttributesXML(names []string) string {
	var out strings.Builder
	for _, name := range names {
		identity, ok := a.identities[name]
		if !ok || len(identity.DKIM) == 0 {
			continue
		}
		out.WriteString("<entry><key>" + xmlEscape(name) + "</key><value><DkimEnabled>true</DkimEnabled><DkimVerificationStatus>Pending</DkimVerificationStatus><DkimTokens>" + sesStringListXML(identity.DKIM) + "</DkimTokens></value></entry>")
	}
	return out.String()
}

func (a *SESAdapter) identitiesXML(identityType string, start, maxItems int) string {
	if identityType == "EmailAddress" {
		return "<Identities/>"
	}
	names := make([]string, 0, len(a.identities))
	for name := range a.identities {
		names = append(names, name)
	}
	sort.Strings(names)
	if start > len(names) {
		start = len(names)
	}
	end := start + maxItems
	if end > len(names) {
		end = len(names)
	}

	var out strings.Builder
	out.WriteString("<Identities>")
	for _, name := range names[start:end] {
		out.WriteString("<member>" + xmlEscape(name) + "</member>")
	}
	out.WriteString("</Identities>")
	if end < len(names) {
		out.WriteString("<NextToken>" + strconv.Itoa(end) + "</NextToken>")
	}
	return out.String()
}

func (a *SESAdapter) templatesXML(start, maxItems int) string {
	names := make([]string, 0, len(a.templates))
	for name := range a.templates {
		names = append(names, name)
	}
	sort.Strings(names)
	if start > len(names) {
		start = len(names)
	}
	end := start + maxItems
	if end > len(names) {
		end = len(names)
	}

	var out strings.Builder
	out.WriteString("<TemplatesMetadata>")
	for _, name := range names[start:end] {
		out.WriteString("<member><Name>" + xmlEscape(name) + "</Name></member>")
	}
	out.WriteString("</TemplatesMetadata>")
	if end < len(names) {
		out.WriteString("<NextToken>" + strconv.Itoa(end) + "</NextToken>")
	}
	return out.String()
}

func (a *SESAdapter) sesV2IdentityList() []map[string]any {
	names := make([]string, 0, len(a.identities))
	for name := range a.identities {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, map[string]any{
			"IdentityName":       name,
			"IdentityType":       sesV2IdentityType(name),
			"SendingEnabled":     false,
			"VerificationStatus": "PENDING",
		})
	}
	return out
}

func sesPolicyNamesXML(policies map[string]string) string {
	names := make([]string, 0, len(policies))
	for name := range policies {
		names = append(names, name)
	}
	sort.Strings(names)

	var out strings.Builder
	for _, name := range names {
		out.WriteString("<member>" + xmlEscape(name) + "</member>")
	}
	return out.String()
}

func sesV2IdentityJSON(identity sesIdentity) map[string]any {
	return map[string]any{
		"IdentityType":             sesV2IdentityType(identity.Name),
		"VerificationStatus":       "PENDING",
		"VerifiedForSendingStatus": false,
		"FeedbackForwardingStatus": true,
		"DkimAttributes":           map[string]any{},
		"Tags":                     sesV2TagsJSON(identity.Tags),
	}
}

func sesV2IdentityType(identity string) string {
	if strings.Contains(identity, "@") {
		return "EMAIL_ADDRESS"
	}
	return "DOMAIN"
}

func sesV2Tags(value any) map[string]string {
	out := map[string]string{}
	for _, item := range anySlice(value) {
		tag, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := stringValue(tag["Key"])
		if key != "" {
			out[key] = stringValue(tag["Value"])
		}
	}
	return out
}

func sesV2TagsJSON(tags map[string]string) []map[string]string {
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

func anySlice(value any) []any {
	switch items := value.(type) {
	case []any:
		return items
	default:
		return nil
	}
}

func sesStringListXML(values []string) string {
	var out strings.Builder
	for _, value := range values {
		out.WriteString("<member>" + xmlEscape(value) + "</member>")
	}
	return out.String()
}

func sesPoliciesXML(policies map[string]string, names []string) string {
	var out strings.Builder
	for _, name := range names {
		policy, ok := policies[name]
		if !ok {
			continue
		}
		out.WriteString("<entry><key>" + xmlEscape(name) + "</key><value>" + xmlEscape(policy) + "</value></entry>")
	}
	return out.String()
}

func sesBulkStatusesXML(firstID, count int) string {
	var out strings.Builder
	out.WriteString("<Status>")
	for i := 0; i < count; i++ {
		out.WriteString("<member><Status>Success</Status><MessageId>homeport-ses-message-" + strconv.Itoa(firstID+i) + "</MessageId></member>")
	}
	out.WriteString("</Status>")
	return out.String()
}

func sesPageStart(token string) (int, bool) {
	if token == "" {
		return 0, true
	}
	start, err := strconv.Atoi(token)
	return start, err == nil && start >= 0
}

func sesMaxItems(body map[string]any) (int, bool) {
	value := stringValue(body["MaxItems"])
	if value == "" {
		return 1000, true
	}
	maxItems, err := strconv.Atoi(value)
	return maxItems, err == nil && maxItems >= 1 && maxItems <= 1000
}

func sesTemplateMaxItems(body map[string]any) (int, bool) {
	value := stringValue(body["MaxItems"])
	if value == "" {
		return 10, true
	}
	maxItems, err := strconv.Atoi(value)
	return maxItems, err == nil && maxItems >= 1 && maxItems <= 100
}

func sesMembers(body map[string]any, prefix string) []string {
	values := []string{}
	for i := 1; ; i++ {
		value := stringValue(body[prefix+strconv.Itoa(i)])
		if value == "" {
			return values
		}
		values = append(values, value)
	}
}

func sesMemberCount(body map[string]any, prefix string) int {
	for i := 1; ; i++ {
		if stringValue(body[prefix+strconv.Itoa(i)]) != "" {
			continue
		}
		nested := false
		for key := range body {
			if strings.HasPrefix(key, prefix+strconv.Itoa(i)+".") {
				nested = true
				break
			}
		}
		if !nested {
			return i - 1
		}
	}
}

func sesHasRecipient(body map[string]any, prefix string) bool {
	for _, kind := range []string{"ToAddresses", "CcAddresses", "BccAddresses"} {
		if sesMemberCount(body, prefix+kind+".member.") > 0 {
			return true
		}
	}
	return false
}

func writeSESResult(w http.ResponseWriter, action, result string) {
	w.Header().Set("Content-Type", "text/xml")
	_, _ = fmt.Fprintf(w, `<%sResponse xmlns="https://email.amazonaws.com/doc/2010-12-01/"><%sResult>%s</%sResult><ResponseMetadata><RequestId>homeport</RequestId></ResponseMetadata></%sResponse>`, action, action, result, action, action)
}

func writeSESV2JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-amzn-requestid", "homeport")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSESV2Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("x-amzn-errortype", code)
	writeSESV2JSON(w, status, map[string]string{"message": message})
}
