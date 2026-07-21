package aws

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type ECRAdapter struct {
	mu              sync.Mutex
	repositories    map[string]ecrRepository
	authorizer      authz.Authorizer
	auditSink       func(authz.Decision)
	repositoryQuota int
}

type ECROption func(*ECRAdapter)

type ecrRepository struct {
	name      string
	createdAt time.Time
	tags      map[string]string
	images    map[string]ecrImage
}
type ecrImage struct {
	digest   string
	manifest string
	tag      string
}

func NewECRAdapter(options ...ECROption) *ECRAdapter {
	adapter := &ECRAdapter{repositories: map[string]ecrRepository{}, authorizer: authz.AllowAll, repositoryQuota: 100}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithECRAuthorizer(authorizer authz.Authorizer) ECROption {
	return func(adapter *ECRAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithECRAuditSink(sink func(authz.Decision)) ECROption {
	return func(adapter *ECRAdapter) { adapter.auditSink = sink }
}

func WithECRRepositoryQuota(maxRepositories int) ECROption {
	return func(adapter *ECRAdapter) { adapter.repositoryQuota = maxRepositories }
}

func (ECRAdapter) Provider() string { return "aws" }
func (ECRAdapter) Service() string  { return "ecr" }
func (ECRAdapter) Routes() []string { return []string{"POST /compat/aws/ecr"} }
func (ECRAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_ECR":    "http://homeport:8080/api/v1/compat/aws/ecr",
		"HOMEPORT_COMPAT_BACKEND": "oci-distribution",
	}
}
func (ECRAdapter) ConformanceChecks() []string {
	return []string{"create-repository", "describe-repositories", "put-image", "describe-images", "list-images", "batch-get-image", "batch-delete-image", "delete-repository"}
}

func (a *ECRAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeECRError(w, "InvalidParameterException", err.Error())
		return
	}
	if !a.authorized(w, r, action, body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch action {
	case "CreateRepository":
		name := stringValue(body["repositoryName"])
		if !ecrRepositoryNameValid(name) {
			writeECRError(w, "InvalidParameterException", "repositoryName is invalid")
			return
		}
		if _, exists := a.repositories[name]; exists {
			writeECRError(w, "RepositoryAlreadyExistsException", "repository already exists")
			return
		}
		if a.repositoryQuota > 0 && len(a.repositories) >= a.repositoryQuota {
			writeECRError(w, "LimitExceededException", "repository quota exceeded")
			return
		}
		tags, ok := ecrTags(body["tags"])
		if !ok {
			writeECRError(w, "InvalidParameterException", "tags are invalid")
			return
		}
		repository := ecrRepository{name: name, createdAt: time.Now().UTC(), tags: tags, images: map[string]ecrImage{}}
		a.repositories[name] = repository
		writeJSON(w, http.StatusOK, map[string]any{"repository": repository.shape()})
	case "DescribeRepositories":
		names, _ := body["repositoryNames"].([]any)
		if len(names) > 0 {
			repositories := make([]map[string]any, 0, len(names))
			for _, value := range names {
				repository, ok := a.repositories[stringValue(value)]
				if !ok {
					writeECRError(w, "RepositoryNotFoundException", "repository not found")
					return
				}
				repositories = append(repositories, repository.shape())
			}
			writeJSON(w, http.StatusOK, map[string]any{"repositories": repositories})
			return
		}
		names = make([]any, 0, len(a.repositories))
		for name := range a.repositories {
			names = append(names, name)
		}
		sort.Slice(names, func(i, j int) bool { return names[i].(string) < names[j].(string) })
		start := 0
		if token := stringValue(body["nextToken"]); token != "" {
			parsed, err := strconv.Atoi(token)
			if err != nil || parsed < 0 || parsed >= len(names) {
				writeECRError(w, "InvalidParameterException", "nextToken is invalid")
				return
			}
			start = parsed
		}
		limit, ok := cloudWatchLogsLimit(body, 100, 1000, "maxResults")
		if !ok {
			writeECRError(w, "InvalidParameterException", "maxResults must be between 1 and 1000")
			return
		}
		end := start + limit
		if end > len(names) {
			end = len(names)
		}
		repositories := make([]map[string]any, 0, end-start)
		for _, value := range names[start:end] {
			repositories = append(repositories, a.repositories[value.(string)].shape())
		}
		response := map[string]any{"repositories": repositories}
		if end < len(names) {
			response["nextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "DescribeImages":
		repository, ok := a.repositories[stringValue(body["repositoryName"])]
		if !ok {
			writeECRError(w, "RepositoryNotFoundException", "repository not found")
			return
		}
		images := make([]ecrImage, 0)
		if identifiers, ok := body["imageIds"].([]any); ok && len(identifiers) > 0 {
			for _, value := range identifiers {
				identifier, _ := value.(map[string]any)
				tag := stringValue(identifier["imageTag"])
				if image, ok := repository.images[tag]; ok {
					images = append(images, image)
				}
			}
		} else {
			for _, image := range repository.images {
				images = append(images, image)
			}
			sort.Slice(images, func(i, j int) bool { return images[i].tag < images[j].tag })
		}
		details := make([]map[string]any, 0, len(images))
		for _, image := range images {
			details = append(details, map[string]any{"imageDigest": image.digest, "imageTags": []string{image.tag}})
		}
		writeJSON(w, http.StatusOK, map[string]any{"imageDetails": details})
	case "ListImages":
		repository, ok := a.repositories[stringValue(body["repositoryName"])]
		if !ok {
			writeECRError(w, "RepositoryNotFoundException", "repository not found")
			return
		}
		tags := make([]string, 0, len(repository.images))
		for tag := range repository.images {
			tags = append(tags, tag)
		}
		sort.Strings(tags)
		start := 0
		if token := stringValue(body["nextToken"]); token != "" {
			parsed, err := strconv.Atoi(token)
			if err != nil || parsed < 0 || parsed >= len(tags) {
				writeECRError(w, "InvalidParameterException", "nextToken is invalid")
				return
			}
			start = parsed
		}
		limit, ok := cloudWatchLogsLimit(body, 100, 1000, "maxResults")
		if !ok {
			writeECRError(w, "InvalidParameterException", "maxResults must be between 1 and 1000")
			return
		}
		end := start + limit
		if end > len(tags) {
			end = len(tags)
		}
		images := make([]map[string]string, 0, end-start)
		for _, tag := range tags[start:end] {
			image := repository.images[tag]
			images = append(images, map[string]string{"imageDigest": image.digest, "imageTag": image.tag})
		}
		response := map[string]any{"imageIds": images}
		if end < len(tags) {
			response["nextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "PutImage":
		repository, ok := a.repositories[stringValue(body["repositoryName"])]
		if !ok {
			writeECRError(w, "RepositoryNotFoundException", "repository not found")
			return
		}
		manifest := stringValue(body["imageManifest"])
		if manifest == "" {
			writeECRError(w, "InvalidParameterException", "imageManifest is required")
			return
		}
		digest := stringValue(body["imageDigest"])
		if digest == "" {
			digest = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(manifest)))
		}
		tag := stringValue(body["imageTag"])
		if tag == "" {
			tag = digest
		}
		image := ecrImage{digest: digest, manifest: manifest, tag: tag}
		repository.images[tag] = image
		a.repositories[repository.name] = repository
		writeJSON(w, http.StatusOK, map[string]any{"image": map[string]any{"registryId": "000000000000", "repositoryName": repository.name, "imageId": map[string]string{"imageDigest": image.digest, "imageTag": image.tag}, "imageManifest": image.manifest}})
	case "BatchDeleteImage":
		repository, ok := a.repositories[stringValue(body["repositoryName"])]
		if !ok {
			writeECRError(w, "RepositoryNotFoundException", "repository not found")
			return
		}
		deleted := make([]map[string]string, 0)
		for _, value := range body["imageIds"].([]any) {
			identifier, _ := value.(map[string]any)
			tag := stringValue(identifier["imageTag"])
			if tag == "" {
				for candidate, image := range repository.images {
					if image.digest == stringValue(identifier["imageDigest"]) {
						tag = candidate
						break
					}
				}
			}
			if image, ok := repository.images[tag]; ok {
				delete(repository.images, tag)
				deleted = append(deleted, map[string]string{"imageDigest": image.digest, "imageTag": image.tag})
			}
		}
		a.repositories[repository.name] = repository
		writeJSON(w, http.StatusOK, map[string]any{"imageIds": deleted, "failures": []any{}})
	case "BatchGetImage":
		repository, ok := a.repositories[stringValue(body["repositoryName"])]
		if !ok {
			writeECRError(w, "RepositoryNotFoundException", "repository not found")
			return
		}
		images := make([]map[string]any, 0)
		for _, value := range body["imageIds"].([]any) {
			identifier, _ := value.(map[string]any)
			tag := stringValue(identifier["imageTag"])
			if tag == "" {
				for candidate, image := range repository.images {
					if image.digest == stringValue(identifier["imageDigest"]) {
						tag = candidate
						break
					}
				}
			}
			if image, ok := repository.images[tag]; ok {
				images = append(images, map[string]any{"registryId": "000000000000", "repositoryName": repository.name, "imageId": map[string]string{"imageDigest": image.digest, "imageTag": image.tag}, "imageManifest": image.manifest})
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"images": images, "failures": []any{}})
	case "ListTagsForResource":
		repository, ok := a.repositories[ecrRepositoryNameFromARN(stringValue(body["resourceArn"]))]
		if !ok {
			writeECRError(w, "RepositoryNotFoundException", "repository not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tags": ecrTagShape(repository.tags)})
	case "TagResource":
		name := ecrRepositoryNameFromARN(stringValue(body["resourceArn"]))
		repository, ok := a.repositories[name]
		if !ok {
			writeECRError(w, "RepositoryNotFoundException", "repository not found")
			return
		}
		tags, ok := ecrTags(body["tags"])
		if !ok {
			writeECRError(w, "InvalidParameterException", "tags are invalid")
			return
		}
		for key, value := range tags {
			repository.tags[key] = value
		}
		a.repositories[name] = repository
		writeJSON(w, http.StatusOK, map[string]any{})
	case "UntagResource":
		name := ecrRepositoryNameFromARN(stringValue(body["resourceArn"]))
		repository, ok := a.repositories[name]
		if !ok {
			writeECRError(w, "RepositoryNotFoundException", "repository not found")
			return
		}
		for _, key := range ecrTagKeys(body["tagKeys"]) {
			delete(repository.tags, key)
		}
		a.repositories[name] = repository
		writeJSON(w, http.StatusOK, map[string]any{})
	case "DeleteRepository":
		name := stringValue(body["repositoryName"])
		repository, ok := a.repositories[name]
		if !ok {
			writeECRError(w, "RepositoryNotFoundException", "repository not found")
			return
		}
		delete(a.repositories, name)
		writeJSON(w, http.StatusOK, map[string]any{"repository": repository.shape()})
	default:
		writeECRError(w, "UnsupportedOperationException", "ECR action is not implemented")
	}
}

func (r ecrRepository) shape() map[string]any {
	return map[string]any{
		"createdAt":      r.createdAt.Unix(),
		"registryId":     "000000000000",
		"repositoryArn":  "arn:aws:ecr:us-east-1:000000000000:repository/" + r.name,
		"repositoryName": r.name,
		"repositoryUri":  "000000000000.dkr.ecr.us-east-1.amazonaws.com/" + r.name,
	}
}

func ecrRepositoryNameValid(name string) bool {
	if len(name) < 2 || len(name) > 256 || !((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= '0' && name[0] <= '9')) {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" {
			return false
		}
		for _, char := range segment {
			if !(char >= 'a' && char <= 'z') && !(char >= '0' && char <= '9') && char != '-' && char != '_' && char != '.' {
				return false
			}
		}
	}
	return true
}

func ecrRepositoryNameFromARN(arn string) string {
	const prefix = "arn:aws:ecr:us-east-1:000000000000:repository/"
	return strings.TrimPrefix(arn, prefix)
}

func ecrTags(value any) (map[string]string, bool) {
	tags := map[string]string{}
	if value == nil {
		return tags, true
	}
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	for _, item := range items {
		tag, ok := item.(map[string]any)
		key, tagKey := tag["Key"].(string)
		if !tagKey {
			key, tagKey = tag["key"].(string)
		}
		value, tagValue := tag["Value"].(string)
		if !tagValue {
			value, tagValue = tag["value"].(string)
		}
		if !ok || !tagKey || !tagValue || key == "" {
			return nil, false
		}
		tags[key] = value
	}
	return tags, true
}

func ecrTagShape(tags map[string]string) []map[string]string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, map[string]string{"key": key, "value": tags[key]})
	}
	return result
}

func ecrTagKeys(value any) []string {
	items, _ := value.([]any)
	keys := make([]string, 0, len(items))
	for _, item := range items {
		if key, ok := item.(string); ok {
			keys = append(keys, key)
		}
	}
	return keys
}

func writeECRError(w http.ResponseWriter, code, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"__type": code, "message": message})
}

func (a *ECRAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	for _, resource := range ecrResources(action, body) {
		if !a.authorizedResource(w, r, action, resource) {
			return false
		}
	}
	return true
}

func ecrResources(action string, body map[string]any) []string {
	if action == "DescribeRepositories" {
		if names, ok := body["repositoryNames"].([]any); ok && len(names) > 0 {
			resources := make([]string, 0, len(names))
			for _, name := range names {
				resources = append(resources, ecrResourceARN(stringValue(name)))
			}
			return resources
		}
	}
	return []string{ecrResourceARN(stringValue(body["repositoryName"]))}
}

func ecrResourceARN(name string) string {
	if name == "" {
		name = "*"
	}
	return "arn:aws:ecr:us-east-1:000000000000:repository/" + name
}

func (a *ECRAdapter) authorizedResource(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	decision, err := a.authorizer.Authorize(r.Context(), authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "ecr:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider": "aws", "service": "ecr", "method": r.Method, "request_id": "homeport",
			"source_ip": sourceIP(r), "current_time": time.Now().UTC().Format(time.RFC3339), "user_agent": r.UserAgent(),
		},
		Claims: awsClaims(r),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"__type": "ServerException", "message": err.Error()})
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
