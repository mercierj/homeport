package aws

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type IAMAdapter struct {
	mu               sync.Mutex
	roles            map[string]iamRole
	instanceProfiles map[string]iamInstanceProfile
	policies         map[string]iamPolicy
	nextID           int
	nextProfileID    int
	nextPolicyID     int
	authorizer       authz.Authorizer
	auditSink        func(authz.Decision)
}

type IAMOption func(*IAMAdapter)

type iamRole struct {
	Name        string
	Path        string
	ID          string
	Arn         string
	Assume      string
	Description string
	CreatedAt   time.Time
	Tags        map[string]string
	Policies    map[string]string
	Attached    map[string]string
}

type iamInstanceProfile struct {
	Name      string
	Path      string
	ID        string
	Arn       string
	CreatedAt time.Time
	Roles     map[string]bool
}

type iamPolicy struct {
	Name        string
	Path        string
	ID          string
	Arn         string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Tags        map[string]string
	Versions    map[string]iamPolicyVersion
	Default     string
	NextVersion int
}

type iamPolicyVersion struct {
	ID        string
	Document  string
	CreatedAt time.Time
}

func NewIAMAdapter(options ...IAMOption) *IAMAdapter {
	adapter := &IAMAdapter{
		roles:            map[string]iamRole{},
		instanceProfiles: map[string]iamInstanceProfile{},
		policies:         map[string]iamPolicy{},
		authorizer:       authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithIAMAuthorizer(authorizer authz.Authorizer) IAMOption {
	return func(adapter *IAMAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithIAMAuditSink(sink func(authz.Decision)) IAMOption {
	return func(adapter *IAMAdapter) {
		adapter.auditSink = sink
	}
}

func (IAMAdapter) Provider() string { return "aws" }
func (IAMAdapter) Service() string  { return "iam" }
func (IAMAdapter) Routes() []string { return []string{"POST /compat/aws/iam"} }
func (IAMAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_IAM":    "http://homeport:8080/api/v1/compat/aws/iam",
		"HOMEPORT_COMPAT_BACKEND": "keycloak",
	}
}
func (IAMAdapter) ConformanceChecks() []string {
	return []string{"create-role", "get-role", "list-roles", "update-role", "delete-role", "put-role-policy", "get-role-policy", "list-role-policies", "delete-role-policy", "create-policy", "get-policy", "get-policy-version", "create-policy-version", "list-policy-versions", "set-default-policy-version", "list-policies", "delete-policy", "delete-policy-version", "attach-role-policy", "list-attached-role-policies", "detach-role-policy", "create-instance-profile", "get-instance-profile", "add-role-to-instance-profile", "remove-role-from-instance-profile", "delete-instance-profile", "list-instance-profiles-for-role", "list-role-tags", "tag-role", "untag-role"}
}

func (a *IAMAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateRole":
		name := stringValue(body["RoleName"])
		if name == "" {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "RoleName is required")
			return
		}
		if _, exists := a.roles[name]; exists {
			writeQueryErrorCode(w, http.StatusConflict, "EntityAlreadyExists", "role already exists")
			return
		}
		if !a.authorized(w, r, "CreateRole", iamRoleResource(name)) {
			return
		}
		a.nextID++
		path := stringValue(body["Path"])
		if path == "" {
			path = "/"
		}
		role := iamRole{
			Name:        name,
			Path:        path,
			ID:          "AROA" + fmt.Sprintf("%017d", a.nextID),
			Arn:         "arn:aws:iam::000000000000:role" + path + name,
			Assume:      stringValue(body["AssumeRolePolicyDocument"]),
			Description: stringValue(body["Description"]),
			CreatedAt:   time.Now().UTC(),
			Tags:        iamTags(body),
			Policies:    map[string]string{},
			Attached:    map[string]string{},
		}
		a.roles[name] = role
		writeIAMResult(w, "CreateRole", "<Role>"+iamRoleXML(role)+"</Role>")
	case "GetRole":
		role, ok := a.roles[stringValue(body["RoleName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "GetRole", role.Arn) {
			return
		}
		writeIAMResult(w, "GetRole", "<Role>"+iamRoleXML(role)+"</Role>")
	case "ListRoles":
		if !a.authorized(w, r, "ListRoles", "*") {
			return
		}
		pathPrefix := stringValue(body["PathPrefix"])
		if pathPrefix == "" {
			pathPrefix = "/"
		}
		names := make([]string, 0, len(a.roles))
		for name, role := range a.roles {
			if !strings.HasPrefix(role.Path, pathPrefix) {
				continue
			}
			names = append(names, name)
		}
		sort.Strings(names)
		start := 0
		if marker := stringValue(body["Marker"]); marker != "" {
			var err error
			start, err = strconv.Atoi(marker)
			if err != nil || start < 0 || start > len(names) {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid Marker")
				return
			}
		}
		maxItems := 0
		if value := stringValue(body["MaxItems"]); value != "" {
			var err error
			maxItems, err = strconv.Atoi(value)
			if err != nil || maxItems < 1 {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid MaxItems")
				return
			}
		}
		end := len(names)
		if maxItems > 0 && start+maxItems < end {
			end = start + maxItems
		}
		var roles strings.Builder
		for _, name := range names[start:end] {
			roles.WriteString("<member>")
			roles.WriteString(iamRoleXML(a.roles[name]))
			roles.WriteString("</member>")
		}
		marker := ""
		if end < len(names) {
			marker = "<Marker>" + strconv.Itoa(end) + "</Marker>"
		}
		writeIAMResult(w, "ListRoles", "<Roles>"+roles.String()+"</Roles><IsTruncated>"+strconv.FormatBool(end < len(names))+"</IsTruncated>"+marker)
	case "PutRolePolicy":
		name := stringValue(body["RoleName"])
		role, ok := a.roles[name]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "PutRolePolicy", role.Arn) {
			return
		}
		if role.Policies == nil {
			role.Policies = map[string]string{}
		}
		role.Policies[stringValue(body["PolicyName"])] = stringValue(body["PolicyDocument"])
		a.roles[name] = role
		writeIAMResult(w, "PutRolePolicy", "")
	case "GetRolePolicy":
		role, ok := a.roles[stringValue(body["RoleName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "GetRolePolicy", role.Arn) {
			return
		}
		policyName := stringValue(body["PolicyName"])
		policyDocument, ok := role.Policies[policyName]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		writeIAMResult(w, "GetRolePolicy", "<RoleName>"+xmlEscape(role.Name)+"</RoleName><PolicyName>"+xmlEscape(policyName)+"</PolicyName><PolicyDocument>"+xmlEscape(policyDocument)+"</PolicyDocument>")
	case "ListRolePolicies":
		role, ok := a.roles[stringValue(body["RoleName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "ListRolePolicies", role.Arn) {
			return
		}
		names := make([]string, 0, len(role.Policies))
		for name := range role.Policies {
			names = append(names, name)
		}
		sort.Strings(names)
		start := 0
		if marker := stringValue(body["Marker"]); marker != "" {
			var err error
			start, err = strconv.Atoi(marker)
			if err != nil || start < 0 || start > len(names) {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid Marker")
				return
			}
		}
		maxItems := 0
		if value := stringValue(body["MaxItems"]); value != "" {
			var err error
			maxItems, err = strconv.Atoi(value)
			if err != nil || maxItems < 1 {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid MaxItems")
				return
			}
		}
		end := len(names)
		if maxItems > 0 && start+maxItems < end {
			end = start + maxItems
		}
		var policies strings.Builder
		for _, name := range names[start:end] {
			policies.WriteString("<member>" + xmlEscape(name) + "</member>")
		}
		marker := ""
		if end < len(names) {
			marker = "<Marker>" + strconv.Itoa(end) + "</Marker>"
		}
		writeIAMResult(w, "ListRolePolicies", "<PolicyNames>"+policies.String()+"</PolicyNames><IsTruncated>"+strconv.FormatBool(end < len(names))+"</IsTruncated>"+marker)
	case "DeleteRolePolicy":
		name := stringValue(body["RoleName"])
		role, ok := a.roles[name]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "DeleteRolePolicy", role.Arn) {
			return
		}
		policyName := stringValue(body["PolicyName"])
		if _, ok := role.Policies[policyName]; !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		delete(role.Policies, policyName)
		a.roles[name] = role
		writeIAMResult(w, "DeleteRolePolicy", "")
	case "CreatePolicy":
		name := stringValue(body["PolicyName"])
		if name == "" {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "PolicyName is required")
			return
		}
		path := stringValue(body["Path"])
		if path == "" {
			path = "/"
		}
		arn := iamPolicyResource(path, name)
		if _, exists := a.policies[arn]; exists {
			writeQueryErrorCode(w, http.StatusConflict, "EntityAlreadyExists", "policy already exists")
			return
		}
		if !a.authorized(w, r, "CreatePolicy", arn) {
			return
		}
		now := time.Now().UTC()
		a.nextPolicyID++
		policy := iamPolicy{
			Name:        name,
			Path:        path,
			ID:          "ANPA" + fmt.Sprintf("%017d", a.nextPolicyID),
			Arn:         arn,
			Description: stringValue(body["Description"]),
			CreatedAt:   now,
			UpdatedAt:   now,
			Tags:        iamTags(body),
			Versions: map[string]iamPolicyVersion{
				"v1": {ID: "v1", Document: stringValue(body["PolicyDocument"]), CreatedAt: now},
			},
			Default:     "v1",
			NextVersion: 1,
		}
		a.policies[arn] = policy
		writeIAMResult(w, "CreatePolicy", "<Policy>"+iamPolicyXML(policy, a.policyAttachmentCount(arn))+"</Policy>")
	case "GetPolicy":
		arn := stringValue(body["PolicyArn"])
		policy, ok := a.policies[arn]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		if !a.authorized(w, r, "GetPolicy", arn) {
			return
		}
		writeIAMResult(w, "GetPolicy", "<Policy>"+iamPolicyXML(policy, a.policyAttachmentCount(arn))+"</Policy>")
	case "GetPolicyVersion":
		arn := stringValue(body["PolicyArn"])
		policy, ok := a.policies[arn]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		version, ok := policy.Versions[stringValue(body["VersionId"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy version not found")
			return
		}
		if !a.authorized(w, r, "GetPolicyVersion", arn) {
			return
		}
		writeIAMResult(w, "GetPolicyVersion", "<PolicyVersion>"+iamPolicyVersionXML(policy, version)+"</PolicyVersion>")
	case "CreatePolicyVersion":
		arn := stringValue(body["PolicyArn"])
		policy, ok := a.policies[arn]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		if !a.authorized(w, r, "CreatePolicyVersion", arn) {
			return
		}
		if len(policy.Versions) >= 5 {
			writeQueryErrorCode(w, http.StatusConflict, "LimitExceeded", "policy version limit exceeded")
			return
		}
		now := time.Now().UTC()
		policy.NextVersion++
		version := iamPolicyVersion{
			ID:        "v" + strconv.Itoa(policy.NextVersion),
			Document:  stringValue(body["PolicyDocument"]),
			CreatedAt: now,
		}
		policy.Versions[version.ID] = version
		if stringValue(body["SetAsDefault"]) == "true" {
			policy.Default = version.ID
		}
		policy.UpdatedAt = now
		a.policies[arn] = policy
		writeIAMResult(w, "CreatePolicyVersion", "<PolicyVersion>"+iamPolicyVersionXML(policy, version)+"</PolicyVersion>")
	case "ListPolicyVersions":
		arn := stringValue(body["PolicyArn"])
		policy, ok := a.policies[arn]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		if !a.authorized(w, r, "ListPolicyVersions", arn) {
			return
		}
		ids := make([]string, 0, len(policy.Versions))
		for id := range policy.Versions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var versions strings.Builder
		for _, id := range ids {
			versions.WriteString("<member>")
			versions.WriteString(iamPolicyVersionXML(policy, policy.Versions[id]))
			versions.WriteString("</member>")
		}
		writeIAMResult(w, "ListPolicyVersions", "<Versions>"+versions.String()+"</Versions>")
	case "SetDefaultPolicyVersion":
		arn := stringValue(body["PolicyArn"])
		policy, ok := a.policies[arn]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		versionID := stringValue(body["VersionId"])
		if _, ok := policy.Versions[versionID]; !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy version not found")
			return
		}
		if !a.authorized(w, r, "SetDefaultPolicyVersion", arn) {
			return
		}
		policy.Default = versionID
		policy.UpdatedAt = time.Now().UTC()
		a.policies[arn] = policy
		writeIAMResult(w, "SetDefaultPolicyVersion", "")
	case "ListPolicies":
		if !a.authorized(w, r, "ListPolicies", "*") {
			return
		}
		pathPrefix := stringValue(body["PathPrefix"])
		if pathPrefix == "" {
			pathPrefix = "/"
		}
		scope := stringValue(body["Scope"])
		onlyAttached := stringValue(body["OnlyAttached"]) == "true"
		arns := make([]string, 0, len(a.policies))
		for arn, policy := range a.policies {
			if scope == "AWS" || !strings.HasPrefix(policy.Path, pathPrefix) {
				continue
			}
			if onlyAttached && a.policyAttachmentCount(arn) == 0 {
				continue
			}
			arns = append(arns, arn)
		}
		sort.Strings(arns)
		start := 0
		if marker := stringValue(body["Marker"]); marker != "" {
			var err error
			start, err = strconv.Atoi(marker)
			if err != nil || start < 0 || start > len(arns) {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid Marker")
				return
			}
		}
		maxItems := 0
		if value := stringValue(body["MaxItems"]); value != "" {
			var err error
			maxItems, err = strconv.Atoi(value)
			if err != nil || maxItems < 1 {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid MaxItems")
				return
			}
		}
		end := len(arns)
		if maxItems > 0 && start+maxItems < end {
			end = start + maxItems
		}
		var policies strings.Builder
		for _, arn := range arns[start:end] {
			policies.WriteString("<member>")
			policies.WriteString(iamPolicyXML(a.policies[arn], a.policyAttachmentCount(arn)))
			policies.WriteString("</member>")
		}
		marker := ""
		if end < len(arns) {
			marker = "<Marker>" + strconv.Itoa(end) + "</Marker>"
		}
		writeIAMResult(w, "ListPolicies", "<Policies>"+policies.String()+"</Policies><IsTruncated>"+strconv.FormatBool(end < len(arns))+"</IsTruncated>"+marker)
	case "DeletePolicy":
		arn := stringValue(body["PolicyArn"])
		if _, ok := a.policies[arn]; !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		if !a.authorized(w, r, "DeletePolicy", arn) {
			return
		}
		if a.policyAttachmentCount(arn) > 0 {
			writeQueryErrorCode(w, http.StatusConflict, "DeleteConflict", "policy is attached to a role")
			return
		}
		delete(a.policies, arn)
		writeIAMResult(w, "DeletePolicy", "")
	case "DeletePolicyVersion":
		arn := stringValue(body["PolicyArn"])
		policy, ok := a.policies[arn]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		versionID := stringValue(body["VersionId"])
		if _, ok := policy.Versions[versionID]; !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy version not found")
			return
		}
		if versionID == policy.Default {
			writeQueryErrorCode(w, http.StatusConflict, "DeleteConflict", "cannot delete default policy version")
			return
		}
		if !a.authorized(w, r, "DeletePolicyVersion", arn) {
			return
		}
		delete(policy.Versions, versionID)
		policy.UpdatedAt = time.Now().UTC()
		a.policies[arn] = policy
		writeIAMResult(w, "DeletePolicyVersion", "")
	case "AttachRolePolicy":
		name := stringValue(body["RoleName"])
		role, ok := a.roles[name]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "AttachRolePolicy", role.Arn) {
			return
		}
		policyARN := stringValue(body["PolicyArn"])
		if _, ok := a.policies[policyARN]; !ok && !strings.HasPrefix(policyARN, "arn:aws:iam::aws:policy/") {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "policy not found")
			return
		}
		if role.Attached == nil {
			role.Attached = map[string]string{}
		}
		role.Attached[policyARN] = iamPolicyName(policyARN)
		a.roles[name] = role
		writeIAMResult(w, "AttachRolePolicy", "")
	case "ListAttachedRolePolicies":
		role, ok := a.roles[stringValue(body["RoleName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "ListAttachedRolePolicies", role.Arn) {
			return
		}
		arns := make([]string, 0, len(role.Attached))
		for arn := range role.Attached {
			arns = append(arns, arn)
		}
		sort.Strings(arns)
		start := 0
		if marker := stringValue(body["Marker"]); marker != "" {
			var err error
			start, err = strconv.Atoi(marker)
			if err != nil || start < 0 || start > len(arns) {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid Marker")
				return
			}
		}
		maxItems := 0
		if value := stringValue(body["MaxItems"]); value != "" {
			var err error
			maxItems, err = strconv.Atoi(value)
			if err != nil || maxItems < 1 {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid MaxItems")
				return
			}
		}
		end := len(arns)
		if maxItems > 0 && start+maxItems < end {
			end = start + maxItems
		}
		var policies strings.Builder
		for _, arn := range arns[start:end] {
			policies.WriteString("<member><PolicyName>" + xmlEscape(role.Attached[arn]) + "</PolicyName><PolicyArn>" + xmlEscape(arn) + "</PolicyArn></member>")
		}
		marker := ""
		if end < len(arns) {
			marker = "<Marker>" + strconv.Itoa(end) + "</Marker>"
		}
		writeIAMResult(w, "ListAttachedRolePolicies", "<AttachedPolicies>"+policies.String()+"</AttachedPolicies><IsTruncated>"+strconv.FormatBool(end < len(arns))+"</IsTruncated>"+marker)
	case "DetachRolePolicy":
		name := stringValue(body["RoleName"])
		role, ok := a.roles[name]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "DetachRolePolicy", role.Arn) {
			return
		}
		delete(role.Attached, stringValue(body["PolicyArn"]))
		a.roles[name] = role
		writeIAMResult(w, "DetachRolePolicy", "")
	case "CreateInstanceProfile":
		name := stringValue(body["InstanceProfileName"])
		if name == "" {
			writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "InstanceProfileName is required")
			return
		}
		if _, exists := a.instanceProfiles[name]; exists {
			writeQueryErrorCode(w, http.StatusConflict, "EntityAlreadyExists", "instance profile already exists")
			return
		}
		path := stringValue(body["Path"])
		if path == "" {
			path = "/"
		}
		if !a.authorized(w, r, "CreateInstanceProfile", iamInstanceProfileResource(path, name)) {
			return
		}
		a.nextProfileID++
		profile := iamInstanceProfile{
			Name:      name,
			Path:      path,
			ID:        "AIPA" + fmt.Sprintf("%017d", a.nextProfileID),
			Arn:       iamInstanceProfileResource(path, name),
			CreatedAt: time.Now().UTC(),
			Roles:     map[string]bool{},
		}
		a.instanceProfiles[name] = profile
		writeIAMResult(w, "CreateInstanceProfile", "<InstanceProfile>"+iamInstanceProfileXML(profile, a.roles)+"</InstanceProfile>")
	case "GetInstanceProfile":
		profile, ok := a.instanceProfiles[stringValue(body["InstanceProfileName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "instance profile not found")
			return
		}
		if !a.authorized(w, r, "GetInstanceProfile", profile.Arn) {
			return
		}
		writeIAMResult(w, "GetInstanceProfile", "<InstanceProfile>"+iamInstanceProfileXML(profile, a.roles)+"</InstanceProfile>")
	case "AddRoleToInstanceProfile":
		profile, ok := a.instanceProfiles[stringValue(body["InstanceProfileName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "instance profile not found")
			return
		}
		roleName := stringValue(body["RoleName"])
		if _, ok := a.roles[roleName]; !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "AddRoleToInstanceProfile", profile.Arn) {
			return
		}
		if profile.Roles == nil {
			profile.Roles = map[string]bool{}
		}
		profile.Roles[roleName] = true
		a.instanceProfiles[profile.Name] = profile
		writeIAMResult(w, "AddRoleToInstanceProfile", "")
	case "RemoveRoleFromInstanceProfile":
		profile, ok := a.instanceProfiles[stringValue(body["InstanceProfileName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "instance profile not found")
			return
		}
		roleName := stringValue(body["RoleName"])
		if _, ok := a.roles[roleName]; !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "RemoveRoleFromInstanceProfile", profile.Arn) {
			return
		}
		delete(profile.Roles, roleName)
		a.instanceProfiles[profile.Name] = profile
		writeIAMResult(w, "RemoveRoleFromInstanceProfile", "")
	case "DeleteInstanceProfile":
		profile, ok := a.instanceProfiles[stringValue(body["InstanceProfileName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "instance profile not found")
			return
		}
		if !a.authorized(w, r, "DeleteInstanceProfile", profile.Arn) {
			return
		}
		delete(a.instanceProfiles, profile.Name)
		writeIAMResult(w, "DeleteInstanceProfile", "")
	case "ListInstanceProfilesForRole":
		role, ok := a.roles[stringValue(body["RoleName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "ListInstanceProfilesForRole", role.Arn) {
			return
		}
		names := make([]string, 0, len(a.instanceProfiles))
		for name, profile := range a.instanceProfiles {
			if profile.Roles[role.Name] {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		start := 0
		if marker := stringValue(body["Marker"]); marker != "" {
			var err error
			start, err = strconv.Atoi(marker)
			if err != nil || start < 0 || start > len(names) {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid Marker")
				return
			}
		}
		maxItems := 0
		if value := stringValue(body["MaxItems"]); value != "" {
			var err error
			maxItems, err = strconv.Atoi(value)
			if err != nil || maxItems < 1 {
				writeQueryErrorCode(w, http.StatusBadRequest, "InvalidInput", "invalid MaxItems")
				return
			}
		}
		end := len(names)
		if maxItems > 0 && start+maxItems < end {
			end = start + maxItems
		}
		var profiles strings.Builder
		for _, name := range names[start:end] {
			profiles.WriteString("<member>")
			profiles.WriteString(iamInstanceProfileXML(a.instanceProfiles[name], a.roles))
			profiles.WriteString("</member>")
		}
		marker := ""
		if end < len(names) {
			marker = "<Marker>" + strconv.Itoa(end) + "</Marker>"
		}
		writeIAMResult(w, "ListInstanceProfilesForRole", "<InstanceProfiles>"+profiles.String()+"</InstanceProfiles><IsTruncated>"+strconv.FormatBool(end < len(names))+"</IsTruncated>"+marker)
	case "ListRoleTags":
		role, ok := a.roles[stringValue(body["RoleName"])]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "ListRoleTags", role.Arn) {
			return
		}
		writeIAMResult(w, "ListRoleTags", iamTagsXML(role.Tags)+"<IsTruncated>false</IsTruncated>")
	case "TagRole":
		name := stringValue(body["RoleName"])
		role, ok := a.roles[name]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "TagRole", role.Arn) {
			return
		}
		if role.Tags == nil {
			role.Tags = map[string]string{}
		}
		mergeStringMap(role.Tags, iamTags(body))
		a.roles[name] = role
		writeIAMResult(w, "TagRole", "")
	case "UntagRole":
		name := stringValue(body["RoleName"])
		role, ok := a.roles[name]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "UntagRole", role.Arn) {
			return
		}
		for _, key := range iamTagKeys(body) {
			delete(role.Tags, key)
		}
		a.roles[name] = role
		writeIAMResult(w, "UntagRole", "")
	case "UpdateRole":
		name := stringValue(body["RoleName"])
		role, ok := a.roles[name]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "UpdateRole", role.Arn) {
			return
		}
		role.Description = stringValue(body["Description"])
		a.roles[name] = role
		writeIAMResult(w, "UpdateRole", "")
	case "DeleteRole":
		name := stringValue(body["RoleName"])
		role, ok := a.roles[name]
		if !ok {
			writeQueryErrorCode(w, http.StatusNotFound, "NoSuchEntity", "role not found")
			return
		}
		if !a.authorized(w, r, "DeleteRole", role.Arn) {
			return
		}
		if len(role.Policies) > 0 || len(role.Attached) > 0 {
			writeQueryErrorCode(w, http.StatusConflict, "DeleteConflict", "role has policies attached")
			return
		}
		for _, profile := range a.instanceProfiles {
			if profile.Roles[name] {
				writeQueryErrorCode(w, http.StatusConflict, "DeleteConflict", "role is attached to an instance profile")
				return
			}
		}
		delete(a.roles, name)
		writeIAMResult(w, "DeleteRole", "")
	default:
		writeQueryErrorCode(w, http.StatusBadRequest, "InvalidAction", "unsupported IAM action")
	}
}

func (a *IAMAdapter) authorized(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "iam:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "iam",
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
		writeQueryErrorCode(w, http.StatusInternalServerError, "InternalFailure", err.Error())
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

func iamRoleResource(name string) string {
	if name == "" {
		return "*"
	}
	return "arn:aws:iam::000000000000:role/" + name
}

func iamPolicyResource(path, name string) string {
	if name == "" {
		return "*"
	}
	if path == "" {
		path = "/"
	}
	return "arn:aws:iam::000000000000:policy" + path + name
}

func iamInstanceProfileResource(path, name string) string {
	if name == "" {
		return "*"
	}
	if path == "" {
		path = "/"
	}
	return "arn:aws:iam::000000000000:instance-profile" + path + name
}

func iamTags(body map[string]any) map[string]string {
	tags := map[string]string{}
	for i := 1; ; i++ {
		key := stringValue(body["Tags.member."+strconv.Itoa(i)+".Key"])
		if key == "" {
			return tags
		}
		tags[key] = stringValue(body["Tags.member."+strconv.Itoa(i)+".Value"])
	}
}

func iamTagKeys(body map[string]any) []string {
	var keys []string
	for i := 1; ; i++ {
		key := stringValue(body["TagKeys.member."+strconv.Itoa(i)])
		if key == "" {
			return keys
		}
		keys = append(keys, key)
	}
}

func iamPolicyName(arn string) string {
	if _, name, ok := strings.Cut(arn, "policy/"); ok {
		if i := strings.LastIndex(name, "/"); i >= 0 {
			return name[i+1:]
		}
		return name
	}
	return arn
}

func (a *IAMAdapter) policyAttachmentCount(arn string) int {
	count := 0
	for _, role := range a.roles {
		if _, ok := role.Attached[arn]; ok {
			count++
		}
	}
	return count
}

func iamTagsXML(tags map[string]string) string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	out.WriteString("<Tags>")
	for _, key := range keys {
		out.WriteString("<member><Key>" + xmlEscape(key) + "</Key><Value>" + xmlEscape(tags[key]) + "</Value></member>")
	}
	out.WriteString("</Tags>")
	return out.String()
}

func writeIAMResult(w http.ResponseWriter, action, result string) {
	w.Header().Set("Content-Type", "text/xml")
	_, _ = fmt.Fprintf(w, `<%sResponse xmlns="https://iam.amazonaws.com/doc/2010-05-08/"><%sResult>%s</%sResult><ResponseMetadata><RequestId>homeport</RequestId></ResponseMetadata></%sResponse>`, action, action, result, action, action)
}

func iamRoleXML(role iamRole) string {
	if len(role.Tags) > 0 {
		return "<Path>" + xmlEscape(role.Path) + "</Path><RoleName>" + xmlEscape(role.Name) + "</RoleName><RoleId>" + xmlEscape(role.ID) + "</RoleId><Arn>" + xmlEscape(role.Arn) + "</Arn><CreateDate>" + role.CreatedAt.Format(time.RFC3339) + "</CreateDate><AssumeRolePolicyDocument>" + xmlEscape(role.Assume) + "</AssumeRolePolicyDocument><Description>" + xmlEscape(role.Description) + "</Description>" + iamTagsXML(role.Tags)
	}
	return "<Path>" + xmlEscape(role.Path) + "</Path><RoleName>" + xmlEscape(role.Name) + "</RoleName><RoleId>" + xmlEscape(role.ID) + "</RoleId><Arn>" + xmlEscape(role.Arn) + "</Arn><CreateDate>" + role.CreatedAt.Format(time.RFC3339) + "</CreateDate><AssumeRolePolicyDocument>" + xmlEscape(role.Assume) + "</AssumeRolePolicyDocument><Description>" + xmlEscape(role.Description) + "</Description>"
}

func iamInstanceProfileXML(profile iamInstanceProfile, roles map[string]iamRole) string {
	roleNames := make([]string, 0, len(profile.Roles))
	for name := range profile.Roles {
		if _, ok := roles[name]; ok {
			roleNames = append(roleNames, name)
		}
	}
	sort.Strings(roleNames)
	var roleXML strings.Builder
	roleXML.WriteString("<Roles>")
	for _, name := range roleNames {
		roleXML.WriteString("<member>")
		roleXML.WriteString(iamRoleXML(roles[name]))
		roleXML.WriteString("</member>")
	}
	roleXML.WriteString("</Roles>")
	return "<Path>" + xmlEscape(profile.Path) + "</Path><InstanceProfileName>" + xmlEscape(profile.Name) + "</InstanceProfileName><InstanceProfileId>" + xmlEscape(profile.ID) + "</InstanceProfileId><Arn>" + xmlEscape(profile.Arn) + "</Arn><CreateDate>" + profile.CreatedAt.Format(time.RFC3339) + "</CreateDate>" + roleXML.String()
}

func iamPolicyXML(policy iamPolicy, attachmentCount int) string {
	out := "<PolicyName>" + xmlEscape(policy.Name) + "</PolicyName><PolicyId>" + xmlEscape(policy.ID) + "</PolicyId><Arn>" + xmlEscape(policy.Arn) + "</Arn><Path>" + xmlEscape(policy.Path) + "</Path><DefaultVersionId>" + xmlEscape(policy.Default) + "</DefaultVersionId><AttachmentCount>" + strconv.Itoa(attachmentCount) + "</AttachmentCount><PermissionsBoundaryUsageCount>0</PermissionsBoundaryUsageCount><IsAttachable>true</IsAttachable><CreateDate>" + policy.CreatedAt.Format(time.RFC3339) + "</CreateDate><UpdateDate>" + policy.UpdatedAt.Format(time.RFC3339) + "</UpdateDate><Description>" + xmlEscape(policy.Description) + "</Description>"
	if len(policy.Tags) > 0 {
		out += iamTagsXML(policy.Tags)
	}
	return out
}

func iamPolicyVersionXML(policy iamPolicy, version iamPolicyVersion) string {
	return "<Document>" + xmlEscape(version.Document) + "</Document><VersionId>" + xmlEscape(version.ID) + "</VersionId><IsDefaultVersion>" + strconv.FormatBool(version.ID == policy.Default) + "</IsDefaultVersion><CreateDate>" + version.CreatedAt.Format(time.RFC3339) + "</CreateDate>"
}
