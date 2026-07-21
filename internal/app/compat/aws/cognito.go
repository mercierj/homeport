package aws

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type CognitoAdapter struct {
	mu           sync.Mutex
	pools        map[string]cognitoUserPool
	domains      map[string]cognitoUserPoolDomain
	nextID       int
	nextClientID int
	poolQuota    int
	authorizer   authz.Authorizer
	auditSink    func(authz.Decision)
}

type CognitoOption func(*CognitoAdapter)

type cognitoUserPool struct {
	ID               string
	Name             string
	MFAConfiguration string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Tags             map[string]string
	Clients          map[string]cognitoUserPoolClient
	Users            map[string]cognitoUser
	Groups           map[string]cognitoGroup
}

type cognitoUserPoolClient struct {
	ID        string
	Name      string
	PoolID    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type cognitoUserPoolDomain struct {
	Domain string
	PoolID string
}

type cognitoUser struct {
	Username   string
	Attributes any
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type cognitoGroup struct {
	Name        string
	PoolID      string
	Description string
	Precedence  any
	RoleArn     string
	Members     map[string]bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func NewCognitoAdapter(options ...CognitoOption) *CognitoAdapter {
	adapter := &CognitoAdapter{
		pools:      map[string]cognitoUserPool{},
		domains:    map[string]cognitoUserPoolDomain{},
		authorizer: authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithCognitoAuthorizer(authorizer authz.Authorizer) CognitoOption {
	return func(adapter *CognitoAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithCognitoAuditSink(sink func(authz.Decision)) CognitoOption {
	return func(adapter *CognitoAdapter) {
		adapter.auditSink = sink
	}
}

func WithCognitoUserPoolQuota(maxPools int) CognitoOption {
	return func(adapter *CognitoAdapter) {
		adapter.poolQuota = maxPools
	}
}

func (CognitoAdapter) Provider() string { return "aws" }
func (CognitoAdapter) Service() string  { return "cognito" }
func (CognitoAdapter) Routes() []string { return []string{"POST /compat/aws/cognito"} }
func (CognitoAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_COGNITO_IDP": "http://homeport:8080/api/v1/compat/aws/cognito",
		"HOMEPORT_COMPAT_BACKEND":      "keycloak-oidc",
	}
}
func (CognitoAdapter) ConformanceChecks() []string {
	return []string{"create-user-pool", "describe-user-pool", "list-user-pools", "update-user-pool", "delete-user-pool", "get-user-pool-mfa-config", "create-user-pool-client", "describe-user-pool-client", "list-user-pool-clients", "update-user-pool-client", "delete-user-pool-client", "create-user-pool-domain", "describe-user-pool-domain", "delete-user-pool-domain", "admin-create-user", "admin-get-user", "list-users", "admin-delete-user", "create-group", "get-group", "list-groups", "update-group", "delete-group", "admin-add-user-to-group", "admin-list-groups-for-user", "admin-remove-user-from-group", "tag-resource", "list-tags-for-resource", "untag-resource"}
}

func (a *CognitoAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeCognitoError(w, "InvalidParameterException", err.Error())
		return
	}
	if !a.authorized(w, r, action, cognitoResource(body)) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateUserPool":
		name := stringValue(body["PoolName"])
		if name == "" {
			writeCognitoError(w, "InvalidParameterException", "PoolName is required")
			return
		}
		if a.poolQuota > 0 && len(a.pools) >= a.poolQuota {
			writeCognitoError(w, "LimitExceededException", "user pool quota exceeded")
			return
		}
		a.nextID++
		pool := cognitoUserPool{
			ID:               "us-east-1_homeport" + strconv.Itoa(a.nextID),
			Name:             name,
			MFAConfiguration: "OFF",
			CreatedAt:        time.Now().UTC(),
			UpdatedAt:        time.Now().UTC(),
			Tags:             mapValue(body["UserPoolTags"]),
			Clients:          map[string]cognitoUserPoolClient{},
			Users:            map[string]cognitoUser{},
			Groups:           map[string]cognitoGroup{},
		}
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]any{"UserPool": cognitoUserPoolShape(pool)})
	case "DescribeUserPool":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"UserPool": cognitoUserPoolShape(pool)})
	case "ListUserPools":
		ids := make([]string, 0, len(a.pools))
		for id := range a.pools {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		start, ok := cognitoPageStart(stringValue(body["NextToken"]), len(ids))
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "invalid NextToken")
			return
		}
		maxResults, ok := cloudWatchLogsLimit(body, 60, 60, "MaxResults")
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "MaxResults must be between 1 and 60")
			return
		}
		end := start + maxResults
		if end > len(ids) {
			end = len(ids)
		}
		pools := make([]map[string]string, 0, end-start)
		for _, id := range ids[start:end] {
			pool := a.pools[id]
			pools = append(pools, map[string]string{
				"Id":     pool.ID,
				"Name":   pool.Name,
				"Status": "Enabled",
			})
		}
		response := map[string]any{"UserPools": pools}
		if end < len(ids) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "UpdateUserPool":
		id := stringValue(body["UserPoolId"])
		pool, ok := a.pools[id]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		if value := stringValue(body["MfaConfiguration"]); value != "" {
			if value != "OFF" && value != "ON" && value != "OPTIONAL" {
				writeCognitoError(w, "InvalidParameterException", "invalid MfaConfiguration")
				return
			}
			pool.MFAConfiguration = value
		}
		if tags := mapValue(body["UserPoolTags"]); len(tags) > 0 {
			pool.Tags = tags
		}
		pool.UpdatedAt = time.Now().UTC()
		a.pools[id] = pool
		writeJSON(w, http.StatusOK, map[string]string{})
	case "GetUserPoolMfaConfig":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"MfaConfiguration": pool.MFAConfiguration})
	case "TagResource", "UntagResource", "ListTagsForResource":
		pool, ok := a.cognitoPoolByARN(stringValue(body["ResourceArn"]))
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		switch action {
		case "TagResource":
			if pool.Tags == nil {
				pool.Tags = map[string]string{}
			}
			mergeStringMap(pool.Tags, mapValue(body["Tags"]))
			a.pools[pool.ID] = *pool
			writeJSON(w, http.StatusOK, map[string]string{})
		case "UntagResource":
			for _, key := range eventBridgeStrings(body["TagKeys"]) {
				delete(pool.Tags, key)
			}
			a.pools[pool.ID] = *pool
			writeJSON(w, http.StatusOK, map[string]string{})
		default:
			writeJSON(w, http.StatusOK, map[string]any{"Tags": pool.Tags})
		}
	case "CreateUserPoolClient":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		name := stringValue(body["ClientName"])
		if name == "" {
			writeCognitoError(w, "InvalidParameterException", "ClientName is required")
			return
		}
		a.nextClientID++
		client := cognitoUserPoolClient{
			ID:        "homeportclient" + strconv.Itoa(a.nextClientID),
			Name:      name,
			PoolID:    pool.ID,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if pool.Clients == nil {
			pool.Clients = map[string]cognitoUserPoolClient{}
		}
		pool.Clients[client.ID] = client
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]any{"UserPoolClient": cognitoUserPoolClientShape(client)})
	case "DescribeUserPoolClient":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		client, ok := pool.Clients[stringValue(body["ClientId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"UserPoolClient": cognitoUserPoolClientShape(client)})
	case "ListUserPoolClients":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		ids := make([]string, 0, len(pool.Clients))
		for id := range pool.Clients {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		start, ok := cognitoPageStart(stringValue(body["NextToken"]), len(ids))
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "invalid NextToken")
			return
		}
		maxResults, ok := cloudWatchLogsLimit(body, 60, 60, "MaxResults")
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "MaxResults must be between 1 and 60")
			return
		}
		end := start + maxResults
		if end > len(ids) {
			end = len(ids)
		}
		clients := make([]map[string]any, 0, end-start)
		for _, id := range ids[start:end] {
			client := pool.Clients[id]
			clients = append(clients, map[string]any{"ClientId": client.ID, "ClientName": client.Name, "UserPoolId": client.PoolID})
		}
		response := map[string]any{"UserPoolClients": clients}
		if end < len(ids) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "UpdateUserPoolClient":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		client, ok := pool.Clients[stringValue(body["ClientId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		if name := stringValue(body["ClientName"]); name != "" {
			client.Name = name
		}
		client.UpdatedAt = time.Now().UTC()
		pool.Clients[client.ID] = client
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]any{"UserPoolClient": cognitoUserPoolClientShape(client)})
	case "DeleteUserPoolClient":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		id := stringValue(body["ClientId"])
		if _, ok := pool.Clients[id]; !ok {
			writeCognitoNotFound(w)
			return
		}
		delete(pool.Clients, id)
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]string{})
	case "CreateUserPoolDomain":
		poolID := stringValue(body["UserPoolId"])
		if _, ok := a.pools[poolID]; !ok {
			writeCognitoNotFound(w)
			return
		}
		domain := stringValue(body["Domain"])
		if domain == "" {
			writeCognitoError(w, "InvalidParameterException", "Domain is required")
			return
		}
		a.domains[domain] = cognitoUserPoolDomain{Domain: domain, PoolID: poolID}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DescribeUserPoolDomain":
		domain, ok := a.domains[stringValue(body["Domain"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"DomainDescription": cognitoUserPoolDomainShape(domain)})
	case "DeleteUserPoolDomain":
		poolID := stringValue(body["UserPoolId"])
		domain := stringValue(body["Domain"])
		if existing, ok := a.domains[domain]; !ok || existing.PoolID != poolID {
			writeCognitoNotFound(w)
			return
		}
		delete(a.domains, domain)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "AdminCreateUser":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		username := stringValue(body["Username"])
		if username == "" {
			writeCognitoError(w, "InvalidParameterException", "Username is required")
			return
		}
		if _, exists := pool.Users[username]; exists {
			writeCognitoError(w, "UsernameExistsException", "user already exists")
			return
		}
		user := cognitoUser{
			Username:   username,
			Attributes: body["UserAttributes"],
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
		}
		if pool.Users == nil {
			pool.Users = map[string]cognitoUser{}
		}
		pool.Users[username] = user
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]any{"User": cognitoUserShape(user)})
	case "AdminGetUser":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		user, ok := pool.Users[stringValue(body["Username"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, cognitoAdminUserShape(user))
	case "ListUsers":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		names := make([]string, 0, len(pool.Users))
		for name := range pool.Users {
			names = append(names, name)
		}
		sort.Strings(names)
		start, ok := cognitoPageStart(stringValue(body["PaginationToken"]), len(names))
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "invalid PaginationToken")
			return
		}
		limit, ok := cloudWatchLogsLimit(body, 60, 60, "Limit")
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "Limit must be between 1 and 60")
			return
		}
		end := start + limit
		if end > len(names) {
			end = len(names)
		}
		users := make([]map[string]any, 0, end-start)
		for _, name := range names[start:end] {
			users = append(users, cognitoUserShape(pool.Users[name]))
		}
		response := map[string]any{"Users": users}
		if end < len(names) {
			response["PaginationToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "AdminDeleteUser":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		username := stringValue(body["Username"])
		if _, ok := pool.Users[username]; !ok {
			writeCognitoNotFound(w)
			return
		}
		delete(pool.Users, username)
		for name, group := range pool.Groups {
			delete(group.Members, username)
			pool.Groups[name] = group
		}
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]string{})
	case "CreateGroup":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		name := stringValue(body["GroupName"])
		if name == "" {
			writeCognitoError(w, "InvalidParameterException", "GroupName is required")
			return
		}
		if _, exists := pool.Groups[name]; exists {
			writeCognitoError(w, "GroupExistsException", "group already exists")
			return
		}
		group := cognitoGroup{
			Name:        name,
			PoolID:      pool.ID,
			Description: stringValue(body["Description"]),
			Precedence:  body["Precedence"],
			RoleArn:     stringValue(body["RoleArn"]),
			Members:     map[string]bool{},
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		if pool.Groups == nil {
			pool.Groups = map[string]cognitoGroup{}
		}
		pool.Groups[name] = group
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]any{"Group": cognitoGroupShape(group)})
	case "GetGroup":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		group, ok := pool.Groups[stringValue(body["GroupName"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Group": cognitoGroupShape(group)})
	case "ListGroups":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		names := make([]string, 0, len(pool.Groups))
		for name := range pool.Groups {
			names = append(names, name)
		}
		sort.Strings(names)
		start, ok := cognitoPageStart(stringValue(body["NextToken"]), len(names))
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "invalid NextToken")
			return
		}
		limit, ok := cloudWatchLogsLimit(body, 60, 60, "Limit")
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "Limit must be between 1 and 60")
			return
		}
		end := start + limit
		if end > len(names) {
			end = len(names)
		}
		groups := make([]map[string]any, 0, end-start)
		for _, name := range names[start:end] {
			groups = append(groups, cognitoGroupShape(pool.Groups[name]))
		}
		response := map[string]any{"Groups": groups}
		if end < len(names) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "UpdateGroup":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		group, ok := pool.Groups[stringValue(body["GroupName"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		if description, ok := body["Description"]; ok {
			group.Description = stringValue(description)
		}
		if precedence, ok := body["Precedence"]; ok {
			group.Precedence = precedence
		}
		if roleARN, ok := body["RoleArn"]; ok {
			group.RoleArn = stringValue(roleARN)
		}
		group.UpdatedAt = time.Now().UTC()
		pool.Groups[group.Name] = group
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]any{"Group": cognitoGroupShape(group)})
	case "DeleteGroup":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		name := stringValue(body["GroupName"])
		if _, ok := pool.Groups[name]; !ok {
			writeCognitoNotFound(w)
			return
		}
		delete(pool.Groups, name)
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]string{})
	case "AdminAddUserToGroup":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		username := stringValue(body["Username"])
		group, groupOK := pool.Groups[stringValue(body["GroupName"])]
		if _, userOK := pool.Users[username]; !userOK || !groupOK {
			writeCognitoNotFound(w)
			return
		}
		if group.Members == nil {
			group.Members = map[string]bool{}
		}
		group.Members[username] = true
		pool.Groups[group.Name] = group
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]string{})
	case "AdminListGroupsForUser":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		username := stringValue(body["Username"])
		if _, ok := pool.Users[username]; !ok {
			writeCognitoNotFound(w)
			return
		}
		names := make([]string, 0, len(pool.Groups))
		for name, group := range pool.Groups {
			if group.Members[username] {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		start, ok := cognitoPageStart(stringValue(body["NextToken"]), len(names))
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "invalid NextToken")
			return
		}
		limit, ok := cloudWatchLogsLimit(body, 60, 60, "Limit")
		if !ok {
			writeCognitoError(w, "InvalidParameterException", "Limit must be between 1 and 60")
			return
		}
		end := start + limit
		if end > len(names) {
			end = len(names)
		}
		groups := make([]map[string]any, 0, end-start)
		for _, name := range names[start:end] {
			groups = append(groups, cognitoGroupShape(pool.Groups[name]))
		}
		response := map[string]any{"Groups": groups}
		if end < len(names) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "AdminRemoveUserFromGroup":
		pool, ok := a.pools[stringValue(body["UserPoolId"])]
		if !ok {
			writeCognitoNotFound(w)
			return
		}
		username := stringValue(body["Username"])
		group, groupOK := pool.Groups[stringValue(body["GroupName"])]
		if _, userOK := pool.Users[username]; !userOK || !groupOK {
			writeCognitoNotFound(w)
			return
		}
		delete(group.Members, username)
		pool.Groups[group.Name] = group
		a.pools[pool.ID] = pool
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DeleteUserPool":
		id := stringValue(body["UserPoolId"])
		if _, ok := a.pools[id]; !ok {
			writeCognitoNotFound(w)
			return
		}
		delete(a.pools, id)
		for domain, existing := range a.domains {
			if existing.PoolID == id {
				delete(a.domains, domain)
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	default:
		writeCognitoError(w, "UnsupportedOperation", "Cognito action is not implemented")
	}
}

func cognitoGroupShape(group cognitoGroup) map[string]any {
	return map[string]any{
		"CreationDate":     group.CreatedAt.Unix(),
		"Description":      group.Description,
		"GroupName":        group.Name,
		"LastModifiedDate": group.UpdatedAt.Unix(),
		"Precedence":       group.Precedence,
		"RoleArn":          group.RoleArn,
		"UserPoolId":       group.PoolID,
	}
}

func cognitoAdminUserShape(user cognitoUser) map[string]any {
	shape := cognitoUserShape(user)
	return map[string]any{
		"Enabled":              shape["Enabled"],
		"UserAttributes":       shape["Attributes"],
		"UserCreateDate":       shape["UserCreateDate"],
		"UserLastModifiedDate": shape["UserLastModifiedDate"],
		"UserStatus":           shape["UserStatus"],
		"Username":             shape["Username"],
	}
}

func cognitoUserShape(user cognitoUser) map[string]any {
	return map[string]any{
		"Attributes":           user.Attributes,
		"Enabled":              true,
		"UserCreateDate":       user.CreatedAt.Unix(),
		"UserLastModifiedDate": user.UpdatedAt.Unix(),
		"UserStatus":           "FORCE_CHANGE_PASSWORD",
		"Username":             user.Username,
	}
}

func cognitoUserPoolDomainShape(domain cognitoUserPoolDomain) map[string]any {
	return map[string]any{
		"AWSAccountId": "000000000000",
		"Domain":       domain.Domain,
		"Status":       "ACTIVE",
		"UserPoolId":   domain.PoolID,
		"Version":      "1",
	}
}

func cognitoUserPoolClientShape(client cognitoUserPoolClient) map[string]any {
	return map[string]any{
		"ClientId":         client.ID,
		"ClientName":       client.Name,
		"UserPoolId":       client.PoolID,
		"CreationDate":     client.CreatedAt.Unix(),
		"LastModifiedDate": client.UpdatedAt.Unix(),
	}
}

func cognitoUserPoolShape(pool cognitoUserPool) map[string]any {
	return map[string]any{
		"Id":                          pool.ID,
		"Name":                        pool.Name,
		"Arn":                         cognitoUserPoolARN(pool.ID),
		"Status":                      "Enabled",
		"MfaConfiguration":            pool.MFAConfiguration,
		"CreationDate":                pool.CreatedAt.Unix(),
		"LastModifiedDate":            pool.UpdatedAt.Unix(),
		"DeletionProtection":          "INACTIVE",
		"EstimatedNumberOfUsers":      0,
		"UserPoolTags":                pool.Tags,
		"Policies":                    map[string]any{"PasswordPolicy": map[string]any{"MinimumLength": 8}},
		"AdminCreateUserConfig":       map[string]any{},
		"VerificationMessageTemplate": map[string]any{},
	}
}

func writeCognitoNotFound(w http.ResponseWriter) {
	writeCognitoError(w, "ResourceNotFoundException", "user pool not found")
}

func writeCognitoError(w http.ResponseWriter, code, message string) {
	writeCognitoErrorStatus(w, http.StatusBadRequest, code, message)
}

func cognitoResource(body map[string]any) string {
	if arn := stringValue(body["ResourceArn"]); arn != "" {
		return arn
	}
	if id := stringValue(body["UserPoolId"]); id != "" {
		return cognitoUserPoolARN(id)
	}
	return "*"
}

func cognitoUserPoolARN(id string) string {
	return "arn:aws:cognito-idp:us-east-1:000000000000:userpool/" + id
}

func (a *CognitoAdapter) cognitoPoolByARN(arn string) (*cognitoUserPool, bool) {
	for _, pool := range a.pools {
		if cognitoUserPoolARN(pool.ID) == arn {
			return &pool, true
		}
	}
	return nil, false
}

func cognitoPageStart(token string, count int) (int, bool) {
	if token == "" {
		return 0, true
	}
	start, err := strconv.Atoi(token)
	return start, err == nil && start >= 0 && start <= count
}

func (a *CognitoAdapter) authorized(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "cognito-idp:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "cognito-idp",
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
		writeCognitoErrorStatus(w, http.StatusInternalServerError, "InternalErrorException", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeCognitoError(w, "NotAuthorizedException", decision.Reason)
		return false
	}
	return true
}

func writeCognitoErrorStatus(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"__type": code, "message": message})
}
