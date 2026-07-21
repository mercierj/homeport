package aws

import (
	"fmt"
	"html"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

// ALBAdapter exposes the local subset of the Elastic Load Balancing v2 API
// needed while migrating an Application Load Balancer to Traefik.
type ALBAdapter struct {
	mu            sync.Mutex
	loadBalancers map[string]albLoadBalancer
	nextID        int
	quota         int
	authorizer    authz.Authorizer
	auditSink     func(authz.Decision)
}

type ALBOption func(*ALBAdapter)

type albLoadBalancer struct {
	ARN        string
	Name       string
	DNSName    string
	Scheme     string
	Type       string
	Subnets    []string
	Attributes map[string]string
}

func NewALBAdapter(options ...ALBOption) *ALBAdapter {
	adapter := &ALBAdapter{loadBalancers: map[string]albLoadBalancer{}, authorizer: authz.AllowAll}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithALBAuthorizer(authorizer authz.Authorizer) ALBOption {
	return func(adapter *ALBAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithALBAuditSink(sink func(authz.Decision)) ALBOption {
	return func(adapter *ALBAdapter) { adapter.auditSink = sink }
}

func WithALBQuota(maxLoadBalancers int) ALBOption {
	return func(adapter *ALBAdapter) { adapter.quota = maxLoadBalancers }
}

func (ALBAdapter) Provider() string { return "aws" }
func (ALBAdapter) Service() string  { return "alb" }
func (ALBAdapter) Routes() []string { return []string{"POST /compat/aws/alb"} }
func (ALBAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_ELBV2":  "http://homeport:8080/api/v1/compat/aws/alb",
		"HOMEPORT_COMPAT_BACKEND": "traefik",
	}
}
func (ALBAdapter) ConformanceChecks() []string {
	return []string{"create-load-balancer", "describe-load-balancers", "modify-load-balancer-attributes", "delete-load-balancer"}
}

func (a *ALBAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeALBErrorStatus(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "ALB compatibility requests must use POST")
		return
	}
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeALBError(w, "ValidationError", err.Error())
		return
	}
	if !a.authorized(w, r, action, body) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateLoadBalancer":
		name := stringValue(body["Name"])
		if !validALBName(name) || !validALBCreateInput(body) {
			writeALBError(w, "ValidationError", "load balancer name is invalid")
			return
		}
		if a.hasName(name) {
			writeALBError(w, "DuplicateLoadBalancerName", "load balancer already exists")
			return
		}
		if a.quota > 0 && len(a.loadBalancers) >= a.quota {
			writeALBError(w, "TooManyLoadBalancers", "load balancer quota exceeded")
			return
		}
		a.nextID++
		loadBalancer := albLoadBalancer{
			ARN:        "arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/app/" + name + "/homeport-" + strconv.Itoa(a.nextID),
			Name:       name,
			DNSName:    name + ".homeport.local",
			Scheme:     defaultALBScheme(stringValue(body["Scheme"])),
			Type:       defaultALBType(stringValue(body["Type"])),
			Subnets:    albFormMembers(body, "Subnets"),
			Attributes: map[string]string{},
		}
		a.loadBalancers[loadBalancer.ARN] = loadBalancer
		writeALBResult(w, "CreateLoadBalancer", "<LoadBalancers><member>"+albXML(loadBalancer)+"</member></LoadBalancers>")
	case "DescribeLoadBalancers":
		loadBalancers, ok := a.describe(body)
		if !ok {
			writeALBError(w, "LoadBalancerNotFound", "load balancer not found")
			return
		}
		start, pageSize, ok := albPage(body, len(loadBalancers))
		if !ok {
			writeALBError(w, "ValidationError", "Marker or PageSize is invalid")
			return
		}
		end := start + pageSize
		if end > len(loadBalancers) {
			end = len(loadBalancers)
		}
		result := albLoadBalancersXML(loadBalancers[start:end])
		if end < len(loadBalancers) {
			result += "<NextMarker>" + strconv.Itoa(end) + "</NextMarker>"
		}
		writeALBResult(w, "DescribeLoadBalancers", result)
	case "ModifyLoadBalancerAttributes":
		arn := stringValue(body["LoadBalancerArn"])
		loadBalancer, ok := a.loadBalancers[arn]
		if !ok {
			writeALBError(w, "LoadBalancerNotFound", "load balancer not found")
			return
		}
		for _, attribute := range albAttributesFromForm(body) {
			loadBalancer.Attributes[attribute.Key] = attribute.Value
		}
		a.loadBalancers[arn] = loadBalancer
		writeALBResult(w, "ModifyLoadBalancerAttributes", albAttributesXML(loadBalancer.Attributes))
	case "DeleteLoadBalancer":
		arn := stringValue(body["LoadBalancerArn"])
		if _, ok := a.loadBalancers[arn]; !ok {
			writeALBError(w, "LoadBalancerNotFound", "load balancer not found")
			return
		}
		delete(a.loadBalancers, arn)
		writeALBResult(w, "DeleteLoadBalancer", "")
	default:
		writeALBError(w, "UnsupportedOperation", "ALB action is not implemented")
	}
}

func (a *ALBAdapter) hasName(name string) bool {
	for _, loadBalancer := range a.loadBalancers {
		if loadBalancer.Name == name {
			return true
		}
	}
	return false
}

func (a *ALBAdapter) describe(body map[string]any) ([]albLoadBalancer, bool) {
	arns := albFormMembers(body, "LoadBalancerArns")
	names := albFormMembers(body, "Names")
	if len(arns) > 0 {
		loadBalancers := make([]albLoadBalancer, 0, len(arns))
		for _, arn := range arns {
			loadBalancer, ok := a.loadBalancers[arn]
			if !ok {
				return nil, false
			}
			loadBalancers = append(loadBalancers, loadBalancer)
		}
		return loadBalancers, true
	}
	if len(names) > 0 {
		loadBalancers := make([]albLoadBalancer, 0, len(names))
		for _, name := range names {
			found := false
			for _, loadBalancer := range a.loadBalancers {
				if loadBalancer.Name == name {
					loadBalancers = append(loadBalancers, loadBalancer)
					found = true
					break
				}
			}
			if !found {
				return nil, false
			}
		}
		return loadBalancers, true
	}
	loadBalancers := make([]albLoadBalancer, 0, len(a.loadBalancers))
	for _, loadBalancer := range a.loadBalancers {
		loadBalancers = append(loadBalancers, loadBalancer)
	}
	sort.Slice(loadBalancers, func(i, j int) bool { return loadBalancers[i].Name < loadBalancers[j].Name })
	return loadBalancers, true
}

func albPage(body map[string]any, count int) (int, int, bool) {
	start := 0
	if marker := stringValue(body["Marker"]); marker != "" {
		parsed, err := strconv.Atoi(marker)
		if err != nil || parsed < 0 || parsed >= count {
			return 0, 0, false
		}
		start = parsed
	}
	pageSize := 400
	if value := stringValue(body["PageSize"]); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 400 {
			return 0, 0, false
		}
		pageSize = parsed
	}
	return start, pageSize, true
}

func (a *ALBAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	for _, resource := range albAuthorizationResources(body) {
		decision, err := a.authorizer.Authorize(r.Context(), authz.Request{
			Principal:           awsPrincipal(r),
			PrincipalAttributes: awsPrincipalAttributes(r),
			Action:              "elasticloadbalancing:" + action,
			Resource:            resource,
			Context: map[string]string{
				"provider": "aws", "service": "alb", "method": r.Method, "request_id": "homeport",
				"source_ip": sourceIP(r), "current_time": time.Now().UTC().Format(time.RFC3339), "user_agent": r.UserAgent(),
			},
			Claims: awsClaims(r),
		})
		if err != nil {
			writeALBErrorStatus(w, http.StatusInternalServerError, "InternalFailure", err.Error())
			return false
		}
		if a.auditSink != nil {
			a.auditSink(decision)
		}
		if !decision.Allowed {
			writeALBErrorStatus(w, http.StatusForbidden, "AccessDenied", decision.Reason)
			return false
		}
	}
	return true
}

func albAuthorizationResources(body map[string]any) []string {
	if arn := stringValue(body["LoadBalancerArn"]); arn != "" {
		return []string{arn}
	}
	if arns := albFormMembers(body, "LoadBalancerArns"); len(arns) > 0 {
		return arns
	}
	if names := albFormMembers(body, "Names"); len(names) > 0 {
		resources := make([]string, 0, len(names))
		for _, name := range names {
			resources = append(resources, "arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/app/"+name+"/*")
		}
		return resources
	}
	if name := stringValue(body["Name"]); name != "" {
		return []string{"arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/app/" + name + "/*"}
	}
	return []string{"*"}
}

func validALBName(name string) bool {
	if name == "" || len(name) > 32 || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.HasPrefix(name, "internal-") {
		return false
	}
	for _, char := range name {
		if char != '-' && (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func validALBCreateInput(body map[string]any) bool {
	if len(albFormMembers(body, "Subnets")) == 0 && !albHasMember(body, "SubnetMappings") {
		return false
	}
	if scheme := stringValue(body["Scheme"]); scheme != "" && scheme != "internet-facing" && scheme != "internal" {
		return false
	}
	if loadBalancerType := stringValue(body["Type"]); loadBalancerType != "" && loadBalancerType != "application" {
		return false
	}
	return true
}

func albHasMember(body map[string]any, name string) bool {
	prefix := name + ".member."
	for key := range body {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func defaultALBScheme(scheme string) string {
	if scheme == "" {
		return "internet-facing"
	}
	return scheme
}

func defaultALBType(loadBalancerType string) string {
	if loadBalancerType == "" {
		return "application"
	}
	return loadBalancerType
}

func albFormMembers(body map[string]any, name string) []string {
	prefix := name + ".member."
	keys := make([]string, 0)
	for key := range body {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := stringValue(body[key]); value != "" {
			values = append(values, value)
		}
	}
	return values
}

type albAttribute struct{ Key, Value string }

func albAttributesFromForm(body map[string]any) []albAttribute {
	const prefix = "Attributes.member."
	keys := make([]string, 0)
	for path := range body {
		if strings.HasPrefix(path, prefix) && strings.HasSuffix(path, ".Key") {
			keys = append(keys, path)
		}
	}
	sort.Strings(keys)
	attributes := make([]albAttribute, 0, len(keys))
	for _, keyPath := range keys {
		key := stringValue(body[keyPath])
		if key == "" {
			continue
		}
		valuePath := strings.TrimSuffix(keyPath, ".Key") + ".Value"
		attributes = append(attributes, albAttribute{Key: key, Value: stringValue(body[valuePath])})
	}
	return attributes
}

func albXML(loadBalancer albLoadBalancer) string {
	return "<LoadBalancerArn>" + html.EscapeString(loadBalancer.ARN) + "</LoadBalancerArn>" +
		"<LoadBalancerName>" + html.EscapeString(loadBalancer.Name) + "</LoadBalancerName>" +
		"<DNSName>" + html.EscapeString(loadBalancer.DNSName) + "</DNSName>" +
		"<Scheme>" + html.EscapeString(loadBalancer.Scheme) + "</Scheme>" +
		"<Type>" + html.EscapeString(loadBalancer.Type) + "</Type><State><Code>active</Code></State>"
}

func albLoadBalancersXML(loadBalancers []albLoadBalancer) string {
	var out strings.Builder
	out.WriteString("<LoadBalancers>")
	for _, loadBalancer := range loadBalancers {
		out.WriteString("<member>")
		out.WriteString(albXML(loadBalancer))
		out.WriteString("</member>")
	}
	out.WriteString("</LoadBalancers>")
	return out.String()
}

func albAttributesXML(attributes map[string]string) string {
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString("<Attributes>")
	for _, key := range keys {
		out.WriteString("<member><Key>")
		out.WriteString(html.EscapeString(key))
		out.WriteString("</Key><Value>")
		out.WriteString(html.EscapeString(attributes[key]))
		out.WriteString("</Value></member>")
	}
	out.WriteString("</Attributes>")
	return out.String()
}

func writeALBError(w http.ResponseWriter, code, message string) {
	writeALBErrorStatus(w, http.StatusBadRequest, code, message)
}

func writeALBErrorStatus(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<ErrorResponse><Error><Code>%s</Code><Message>%s</Message></Error><RequestId>homeport</RequestId></ErrorResponse>", html.EscapeString(code), html.EscapeString(message))
}

func writeALBResult(w http.ResponseWriter, action, result string) {
	w.Header().Set("Content-Type", "text/xml")
	_, _ = fmt.Fprintf(w, `<%sResponse xmlns="http://elasticloadbalancing.amazonaws.com/doc/2015-12-01/"><%sResult>%s</%sResult><ResponseMetadata><RequestId>homeport</RequestId></ResponseMetadata></%sResponse>`, action, action, result, action, action)
}
