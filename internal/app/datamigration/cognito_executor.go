package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CognitoToKeycloakExecutor migrates Cognito user pools to Keycloak import format.
type CognitoToKeycloakExecutor struct{}

// NewCognitoToKeycloakExecutor creates a new Cognito to Keycloak executor.
func NewCognitoToKeycloakExecutor() *CognitoToKeycloakExecutor {
	return &CognitoToKeycloakExecutor{}
}

// Type returns the migration type.
func (e *CognitoToKeycloakExecutor) Type() string {
	return "cognito_to_keycloak"
}

// GetPhases returns the migration phases.
func (e *CognitoToKeycloakExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching user pool configuration",
		"Exporting users",
		"Exporting groups",
		"Generating Keycloak import",
	}
}

// Validate validates the migration configuration.
func (e *CognitoToKeycloakExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	// Validate source config
	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["user_pool_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.user_pool_id is required")
		}
		if _, ok := config.Source["access_key_id"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.access_key_id is required")
		}
		if _, ok := config.Source["secret_access_key"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.secret_access_key is required")
		}
		if _, ok := config.Source["region"].(string); !ok {
			result.Warnings = append(result.Warnings, "source.region not specified, using default us-east-1")
		}
	}

	// Validate destination config
	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	// Add warning about password migration
	result.Warnings = append(result.Warnings, "Cognito passwords cannot be migrated (they are hashed). Users will need to reset their passwords in Keycloak.")

	return result, nil
}

// cognitoUserPool represents a Cognito user pool description.
type cognitoUserPool struct {
	UserPool struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
		Policies struct {
			PasswordPolicy struct {
				MinimumLength                 int  `json:"MinimumLength"`
				RequireUppercase              bool `json:"RequireUppercase"`
				RequireLowercase              bool `json:"RequireLowercase"`
				RequireNumbers                bool `json:"RequireNumbers"`
				RequireSymbols                bool `json:"RequireSymbols"`
				TemporaryPasswordValidityDays int  `json:"TemporaryPasswordValidityDays"`
			} `json:"PasswordPolicy"`
		} `json:"Policies"`
		MfaConfiguration string `json:"MfaConfiguration"`
	} `json:"UserPool"`
}

// cognitoUser represents a Cognito user.
type cognitoUser struct {
	Username       string `json:"Username"`
	Enabled        bool   `json:"Enabled"`
	UserStatus     string `json:"UserStatus"`
	Attributes     []struct {
		Name  string `json:"Name"`
		Value string `json:"Value"`
	} `json:"Attributes"`
	UserCreateDate    string `json:"UserCreateDate"`
	UserLastModifiedDate string `json:"UserLastModifiedDate"`
}

// cognitoUsersResponse represents the response from list-users.
type cognitoUsersResponse struct {
	Users           []cognitoUser `json:"Users"`
	PaginationToken string        `json:"PaginationToken,omitempty"`
}

// cognitoGroup represents a Cognito group.
type cognitoGroup struct {
	GroupName   string `json:"GroupName"`
	Description string `json:"Description"`
	RoleArn     string `json:"RoleArn"`
	Precedence  int    `json:"Precedence"`
}

// cognitoGroupsResponse represents the response from list-groups.
type cognitoGroupsResponse struct {
	Groups        []cognitoGroup `json:"Groups"`
	NextToken     string         `json:"NextToken,omitempty"`
}

// cognitoGroupMembersResponse represents the response from list-users-in-group.
type cognitoGroupMembersResponse struct {
	Users         []cognitoUser `json:"Users"`
	NextToken     string        `json:"NextToken,omitempty"`
}

// keycloakRealmExport represents the Keycloak realm export format.
type keycloakRealmExport struct {
	Realm                  string            `json:"realm"`
	Enabled                bool              `json:"enabled"`
	PasswordPolicy         string            `json:"passwordPolicy,omitempty"`
	Users                  []keycloakUser    `json:"users,omitempty"`
	Groups                 []keycloakGroup   `json:"groups,omitempty"`
	Roles                  *keycloakRoles    `json:"roles,omitempty"`
	DefaultRoles           []string          `json:"defaultRoles,omitempty"`
	RequiredActions        []string          `json:"requiredActions,omitempty"`
}

// keycloakUser represents a Keycloak user.
type keycloakUser struct {
	Username          string            `json:"username"`
	Email             string            `json:"email,omitempty"`
	EmailVerified     bool              `json:"emailVerified"`
	Enabled           bool              `json:"enabled"`
	FirstName         string            `json:"firstName,omitempty"`
	LastName          string            `json:"lastName,omitempty"`
	Attributes        map[string][]string `json:"attributes,omitempty"`
	RequiredActions   []string          `json:"requiredActions,omitempty"`
	Groups            []string          `json:"groups,omitempty"`
	RealmRoles        []string          `json:"realmRoles,omitempty"`
	CreatedTimestamp  int64             `json:"createdTimestamp,omitempty"`
}

// keycloakGroup represents a Keycloak group.
type keycloakGroup struct {
	Name       string           `json:"name"`
	Path       string           `json:"path"`
	Attributes map[string][]string `json:"attributes,omitempty"`
	RealmRoles []string         `json:"realmRoles,omitempty"`
	SubGroups  []keycloakGroup  `json:"subGroups,omitempty"`
}

// keycloakRoles represents Keycloak roles configuration.
type keycloakRoles struct {
	Realm []keycloakRole `json:"realm,omitempty"`
}

// keycloakRole represents a single Keycloak role.
type keycloakRole struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Composite   bool   `json:"composite"`
}

// Execute performs the migration.
func (e *CognitoToKeycloakExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	// Extract source configuration
	userPoolID := config.Source["user_pool_id"].(string)
	accessKeyID := config.Source["access_key_id"].(string)
	secretAccessKey := config.Source["secret_access_key"].(string)
	region, _ := config.Source["region"].(string)
	if region == "" {
		region = "us-east-1"
	}

	// Extract destination configuration
	outputDir := config.Destination["output_dir"].(string)
	realmName, _ := config.Destination["realm_name"].(string)
	if realmName == "" {
		realmName = "migrated-realm"
	}

	// Prepare AWS environment
	awsEnv := append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		"AWS_DEFAULT_REGION="+region,
	)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating AWS credentials")
	EmitProgress(m, 5, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Test AWS credentials
	testCmd := exec.CommandContext(ctx, "aws", "sts", "get-caller-identity", "--region", region)
	testCmd.Env = awsEnv
	if output, err := testCmd.CombinedOutput(); err != nil {
		EmitLog(m, "error", fmt.Sprintf("AWS credentials validation failed: %s", string(output)))
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}
	EmitLog(m, "info", "AWS credentials validated successfully")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching user pool configuration
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching user pool configuration: %s", userPoolID))
	EmitProgress(m, 15, "Getting user pool config")

	describePoolCmd := exec.CommandContext(ctx, "aws", "cognito-idp", "describe-user-pool",
		"--user-pool-id", userPoolID,
		"--region", region,
		"--output", "json",
	)
	describePoolCmd.Env = awsEnv

	poolOutput, err := describePoolCmd.Output()
	if err != nil {
		EmitLog(m, "error", "Failed to describe user pool")
		return fmt.Errorf("failed to describe user pool: %w", err)
	}

	var userPool cognitoUserPool
	if err := json.Unmarshal(poolOutput, &userPool); err != nil {
		return fmt.Errorf("failed to parse user pool configuration: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("User pool: %s (%s)", userPool.UserPool.Name, userPool.UserPool.ID))
	EmitLog(m, "info", fmt.Sprintf("MFA configuration: %s", userPool.UserPool.MfaConfiguration))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting users
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Exporting users from Cognito")
	EmitProgress(m, 30, "Exporting users")

	var allUsers []cognitoUser
	var paginationToken string

	for {
		args := []string{"cognito-idp", "list-users",
			"--user-pool-id", userPoolID,
			"--region", region,
			"--output", "json",
		}
		if paginationToken != "" {
			args = append(args, "--pagination-token", paginationToken)
		}

		listUsersCmd := exec.CommandContext(ctx, "aws", args...)
		listUsersCmd.Env = awsEnv

		usersOutput, err := listUsersCmd.Output()
		if err != nil {
			EmitLog(m, "error", "Failed to list users")
			return fmt.Errorf("failed to list users: %w", err)
		}

		var usersResp cognitoUsersResponse
		if err := json.Unmarshal(usersOutput, &usersResp); err != nil {
			return fmt.Errorf("failed to parse users response: %w", err)
		}

		allUsers = append(allUsers, usersResp.Users...)
		EmitLog(m, "info", fmt.Sprintf("Fetched %d users (total: %d)", len(usersResp.Users), len(allUsers)))

		if usersResp.PaginationToken == "" {
			break
		}
		paginationToken = usersResp.PaginationToken

		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}
	}

	EmitLog(m, "info", fmt.Sprintf("Total users exported: %d", len(allUsers)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Exporting groups
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Exporting groups from Cognito")
	EmitProgress(m, 55, "Exporting groups")

	var allGroups []cognitoGroup
	var nextToken string

	for {
		args := []string{"cognito-idp", "list-groups",
			"--user-pool-id", userPoolID,
			"--region", region,
			"--output", "json",
		}
		if nextToken != "" {
			args = append(args, "--next-token", nextToken)
		}

		listGroupsCmd := exec.CommandContext(ctx, "aws", args...)
		listGroupsCmd.Env = awsEnv

		groupsOutput, err := listGroupsCmd.Output()
		if err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to list groups: %v", err))
			break
		}

		var groupsResp cognitoGroupsResponse
		if err := json.Unmarshal(groupsOutput, &groupsResp); err != nil {
			EmitLog(m, "warn", fmt.Sprintf("Failed to parse groups response: %v", err))
			break
		}

		allGroups = append(allGroups, groupsResp.Groups...)
		EmitLog(m, "info", fmt.Sprintf("Fetched %d groups (total: %d)", len(groupsResp.Groups), len(allGroups)))

		if groupsResp.NextToken == "" {
			break
		}
		nextToken = groupsResp.NextToken

		if m.IsCancelled() {
			return fmt.Errorf("migration cancelled")
		}
	}

	// Get group memberships
	groupMembers := make(map[string][]string) // groupName -> []username
	for _, group := range allGroups {
		var members []string
		var memberToken string

		for {
			args := []string{"cognito-idp", "list-users-in-group",
				"--user-pool-id", userPoolID,
				"--group-name", group.GroupName,
				"--region", region,
				"--output", "json",
			}
			if memberToken != "" {
				args = append(args, "--next-token", memberToken)
			}

			listMembersCmd := exec.CommandContext(ctx, "aws", args...)
			listMembersCmd.Env = awsEnv

			membersOutput, err := listMembersCmd.Output()
			if err != nil {
				EmitLog(m, "warn", fmt.Sprintf("Failed to list members of group %s: %v", group.GroupName, err))
				break
			}

			var membersResp cognitoGroupMembersResponse
			if err := json.Unmarshal(membersOutput, &membersResp); err != nil {
				break
			}

			for _, user := range membersResp.Users {
				members = append(members, user.Username)
			}

			if membersResp.NextToken == "" {
				break
			}
			memberToken = membersResp.NextToken
		}

		groupMembers[group.GroupName] = members
		EmitLog(m, "info", fmt.Sprintf("Group '%s' has %d members", group.GroupName, len(members)))
	}

	EmitLog(m, "info", fmt.Sprintf("Total groups exported: %d", len(allGroups)))

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Generating Keycloak import
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating Keycloak realm import file")
	EmitProgress(m, 75, "Generating import file")

	// Build user-to-groups map for efficient lookup
	userGroups := make(map[string][]string) // username -> []groupName
	for groupName, members := range groupMembers {
		for _, username := range members {
			userGroups[username] = append(userGroups[username], groupName)
		}
	}

	// Convert Cognito users to Keycloak users
	keycloakUsers := make([]keycloakUser, 0, len(allUsers))
	for _, cogUser := range allUsers {
		kcUser := keycloakUser{
			Username:        cogUser.Username,
			Enabled:         cogUser.Enabled,
			EmailVerified:   false,
			RequiredActions: []string{"UPDATE_PASSWORD"},
			Attributes:      make(map[string][]string),
		}

		// Map Cognito attributes to Keycloak
		for _, attr := range cogUser.Attributes {
			switch attr.Name {
			case "email":
				kcUser.Email = attr.Value
			case "email_verified":
				kcUser.EmailVerified = attr.Value == "true"
			case "given_name":
				kcUser.FirstName = attr.Value
			case "family_name":
				kcUser.LastName = attr.Value
			case "phone_number":
				kcUser.Attributes["phoneNumber"] = []string{attr.Value}
			case "phone_number_verified":
				kcUser.Attributes["phoneNumberVerified"] = []string{attr.Value}
			case "sub":
				kcUser.Attributes["cognitoSub"] = []string{attr.Value}
			default:
				// Store other custom attributes
				if strings.HasPrefix(attr.Name, "custom:") {
					attrName := strings.TrimPrefix(attr.Name, "custom:")
					kcUser.Attributes[attrName] = []string{attr.Value}
				} else {
					kcUser.Attributes[attr.Name] = []string{attr.Value}
				}
			}
		}

		// Add user status as attribute
		kcUser.Attributes["cognitoUserStatus"] = []string{cogUser.UserStatus}

		// Add group memberships
		if groups, ok := userGroups[cogUser.Username]; ok {
			kcUser.Groups = make([]string, len(groups))
			for i, g := range groups {
				kcUser.Groups[i] = "/" + g
			}
		}

		keycloakUsers = append(keycloakUsers, kcUser)
	}

	// Convert Cognito groups to Keycloak groups
	keycloakGroups := make([]keycloakGroup, 0, len(allGroups))
	for _, cogGroup := range allGroups {
		kcGroup := keycloakGroup{
			Name:       cogGroup.GroupName,
			Path:       "/" + cogGroup.GroupName,
			Attributes: make(map[string][]string),
			SubGroups:  []keycloakGroup{},
		}

		if cogGroup.Description != "" {
			kcGroup.Attributes["description"] = []string{cogGroup.Description}
		}
		if cogGroup.RoleArn != "" {
			kcGroup.Attributes["cognitoRoleArn"] = []string{cogGroup.RoleArn}
		}

		keycloakGroups = append(keycloakGroups, kcGroup)
	}

	// Create roles from Cognito groups
	keycloakRolesList := make([]keycloakRole, 0, len(allGroups))
	for _, cogGroup := range allGroups {
		role := keycloakRole{
			Name:        cogGroup.GroupName,
			Description: fmt.Sprintf("Role migrated from Cognito group: %s", cogGroup.GroupName),
			Composite:   false,
		}
		keycloakRolesList = append(keycloakRolesList, role)
	}

	// Build password policy from Cognito configuration
	var passwordPolicyParts []string
	policy := userPool.UserPool.Policies.PasswordPolicy
	if policy.MinimumLength > 0 {
		passwordPolicyParts = append(passwordPolicyParts, fmt.Sprintf("length(%d)", policy.MinimumLength))
	}
	if policy.RequireUppercase {
		passwordPolicyParts = append(passwordPolicyParts, "upperCase(1)")
	}
	if policy.RequireLowercase {
		passwordPolicyParts = append(passwordPolicyParts, "lowerCase(1)")
	}
	if policy.RequireNumbers {
		passwordPolicyParts = append(passwordPolicyParts, "digits(1)")
	}
	if policy.RequireSymbols {
		passwordPolicyParts = append(passwordPolicyParts, "specialChars(1)")
	}

	// Build the realm export
	realmExport := keycloakRealmExport{
		Realm:          realmName,
		Enabled:        true,
		Users:          keycloakUsers,
		Groups:         keycloakGroups,
		RequiredActions: []string{"UPDATE_PASSWORD"},
	}

	if len(passwordPolicyParts) > 0 {
		realmExport.PasswordPolicy = strings.Join(passwordPolicyParts, " and ")
	}

	if len(keycloakRolesList) > 0 {
		realmExport.Roles = &keycloakRoles{
			Realm: keycloakRolesList,
		}
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		EmitLog(m, "error", fmt.Sprintf("Failed to create output directory: %v", err))
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write realm export file
	outputFile := filepath.Join(outputDir, "realm-export.json")
	exportJSON, err := json.MarshalIndent(realmExport, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal realm export: %w", err)
	}

	if err := os.WriteFile(outputFile, exportJSON, 0644); err != nil {
		EmitLog(m, "error", fmt.Sprintf("Failed to write realm export: %v", err))
		return fmt.Errorf("failed to write realm export: %w", err)
	}

	EmitLog(m, "info", fmt.Sprintf("Keycloak realm export written to: %s", outputFile))
	EmitLog(m, "info", fmt.Sprintf("Users exported: %d", len(keycloakUsers)))
	EmitLog(m, "info", fmt.Sprintf("Groups exported: %d", len(keycloakGroups)))
	EmitLog(m, "info", fmt.Sprintf("Roles created: %d", len(keycloakRolesList)))

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", "Cognito to Keycloak migration completed successfully")
	EmitLog(m, "warn", "Remember: Users will need to reset their passwords as Cognito password hashes cannot be imported into Keycloak")

	return nil
}
