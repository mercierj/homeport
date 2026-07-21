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

type APIGatewayAdapter struct {
	mu           sync.Mutex
	restAPIs     map[string]apiGatewayRestAPI
	domainNames  map[string]apiGatewayDomainName
	authorizer   authz.Authorizer
	auditSink    func(authz.Decision)
	restAPIQuota int
	nextID       int
}

type APIGatewayOption func(*APIGatewayAdapter)

type apiGatewayDomainName struct {
	DomainName             string
	DomainNameARN          string
	DomainNameID           string
	RegionalCertificateARN string
	RegionalDomainName     string
	SecurityPolicy         string
	EndpointTypes          []string
	Tags                   map[string]string
}

type apiGatewayRestAPI struct {
	ID             string
	Name           string
	Description    string
	CreatedAt      time.Time
	Tags           map[string]string
	RootResourceID string
	Resources      map[string]apiGatewayResource
	Deployments    map[string]apiGatewayDeployment
	Stages         map[string]apiGatewayStage
}

type apiGatewayStage struct {
	Name         string
	DeploymentID string
	Description  string
	CreatedAt    time.Time
	Variables    map[string]string
}

type apiGatewayDeployment struct {
	ID               string
	Description      string
	CreatedAt        time.Time
	StageName        string
	StageDescription string
	Variables        map[string]string
}

type apiGatewayResource struct {
	ID       string
	ParentID string
	PathPart string
	Path     string
	Methods  map[string]apiGatewayMethod
}

type apiGatewayMethod struct {
	HTTPMethod        string
	AuthorizationType string
	APIKeyRequired    bool
	Integration       *apiGatewayIntegration
	Responses         map[string]apiGatewayMethodResponse
}

type apiGatewayMethodResponse struct {
	StatusCode         string
	ResponseModels     map[string]string
	ResponseParameters map[string]bool
}

type apiGatewayIntegration struct {
	Type                string
	HTTPMethod          string
	URI                 string
	PassthroughBehavior string
	Responses           map[string]apiGatewayIntegrationResponse
}

type apiGatewayIntegrationResponse struct {
	StatusCode         string
	SelectionPattern   string
	ResponseParameters map[string]string
	ResponseTemplates  map[string]string
}

func NewAPIGatewayAdapter(options ...APIGatewayOption) *APIGatewayAdapter {
	adapter := &APIGatewayAdapter{
		restAPIs:    map[string]apiGatewayRestAPI{},
		domainNames: map[string]apiGatewayDomainName{},
		authorizer:  authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithAPIGatewayAuthorizer(authorizer authz.Authorizer) APIGatewayOption {
	return func(adapter *APIGatewayAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithAPIGatewayAuditSink(sink func(authz.Decision)) APIGatewayOption {
	return func(adapter *APIGatewayAdapter) {
		adapter.auditSink = sink
	}
}

func WithAPIGatewayRestAPIQuota(maxAPIs int) APIGatewayOption {
	return func(adapter *APIGatewayAdapter) {
		adapter.restAPIQuota = maxAPIs
	}
}

func (APIGatewayAdapter) Provider() string { return "aws" }
func (APIGatewayAdapter) Service() string  { return "apigateway" }
func (APIGatewayAdapter) Routes() []string { return []string{"ANY /compat/aws/apigateway"} }
func (APIGatewayAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_APIGATEWAY": "http://homeport:8080/api/v1/compat/aws/apigateway",
		"HOMEPORT_COMPAT_BACKEND":     "kong",
	}
}
func (APIGatewayAdapter) ConformanceChecks() []string {
	return []string{"create-rest-api", "get-rest-api", "get-rest-apis", "update-rest-api", "delete-rest-api"}
}

func (a *APIGatewayAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/restapis":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "invalid JSON request body")
			return
		}
		id := "homeport-api-" + strconv.Itoa(a.nextID+1)
		if !a.authorized(w, r, "CreateRestApi", "arn:aws:apigateway:us-east-1::/restapis/"+id) {
			return
		}
		name := stringValue(body["name"])
		if name == "" {
			writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "name is required")
			return
		}
		if a.restAPIQuota > 0 && len(a.restAPIs) >= a.restAPIQuota {
			writeAPIGatewayError(w, http.StatusTooManyRequests, "LimitExceededException", "rest api quota exceeded")
			return
		}
		a.nextID++
		rootID := id + "-root"
		api := apiGatewayRestAPI{
			ID:             id,
			Name:           name,
			Description:    stringValue(body["description"]),
			CreatedAt:      time.Now().UTC(),
			Tags:           mapValue(body["tags"]),
			RootResourceID: rootID,
			Resources: map[string]apiGatewayResource{
				rootID: {ID: rootID, Path: "/", Methods: map[string]apiGatewayMethod{}},
			},
			Deployments: map[string]apiGatewayDeployment{},
			Stages:      map[string]apiGatewayStage{},
		}
		a.restAPIs[api.ID] = api
		writeAPIGatewayJSON(w, http.StatusCreated, apiGatewayRestAPIJSON(api))
	case r.Method == http.MethodGet && r.URL.Path == "/restapis":
		if !a.authorized(w, r, "GetRestApis", "*") {
			return
		}
		ids := make([]string, 0, len(a.restAPIs))
		for id := range a.restAPIs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		start := 0
		if position := r.URL.Query().Get("position"); position != "" {
			parsed, err := strconv.Atoi(position)
			if err != nil || parsed < 0 || parsed > len(ids) {
				writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "invalid position")
				return
			}
			start = parsed
		}
		end := len(ids)
		if limitText := r.URL.Query().Get("limit"); limitText != "" {
			limit, err := strconv.Atoi(limitText)
			if err != nil || limit < 0 {
				writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "invalid limit")
				return
			}
			if limit > 0 && start+limit < end {
				end = start + limit
			}
		}
		items := make([]map[string]any, 0, end-start)
		for _, id := range ids[start:end] {
			items = append(items, apiGatewayRestAPIJSON(a.restAPIs[id]))
		}
		response := map[string]any{"item": items}
		if end < len(ids) {
			response["position"] = strconv.Itoa(end)
		}
		writeAPIGatewayJSON(w, http.StatusOK, response)
	case r.URL.Path == "/domainnames" || strings.HasPrefix(r.URL.Path, "/domainnames/"):
		a.serveDomainNames(w, r, strings.TrimPrefix(r.URL.Path, "/domainnames"))
	case strings.HasPrefix(r.URL.Path, "/tags/"):
		a.serveTags(w, r, strings.TrimPrefix(r.URL.Path, "/tags/"))
	case strings.HasPrefix(r.URL.Path, "/restapis/"):
		a.serveRestAPI(w, r, strings.TrimPrefix(r.URL.Path, "/restapis/"))
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func (a *APIGatewayAdapter) serveDomainNames(w http.ResponseWriter, r *http.Request, tail string) {
	if tail == "" {
		switch r.Method {
		case http.MethodPost:
			if !a.authorized(w, r, "CreateDomainName", "*") {
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			name := stringValue(body["domainName"])
			if name == "" {
				writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "domainName is required")
				return
			}
			if _, exists := a.domainNames[name]; exists {
				writeAPIGatewayError(w, http.StatusConflict, "ConflictException", "domain name already exists")
				return
			}
			a.nextID++
			domain := apiGatewayDomainName{
				DomainName:             name,
				DomainNameARN:          "arn:aws:apigateway:us-east-1::/domainnames/" + name,
				DomainNameID:           "homeport-domain-" + strconv.Itoa(a.nextID),
				RegionalCertificateARN: stringValue(body["regionalCertificateArn"]),
				RegionalDomainName:     name + ".execute-api.us-east-1.amazonaws.com",
				SecurityPolicy:         stringValue(body["securityPolicy"]),
				EndpointTypes:          endpointTypes(body["endpointConfiguration"]),
				Tags:                   mapValue(body["tags"]),
			}
			a.domainNames[name] = domain
			writeAPIGatewayJSON(w, http.StatusCreated, apiGatewayDomainNameJSON(domain))
		case http.MethodGet:
			if !a.authorized(w, r, "GetDomainNames", "*") {
				return
			}
			names := make([]string, 0, len(a.domainNames))
			for name := range a.domainNames {
				names = append(names, name)
			}
			sort.Strings(names)

			start := 0
			if position := r.URL.Query().Get("position"); position != "" {
				parsed, err := strconv.Atoi(position)
				if err != nil || parsed < 0 || parsed > len(names) {
					writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "invalid position")
					return
				}
				start = parsed
			}
			end := len(names)
			if limitText := r.URL.Query().Get("limit"); limitText != "" {
				limit, err := strconv.Atoi(limitText)
				if err != nil || limit < 0 {
					writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "invalid limit")
					return
				}
				if limit > 0 && start+limit < end {
					end = start + limit
				}
			}

			items := make([]map[string]any, 0, end-start)
			for _, name := range names[start:end] {
				items = append(items, apiGatewayDomainNameJSON(a.domainNames[name]))
			}
			response := map[string]any{"item": items}
			if end < len(names) {
				response["position"] = strconv.Itoa(end)
			}
			writeAPIGatewayJSON(w, http.StatusOK, response)
		default:
			writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
		}
		return
	}

	name := strings.TrimPrefix(tail, "/")
	domain, ok := a.domainNames[name]
	if !ok {
		writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "domain name not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !a.authorized(w, r, "GetDomainName", "*") {
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayDomainNameJSON(domain))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteDomainName", "*") {
			return
		}
		delete(a.domainNames, name)
		w.WriteHeader(http.StatusAccepted)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func apiGatewayDomainNameJSON(domain apiGatewayDomainName) map[string]any {
	return map[string]any{
		"domainName":             domain.DomainName,
		"domainNameArn":          domain.DomainNameARN,
		"domainNameId":           domain.DomainNameID,
		"domainNameStatus":       "AVAILABLE",
		"regionalCertificateArn": domain.RegionalCertificateARN,
		"regionalDomainName":     domain.RegionalDomainName,
		"regionalHostedZoneId":   "ZHOMEPORT",
		"securityPolicy":         domain.SecurityPolicy,
		"endpointConfiguration":  map[string]any{"types": domain.EndpointTypes},
		"tags":                   domain.Tags,
	}
}

func (a *APIGatewayAdapter) serveRestAPI(w http.ResponseWriter, r *http.Request, tail string) {
	parts := strings.Split(tail, "/")
	id := parts[0]
	api, ok := a.restAPIs[id]
	if !ok {
		writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "rest api not found")
		return
	}
	if len(parts) >= 2 && parts[1] == "resources" {
		a.serveResources(w, r, api, parts[2:])
		return
	}
	if len(parts) >= 2 && parts[1] == "deployments" {
		a.serveDeployments(w, r, api, parts[2:])
		return
	}
	if len(parts) >= 2 && parts[1] == "stages" {
		a.serveStages(w, r, api, parts[2:])
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !a.authorized(w, r, "GetRestApi", "*") {
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayRestAPIJSON(api))
	case http.MethodPatch:
		if !a.authorized(w, r, "UpdateRestApi", "*") {
			return
		}
		var body struct {
			PatchOperations []struct {
				Op    string `json:"op"`
				Path  string `json:"path"`
				Value string `json:"value"`
			} `json:"patchOperations"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, operation := range body.PatchOperations {
			if operation.Op == "replace" && operation.Path == "/name" {
				api.Name = operation.Value
			}
			if operation.Op == "replace" && operation.Path == "/description" {
				api.Description = operation.Value
			}
		}
		a.restAPIs[id] = api
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayRestAPIJSON(api))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteRestApi", "*") {
			return
		}
		delete(a.restAPIs, id)
		w.WriteHeader(http.StatusAccepted)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func (a *APIGatewayAdapter) serveDeployments(w http.ResponseWriter, r *http.Request, api apiGatewayRestAPI, parts []string) {
	if api.Deployments == nil {
		api.Deployments = map[string]apiGatewayDeployment{}
	}
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodPost:
			if !a.authorized(w, r, "CreateDeployment", "*") {
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			id := api.ID + "-deployment-" + strconv.Itoa(len(api.Deployments)+1)
			deployment := apiGatewayDeployment{
				ID:               id,
				Description:      stringValue(body["description"]),
				CreatedAt:        time.Now().UTC(),
				StageName:        stringValue(body["stageName"]),
				StageDescription: stringValue(body["stageDescription"]),
				Variables:        mapValue(body["variables"]),
			}
			api.Deployments[id] = deployment
			a.restAPIs[api.ID] = api
			writeAPIGatewayJSON(w, http.StatusCreated, apiGatewayDeploymentJSON(deployment))
		case http.MethodGet:
			if !a.authorized(w, r, "GetDeployments", "*") {
				return
			}
			ids := make([]string, 0, len(api.Deployments))
			for id := range api.Deployments {
				ids = append(ids, id)
			}
			sort.Strings(ids)

			start := 0
			if position := r.URL.Query().Get("position"); position != "" {
				parsed, err := strconv.Atoi(position)
				if err != nil || parsed < 0 || parsed > len(ids) {
					writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "invalid position")
					return
				}
				start = parsed
			}
			end := len(ids)
			if limitText := r.URL.Query().Get("limit"); limitText != "" {
				limit, err := strconv.Atoi(limitText)
				if err != nil || limit < 0 {
					writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "invalid limit")
					return
				}
				if limit > 0 && start+limit < end {
					end = start + limit
				}
			}

			items := make([]map[string]any, 0, end-start)
			for _, id := range ids[start:end] {
				items = append(items, apiGatewayDeploymentJSON(api.Deployments[id]))
			}
			response := map[string]any{"item": items}
			if end < len(ids) {
				response["position"] = strconv.Itoa(end)
			}
			writeAPIGatewayJSON(w, http.StatusOK, response)
		default:
			writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
		}
		return
	}

	deployment, ok := api.Deployments[parts[0]]
	if !ok {
		writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "deployment not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !a.authorized(w, r, "GetDeployment", "*") {
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayDeploymentJSON(deployment))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteDeployment", "*") {
			return
		}
		delete(api.Deployments, parts[0])
		a.restAPIs[api.ID] = api
		w.WriteHeader(http.StatusAccepted)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func apiGatewayDeploymentJSON(deployment apiGatewayDeployment) map[string]any {
	return map[string]any{
		"id":          deployment.ID,
		"description": deployment.Description,
		"createdDate": deployment.CreatedAt.Unix(),
	}
}

func (a *APIGatewayAdapter) serveStages(w http.ResponseWriter, r *http.Request, api apiGatewayRestAPI, parts []string) {
	if api.Stages == nil {
		api.Stages = map[string]apiGatewayStage{}
	}
	if len(parts) == 0 {
		switch r.Method {
		case http.MethodPost:
			if !a.authorized(w, r, "CreateStage", "*") {
				return
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			stage := apiGatewayStage{
				Name:         stringValue(body["stageName"]),
				DeploymentID: stringValue(body["deploymentId"]),
				Description:  stringValue(body["description"]),
				CreatedAt:    time.Now().UTC(),
				Variables:    mapValue(body["variables"]),
			}
			if _, ok := api.Deployments[stage.DeploymentID]; !ok {
				writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "deployment not found")
				return
			}
			if _, exists := api.Stages[stage.Name]; exists {
				writeAPIGatewayError(w, http.StatusConflict, "ConflictException", "stage already exists")
				return
			}
			api.Stages[stage.Name] = stage
			a.restAPIs[api.ID] = api
			writeAPIGatewayJSON(w, http.StatusCreated, apiGatewayStageJSON(stage))
		case http.MethodGet:
			if !a.authorized(w, r, "GetStages", "*") {
				return
			}
			names := make([]string, 0, len(api.Stages))
			for name := range api.Stages {
				names = append(names, name)
			}
			sort.Strings(names)
			items := make([]map[string]any, 0, len(names))
			for _, name := range names {
				items = append(items, apiGatewayStageJSON(api.Stages[name]))
			}
			writeAPIGatewayJSON(w, http.StatusOK, map[string]any{"item": items})
		default:
			writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
		}
		return
	}

	stage, ok := api.Stages[parts[0]]
	if !ok {
		writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "stage not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !a.authorized(w, r, "GetStage", "*") {
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayStageJSON(stage))
	case http.MethodPatch:
		if !a.authorized(w, r, "UpdateStage", "*") {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, raw := range anySlice(body["patchOperations"]) {
			operation, _ := raw.(map[string]any)
			path := stringValue(operation["path"])
			value := stringValue(operation["value"])
			if path == "/description" {
				stage.Description = value
				continue
			}
			if path == "/deploymentId" {
				if _, ok := api.Deployments[value]; !ok {
					writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "deployment not found")
					return
				}
				stage.DeploymentID = value
				continue
			}
			if key, ok := strings.CutPrefix(path, "/variables/"); ok && key != "" {
				if stage.Variables == nil {
					stage.Variables = map[string]string{}
				}
				if stringValue(operation["op"]) == "remove" {
					delete(stage.Variables, key)
				} else {
					stage.Variables[key] = value
				}
				continue
			}
			writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported stage patch path")
			return
		}
		api.Stages[stage.Name] = stage
		a.restAPIs[api.ID] = api
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayStageJSON(stage))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteStage", "*") {
			return
		}
		delete(api.Stages, parts[0])
		a.restAPIs[api.ID] = api
		w.WriteHeader(http.StatusAccepted)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func apiGatewayStageJSON(stage apiGatewayStage) map[string]any {
	return map[string]any{
		"stageName":    stage.Name,
		"deploymentId": stage.DeploymentID,
		"description":  stage.Description,
		"createdDate":  stage.CreatedAt.Unix(),
		"variables":    stage.Variables,
	}
}

func apiGatewayRestAPIJSON(api apiGatewayRestAPI) map[string]any {
	response := map[string]any{
		"id":             api.ID,
		"name":           api.Name,
		"description":    api.Description,
		"createdDate":    api.CreatedAt.Unix(),
		"rootResourceId": api.RootResourceID,
	}
	if len(api.Tags) > 0 {
		response["tags"] = api.Tags
	}
	return response
}

func (a *APIGatewayAdapter) serveResources(w http.ResponseWriter, r *http.Request, api apiGatewayRestAPI, parts []string) {
	if len(parts) == 0 {
		if r.Method != http.MethodGet {
			writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
			return
		}
		if !a.authorized(w, r, "GetResources", "*") {
			return
		}
		resources := make([]apiGatewayResource, 0, len(api.Resources))
		for _, resource := range api.Resources {
			resources = append(resources, resource)
		}
		sort.Slice(resources, func(i, j int) bool { return resources[i].Path < resources[j].Path })

		start := 0
		if position := r.URL.Query().Get("position"); position != "" {
			parsed, err := strconv.Atoi(position)
			if err != nil || parsed < 0 || parsed > len(resources) {
				writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "invalid position")
				return
			}
			start = parsed
		}
		end := len(resources)
		if limitText := r.URL.Query().Get("limit"); limitText != "" {
			limit, err := strconv.Atoi(limitText)
			if err != nil || limit < 0 {
				writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "invalid limit")
				return
			}
			if limit > 0 && start+limit < end {
				end = start + limit
			}
		}

		items := make([]map[string]any, 0, end-start)
		for _, resource := range resources[start:end] {
			items = append(items, apiGatewayResourceJSON(resource))
		}
		response := map[string]any{"item": items}
		if end < len(resources) {
			response["position"] = strconv.Itoa(end)
		}
		writeAPIGatewayJSON(w, http.StatusOK, response)
		return
	}

	resourceID := parts[0]
	if r.Method == http.MethodPost {
		if !a.authorized(w, r, "CreateResource", "*") {
			return
		}
		parent, ok := api.Resources[resourceID]
		if !ok {
			writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "resource not found")
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		pathPart := stringValue(body["pathPart"])
		for _, existing := range api.Resources {
			if existing.ParentID == parent.ID && existing.PathPart == pathPart {
				writeAPIGatewayError(w, http.StatusConflict, "ConflictException", "resource already exists")
				return
			}
		}
		id := api.ID + "-resource-" + strconv.Itoa(len(api.Resources)+1)
		path := "/" + pathPart
		if parent.Path != "/" {
			path = parent.Path + "/" + pathPart
		}
		resource := apiGatewayResource{ID: id, ParentID: parent.ID, PathPart: pathPart, Path: path, Methods: map[string]apiGatewayMethod{}}
		api.Resources[id] = resource
		a.restAPIs[api.ID] = api
		writeAPIGatewayJSON(w, http.StatusCreated, apiGatewayResourceJSON(resource))
		return
	}

	resource, ok := api.Resources[resourceID]
	if !ok {
		writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "resource not found")
		return
	}
	if len(parts) >= 3 && parts[1] == "methods" {
		a.serveMethod(w, r, api, resource, parts[2:])
		return
	}
	switch r.Method {
	case http.MethodGet:
		if !a.authorized(w, r, "GetResource", "*") {
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayResourceJSON(resource))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteResource", "*") {
			return
		}
		delete(api.Resources, resourceID)
		a.restAPIs[api.ID] = api
		w.WriteHeader(http.StatusAccepted)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func apiGatewayResourceJSON(resource apiGatewayResource) map[string]any {
	response := map[string]any{
		"id":   resource.ID,
		"path": resource.Path,
	}
	if resource.ParentID != "" {
		response["parentId"] = resource.ParentID
	}
	if resource.PathPart != "" {
		response["pathPart"] = resource.PathPart
	}
	if len(resource.Methods) > 0 {
		methods := map[string]any{}
		for method, value := range resource.Methods {
			methods[method] = apiGatewayMethodJSON(value)
		}
		response["resourceMethods"] = methods
	}
	return response
}

func (a *APIGatewayAdapter) serveMethod(w http.ResponseWriter, r *http.Request, api apiGatewayRestAPI, resource apiGatewayResource, parts []string) {
	if resource.Methods == nil {
		resource.Methods = map[string]apiGatewayMethod{}
	}
	httpMethod := parts[0]
	if len(parts) >= 2 && parts[1] == "integration" {
		a.serveIntegration(w, r, api, resource, httpMethod, parts[2:])
		return
	}
	if len(parts) >= 3 && parts[1] == "responses" {
		a.serveMethodResponse(w, r, api, resource, httpMethod, parts[2])
		return
	}
	switch r.Method {
	case http.MethodPut:
		if !a.authorized(w, r, "PutMethod", "*") {
			return
		}
		if _, exists := resource.Methods[httpMethod]; exists {
			writeAPIGatewayError(w, http.StatusConflict, "ConflictException", "method already exists")
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := apiGatewayMethod{
			HTTPMethod:        httpMethod,
			AuthorizationType: stringValue(body["authorizationType"]),
			APIKeyRequired:    boolValue(body["apiKeyRequired"]),
			Responses:         map[string]apiGatewayMethodResponse{},
		}
		resource.Methods[httpMethod] = method
		api.Resources[resource.ID] = resource
		a.restAPIs[api.ID] = api
		writeAPIGatewayJSON(w, http.StatusCreated, apiGatewayMethodJSON(method))
	case http.MethodGet:
		if !a.authorized(w, r, "GetMethod", "*") {
			return
		}
		method, ok := resource.Methods[httpMethod]
		if !ok {
			writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "method not found")
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayMethodJSON(method))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteMethod", "*") {
			return
		}
		delete(resource.Methods, httpMethod)
		api.Resources[resource.ID] = resource
		a.restAPIs[api.ID] = api
		w.WriteHeader(http.StatusAccepted)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func apiGatewayMethodJSON(method apiGatewayMethod) map[string]any {
	response := map[string]any{
		"httpMethod":        method.HTTPMethod,
		"authorizationType": method.AuthorizationType,
		"apiKeyRequired":    method.APIKeyRequired,
	}
	if method.Integration != nil {
		response["methodIntegration"] = apiGatewayIntegrationJSON(*method.Integration)
	}
	if len(method.Responses) > 0 {
		responses := map[string]any{}
		for statusCode, value := range method.Responses {
			responses[statusCode] = apiGatewayMethodResponseJSON(value)
		}
		response["methodResponses"] = responses
	}
	return response
}

func (a *APIGatewayAdapter) serveMethodResponse(w http.ResponseWriter, r *http.Request, api apiGatewayRestAPI, resource apiGatewayResource, httpMethod, statusCode string) {
	method, ok := resource.Methods[httpMethod]
	if !ok {
		writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "method not found")
		return
	}
	if method.Responses == nil {
		method.Responses = map[string]apiGatewayMethodResponse{}
	}
	switch r.Method {
	case http.MethodPut:
		if !a.authorized(w, r, "PutMethodResponse", "*") {
			return
		}
		if _, exists := method.Responses[statusCode]; exists {
			writeAPIGatewayError(w, http.StatusConflict, "ConflictException", "method response already exists")
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		response := apiGatewayMethodResponse{
			StatusCode:         statusCode,
			ResponseModels:     mapValue(body["responseModels"]),
			ResponseParameters: boolMapValue(body["responseParameters"]),
		}
		method.Responses[statusCode] = response
		resource.Methods[httpMethod] = method
		api.Resources[resource.ID] = resource
		a.restAPIs[api.ID] = api
		writeAPIGatewayJSON(w, http.StatusCreated, apiGatewayMethodResponseJSON(response))
	case http.MethodGet:
		if !a.authorized(w, r, "GetMethodResponse", "*") {
			return
		}
		response, ok := method.Responses[statusCode]
		if !ok {
			writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "method response not found")
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayMethodResponseJSON(response))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteMethodResponse", "*") {
			return
		}
		delete(method.Responses, statusCode)
		resource.Methods[httpMethod] = method
		api.Resources[resource.ID] = resource
		a.restAPIs[api.ID] = api
		w.WriteHeader(http.StatusAccepted)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func apiGatewayMethodResponseJSON(response apiGatewayMethodResponse) map[string]any {
	return map[string]any{
		"statusCode":         response.StatusCode,
		"responseModels":     response.ResponseModels,
		"responseParameters": response.ResponseParameters,
	}
}

func (a *APIGatewayAdapter) serveIntegration(w http.ResponseWriter, r *http.Request, api apiGatewayRestAPI, resource apiGatewayResource, httpMethod string, parts []string) {
	method, ok := resource.Methods[httpMethod]
	if !ok {
		writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "method not found")
		return
	}
	if len(parts) >= 2 && parts[0] == "responses" {
		a.serveIntegrationResponse(w, r, api, resource, method, parts[1])
		return
	}
	switch r.Method {
	case http.MethodPut:
		if !a.authorized(w, r, "PutIntegration", "*") {
			return
		}
		if method.Integration != nil {
			writeAPIGatewayError(w, http.StatusConflict, "ConflictException", "integration already exists")
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		integration := apiGatewayIntegration{
			Type:                stringValue(body["type"]),
			HTTPMethod:          stringValue(body["httpMethod"]),
			URI:                 stringValue(body["uri"]),
			PassthroughBehavior: stringValue(body["passthroughBehavior"]),
			Responses:           map[string]apiGatewayIntegrationResponse{},
		}
		method.Integration = &integration
		resource.Methods[httpMethod] = method
		api.Resources[resource.ID] = resource
		a.restAPIs[api.ID] = api
		writeAPIGatewayJSON(w, http.StatusCreated, apiGatewayIntegrationJSON(integration))
	case http.MethodGet:
		if !a.authorized(w, r, "GetIntegration", "*") {
			return
		}
		if method.Integration == nil {
			writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "integration not found")
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayIntegrationJSON(*method.Integration))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteIntegration", "*") {
			return
		}
		method.Integration = nil
		resource.Methods[httpMethod] = method
		api.Resources[resource.ID] = resource
		a.restAPIs[api.ID] = api
		w.WriteHeader(http.StatusAccepted)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func apiGatewayIntegrationJSON(integration apiGatewayIntegration) map[string]any {
	response := map[string]any{
		"type":                integration.Type,
		"httpMethod":          integration.HTTPMethod,
		"uri":                 integration.URI,
		"passthroughBehavior": integration.PassthroughBehavior,
	}
	if len(integration.Responses) > 0 {
		responses := map[string]any{}
		for statusCode, value := range integration.Responses {
			responses[statusCode] = apiGatewayIntegrationResponseJSON(value)
		}
		response["integrationResponses"] = responses
	}
	return response
}

func (a *APIGatewayAdapter) serveIntegrationResponse(w http.ResponseWriter, r *http.Request, api apiGatewayRestAPI, resource apiGatewayResource, method apiGatewayMethod, statusCode string) {
	if method.Integration == nil {
		writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "integration not found")
		return
	}
	if method.Integration.Responses == nil {
		method.Integration.Responses = map[string]apiGatewayIntegrationResponse{}
	}
	switch r.Method {
	case http.MethodPut:
		if !a.authorized(w, r, "PutIntegrationResponse", "*") {
			return
		}
		if _, exists := method.Integration.Responses[statusCode]; exists {
			writeAPIGatewayError(w, http.StatusConflict, "ConflictException", "integration response already exists")
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		response := apiGatewayIntegrationResponse{
			StatusCode:         statusCode,
			SelectionPattern:   stringValue(body["selectionPattern"]),
			ResponseParameters: mapValue(body["responseParameters"]),
			ResponseTemplates:  mapValue(body["responseTemplates"]),
		}
		method.Integration.Responses[statusCode] = response
		resource.Methods[method.HTTPMethod] = method
		api.Resources[resource.ID] = resource
		a.restAPIs[api.ID] = api
		writeAPIGatewayJSON(w, http.StatusCreated, apiGatewayIntegrationResponseJSON(response))
	case http.MethodGet:
		if !a.authorized(w, r, "GetIntegrationResponse", "*") {
			return
		}
		response, ok := method.Integration.Responses[statusCode]
		if !ok {
			writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "integration response not found")
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, apiGatewayIntegrationResponseJSON(response))
	case http.MethodDelete:
		if !a.authorized(w, r, "DeleteIntegrationResponse", "*") {
			return
		}
		delete(method.Integration.Responses, statusCode)
		resource.Methods[method.HTTPMethod] = method
		api.Resources[resource.ID] = resource
		a.restAPIs[api.ID] = api
		w.WriteHeader(http.StatusAccepted)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func apiGatewayIntegrationResponseJSON(response apiGatewayIntegrationResponse) map[string]any {
	return map[string]any{
		"statusCode":         response.StatusCode,
		"selectionPattern":   response.SelectionPattern,
		"responseParameters": response.ResponseParameters,
		"responseTemplates":  response.ResponseTemplates,
	}
}

func (a *APIGatewayAdapter) serveTags(w http.ResponseWriter, r *http.Request, resourceARN string) {
	if strings.Contains(resourceARN, "/domainnames/") {
		name := resourceARN[strings.LastIndex(resourceARN, "/")+1:]
		domain, ok := a.domainNames[name]
		if !ok {
			writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "domain name not found")
			return
		}
		if domain.Tags == nil {
			domain.Tags = map[string]string{}
		}
		switch r.Method {
		case http.MethodGet:
			if !a.authorized(w, r, "GetTags", "*") {
				return
			}
			writeAPIGatewayJSON(w, http.StatusOK, map[string]any{"tags": domain.Tags})
		case http.MethodPut:
			if !a.authorized(w, r, "TagResource", "*") {
				return
			}
			var body struct {
				Tags map[string]string `json:"tags"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			for key, value := range body.Tags {
				domain.Tags[key] = value
			}
			a.domainNames[name] = domain
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			if !a.authorized(w, r, "UntagResource", "*") {
				return
			}
			for _, key := range r.URL.Query()["tagKeys"] {
				delete(domain.Tags, key)
			}
			a.domainNames[name] = domain
			w.WriteHeader(http.StatusNoContent)
		default:
			writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
		}
		return
	}

	id := resourceARN[strings.LastIndex(resourceARN, "/")+1:]
	api, ok := a.restAPIs[id]
	if !ok {
		writeAPIGatewayError(w, http.StatusNotFound, "NotFoundException", "rest api not found")
		return
	}
	if api.Tags == nil {
		api.Tags = map[string]string{}
	}

	switch r.Method {
	case http.MethodGet:
		if !a.authorized(w, r, "GetTags", "*") {
			return
		}
		writeAPIGatewayJSON(w, http.StatusOK, map[string]any{"tags": api.Tags})
	case http.MethodPut:
		if !a.authorized(w, r, "TagResource", "*") {
			return
		}
		var body struct {
			Tags map[string]string `json:"tags"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for key, value := range body.Tags {
			api.Tags[key] = value
		}
		a.restAPIs[id] = api
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if !a.authorized(w, r, "UntagResource", "*") {
			return
		}
		for _, key := range r.URL.Query()["tagKeys"] {
			delete(api.Tags, key)
		}
		a.restAPIs[id] = api
		w.WriteHeader(http.StatusNoContent)
	default:
		writeAPIGatewayError(w, http.StatusBadRequest, "BadRequestException", "unsupported API Gateway action")
	}
}

func writeAPIGatewayJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (a *APIGatewayAdapter) authorized(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "apigateway:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "apigateway",
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
		writeAPIGatewayError(w, http.StatusInternalServerError, "InternalFailure", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeAPIGatewayError(w, http.StatusForbidden, "AccessDenied", decision.Reason)
		return false
	}
	return true
}

func writeAPIGatewayError(w http.ResponseWriter, status int, code, message string) {
	writeAPIGatewayJSON(w, status, map[string]string{"__type": code, "message": message})
}

func boolValue(value any) bool {
	got, _ := value.(bool)
	return got
}

func boolMapValue(value any) map[string]bool {
	result := map[string]bool{}
	values, _ := value.(map[string]any)
	for key, raw := range values {
		result[key] = boolValue(raw)
	}
	return result
}

func endpointTypes(value any) []string {
	config, _ := value.(map[string]any)
	rawTypes, _ := config["types"].([]any)
	types := make([]string, 0, len(rawTypes))
	for _, raw := range rawTypes {
		if endpointType := stringValue(raw); endpointType != "" {
			types = append(types, endpointType)
		}
	}
	return types
}
