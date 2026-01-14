package policy

// KeycloakMapping represents how a cloud policy maps to Keycloak RBAC.
type KeycloakMapping struct {
	// Realm is the Keycloak realm name
	Realm string `json:"realm"`

	// Roles are the generated Keycloak roles
	Roles []KeycloakRole `json:"roles"`

	// Clients are any client configurations needed
	Clients []KeycloakClient `json:"clients,omitempty"`

	// Policies are the Keycloak authorization policies
	Policies []KeycloakPolicy `json:"policies,omitempty"`

	// MappingConfidence is 0-1 score of how reliable the mapping is
	MappingConfidence float64 `json:"mapping_confidence"`

	// ManualReviewNotes contains notes for manual review
	ManualReviewNotes []string `json:"manual_review_notes,omitempty"`

	// UnmappedActions are actions that couldn't be mapped
	UnmappedActions []string `json:"unmapped_actions,omitempty"`
}

// KeycloakRole represents a Keycloak role.
type KeycloakRole struct {
	// Name is the role name
	Name string `json:"name"`

	// Description describes the role
	Description string `json:"description,omitempty"`

	// Composite indicates if this is a composite role
	Composite bool `json:"composite,omitempty"`

	// CompositeRoles are roles included in this composite
	CompositeRoles []string `json:"composite_roles,omitempty"`

	// Attributes are custom role attributes
	Attributes map[string][]string `json:"attributes,omitempty"`

	// SourceActions are the cloud actions this role grants
	SourceActions []string `json:"source_actions,omitempty"`
}

// KeycloakClient represents a Keycloak client configuration.
type KeycloakClient struct {
	// ClientID is the client identifier
	ClientID string `json:"client_id"`

	// Name is the client display name
	Name string `json:"name,omitempty"`

	// ServiceAccountsEnabled indicates if service account is enabled
	ServiceAccountsEnabled bool `json:"service_accounts_enabled"`

	// AuthorizationEnabled indicates if authorization is enabled
	AuthorizationEnabled bool `json:"authorization_enabled"`

	// StandardFlowEnabled indicates if standard flow is enabled
	StandardFlowEnabled bool `json:"standard_flow_enabled"`

	// DirectAccessGrantsEnabled indicates if direct access is enabled
	DirectAccessGrantsEnabled bool `json:"direct_access_grants_enabled"`
}

// KeycloakPolicy represents a Keycloak authorization policy.
type KeycloakPolicy struct {
	// Name is the policy name
	Name string `json:"name"`

	// Type is the policy type (role, user, aggregate, etc.)
	Type string `json:"type"`

	// Logic is the policy logic (POSITIVE or NEGATIVE)
	Logic string `json:"logic"`

	// DecisionStrategy is the decision strategy (UNANIMOUS, AFFIRMATIVE, CONSENSUS)
	DecisionStrategy string `json:"decision_strategy,omitempty"`

	// Roles are roles required for role-based policies
	Roles []string `json:"roles,omitempty"`

	// Scopes are scopes this policy grants
	Scopes []string `json:"scopes,omitempty"`

	// Resources are resources this policy applies to
	Resources []string `json:"resources,omitempty"`
}

// ScopeMapping maps cloud actions to Keycloak scopes.
var ScopeMapping = map[string]map[string]string{
	"aws": {
		// S3 actions
		"s3:GetObject":    "storage:read",
		"s3:PutObject":    "storage:write",
		"s3:DeleteObject": "storage:delete",
		"s3:ListBucket":   "storage:list",
		"s3:*":            "storage:admin",

		// DynamoDB actions
		"dynamodb:GetItem":    "database:read",
		"dynamodb:PutItem":    "database:write",
		"dynamodb:DeleteItem": "database:delete",
		"dynamodb:Query":      "database:read",
		"dynamodb:Scan":       "database:read",
		"dynamodb:*":          "database:admin",

		// Lambda actions
		"lambda:InvokeFunction": "compute:invoke",
		"lambda:GetFunction":    "compute:read",
		"lambda:CreateFunction": "compute:manage",
		"lambda:UpdateFunction": "compute:manage",
		"lambda:*":              "compute:admin",

		// SQS actions
		"sqs:SendMessage":    "messaging:send",
		"sqs:ReceiveMessage": "messaging:receive",
		"sqs:DeleteMessage":  "messaging:delete",
		"sqs:GetQueueUrl":    "messaging:read",
		"sqs:*":              "messaging:admin",

		// SNS actions
		"sns:Publish":   "messaging:send",
		"sns:Subscribe": "messaging:subscribe",
		"sns:*":         "messaging:admin",

		// KMS actions
		"kms:Encrypt":    "security:encrypt",
		"kms:Decrypt":    "security:decrypt",
		"kms:GenerateKey": "security:manage-keys",
		"kms:*":          "security:admin",

		// EC2 actions
		"ec2:DescribeInstances": "compute:read",
		"ec2:RunInstances":      "compute:manage",
		"ec2:TerminateInstances": "compute:manage",
		"ec2:*":                 "compute:admin",

		// IAM actions (high risk)
		"iam:*":             "security:admin",
		"iam:CreateRole":    "security:manage",
		"iam:DeleteRole":    "security:manage",
		"iam:AttachPolicy":  "security:manage",

		// Secrets Manager
		"secretsmanager:GetSecretValue":  "security:read",
		"secretsmanager:CreateSecret":    "security:manage",
		"secretsmanager:*":               "security:admin",
	},
	"gcp": {
		// Cloud Storage
		"storage.objects.get":    "storage:read",
		"storage.objects.create": "storage:write",
		"storage.objects.delete": "storage:delete",
		"storage.objects.list":   "storage:list",
		"storage.*":              "storage:admin",

		// BigQuery
		"bigquery.tables.getData": "database:read",
		"bigquery.tables.update":  "database:write",
		"bigquery.*":              "database:admin",

		// Cloud Functions
		"cloudfunctions.functions.invoke": "compute:invoke",
		"cloudfunctions.functions.get":    "compute:read",
		"cloudfunctions.*":                "compute:admin",

		// Pub/Sub
		"pubsub.topics.publish":         "messaging:send",
		"pubsub.subscriptions.consume":  "messaging:receive",
		"pubsub.*":                      "messaging:admin",

		// Compute Engine
		"compute.instances.get":    "compute:read",
		"compute.instances.create": "compute:manage",
		"compute.instances.delete": "compute:manage",
		"compute.*":                "compute:admin",

		// Secret Manager
		"secretmanager.versions.access": "security:read",
		"secretmanager.secrets.create":  "security:manage",
		"secretmanager.*":               "security:admin",
	},
	"azure": {
		// Storage
		"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read":   "storage:read",
		"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/write":  "storage:write",
		"Microsoft.Storage/storageAccounts/blobServices/containers/blobs/delete": "storage:delete",
		"Microsoft.Storage/*":                                                    "storage:admin",

		// Cosmos DB
		"Microsoft.DocumentDB/databaseAccounts/readonlykeys/action": "database:read",
		"Microsoft.DocumentDB/databaseAccounts/listKeys/action":     "database:admin",
		"Microsoft.DocumentDB/*":                                    "database:admin",

		// Functions
		"Microsoft.Web/sites/functions/action": "compute:invoke",
		"Microsoft.Web/sites/read":             "compute:read",
		"Microsoft.Web/*":                      "compute:admin",

		// Service Bus
		"Microsoft.ServiceBus/namespaces/queues/send/action":    "messaging:send",
		"Microsoft.ServiceBus/namespaces/queues/receive/action": "messaging:receive",
		"Microsoft.ServiceBus/*":                                "messaging:admin",

		// Key Vault
		"Microsoft.KeyVault/vaults/secrets/read":  "security:read",
		"Microsoft.KeyVault/vaults/secrets/write": "security:manage",
		"Microsoft.KeyVault/*":                    "security:admin",
	},
}

// MapActionsToScopes converts cloud actions to Keycloak scopes.
func MapActionsToScopes(provider Provider, actions []string) (scopes []string, unmapped []string) {
	mapping := ScopeMapping[string(provider)]
	if mapping == nil {
		return nil, actions
	}

	scopeSet := make(map[string]bool)
	for _, action := range actions {
		if scope, ok := mapping[action]; ok {
			scopeSet[scope] = true
		} else {
			// Try wildcard match
			matched := false
			for pattern, scope := range mapping {
				if matchActionPattern(pattern, action) {
					scopeSet[scope] = true
					matched = true
					break
				}
			}
			if !matched {
				unmapped = append(unmapped, action)
			}
		}
	}

	for scope := range scopeSet {
		scopes = append(scopes, scope)
	}
	return scopes, unmapped
}

// matchActionPattern checks if an action matches a pattern with wildcards.
func matchActionPattern(pattern, action string) bool {
	// Simple wildcard matching for patterns like "s3:*"
	if len(pattern) > 0 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(action) >= len(prefix) && action[:len(prefix)] == prefix
	}
	return pattern == action
}

// CalculateConfidence computes mapping confidence based on unmapped actions.
func CalculateConfidence(totalActions int, unmappedCount int) float64 {
	if totalActions == 0 {
		return 1.0
	}
	mapped := totalActions - unmappedCount
	return float64(mapped) / float64(totalActions)
}
