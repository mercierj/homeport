package aws

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type EFSAdapter struct {
	mu                sync.Mutex
	fileSystems       map[string]efsFileSystem
	mountTargets      map[string]efsMountTarget
	accessPoints      map[string]efsAccessPoint
	nextID            int
	nextMountTargetID int
	nextAccessPointID int
	authorizer        authz.Authorizer
	auditSink         func(authz.Decision)
}

type EFSOption func(*EFSAdapter)

type efsFileSystem struct {
	ID               string
	CreationToken    string
	ThroughputMode   string
	Tags             []map[string]string
	MountTargetCount int
	CreatedAt        time.Time
	Policy           string
}

type efsMountTarget struct {
	ID           string
	FileSystemID string
	SubnetID     string
	IPAddress    string
}

type efsAccessPoint struct {
	ID            string
	Arn           string
	ClientToken   string
	FileSystemID  string
	PosixUser     map[string]any
	RootDirectory map[string]any
	Tags          []map[string]string
}

func NewEFSAdapter(options ...EFSOption) *EFSAdapter {
	adapter := &EFSAdapter{
		fileSystems:  map[string]efsFileSystem{},
		mountTargets: map[string]efsMountTarget{},
		accessPoints: map[string]efsAccessPoint{},
		authorizer:   authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithEFSAuthorizer(authorizer authz.Authorizer) EFSOption {
	return func(adapter *EFSAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithEFSAuditSink(sink func(authz.Decision)) EFSOption {
	return func(adapter *EFSAdapter) {
		adapter.auditSink = sink
	}
}

func (EFSAdapter) Provider() string { return "aws" }
func (EFSAdapter) Service() string  { return "efs" }
func (EFSAdapter) Routes() []string { return []string{"ANY /compat/aws/efs"} }
func (EFSAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_EFS":    "http://homeport:8080/api/v1/compat/aws/efs",
		"HOMEPORT_COMPAT_BACKEND": "nfs",
	}
}
func (EFSAdapter) ConformanceChecks() []string {
	return []string{"create-file-system", "describe-file-systems", "update-file-system", "delete-file-system", "create-mount-target", "describe-mount-targets", "delete-mount-target", "create-access-point", "describe-access-points", "delete-access-point"}
}

func (a *EFSAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/2015-02-01/file-systems":
		body, ok := decodeEFSBody(w, r)
		if !ok {
			return
		}
		token := stringValue(body["CreationToken"])
		if token == "" {
			writeEFSError(w, http.StatusBadRequest, "BadRequest", "CreationToken is required")
			return
		}
		if !a.authorized(w, r, "CreateFileSystem", "*") {
			return
		}
		for _, fs := range a.fileSystems {
			if fs.CreationToken == token {
				writeEFSJSON(w, http.StatusOK, efsFileSystemJSON(fs))
				return
			}
		}
		a.nextID++
		fs := efsFileSystem{
			ID:             "fs-" + strconv.FormatInt(int64(a.nextID), 16),
			CreationToken:  token,
			ThroughputMode: efsStringDefault(stringValue(body["ThroughputMode"]), "bursting"),
			Tags:           efsTags(body["Tags"]),
			CreatedAt:      time.Now().UTC(),
		}
		a.fileSystems[fs.ID] = fs
		writeEFSJSON(w, http.StatusCreated, efsFileSystemJSON(fs))
	case r.Method == http.MethodGet && r.URL.Path == "/2015-02-01/file-systems":
		if !a.authorized(w, r, "DescribeFileSystems", efsResource(r.URL.Query().Get("FileSystemId"))) {
			return
		}
		writeEFSJSON(w, http.StatusOK, a.describeFileSystems(r))
	case r.Method == http.MethodPost && r.URL.Path == "/2015-02-01/mount-targets":
		a.createMountTarget(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/2015-02-01/mount-targets":
		a.describeMountTargets(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/2015-02-01/mount-targets/"):
		a.deleteMountTarget(w, r, strings.TrimPrefix(r.URL.Path, "/2015-02-01/mount-targets/"))
	case r.Method == http.MethodPost && r.URL.Path == "/2015-02-01/access-points":
		a.createAccessPoint(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/2015-02-01/access-points":
		a.describeAccessPoints(w, r)
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/2015-02-01/access-points/"):
		a.deleteAccessPoint(w, r, strings.TrimPrefix(r.URL.Path, "/2015-02-01/access-points/"))
	case strings.HasPrefix(r.URL.Path, "/2015-02-01/file-systems/"):
		a.serveFileSystem(w, r, strings.TrimPrefix(r.URL.Path, "/2015-02-01/file-systems/"))
	default:
		writeEFSError(w, http.StatusBadRequest, "BadRequest", "unsupported EFS action")
	}
}

func (a *EFSAdapter) createMountTarget(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeEFSBody(w, r)
	if !ok {
		return
	}
	fileSystemID := stringValue(body["FileSystemId"])
	if _, ok := a.fileSystems[fileSystemID]; !ok {
		writeEFSError(w, http.StatusNotFound, "FileSystemNotFound", "file system not found")
		return
	}
	if !a.authorized(w, r, "CreateMountTarget", efsResource(fileSystemID)) {
		return
	}
	a.nextMountTargetID++
	target := efsMountTarget{
		ID:           "fsmt-" + strconv.FormatInt(int64(a.nextMountTargetID), 16),
		FileSystemID: fileSystemID,
		SubnetID:     stringValue(body["SubnetId"]),
		IPAddress:    efsStringDefault(stringValue(body["IpAddress"]), "192.0.2."+strconv.Itoa(a.nextMountTargetID)),
	}
	a.mountTargets[target.ID] = target
	fs := a.fileSystems[fileSystemID]
	fs.MountTargetCount++
	a.fileSystems[fileSystemID] = fs
	writeEFSJSON(w, http.StatusCreated, efsMountTargetJSON(target))
}

func (a *EFSAdapter) describeMountTargets(w http.ResponseWriter, r *http.Request) {
	fileSystemID := r.URL.Query().Get("FileSystemId")
	mountTargetID := r.URL.Query().Get("MountTargetId")
	if !a.authorized(w, r, "DescribeMountTargets", efsResource(fileSystemID)) {
		return
	}
	ids := make([]string, 0, len(a.mountTargets))
	for id := range a.mountTargets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		target := a.mountTargets[id]
		if fileSystemID != "" && target.FileSystemID != fileSystemID {
			continue
		}
		if mountTargetID != "" && target.ID != mountTargetID {
			continue
		}
		out = append(out, efsMountTargetJSON(target))
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("Marker"))
	maxItems, _ := strconv.Atoi(r.URL.Query().Get("MaxItems"))
	if start < 0 || start > len(out) {
		start = 0
	}
	end := len(out)
	if maxItems > 0 && start+maxItems < end {
		end = start + maxItems
	}
	response := map[string]any{"MountTargets": out[start:end]}
	if end < len(out) {
		response["NextMarker"] = strconv.Itoa(end)
	}
	writeEFSJSON(w, http.StatusOK, response)
}

func (a *EFSAdapter) deleteMountTarget(w http.ResponseWriter, r *http.Request, id string) {
	target, ok := a.mountTargets[id]
	if !ok {
		writeEFSError(w, http.StatusNotFound, "MountTargetNotFound", "mount target not found")
		return
	}
	if !a.authorized(w, r, "DeleteMountTarget", efsResource(target.FileSystemID)) {
		return
	}
	delete(a.mountTargets, id)
	fs := a.fileSystems[target.FileSystemID]
	if fs.MountTargetCount > 0 {
		fs.MountTargetCount--
		a.fileSystems[target.FileSystemID] = fs
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *EFSAdapter) createAccessPoint(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeEFSBody(w, r)
	if !ok {
		return
	}
	fileSystemID := stringValue(body["FileSystemId"])
	if _, ok := a.fileSystems[fileSystemID]; !ok {
		writeEFSError(w, http.StatusNotFound, "FileSystemNotFound", "file system not found")
		return
	}
	if !a.authorized(w, r, "CreateAccessPoint", efsResource(fileSystemID)) {
		return
	}
	clientToken := stringValue(body["ClientToken"])
	for _, accessPoint := range a.accessPoints {
		if clientToken != "" && accessPoint.ClientToken == clientToken {
			writeEFSJSON(w, http.StatusOK, efsAccessPointJSON(accessPoint))
			return
		}
	}
	a.nextAccessPointID++
	id := "fsap-" + strconv.FormatInt(int64(a.nextAccessPointID), 16)
	accessPoint := efsAccessPoint{
		ID:            id,
		Arn:           "arn:aws:elasticfilesystem:us-east-1:000000000000:access-point/" + id,
		ClientToken:   clientToken,
		FileSystemID:  fileSystemID,
		PosixUser:     efsAnyMap(body["PosixUser"]),
		RootDirectory: efsAnyMap(body["RootDirectory"]),
		Tags:          efsTags(body["Tags"]),
	}
	a.accessPoints[id] = accessPoint
	writeEFSJSON(w, http.StatusCreated, efsAccessPointJSON(accessPoint))
}

func (a *EFSAdapter) describeAccessPoints(w http.ResponseWriter, r *http.Request) {
	fileSystemID := r.URL.Query().Get("FileSystemId")
	accessPointID := r.URL.Query().Get("AccessPointId")
	if !a.authorized(w, r, "DescribeAccessPoints", efsResource(fileSystemID)) {
		return
	}
	ids := make([]string, 0, len(a.accessPoints))
	for id := range a.accessPoints {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		accessPoint := a.accessPoints[id]
		if fileSystemID != "" && accessPoint.FileSystemID != fileSystemID {
			continue
		}
		if accessPointID != "" && accessPoint.ID != accessPointID {
			continue
		}
		out = append(out, efsAccessPointJSON(accessPoint))
	}
	start, _ := strconv.Atoi(r.URL.Query().Get("NextToken"))
	maxResults, _ := strconv.Atoi(r.URL.Query().Get("MaxResults"))
	if start < 0 || start > len(out) {
		start = 0
	}
	end := len(out)
	if maxResults > 0 && start+maxResults < end {
		end = start + maxResults
	}
	response := map[string]any{"AccessPoints": out[start:end]}
	if end < len(out) {
		response["NextToken"] = strconv.Itoa(end)
	}
	writeEFSJSON(w, http.StatusOK, response)
}

func (a *EFSAdapter) deleteAccessPoint(w http.ResponseWriter, r *http.Request, id string) {
	accessPoint, ok := a.accessPoints[id]
	if !ok {
		writeEFSError(w, http.StatusNotFound, "AccessPointNotFound", "access point not found")
		return
	}
	if !a.authorized(w, r, "DeleteAccessPoint", efsResource(accessPoint.FileSystemID)) {
		return
	}
	delete(a.accessPoints, id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *EFSAdapter) describeFileSystems(r *http.Request) map[string]any {
	ids := make([]string, 0, len(a.fileSystems))
	for id := range a.fileSystems {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	fileSystemID := r.URL.Query().Get("FileSystemId")
	creationToken := r.URL.Query().Get("CreationToken")
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		fs := a.fileSystems[id]
		if fileSystemID != "" && fs.ID != fileSystemID {
			continue
		}
		if creationToken != "" && fs.CreationToken != creationToken {
			continue
		}
		out = append(out, efsFileSystemJSON(fs))
	}

	start, _ := strconv.Atoi(r.URL.Query().Get("Marker"))
	maxItems, _ := strconv.Atoi(r.URL.Query().Get("MaxItems"))
	if start < 0 || start > len(out) {
		start = 0
	}
	end := len(out)
	if maxItems > 0 && start+maxItems < end {
		end = start + maxItems
	}
	response := map[string]any{"FileSystems": out[start:end]}
	if end < len(out) {
		response["NextMarker"] = strconv.Itoa(end)
	}
	return response
}

func (a *EFSAdapter) serveFileSystem(w http.ResponseWriter, r *http.Request, id string) {
	subresource := ""
	if before, after, ok := strings.Cut(id, "/"); ok {
		id = before
		subresource = after
	}
	fs, ok := a.fileSystems[id]
	if !ok {
		writeEFSError(w, http.StatusNotFound, "FileSystemNotFound", "file system not found")
		return
	}
	if subresource == "lifecycle-configuration" {
		if r.Method != http.MethodGet {
			writeEFSError(w, http.StatusBadRequest, "BadRequest", "unsupported EFS action")
			return
		}
		if !a.authorized(w, r, "DescribeLifecycleConfiguration", efsResource(id)) {
			return
		}
		writeEFSJSON(w, http.StatusOK, map[string]any{"LifecyclePolicies": []any{}})
		return
	}
	if subresource == "policy" {
		a.serveFileSystemPolicy(w, r, id, fs)
		return
	}

	switch r.Method {
	case http.MethodPut:
		if !a.authorized(w, r, "UpdateFileSystem", efsResource(id)) {
			return
		}
		body, ok := decodeEFSBody(w, r)
		if !ok {
			return
		}
		if mode := stringValue(body["ThroughputMode"]); mode != "" {
			fs.ThroughputMode = mode
		}
		a.fileSystems[id] = fs
		writeEFSJSON(w, http.StatusOK, efsFileSystemJSON(fs))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteFileSystem", efsResource(id)) {
			return
		}
		for _, target := range a.mountTargets {
			if target.FileSystemID == id {
				writeEFSError(w, http.StatusConflict, "FileSystemInUse", "file system has mount targets")
				return
			}
		}
		for _, accessPoint := range a.accessPoints {
			if accessPoint.FileSystemID == id {
				writeEFSError(w, http.StatusConflict, "FileSystemInUse", "file system has access points")
				return
			}
		}
		delete(a.fileSystems, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		writeEFSError(w, http.StatusBadRequest, "BadRequest", "unsupported EFS action")
	}
}

func (a *EFSAdapter) serveFileSystemPolicy(w http.ResponseWriter, r *http.Request, id string, fs efsFileSystem) {
	switch r.Method {
	case http.MethodPut:
		if !a.authorized(w, r, "PutFileSystemPolicy", efsResource(id)) {
			return
		}
		body, ok := decodeEFSBody(w, r)
		if !ok {
			return
		}
		fs.Policy = stringValue(body["Policy"])
		a.fileSystems[id] = fs
		writeEFSJSON(w, http.StatusOK, map[string]any{"FileSystemId": id, "Policy": fs.Policy})
	case http.MethodGet:
		if !a.authorized(w, r, "DescribeFileSystemPolicy", efsResource(id)) {
			return
		}
		if fs.Policy == "" {
			writeEFSError(w, http.StatusNotFound, "PolicyNotFound", "policy not found")
			return
		}
		writeEFSJSON(w, http.StatusOK, map[string]any{"FileSystemId": id, "Policy": fs.Policy})
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteFileSystemPolicy", efsResource(id)) {
			return
		}
		fs.Policy = ""
		a.fileSystems[id] = fs
		w.WriteHeader(http.StatusNoContent)
	default:
		writeEFSError(w, http.StatusBadRequest, "BadRequest", "unsupported EFS action")
	}
}

func efsAccessPointJSON(accessPoint efsAccessPoint) map[string]any {
	out := map[string]any{
		"AccessPointArn": accessPoint.Arn,
		"AccessPointId":  accessPoint.ID,
		"ClientToken":    accessPoint.ClientToken,
		"FileSystemId":   accessPoint.FileSystemID,
		"LifeCycleState": "available",
		"OwnerId":        "000000000000",
		"Tags":           accessPoint.Tags,
	}
	if len(accessPoint.PosixUser) > 0 {
		out["PosixUser"] = accessPoint.PosixUser
	}
	if len(accessPoint.RootDirectory) > 0 {
		out["RootDirectory"] = accessPoint.RootDirectory
	}
	return out
}

func efsMountTargetJSON(target efsMountTarget) map[string]any {
	return map[string]any{
		"FileSystemId":       target.FileSystemID,
		"IpAddress":          target.IPAddress,
		"LifeCycleState":     "available",
		"MountTargetId":      target.ID,
		"NetworkInterfaceId": "eni-" + strings.TrimPrefix(target.ID, "fsmt-"),
		"OwnerId":            "000000000000",
		"SubnetId":           target.SubnetID,
		"VpcId":              "vpc-homeport",
	}
}

func (a *EFSAdapter) authorized(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "elasticfilesystem:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "efs",
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
		writeEFSError(w, http.StatusInternalServerError, "InternalServerError", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeEFSError(w, http.StatusForbidden, "AccessDenied", decision.Reason)
		return false
	}
	return true
}

func efsResource(id string) string {
	if id == "" {
		return "*"
	}
	return "arn:aws:elasticfilesystem:us-east-1:000000000000:file-system/" + id
}

func efsFileSystemJSON(fs efsFileSystem) map[string]any {
	return map[string]any{
		"CreationTime":         fs.CreatedAt.Unix(),
		"CreationToken":        fs.CreationToken,
		"FileSystemId":         fs.ID,
		"LifeCycleState":       "available",
		"NumberOfMountTargets": fs.MountTargetCount,
		"OwnerId":              "000000000000",
		"PerformanceMode":      "generalPurpose",
		"SizeInBytes":          map[string]int{"Value": 0},
		"Tags":                 fs.Tags,
		"ThroughputMode":       fs.ThroughputMode,
	}
}

func efsTags(value any) []map[string]string {
	values, _ := value.([]any)
	tags := make([]map[string]string, 0, len(values))
	for _, value := range values {
		tag, _ := value.(map[string]any)
		if len(tag) == 0 {
			continue
		}
		tags = append(tags, map[string]string{
			"Key":   stringValue(tag["Key"]),
			"Value": stringValue(tag["Value"]),
		})
	}
	return tags
}

func efsAnyMap(value any) map[string]any {
	out, _ := value.(map[string]any)
	return out
}

func efsStringDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func writeEFSJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func decodeEFSBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeEFSError(w, http.StatusBadRequest, "BadRequest", "invalid JSON request body")
		return nil, false
	}
	return body, true
}

func writeEFSError(w http.ResponseWriter, status int, code, message string) {
	writeEFSJSON(w, status, map[string]string{"__type": code, "message": message})
}
