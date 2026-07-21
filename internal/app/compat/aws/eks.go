package aws

import (
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type EKSAdapter struct {
	mu                      sync.Mutex
	clusters                map[string]eksCluster
	nodegroups              map[string]eksNodegroup
	addons                  map[string]eksAddon
	accessEntries           map[string]eksAccessEntry
	createClusterTokens     map[string]eksCreateClusterReplay
	createNodeTokens        map[string]eksCreateNodeReplay
	createAddonTokens       map[string]eksCreateAddonReplay
	createAccessTokens      map[string]eksCreateAccessReplay
	updateAddonTokens       map[string]eksUpdateAddonReplay
	updateNodeTokens        map[string]eksUpdateNodeReplay
	updateNodeVersionTokens map[string]eksUpdateNodeReplay
	updateAccessTokens      map[string]eksUpdateAccessReplay
	authorizer              authz.Authorizer
	auditSink               func(authz.Decision)
	clusterQuota            int
	nextID                  int
}

type EKSOption func(*EKSAdapter)

type eksCluster struct {
	Name               string
	Arn                string
	RoleArn            string
	DeletionProtection bool
	Tags               map[string]string
	CreatedAt          time.Time
	UpdateID           int
}

type eksCreateClusterReplay struct {
	Name      string
	Signature string
}

type eksCreateNodeReplay struct {
	Key       string
	Signature string
}

type eksCreateAddonReplay struct {
	Key       string
	Signature string
}

type eksCreateAccessReplay struct {
	Key       string
	Signature string
}

type eksUpdateAddonReplay struct {
	Key       string
	Signature string
}

type eksUpdateNodeReplay struct {
	Key       string
	Signature string
}

type eksUpdateAccessReplay struct {
	Key       string
	Signature string
}

type eksNodegroup struct {
	Cluster       string
	Name          string
	Arn           string
	NodeRole      string
	Subnets       []string
	ScalingConfig any
	Labels        map[string]string
	Tags          map[string]string
	Version       string
	Release       string
	CreatedAt     time.Time
	UpdateID      int
}

type eksAddon struct {
	Cluster                 string
	Name                    string
	Arn                     string
	Version                 string
	ConfigurationValues     string
	Namespace               string
	PodIdentityAssociations []string
	ServiceAccountRoleArn   string
	Tags                    map[string]string
	CreatedAt               time.Time
	UpdateID                int
}

type eksAccessEntry struct {
	Cluster          string
	Arn              string
	Principal        string
	KubernetesGroups []string
	Tags             map[string]string
	Type             string
	Username         string
	Policies         map[string]eksAssociatedAccessPolicy
	CreatedAt        time.Time
}

type eksAssociatedAccessPolicy struct {
	PolicyArn    string
	ScopeType    string
	Namespaces   []string
	AssociatedAt time.Time
}

type eksTagTarget struct {
	Arn  string
	Tags map[string]string
	Save func(map[string]string)
}

func NewEKSAdapter(options ...EKSOption) *EKSAdapter {
	adapter := &EKSAdapter{
		clusters:                map[string]eksCluster{},
		nodegroups:              map[string]eksNodegroup{},
		addons:                  map[string]eksAddon{},
		accessEntries:           map[string]eksAccessEntry{},
		createClusterTokens:     map[string]eksCreateClusterReplay{},
		createNodeTokens:        map[string]eksCreateNodeReplay{},
		createAddonTokens:       map[string]eksCreateAddonReplay{},
		createAccessTokens:      map[string]eksCreateAccessReplay{},
		updateAddonTokens:       map[string]eksUpdateAddonReplay{},
		updateNodeTokens:        map[string]eksUpdateNodeReplay{},
		updateNodeVersionTokens: map[string]eksUpdateNodeReplay{},
		updateAccessTokens:      map[string]eksUpdateAccessReplay{},
		authorizer:              authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithEKSAuthorizer(authorizer authz.Authorizer) EKSOption {
	return func(adapter *EKSAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithEKSAuditSink(sink func(authz.Decision)) EKSOption {
	return func(adapter *EKSAdapter) {
		adapter.auditSink = sink
	}
}

func WithEKSClusterQuota(maxClusters int) EKSOption {
	return func(adapter *EKSAdapter) {
		adapter.clusterQuota = maxClusters
	}
}

func (EKSAdapter) Provider() string { return "aws" }
func (EKSAdapter) Service() string  { return "eks" }
func (EKSAdapter) Routes() []string { return []string{"ANY /compat/aws/eks"} }
func (EKSAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_EKS":    "http://homeport:8080/api/v1/compat/aws/eks",
		"HOMEPORT_COMPAT_BACKEND": "k3s",
	}
}
func (EKSAdapter) ConformanceChecks() []string {
	return []string{"create-cluster", "describe-cluster", "list-clusters", "update-cluster-config", "delete-cluster"}
}

func (a *EKSAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case strings.HasPrefix(r.URL.Path, "/tags/"):
		a.serveTags(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/clusters":
		a.createCluster(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/clusters":
		a.listClusters(w, r)
	case strings.HasPrefix(r.URL.Path, "/clusters/"):
		a.serveCluster(w, r, strings.TrimPrefix(r.URL.Path, "/clusters/"))
	default:
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
	}
}

func (a *EKSAdapter) createCluster(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid JSON request body")
		return
	}
	if !a.authorized(w, r, "CreateCluster", "*") {
		return
	}
	name := stringValue(body["name"])
	if name == "" {
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "name is required")
		return
	}
	token := stringValue(body["clientRequestToken"])
	signature := eksCreateClusterSignature(body)
	if token != "" {
		if replay, ok := a.createClusterTokens[token]; ok {
			if replay.Signature != signature {
				writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "client token does not match previous request")
				return
			}
			writeEKSJSON(w, http.StatusOK, map[string]any{"cluster": eksClusterJSON(a.clusters[replay.Name])})
			return
		}
	}
	if _, exists := a.clusters[name]; exists {
		writeEKSError(w, http.StatusConflict, "ResourceInUseException", "cluster already exists")
		return
	}
	if a.clusterQuota > 0 && len(a.clusters) >= a.clusterQuota {
		writeEKSError(w, http.StatusTooManyRequests, "ResourceLimitExceededException", "cluster quota exceeded")
		return
	}
	a.nextID++
	cluster := eksCluster{
		Name:      name,
		Arn:       "arn:aws:eks:us-east-1:000000000000:cluster/" + name,
		RoleArn:   stringValue(body["roleArn"]),
		Tags:      mapValue(body["tags"]),
		CreatedAt: time.Now().UTC(),
		UpdateID:  a.nextID,
	}
	a.clusters[name] = cluster
	if token != "" {
		a.createClusterTokens[token] = eksCreateClusterReplay{Name: name, Signature: signature}
	}
	writeEKSJSON(w, http.StatusOK, map[string]any{"cluster": eksClusterJSON(cluster)})
}

func (a *EKSAdapter) listClusters(w http.ResponseWriter, r *http.Request) {
	if !a.authorized(w, r, "ListClusters", "*") {
		return
	}
	names := make([]string, 0, len(a.clusters))
	for name := range a.clusters {
		names = append(names, name)
	}
	sort.Strings(names)

	page, nextToken, ok := eksStringPage(names, r)
	if !ok {
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid pagination")
		return
	}
	body := map[string]any{"clusters": page}
	if nextToken != "" {
		body["nextToken"] = nextToken
	}
	writeEKSJSON(w, http.StatusOK, body)
}

func (a *EKSAdapter) serveTags(w http.ResponseWriter, r *http.Request) {
	resourceARN, err := url.PathUnescape(strings.TrimPrefix(r.URL.EscapedPath(), "/tags/"))
	if err != nil {
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid resource ARN")
		return
	}
	target, ok := a.tagTargetByARN(resourceARN)
	if !ok {
		writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "resource not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !a.authorized(w, r, "ListTagsForResource", target.Arn) {
			return
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"tags": target.Tags})
	case http.MethodPost:
		if !a.authorized(w, r, "TagResource", target.Arn) {
			return
		}
		body, ok := decodeEKSBody(w, r)
		if !ok {
			return
		}
		if target.Tags == nil {
			target.Tags = map[string]string{}
		}
		mergeStringMap(target.Tags, mapValue(body["tags"]))
		target.Save(target.Tags)
		writeEKSJSON(w, http.StatusOK, map[string]string{})
	case http.MethodDelete:
		if !a.authorized(w, r, "UntagResource", target.Arn) {
			return
		}
		for _, tagKey := range r.URL.Query()["tagKeys"] {
			delete(target.Tags, tagKey)
		}
		target.Save(target.Tags)
		writeEKSJSON(w, http.StatusOK, map[string]string{})
	default:
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
	}
}

func (a *EKSAdapter) serveCluster(w http.ResponseWriter, r *http.Request, path string) {
	if strings.Contains(path, "/access-entries") {
		a.serveAccessEntries(w, r, path)
		return
	}
	if strings.Contains(path, "/addons") {
		a.serveAddons(w, r, path)
		return
	}
	if strings.Contains(path, "/node-groups") {
		a.serveNodegroups(w, r, path)
		return
	}

	name := strings.TrimSuffix(path, "/update-config")
	cluster, ok := a.clusters[name]
	if !ok {
		writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "cluster not found")
		return
	}

	switch {
	case r.Method == http.MethodGet && path == name:
		if !a.authorized(w, r, "DescribeCluster", "*") {
			return
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"cluster": eksClusterJSON(cluster)})
	case r.Method == http.MethodDelete && path == name:
		if !a.authorized(w, r, "DeleteCluster", "*") {
			return
		}
		delete(a.clusters, name)
		writeEKSJSON(w, http.StatusOK, map[string]any{"cluster": eksClusterJSON(cluster)})
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/update-config"):
		if !a.authorized(w, r, "UpdateClusterConfig", "*") {
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid JSON request body")
			return
		}
		cluster.DeletionProtection, _ = body["deletionProtection"].(bool)
		cluster.UpdateID++
		a.clusters[name] = cluster
		writeEKSJSON(w, http.StatusOK, map[string]any{"update": eksUpdateJSON(cluster)})
	default:
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
	}
}

func (a *EKSAdapter) serveAccessEntries(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "access-entries" {
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
		return
	}
	clusterName := parts[0]
	if _, ok := a.clusters[clusterName]; !ok {
		writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "cluster not found")
		return
	}

	switch {
	case r.Method == http.MethodPost && len(parts) == 2:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid JSON request body")
			return
		}
		principal := stringValue(body["principalArn"])
		if principal == "" {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "principalArn is required")
			return
		}
		entry := eksAccessEntry{
			Cluster:          clusterName,
			Arn:              eksAccessEntryARN(clusterName, principal),
			Principal:        principal,
			KubernetesGroups: eksStringList(body["kubernetesGroups"]),
			Tags:             mapValue(body["tags"]),
			Type:             stringValue(body["type"]),
			Username:         stringValue(body["username"]),
			Policies:         map[string]eksAssociatedAccessPolicy{},
			CreatedAt:        time.Now().UTC(),
		}
		if entry.Type == "" {
			entry.Type = "STANDARD"
		}
		if !a.authorized(w, r, "CreateAccessEntry", entry.Arn) {
			return
		}
		key := eksAccessEntryKey(clusterName, principal)
		token := stringValue(body["clientRequestToken"])
		signature := eksRequestSignature(body)
		if token != "" {
			if replay, ok := a.createAccessTokens[token]; ok {
				if replay.Signature != signature {
					writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "client token does not match previous request")
					return
				}
				writeEKSJSON(w, http.StatusOK, map[string]any{"accessEntry": eksAccessEntryJSON(a.accessEntries[replay.Key])})
				return
			}
		}
		if _, exists := a.accessEntries[key]; exists {
			writeEKSError(w, http.StatusConflict, "ResourceInUseException", "access entry already exists")
			return
		}
		a.accessEntries[key] = entry
		if token != "" {
			a.createAccessTokens[token] = eksCreateAccessReplay{Key: key, Signature: signature}
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"accessEntry": eksAccessEntryJSON(entry)})
	case r.Method == http.MethodGet && len(parts) == 2:
		if !a.authorized(w, r, "ListAccessEntries", "*") {
			return
		}
		principals := make([]string, 0, len(a.accessEntries))
		for _, entry := range a.accessEntries {
			if entry.Cluster == clusterName {
				principals = append(principals, entry.Principal)
			}
		}
		sort.Strings(principals)
		page, nextToken, ok := eksStringPage(principals, r)
		if !ok {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid pagination")
			return
		}
		body := map[string]any{"accessEntries": page}
		if nextToken != "" {
			body["nextToken"] = nextToken
		}
		writeEKSJSON(w, http.StatusOK, body)
	case r.Method == http.MethodPost && len(parts) >= 4 && strings.Contains(path, "/access-policies"):
		principal, tail, ok := eksAccessEntryPath(parts)
		if !ok || len(tail) != 1 || tail[0] != "access-policies" {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
			return
		}
		key := eksAccessEntryKey(clusterName, principal)
		entry, ok := a.accessEntries[key]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "access entry not found")
			return
		}
		if !a.authorized(w, r, "AssociateAccessPolicy", entry.Arn) {
			return
		}
		body, ok := decodeEKSBody(w, r)
		if !ok {
			return
		}
		policyARN := stringValue(body["policyArn"])
		if policyARN == "" {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "policyArn is required")
			return
		}
		scope, _ := body["accessScope"].(map[string]any)
		policy := eksAssociatedAccessPolicy{
			PolicyArn:    policyARN,
			ScopeType:    stringValue(scope["type"]),
			Namespaces:   eksStringList(scope["namespaces"]),
			AssociatedAt: time.Now().UTC(),
		}
		entry.Policies[policyARN] = policy
		a.accessEntries[key] = entry
		writeEKSJSON(w, http.StatusOK, map[string]any{
			"associatedAccessPolicy": eksAssociatedAccessPolicyJSON(policy),
			"clusterName":            clusterName,
			"principalArn":           entry.Principal,
		})
	case r.Method == http.MethodGet && len(parts) >= 4 && strings.Contains(path, "/access-policies"):
		principal, tail, ok := eksAccessEntryPath(parts)
		if !ok || len(tail) != 1 || tail[0] != "access-policies" {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
			return
		}
		entry, ok := a.accessEntries[eksAccessEntryKey(clusterName, principal)]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "access entry not found")
			return
		}
		if !a.authorized(w, r, "ListAssociatedAccessPolicies", entry.Arn) {
			return
		}
		policyARNs := make([]string, 0, len(entry.Policies))
		for policyARN := range entry.Policies {
			policyARNs = append(policyARNs, policyARN)
		}
		sort.Strings(policyARNs)
		page, nextToken, ok := eksStringPage(policyARNs, r)
		if !ok {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid pagination")
			return
		}
		policies := make([]map[string]any, 0, len(page))
		for _, policyARN := range page {
			policies = append(policies, eksAssociatedAccessPolicyJSON(entry.Policies[policyARN]))
		}
		body := map[string]any{
			"associatedAccessPolicies": policies,
			"clusterName":              clusterName,
			"principalArn":             entry.Principal,
		}
		if nextToken != "" {
			body["nextToken"] = nextToken
		}
		writeEKSJSON(w, http.StatusOK, body)
	case r.Method == http.MethodDelete && len(parts) >= 5 && strings.Contains(path, "/access-policies"):
		principal, tail, ok := eksAccessEntryPath(parts)
		if !ok || len(tail) < 2 || tail[0] != "access-policies" {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
			return
		}
		key := eksAccessEntryKey(clusterName, principal)
		entry, ok := a.accessEntries[key]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "access entry not found")
			return
		}
		if !a.authorized(w, r, "DisassociateAccessPolicy", entry.Arn) {
			return
		}
		policyARN, err := url.PathUnescape(strings.Join(tail[1:], "/"))
		if err != nil {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid policyArn")
			return
		}
		delete(entry.Policies, policyARN)
		a.accessEntries[key] = entry
		writeEKSJSON(w, http.StatusOK, map[string]string{})
	case r.Method == http.MethodGet && len(parts) >= 3:
		principal, tail, ok := eksAccessEntryPath(parts)
		if !ok || len(tail) != 0 {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
			return
		}
		entry, ok := a.accessEntries[eksAccessEntryKey(clusterName, principal)]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "access entry not found")
			return
		}
		if !a.authorized(w, r, "DescribeAccessEntry", entry.Arn) {
			return
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"accessEntry": eksAccessEntryJSON(entry)})
	case r.Method == http.MethodPost && len(parts) >= 3:
		principal, tail, ok := eksAccessEntryPath(parts)
		if !ok || len(tail) != 0 {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
			return
		}
		key := eksAccessEntryKey(clusterName, principal)
		entry, ok := a.accessEntries[key]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "access entry not found")
			return
		}
		if !a.authorized(w, r, "UpdateAccessEntry", entry.Arn) {
			return
		}
		body, ok := decodeEKSBody(w, r)
		if !ok {
			return
		}
		token := stringValue(body["clientRequestToken"])
		signature := eksRequestSignature(body)
		if token != "" {
			if replay, ok := a.updateAccessTokens[token]; ok {
				if replay.Signature != signature {
					writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "client token does not match previous request")
					return
				}
				writeEKSJSON(w, http.StatusOK, map[string]any{"accessEntry": eksAccessEntryJSON(a.accessEntries[replay.Key])})
				return
			}
		}
		if groups, ok := body["kubernetesGroups"]; ok {
			entry.KubernetesGroups = eksStringList(groups)
		}
		if username := stringValue(body["username"]); username != "" {
			entry.Username = username
		}
		a.accessEntries[key] = entry
		if token != "" {
			a.updateAccessTokens[token] = eksUpdateAccessReplay{Key: key, Signature: signature}
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"accessEntry": eksAccessEntryJSON(entry)})
	case r.Method == http.MethodDelete && len(parts) >= 3:
		principal, tail, ok := eksAccessEntryPath(parts)
		if !ok || len(tail) != 0 {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
			return
		}
		key := eksAccessEntryKey(clusterName, principal)
		entry, ok := a.accessEntries[key]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "access entry not found")
			return
		}
		if !a.authorized(w, r, "DeleteAccessEntry", entry.Arn) {
			return
		}
		delete(a.accessEntries, key)
		writeEKSJSON(w, http.StatusOK, map[string]string{})
	default:
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
	}
}

func eksAccessEntryPath(parts []string) (string, []string, bool) {
	marker := len(parts)
	for index := 2; index < len(parts); index++ {
		if parts[index] == "access-policies" {
			marker = index
			break
		}
	}
	if marker == 2 {
		return "", nil, false
	}
	principal, err := url.PathUnescape(strings.Join(parts[2:marker], "/"))
	if err != nil || principal == "" {
		return "", nil, false
	}
	return principal, parts[marker:], true
}

func (a *EKSAdapter) serveAddons(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "addons" {
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
		return
	}
	clusterName := parts[0]
	if _, ok := a.clusters[clusterName]; !ok {
		writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "cluster not found")
		return
	}

	switch {
	case r.Method == http.MethodPost && len(parts) == 2:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid JSON request body")
			return
		}
		addonName := stringValue(body["addonName"])
		if addonName == "" {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "addonName is required")
			return
		}
		addon := eksAddon{
			Cluster:                 clusterName,
			Name:                    addonName,
			Arn:                     eksAddonARN(clusterName, addonName),
			Version:                 stringValue(body["addonVersion"]),
			ConfigurationValues:     stringValue(body["configurationValues"]),
			Namespace:               eksAddonNamespace(body["namespaceConfig"]),
			PodIdentityAssociations: eksAddonPodIdentityAssociations(body["podIdentityAssociations"]),
			ServiceAccountRoleArn:   stringValue(body["serviceAccountRoleArn"]),
			Tags:                    mapValue(body["tags"]),
			CreatedAt:               time.Now().UTC(),
		}
		if !a.authorized(w, r, "CreateAddon", addon.Arn) {
			return
		}
		key := eksAddonKey(clusterName, addonName)
		token := stringValue(body["clientRequestToken"])
		signature := eksRequestSignature(body)
		if token != "" {
			if replay, ok := a.createAddonTokens[token]; ok {
				if replay.Signature != signature {
					writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "client token does not match previous request")
					return
				}
				writeEKSJSON(w, http.StatusOK, map[string]any{"addon": eksAddonJSON(a.addons[replay.Key])})
				return
			}
		}
		if _, exists := a.addons[key]; exists {
			writeEKSError(w, http.StatusConflict, "ResourceInUseException", "addon already exists")
			return
		}
		a.addons[key] = addon
		if token != "" {
			a.createAddonTokens[token] = eksCreateAddonReplay{Key: key, Signature: signature}
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"addon": eksAddonJSON(addon)})
	case r.Method == http.MethodGet && len(parts) == 2:
		if !a.authorized(w, r, "ListAddons", "*") {
			return
		}
		names := make([]string, 0, len(a.addons))
		for _, addon := range a.addons {
			if addon.Cluster == clusterName {
				names = append(names, addon.Name)
			}
		}
		sort.Strings(names)
		page, nextToken, ok := eksStringPage(names, r)
		if !ok {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid pagination")
			return
		}
		body := map[string]any{"addons": page}
		if nextToken != "" {
			body["nextToken"] = nextToken
		}
		writeEKSJSON(w, http.StatusOK, body)
	case r.Method == http.MethodGet && len(parts) == 3:
		addon, ok := a.addons[eksAddonKey(clusterName, parts[2])]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "addon not found")
			return
		}
		if !a.authorized(w, r, "DescribeAddon", addon.Arn) {
			return
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"addon": eksAddonJSON(addon)})
	case r.Method == http.MethodPost && len(parts) == 4 && parts[3] == "update":
		key := eksAddonKey(clusterName, parts[2])
		addon, ok := a.addons[key]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "addon not found")
			return
		}
		if !a.authorized(w, r, "UpdateAddon", addon.Arn) {
			return
		}
		body, ok := decodeEKSBody(w, r)
		if !ok {
			return
		}
		token := stringValue(body["clientRequestToken"])
		signature := eksRequestSignature(body)
		if token != "" {
			if replay, ok := a.updateAddonTokens[token]; ok {
				if replay.Signature != signature {
					writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "client token does not match previous request")
					return
				}
				writeEKSJSON(w, http.StatusOK, map[string]any{"update": eksAddonUpdateJSON(a.addons[replay.Key])})
				return
			}
		}
		if version := stringValue(body["addonVersion"]); version != "" {
			addon.Version = version
		}
		if config := stringValue(body["configurationValues"]); config != "" {
			addon.ConfigurationValues = config
		}
		if role := stringValue(body["serviceAccountRoleArn"]); role != "" {
			addon.ServiceAccountRoleArn = role
		}
		if raw, ok := body["podIdentityAssociations"]; ok {
			addon.PodIdentityAssociations = eksAddonPodIdentityAssociations(raw)
		}
		addon.UpdateID++
		a.addons[key] = addon
		if token != "" {
			a.updateAddonTokens[token] = eksUpdateAddonReplay{Key: key, Signature: signature}
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"update": eksAddonUpdateJSON(addon)})
	case r.Method == http.MethodDelete && len(parts) == 3:
		key := eksAddonKey(clusterName, parts[2])
		addon, ok := a.addons[key]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "addon not found")
			return
		}
		if !a.authorized(w, r, "DeleteAddon", addon.Arn) {
			return
		}
		delete(a.addons, key)
		writeEKSJSON(w, http.StatusOK, map[string]any{"addon": eksAddonJSON(addon)})
	default:
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
	}
}

func (a *EKSAdapter) serveNodegroups(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "node-groups" {
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
		return
	}
	clusterName := parts[0]
	if _, ok := a.clusters[clusterName]; !ok {
		writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "cluster not found")
		return
	}

	switch {
	case r.Method == http.MethodPost && len(parts) == 2:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid JSON request body")
			return
		}
		nodegroupName := stringValue(body["nodegroupName"])
		if nodegroupName == "" {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "nodegroupName is required")
			return
		}
		nodeRole := stringValue(body["nodeRole"])
		if nodeRole == "" {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "nodeRole is required")
			return
		}
		subnets := eksStringList(body["subnets"])
		if len(subnets) == 0 {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "subnets are required")
			return
		}
		nodegroup := eksNodegroup{
			Cluster:       clusterName,
			Name:          nodegroupName,
			Arn:           eksNodegroupARN(clusterName, nodegroupName),
			NodeRole:      nodeRole,
			Subnets:       subnets,
			ScalingConfig: body["scalingConfig"],
			Labels:        mapValue(body["labels"]),
			Tags:          mapValue(body["tags"]),
			Version:       "1.30",
			CreatedAt:     time.Now().UTC(),
			UpdateID:      a.nextID,
		}
		if !a.authorized(w, r, "CreateNodegroup", nodegroup.Arn) {
			return
		}
		key := eksNodegroupKey(clusterName, nodegroupName)
		token := stringValue(body["clientRequestToken"])
		signature := eksRequestSignature(body)
		if token != "" {
			if replay, ok := a.createNodeTokens[token]; ok {
				if replay.Signature != signature {
					writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "client token does not match previous request")
					return
				}
				writeEKSJSON(w, http.StatusOK, map[string]any{"nodegroup": eksNodegroupJSON(a.nodegroups[replay.Key])})
				return
			}
		}
		if _, exists := a.nodegroups[key]; exists {
			writeEKSError(w, http.StatusConflict, "ResourceInUseException", "nodegroup already exists")
			return
		}
		a.nodegroups[key] = nodegroup
		if token != "" {
			a.createNodeTokens[token] = eksCreateNodeReplay{Key: key, Signature: signature}
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"nodegroup": eksNodegroupJSON(nodegroup)})
	case r.Method == http.MethodGet && len(parts) == 2:
		if !a.authorized(w, r, "ListNodegroups", "*") {
			return
		}
		names := make([]string, 0, len(a.nodegroups))
		for _, nodegroup := range a.nodegroups {
			if nodegroup.Cluster == clusterName {
				names = append(names, nodegroup.Name)
			}
		}
		sort.Strings(names)
		page, nextToken, ok := eksStringPage(names, r)
		if !ok {
			writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid pagination")
			return
		}
		body := map[string]any{"nodegroups": page}
		if nextToken != "" {
			body["nextToken"] = nextToken
		}
		writeEKSJSON(w, http.StatusOK, body)
	case r.Method == http.MethodPost && len(parts) == 4 && parts[3] == "update-config":
		key := eksNodegroupKey(clusterName, parts[2])
		nodegroup, ok := a.nodegroups[key]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "nodegroup not found")
			return
		}
		if !a.authorized(w, r, "UpdateNodegroupConfig", nodegroup.Arn) {
			return
		}
		body, ok := decodeEKSBody(w, r)
		if !ok {
			return
		}
		token := stringValue(body["clientRequestToken"])
		signature := eksRequestSignature(body)
		if token != "" {
			if replay, ok := a.updateNodeTokens[token]; ok {
				if replay.Signature != signature {
					writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "client token does not match previous request")
					return
				}
				writeEKSJSON(w, http.StatusOK, map[string]any{"update": eksNodegroupUpdateJSON(a.nodegroups[replay.Key])})
				return
			}
		}
		if scalingConfig, ok := body["scalingConfig"]; ok {
			nodegroup.ScalingConfig = scalingConfig
		}
		eksApplyLabelUpdate(nodegroup.Labels, body["labels"])
		nodegroup.UpdateID++
		a.nodegroups[key] = nodegroup
		if token != "" {
			a.updateNodeTokens[token] = eksUpdateNodeReplay{Key: key, Signature: signature}
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"update": eksNodegroupUpdateJSON(nodegroup)})
	case r.Method == http.MethodPost && len(parts) == 4 && parts[3] == "update-version":
		key := eksNodegroupKey(clusterName, parts[2])
		nodegroup, ok := a.nodegroups[key]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "nodegroup not found")
			return
		}
		if !a.authorized(w, r, "UpdateNodegroupVersion", nodegroup.Arn) {
			return
		}
		body, ok := decodeEKSBody(w, r)
		if !ok {
			return
		}
		token := stringValue(body["clientRequestToken"])
		signature := eksRequestSignature(body)
		if token != "" {
			if replay, ok := a.updateNodeVersionTokens[token]; ok {
				if replay.Signature != signature {
					writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "client token does not match previous request")
					return
				}
				writeEKSJSON(w, http.StatusOK, map[string]any{"update": eksNodegroupUpdateJSON(a.nodegroups[replay.Key])})
				return
			}
		}
		if version := stringValue(body["version"]); version != "" {
			nodegroup.Version = version
		}
		if release := stringValue(body["releaseVersion"]); release != "" {
			nodegroup.Release = release
		}
		nodegroup.UpdateID++
		a.nodegroups[key] = nodegroup
		if token != "" {
			a.updateNodeVersionTokens[token] = eksUpdateNodeReplay{Key: key, Signature: signature}
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"update": eksNodegroupUpdateJSON(nodegroup)})
	case r.Method == http.MethodGet && len(parts) == 3:
		nodegroup, ok := a.nodegroups[eksNodegroupKey(clusterName, parts[2])]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "nodegroup not found")
			return
		}
		if !a.authorized(w, r, "DescribeNodegroup", nodegroup.Arn) {
			return
		}
		writeEKSJSON(w, http.StatusOK, map[string]any{"nodegroup": eksNodegroupJSON(nodegroup)})
	case r.Method == http.MethodDelete && len(parts) == 3:
		key := eksNodegroupKey(clusterName, parts[2])
		nodegroup, ok := a.nodegroups[key]
		if !ok {
			writeEKSError(w, http.StatusNotFound, "ResourceNotFoundException", "nodegroup not found")
			return
		}
		if !a.authorized(w, r, "DeleteNodegroup", nodegroup.Arn) {
			return
		}
		delete(a.nodegroups, key)
		writeEKSJSON(w, http.StatusOK, map[string]any{"nodegroup": eksNodegroupJSON(nodegroup)})
	default:
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "unsupported EKS action")
	}
}

func (a *EKSAdapter) clusterByARN(arn string) (string, eksCluster, bool) {
	for name, cluster := range a.clusters {
		if cluster.Arn == arn {
			return name, cluster, true
		}
	}
	return "", eksCluster{}, false
}

func (a *EKSAdapter) tagTargetByARN(arn string) (eksTagTarget, bool) {
	for name, cluster := range a.clusters {
		if cluster.Arn == arn {
			return eksTagTarget{
				Arn:  cluster.Arn,
				Tags: cluster.Tags,
				Save: func(tags map[string]string) {
					cluster.Tags = tags
					a.clusters[name] = cluster
				},
			}, true
		}
	}
	for key, nodegroup := range a.nodegroups {
		if nodegroup.Arn == arn {
			return eksTagTarget{
				Arn:  nodegroup.Arn,
				Tags: nodegroup.Tags,
				Save: func(tags map[string]string) {
					nodegroup.Tags = tags
					a.nodegroups[key] = nodegroup
				},
			}, true
		}
	}
	for key, addon := range a.addons {
		if addon.Arn == arn {
			return eksTagTarget{
				Arn:  addon.Arn,
				Tags: addon.Tags,
				Save: func(tags map[string]string) {
					addon.Tags = tags
					a.addons[key] = addon
				},
			}, true
		}
	}
	for key, entry := range a.accessEntries {
		if entry.Arn == arn {
			return eksTagTarget{
				Arn:  entry.Arn,
				Tags: entry.Tags,
				Save: func(tags map[string]string) {
					entry.Tags = tags
					a.accessEntries[key] = entry
				},
			}, true
		}
	}
	return eksTagTarget{}, false
}

func eksNodegroupKey(cluster, name string) string {
	return cluster + "/" + name
}

func eksAddonKey(cluster, name string) string {
	return cluster + "/" + name
}

func eksAccessEntryKey(cluster, principal string) string {
	return cluster + "/" + principal
}

func eksNodegroupARN(cluster, name string) string {
	return "arn:aws:eks:us-east-1:000000000000:nodegroup/" + cluster + "/" + name + "/homeport"
}

func eksAddonARN(cluster, name string) string {
	return "arn:aws:eks:us-east-1:000000000000:addon/" + cluster + "/" + name + "/homeport"
}

func eksAccessEntryARN(cluster, principal string) string {
	return "arn:aws:eks:us-east-1:000000000000:access-entry/" + cluster + "/" + principal
}

func eksCreateClusterSignature(body map[string]any) string {
	return eksRequestSignature(body)
}

func eksRequestSignature(body map[string]any) string {
	clone := make(map[string]any, len(body))
	for key, value := range body {
		if key != "clientRequestToken" {
			clone[key] = value
		}
	}
	data, _ := json.Marshal(clone)
	return string(data)
}

func eksStringList(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, stringValue(item))
	}
	return out
}

func eksStringPage(items []string, r *http.Request) ([]string, string, bool) {
	start := 0
	if token := r.URL.Query().Get("nextToken"); token != "" {
		offset, err := strconv.Atoi(token)
		if err != nil || offset < 0 || offset > len(items) {
			return nil, "", false
		}
		start = offset
	}
	end := len(items)
	if rawMax := r.URL.Query().Get("maxResults"); rawMax != "" {
		maxResults, err := strconv.Atoi(rawMax)
		if err != nil || maxResults <= 0 {
			return nil, "", false
		}
		if start+maxResults < end {
			end = start + maxResults
		}
	}
	if end < len(items) {
		return items[start:end], strconv.Itoa(end), true
	}
	return items[start:end], "", true
}

func eksApplyLabelUpdate(labels map[string]string, raw any) {
	update, _ := raw.(map[string]any)
	for key, value := range mapValue(update["addOrUpdateLabels"]) {
		labels[key] = value
	}
	for _, key := range eksStringList(update["removeLabels"]) {
		delete(labels, key)
	}
}

func eksClusterJSON(cluster eksCluster) map[string]any {
	return map[string]any{
		"arn":                cluster.Arn,
		"createdAt":          cluster.CreatedAt.Unix(),
		"deletionProtection": cluster.DeletionProtection,
		"endpoint":           "https://" + cluster.Name + ".eks.homeport.local",
		"name":               cluster.Name,
		"roleArn":            cluster.RoleArn,
		"status":             "ACTIVE",
		"tags":               cluster.Tags,
		"version":            "1.30",
	}
}

func eksNodegroupJSON(nodegroup eksNodegroup) map[string]any {
	return map[string]any{
		"clusterName":    nodegroup.Cluster,
		"createdAt":      nodegroup.CreatedAt.Unix(),
		"modifiedAt":     nodegroup.CreatedAt.Unix(),
		"nodegroupArn":   nodegroup.Arn,
		"nodegroupName":  nodegroup.Name,
		"nodeRole":       nodegroup.NodeRole,
		"labels":         nodegroup.Labels,
		"scalingConfig":  nodegroup.ScalingConfig,
		"status":         "ACTIVE",
		"subnets":        nodegroup.Subnets,
		"tags":           nodegroup.Tags,
		"version":        nodegroup.Version,
		"releaseVersion": nodegroup.Release,
	}
}

func eksAddonJSON(addon eksAddon) map[string]any {
	body := map[string]any{
		"addonArn":              addon.Arn,
		"addonName":             addon.Name,
		"addonVersion":          addon.Version,
		"clusterName":           addon.Cluster,
		"configurationValues":   addon.ConfigurationValues,
		"createdAt":             addon.CreatedAt.Unix(),
		"modifiedAt":            addon.CreatedAt.Unix(),
		"serviceAccountRoleArn": addon.ServiceAccountRoleArn,
		"status":                "ACTIVE",
		"tags":                  addon.Tags,
	}
	if addon.Namespace != "" {
		body["namespaceConfig"] = map[string]any{"namespace": addon.Namespace}
	}
	if addon.PodIdentityAssociations != nil {
		body["podIdentityAssociations"] = addon.PodIdentityAssociations
	}
	return body
}

func eksAddonNamespace(raw any) string {
	config, _ := raw.(map[string]any)
	return stringValue(config["namespace"])
}

func eksAddonPodIdentityAssociations(raw any) []string {
	items, _ := raw.([]any)
	if items == nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		association, _ := item.(map[string]any)
		serviceAccount := stringValue(association["serviceAccount"])
		roleARN := stringValue(association["roleArn"])
		if serviceAccount != "" || roleARN != "" {
			out = append(out, serviceAccount+"="+roleARN)
		}
	}
	return out
}

func eksAddonUpdateJSON(addon eksAddon) map[string]any {
	return map[string]any{
		"id":     "homeport-addon-update-" + strconv.Itoa(addon.UpdateID),
		"status": "Successful",
		"type":   "AddonUpdate",
		"params": []map[string]string{{"type": "AddonVersion", "value": addon.Version}},
	}
}

func eksAccessEntryJSON(entry eksAccessEntry) map[string]any {
	return map[string]any{
		"accessEntryArn":   entry.Arn,
		"clusterName":      entry.Cluster,
		"createdAt":        entry.CreatedAt.Unix(),
		"kubernetesGroups": entry.KubernetesGroups,
		"modifiedAt":       entry.CreatedAt.Unix(),
		"principalArn":     entry.Principal,
		"tags":             entry.Tags,
		"type":             entry.Type,
		"username":         entry.Username,
	}
}

func eksAssociatedAccessPolicyJSON(policy eksAssociatedAccessPolicy) map[string]any {
	return map[string]any{
		"accessScope": map[string]any{
			"namespaces": policy.Namespaces,
			"type":       policy.ScopeType,
		},
		"associatedAt": policy.AssociatedAt.Unix(),
		"modifiedAt":   policy.AssociatedAt.Unix(),
		"policyArn":    policy.PolicyArn,
	}
}

func eksNodegroupUpdateJSON(nodegroup eksNodegroup) map[string]any {
	return map[string]any{
		"id":     "homeport-nodegroup-update-" + strconv.Itoa(nodegroup.UpdateID),
		"status": "Successful",
		"type":   "ConfigUpdate",
	}
}

func eksUpdateJSON(cluster eksCluster) map[string]any {
	return map[string]any{
		"id":     "homeport-update-" + strconv.Itoa(cluster.UpdateID),
		"status": "Successful",
		"type":   "ConfigUpdate",
	}
}

func writeEKSJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeEKSError(w http.ResponseWriter, status int, code, message string) {
	writeEKSJSON(w, status, map[string]string{"__type": code, "message": message})
}

func decodeEKSBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeEKSError(w, http.StatusBadRequest, "InvalidParameterException", "invalid JSON request body")
		return nil, false
	}
	return body, true
}

func (a *EKSAdapter) authorized(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "eks:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "eks",
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
		writeEKSError(w, http.StatusInternalServerError, "ServerException", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeEKSError(w, http.StatusForbidden, "AccessDenied", decision.Reason)
		return false
	}
	return true
}
