package aws

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type CodeBuildAdapter struct {
	mu         sync.Mutex
	projects   map[string]codeBuildProject
	quota      int
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type CodeBuildOption func(*CodeBuildAdapter)

type codeBuildProject struct {
	name        string
	description string
	artifacts   any
	environment any
	serviceRole string
	source      any
	tags        map[string]string
	createdAt   time.Time
	updatedAt   time.Time
}

func NewCodeBuildAdapter(options ...CodeBuildOption) *CodeBuildAdapter {
	adapter := &CodeBuildAdapter{projects: map[string]codeBuildProject{}, authorizer: authz.AllowAll}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithCodeBuildProjectQuota(maxProjects int) CodeBuildOption {
	return func(adapter *CodeBuildAdapter) { adapter.quota = maxProjects }
}

func WithCodeBuildAuthorizer(authorizer authz.Authorizer) CodeBuildOption {
	return func(adapter *CodeBuildAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}
func WithCodeBuildAuditSink(sink func(authz.Decision)) CodeBuildOption {
	return func(adapter *CodeBuildAdapter) { adapter.auditSink = sink }
}

func (CodeBuildAdapter) Provider() string { return "aws" }
func (CodeBuildAdapter) Service() string  { return "codebuild" }
func (CodeBuildAdapter) Routes() []string { return []string{"POST /compat/aws/codebuild"} }
func (CodeBuildAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_CODEBUILD": "http://homeport:8080/api/v1/compat/aws/codebuild",
		"HOMEPORT_COMPAT_BACKEND":    "gitlab-ci",
	}
}
func (CodeBuildAdapter) ConformanceChecks() []string {
	return []string{"create-project", "batch-get-projects", "list-projects", "update-project", "delete-project", "project-tag-round-trip"}
}

func (a *CodeBuildAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeCodeBuildError(w, "InvalidInputException", err.Error())
		return
	}
	if !a.authorized(w, r, action, body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch action {
	case "CreateProject":
		name := stringValue(body["name"])
		if !codeBuildProjectNameValid(name) || body["artifacts"] == nil || body["environment"] == nil || body["source"] == nil || stringValue(body["serviceRole"]) == "" {
			writeCodeBuildError(w, "InvalidInputException", "name, artifacts, environment, serviceRole, and source are required")
			return
		}
		if _, exists := a.projects[name]; exists {
			writeCodeBuildError(w, "ResourceAlreadyExistsException", "project already exists")
			return
		}
		if a.quota > 0 && len(a.projects) >= a.quota {
			writeCodeBuildError(w, "AccountLimitExceededException", "project quota exceeded")
			return
		}
		tags, ok := ecrTags(body["tags"])
		if !ok {
			writeCodeBuildError(w, "InvalidInputException", "tags are invalid")
			return
		}
		project := codeBuildProject{name: name, description: stringValue(body["description"]), artifacts: body["artifacts"], environment: body["environment"], serviceRole: stringValue(body["serviceRole"]), source: body["source"], tags: tags, createdAt: time.Now().UTC(), updatedAt: time.Now().UTC()}
		a.projects[name] = project
		writeJSON(w, http.StatusOK, map[string]any{"project": project.shape()})
	case "BatchGetProjects":
		names, _ := body["names"].([]any)
		projects := make([]map[string]any, 0, len(names))
		missing := make([]string, 0)
		for _, value := range names {
			name := stringValue(value)
			if project, ok := a.projects[name]; ok {
				projects = append(projects, project.shape())
			} else {
				missing = append(missing, name)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"projects": projects, "projectsNotFound": missing})
	case "ListProjects":
		if sortBy := stringValue(body["sortBy"]); sortBy != "" && sortBy != "NAME" {
			writeCodeBuildError(w, "InvalidInputException", "sortBy is unsupported")
			return
		}
		names := make([]string, 0, len(a.projects))
		for name := range a.projects {
			names = append(names, name)
		}
		sort.Strings(names)
		if stringValue(body["sortOrder"]) == "DESCENDING" {
			for left, right := 0, len(names)-1; left < right; left, right = left+1, right-1 {
				names[left], names[right] = names[right], names[left]
			}
		}
		start, ok := codeBuildStart(body, len(names))
		if !ok {
			writeCodeBuildError(w, "InvalidInputException", "nextToken is invalid")
			return
		}
		end := start + 100
		if end > len(names) {
			end = len(names)
		}
		response := map[string]any{"projects": names[start:end]}
		if end < len(names) {
			response["nextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "UpdateProject":
		name := stringValue(body["name"])
		project, ok := a.projects[name]
		if !ok {
			writeCodeBuildError(w, "ResourceNotFoundException", "project does not exist")
			return
		}
		if description, ok := body["description"].(string); ok {
			project.description = description
		}
		if artifacts := body["artifacts"]; artifacts != nil {
			project.artifacts = artifacts
		}
		if environment := body["environment"]; environment != nil {
			project.environment = environment
		}
		if serviceRole, ok := body["serviceRole"].(string); ok {
			project.serviceRole = serviceRole
		}
		if source := body["source"]; source != nil {
			project.source = source
		}
		project.updatedAt = time.Now().UTC()
		a.projects[name] = project
		writeJSON(w, http.StatusOK, map[string]any{"project": project.shape()})
	case "DeleteProject":
		name := stringValue(body["name"])
		if _, ok := a.projects[name]; !ok {
			writeCodeBuildError(w, "ResourceNotFoundException", "project does not exist")
			return
		}
		delete(a.projects, name)
		writeJSON(w, http.StatusOK, map[string]any{})
	default:
		writeCodeBuildError(w, "InvalidInputException", "CodeBuild action is not implemented")
	}
}

func (p codeBuildProject) shape() map[string]any {
	return map[string]any{"arn": "arn:aws:codebuild:us-east-1:000000000000:project/" + p.name, "name": p.name, "description": p.description, "artifacts": p.artifacts, "environment": p.environment, "serviceRole": p.serviceRole, "source": p.source, "tags": ecrTagShape(p.tags), "created": p.createdAt.Unix(), "lastModified": p.updatedAt.Unix()}
}

func codeBuildProjectNameValid(name string) bool {
	if len(name) == 0 || len(name) > 255 {
		return false
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}

func codeBuildStart(body map[string]any, count int) (int, bool) {
	token := stringValue(body["nextToken"])
	if token == "" {
		return 0, true
	}
	start, err := strconv.Atoi(token)
	return start, err == nil && start >= 0 && start < count
}

func (a *CodeBuildAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	for _, resource := range codeBuildResources(action, body) {
		if !a.authorizedResource(w, r, action, resource) {
			return false
		}
	}
	return true
}

func codeBuildResources(action string, body map[string]any) []string {
	if action == "BatchGetProjects" {
		if names, ok := body["names"].([]any); ok && len(names) > 0 {
			resources := make([]string, 0, len(names))
			for _, name := range names {
				resources = append(resources, codeBuildProjectARN(stringValue(name)))
			}
			return resources
		}
	}
	return []string{codeBuildProjectARN(stringValue(body["name"]))}
}

func codeBuildProjectARN(name string) string {
	if name == "" {
		name = "*"
	}
	return "arn:aws:codebuild:us-east-1:000000000000:project/" + name
}

func (a *CodeBuildAdapter) authorizedResource(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	decision, err := a.authorizer.Authorize(r.Context(), authz.Request{Principal: awsPrincipal(r), PrincipalAttributes: awsPrincipalAttributes(r), Action: "codebuild:" + action, Resource: resource, Context: map[string]string{"provider": "aws", "service": "codebuild", "method": r.Method, "source_ip": sourceIP(r), "current_time": time.Now().UTC().Format(time.RFC3339), "user_agent": r.UserAgent()}, Claims: awsClaims(r)})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"__type": "InternalError", "message": err.Error()})
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{"__type": "AccessDeniedException", "message": decision.Reason})
		return false
	}
	return true
}

func writeCodeBuildError(w http.ResponseWriter, code, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"__type": code, "message": message})
}
