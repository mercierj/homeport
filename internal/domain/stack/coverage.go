package stack

import (
	"fmt"
	"sort"

	"github.com/homeport/homeport/internal/domain/resource"
)

// ═══════════════════════════════════════════════════════════════════════════════
// ResourceMapping - Complete mapping of all 84 resource types to stack types
// ═══════════════════════════════════════════════════════════════════════════════

// ResourceMapping maps every resource.Type constant to its corresponding StackType.
// This provides 100% coverage of all 84 resource types (30 AWS + 25 GCP + 29 Azure).
var ResourceMapping = map[resource.Type]StackType{
	// ─────────────────────────────────────────────────────
	// AWS Resource Types (30 types)
	// ─────────────────────────────────────────────────────

	// AWS Compute (5 types) - Passthrough except serverless
	resource.TypeEC2Instance:    StackTypePassthrough, // VMs stay individual
	resource.TypeLambdaFunction: StackTypeCompute,     // Serverless consolidates
	resource.TypeECSService:     StackTypePassthrough, // Container orchestration stays individual
	resource.TypeECSTaskDef:     StackTypePassthrough, // Container definitions stay individual
	resource.TypeEKSCluster:     StackTypePassthrough, // Kubernetes stays individual

	// AWS Storage (3 types)
	resource.TypeS3Bucket:  StackTypeStorage, // Object storage consolidates
	resource.TypeEBSVolume: StackTypePassthrough, // Block storage stays with VMs
	resource.TypeEFSVolume: StackTypeStorage, // File storage consolidates

	// AWS Database (4 types)
	resource.TypeRDSInstance:   StackTypeDatabase, // SQL databases consolidate
	resource.TypeRDSCluster:    StackTypeDatabase, // Aurora clusters consolidate
	resource.TypeDynamoDBTable: StackTypeDatabase, // NoSQL consolidates
	resource.TypeElastiCache:   StackTypeCache,    // Cache consolidates

	// AWS Networking (5 types) - All passthrough
	resource.TypeALB:         StackTypePassthrough, // Load balancers stay individual
	resource.TypeAPIGateway:  StackTypePassthrough, // API gateways stay individual
	resource.TypeRoute53Zone: StackTypePassthrough, // DNS stays individual
	resource.TypeCloudFront:  StackTypePassthrough, // CDN stays individual
	resource.TypeVPC:         StackTypePassthrough, // VPC stays individual

	// AWS Security (4 types)
	resource.TypeCognitoPool:    StackTypeAuth,        // Auth consolidates
	resource.TypeSecretsManager: StackTypeSecrets,     // Secrets consolidate
	resource.TypeIAMRole:        StackTypePassthrough, // IAM doesn't map to self-hosted
	resource.TypeACMCertificate: StackTypePassthrough, // Certificates handled separately

	// AWS Messaging (5 types)
	resource.TypeSQSQueue:    StackTypeMessaging, // Queue consolidates
	resource.TypeSNSTopic:    StackTypeMessaging, // PubSub consolidates
	resource.TypeEventBridge: StackTypeMessaging, // Event routing consolidates
	resource.TypeKinesis:     StackTypeMessaging, // Stream consolidates
	resource.TypeSESIdentity: StackTypeMessaging, // Email service consolidates

	// AWS Security additional (1 type)
	resource.TypeKMSKey: StackTypeSecrets, // Key management consolidates with secrets

	// AWS Monitoring (3 types)
	resource.TypeCloudWatchMetricAlarm: StackTypeObservability, // Monitoring consolidates
	resource.TypeCloudWatchLogGroup:    StackTypeObservability, // Logging consolidates
	resource.TypeCloudWatchDashboard:   StackTypeObservability, // Dashboards consolidate

	// ─────────────────────────────────────────────────────
	// GCP Resource Types (25 types)
	// ─────────────────────────────────────────────────────

	// GCP Compute (5 types) - Passthrough except serverless
	resource.TypeGCEInstance:   StackTypePassthrough, // VMs stay individual
	resource.TypeCloudRun:      StackTypePassthrough, // Container platform stays individual
	resource.TypeCloudFunction: StackTypeCompute,     // Serverless consolidates
	resource.TypeGKE:           StackTypePassthrough, // Kubernetes stays individual
	resource.TypeAppEngine:     StackTypePassthrough, // App Engine stays individual

	// GCP Storage (3 types)
	resource.TypeGCSBucket:      StackTypeStorage,     // Object storage consolidates
	resource.TypePersistentDisk: StackTypePassthrough, // Block storage stays with VMs
	resource.TypeFilestore:      StackTypeStorage,     // File storage consolidates

	// GCP Database (5 types)
	resource.TypeCloudSQL:    StackTypeDatabase, // SQL databases consolidate
	resource.TypeFirestore:   StackTypeDatabase, // NoSQL consolidates
	resource.TypeBigtable:    StackTypeDatabase, // Wide-column consolidates
	resource.TypeMemorystore: StackTypeCache,    // Cache consolidates
	resource.TypeSpanner:     StackTypeDatabase, // Distributed SQL consolidates

	// GCP Networking (5 types) - All passthrough
	resource.TypeCloudLB:       StackTypePassthrough, // Load balancers stay individual
	resource.TypeCloudDNS:      StackTypePassthrough, // DNS stays individual
	resource.TypeCloudCDN:      StackTypePassthrough, // CDN stays individual
	resource.TypeCloudArmor:    StackTypePassthrough, // Firewall stays individual
	resource.TypeGCPVPCNetwork: StackTypePassthrough, // VPC stays individual

	// GCP Security (3 types)
	resource.TypeIdentityPlatform: StackTypeAuth,        // Auth consolidates
	resource.TypeSecretManager:    StackTypeSecrets,     // Secrets consolidate
	resource.TypeGCPIAM:           StackTypePassthrough, // IAM doesn't map to self-hosted

	// GCP Messaging (4 types)
	resource.TypePubSubTopic:        StackTypeMessaging, // PubSub consolidates
	resource.TypePubSubSubscription: StackTypeMessaging, // Subscriptions consolidate
	resource.TypeCloudTasks:         StackTypeMessaging, // Task queues consolidate
	resource.TypeCloudScheduler:     StackTypeMessaging, // Scheduled jobs consolidate

	// ─────────────────────────────────────────────────────
	// Azure Resource Types (29 types)
	// ─────────────────────────────────────────────────────

	// Azure Compute (6 types) - Passthrough except serverless
	resource.TypeAzureVM:           StackTypePassthrough, // VMs stay individual
	resource.TypeAzureVMWindows:    StackTypePassthrough, // VMs stay individual
	resource.TypeAzureFunction:     StackTypeCompute,     // Serverless consolidates
	resource.TypeContainerInstance: StackTypePassthrough, // Container instances stay individual
	resource.TypeAKS:               StackTypePassthrough, // Kubernetes stays individual
	resource.TypeAppService:        StackTypePassthrough, // App Service stays individual

	// Azure Storage (4 types)
	resource.TypeBlobStorage:      StackTypeStorage,     // Object storage consolidates
	resource.TypeAzureStorageAcct: StackTypeStorage,     // Storage accounts consolidate
	resource.TypeManagedDisk:      StackTypePassthrough, // Block storage stays with VMs
	resource.TypeAzureFiles:       StackTypeStorage,     // File storage consolidates

	// Azure Database (5 types)
	resource.TypeAzureSQL:      StackTypeDatabase, // SQL Server consolidates
	resource.TypeAzurePostgres: StackTypeDatabase, // PostgreSQL consolidates
	resource.TypeAzureMySQL:    StackTypeDatabase, // MySQL consolidates
	resource.TypeCosmosDB:      StackTypeDatabase, // NoSQL consolidates
	resource.TypeAzureCache:    StackTypeCache,    // Cache consolidates

	// Azure Networking (6 types) - All passthrough
	resource.TypeAzureLB:       StackTypePassthrough, // Load balancers stay individual
	resource.TypeAppGateway:    StackTypePassthrough, // Application gateways stay individual
	resource.TypeAzureDNS:      StackTypePassthrough, // DNS stays individual
	resource.TypeAzureCDN:      StackTypePassthrough, // CDN stays individual
	resource.TypeFrontDoor:     StackTypePassthrough, // Front Door stays individual
	resource.TypeAzureVNet:     StackTypePassthrough, // VNet stays individual

	// Azure Security (3 types)
	resource.TypeAzureADB2C:   StackTypeAuth,        // Auth consolidates
	resource.TypeKeyVault:     StackTypeSecrets,     // Secrets consolidate
	resource.TypeAzureFirewall: StackTypePassthrough, // Firewall stays individual

	// Azure Messaging (5 types)
	resource.TypeServiceBus:      StackTypeMessaging, // Service Bus consolidates
	resource.TypeServiceBusQueue: StackTypeMessaging, // Queues consolidate
	resource.TypeEventHub:        StackTypeMessaging, // Event Hubs consolidate
	resource.TypeEventGrid:       StackTypeMessaging, // Event Grid consolidates
	resource.TypeLogicApp:        StackTypeMessaging, // Logic Apps consolidate (workflow/integration)
}

// GetStackTypeForResource returns the StackType for a given resource.Type.
// Returns the mapped StackType and true if found, or StackTypePassthrough and false if not found.
func GetStackTypeForResource(resourceType resource.Type) (StackType, bool) {
	if stackType, ok := ResourceMapping[resourceType]; ok {
		return stackType, true
	}
	return StackTypePassthrough, false
}

// GetStackTypeForResourceString returns the StackType for a resource type string.
// This is useful when you have just the type string without a resource.Type constant.
func GetStackTypeForResourceString(resourceTypeStr string) (StackType, bool) {
	return GetStackTypeForResource(resource.Type(resourceTypeStr))
}

// ValidateMappingCoverage verifies that all resource types in resource.CategoryMapping
// have a corresponding entry in ResourceMapping. Returns an error if any are missing.
func ValidateMappingCoverage() error {
	var missing []string

	for resType := range resource.CategoryMapping {
		if _, ok := ResourceMapping[resType]; !ok {
			missing = append(missing, string(resType))
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("missing mappings for %d resource types: %v", len(missing), missing)
	}

	return nil
}

// GetCoverageStats returns statistics about the ResourceMapping coverage.
type CoverageStats struct {
	TotalTypes         int
	CoveredTypes       int
	UncoveredTypes     []string
	CoveragePercentage float64
	ByStackType        map[StackType][]string
	ByProvider         map[resource.Provider]int
}

// GetResourceMappingStats computes coverage statistics for the ResourceMapping.
func GetResourceMappingStats() *CoverageStats {
	stats := &CoverageStats{
		UncoveredTypes: make([]string, 0),
		ByStackType:    make(map[StackType][]string),
		ByProvider:     make(map[resource.Provider]int),
	}

	// Initialize ByStackType for all stack types
	for _, st := range AllStackTypes() {
		stats.ByStackType[st] = make([]string, 0)
	}

	// Count types from CategoryMapping (the source of truth)
	for resType := range resource.CategoryMapping {
		stats.TotalTypes++

		provider := resType.Provider()
		stats.ByProvider[provider]++

		if stackType, ok := ResourceMapping[resType]; ok {
			stats.CoveredTypes++
			stats.ByStackType[stackType] = append(stats.ByStackType[stackType], string(resType))
		} else {
			stats.UncoveredTypes = append(stats.UncoveredTypes, string(resType))
		}
	}

	if stats.TotalTypes > 0 {
		stats.CoveragePercentage = float64(stats.CoveredTypes) / float64(stats.TotalTypes) * 100
	}

	// Sort for consistent output
	sort.Strings(stats.UncoveredTypes)
	for st := range stats.ByStackType {
		sort.Strings(stats.ByStackType[st])
	}

	return stats
}

// AllResourceTypes returns all resource types that have a stack mapping.
func AllResourceTypes() []resource.Type {
	types := make([]resource.Type, 0, len(ResourceMapping))
	for t := range ResourceMapping {
		types = append(types, t)
	}
	return types
}

// GetResourceTypesForStack returns all resource types that map to a specific stack type.
func GetResourceTypesForStack(stackType StackType) []resource.Type {
	var types []resource.Type
	for resType, st := range ResourceMapping {
		if st == stackType {
			types = append(types, resType)
		}
	}
	return types
}

// GetResourceTypesForStackByProvider returns resource types for a stack type, filtered by provider.
func GetResourceTypesForStackByProvider(stackType StackType, provider resource.Provider) []resource.Type {
	var types []resource.Type
	for resType, st := range ResourceMapping {
		if st == stackType && resType.Provider() == provider {
			types = append(types, resType)
		}
	}
	return types
}

// ═══════════════════════════════════════════════════════════════════════════════
// CoverageReport - Legacy compatibility layer
// ═══════════════════════════════════════════════════════════════════════════════

// CoverageReport shows which resource types map to which stacks.
// It helps validate that all known resource types are accounted for.
type CoverageReport struct {
	// TotalTypes is the count of all known resource types
	TotalTypes int

	// CoveredTypes is the count of types that have a mapping
	CoveredTypes int

	// UncoveredTypes lists resource types without a mapping
	UncoveredTypes []string

	// ByStack maps stack types to their assigned resource types
	ByStack map[StackType][]string

	// ByCategory maps categories to their resource types
	ByCategory map[resource.Category][]string

	// CoveragePercentage is the percentage of types that are covered
	CoveragePercentage float64
}

// ValidateCoverage checks that all known resource types are mapped.
// Returns a report showing coverage statistics and any gaps.
func ValidateCoverage(mapping *ResourceStackMapping, knownTypes []string) *CoverageReport {
	report := &CoverageReport{
		TotalTypes:     len(knownTypes),
		UncoveredTypes: make([]string, 0),
		ByStack:        make(map[StackType][]string),
		ByCategory:     make(map[resource.Category][]string),
	}

	// Initialize ByStack for all stack types
	for _, st := range AllStackTypes() {
		report.ByStack[st] = make([]string, 0)
	}

	coveredCount := 0

	for _, typeStr := range knownTypes {
		stackType := mapping.GetStackTypeForString(typeStr)

		// Check if we have any mapping for this type
		hasCategoryMapping := false
		hasTypeMapping := false
		isPassthrough := mapping.PassthroughTypes[typeStr]

		// Check ByType
		if _, ok := mapping.ByType[typeStr]; ok {
			hasTypeMapping = true
		}

		// Check ByCategory
		resType := resource.Type(typeStr)
		category := resType.GetCategory()
		if _, ok := mapping.ByCategory[category]; ok {
			hasCategoryMapping = true
		}

		// Track by category
		report.ByCategory[category] = append(report.ByCategory[category], typeStr)

		// A type is "covered" if it has any mapping (explicit, category-based, or passthrough)
		isCovered := hasTypeMapping || hasCategoryMapping || isPassthrough

		if isCovered {
			coveredCount++
			report.ByStack[stackType] = append(report.ByStack[stackType], typeStr)
		} else {
			report.UncoveredTypes = append(report.UncoveredTypes, typeStr)
			// Still add to passthrough by default
			report.ByStack[StackTypePassthrough] = append(report.ByStack[StackTypePassthrough], typeStr)
		}
	}

	report.CoveredTypes = coveredCount

	if report.TotalTypes > 0 {
		report.CoveragePercentage = float64(report.CoveredTypes) / float64(report.TotalTypes) * 100
	}

	// Sort all slices for consistent output
	sort.Strings(report.UncoveredTypes)
	for st := range report.ByStack {
		sort.Strings(report.ByStack[st])
	}
	for cat := range report.ByCategory {
		sort.Strings(report.ByCategory[cat])
	}

	return report
}

// GetKnownResourceTypes returns all resource types the system knows about.
// This aggregates all types defined in the resource package.
func GetKnownResourceTypes() []string {
	types := make([]string, 0)

	// Add all types from the CategoryMapping
	for t := range resource.CategoryMapping {
		types = append(types, string(t))
	}

	sort.Strings(types)
	return types
}

// GetKnownResourceTypesByProvider returns resource types grouped by provider.
func GetKnownResourceTypesByProvider() map[resource.Provider][]string {
	result := make(map[resource.Provider][]string)

	for t := range resource.CategoryMapping {
		provider := t.Provider()
		result[provider] = append(result[provider], string(t))
	}

	// Sort each provider's types
	for provider := range result {
		sort.Strings(result[provider])
	}

	return result
}

// IsFullyCovered returns true if all known types have a mapping.
func (r *CoverageReport) IsFullyCovered() bool {
	return len(r.UncoveredTypes) == 0
}

// GetStackSummary returns a summary of types per stack.
func (r *CoverageReport) GetStackSummary() map[StackType]int {
	summary := make(map[StackType]int)
	for st, types := range r.ByStack {
		summary[st] = len(types)
	}
	return summary
}

// GetCategorySummary returns a summary of types per category.
func (r *CoverageReport) GetCategorySummary() map[resource.Category]int {
	summary := make(map[resource.Category]int)
	for cat, types := range r.ByCategory {
		summary[cat] = len(types)
	}
	return summary
}

// MappingStats provides statistics about a ResourceStackMapping.
type MappingStats struct {
	// TotalTypeOverrides is the count of type-specific overrides
	TotalTypeOverrides int

	// TotalCategoryMappings is the count of category mappings
	TotalCategoryMappings int

	// TotalPassthroughTypes is the count of passthrough types
	TotalPassthroughTypes int

	// TypesPerStack maps stack types to their override count
	TypesPerStack map[StackType]int
}

// GetMappingStats returns statistics about the mapping configuration.
func GetMappingStats(mapping *ResourceStackMapping) *MappingStats {
	stats := &MappingStats{
		TotalTypeOverrides:    len(mapping.ByType),
		TotalCategoryMappings: len(mapping.ByCategory),
		TotalPassthroughTypes: len(mapping.PassthroughTypes),
		TypesPerStack:         make(map[StackType]int),
	}

	// Count type overrides per stack
	for _, st := range mapping.ByType {
		stats.TypesPerStack[st]++
	}

	return stats
}

// ValidateDefaultMapping validates the default mapping against known types.
// This is useful for testing that the mapping is complete.
func ValidateDefaultMapping() *CoverageReport {
	mapping := DefaultMapping()
	knownTypes := GetKnownResourceTypes()
	return ValidateCoverage(mapping, knownTypes)
}

// GetUnmappedCategories returns categories that don't have a stack mapping.
func GetUnmappedCategories(mapping *ResourceStackMapping) []resource.Category {
	var unmapped []resource.Category

	// List of all categories we care about
	allCategories := []resource.Category{
		resource.CategoryCompute,
		resource.CategoryContainer,
		resource.CategoryServerless,
		resource.CategoryKubernetes,
		resource.CategoryObjectStorage,
		resource.CategoryBlockStorage,
		resource.CategoryFileStorage,
		resource.CategorySQLDatabase,
		resource.CategoryNoSQLDatabase,
		resource.CategoryCache,
		resource.CategoryQueue,
		resource.CategoryPubSub,
		resource.CategoryStream,
		resource.CategoryLoadBalancer,
		resource.CategoryCDN,
		resource.CategoryDNS,
		resource.CategoryAPIGateway,
		resource.CategoryVPC,
		resource.CategoryAuth,
		resource.CategorySecrets,
		resource.CategoryIAM,
		resource.CategoryFirewall,
		resource.CategoryCertificate,
		resource.CategoryMonitoring,
		resource.CategoryLogging,
		resource.CategoryTracing,
	}

	for _, cat := range allCategories {
		if _, ok := mapping.ByCategory[cat]; !ok {
			unmapped = append(unmapped, cat)
		}
	}

	return unmapped
}
