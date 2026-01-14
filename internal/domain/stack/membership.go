package stack

import (
	"github.com/homeport/homeport/internal/domain/resource"
)

// ResourceStackMapping defines how cloud resources map to stack types.
// It provides a flexible way to determine which stack a resource belongs to.
type ResourceStackMapping struct {
	// ByCategory maps resource categories to stack types.
	// This is the default mapping used when no specific type override exists.
	ByCategory map[resource.Category]StackType

	// ByType maps specific resource types to stack types.
	// These override the category-based mapping for fine-grained control.
	ByType map[string]StackType

	// PassthroughTypes are resource types that don't consolidate.
	// These remain as individual services in the output.
	PassthroughTypes map[string]bool
}

// DefaultMapping returns the standard resource-to-stack mapping.
// This mapping consolidates cloud resources into logical stacks based on function.
func DefaultMapping() *ResourceStackMapping {
	return &ResourceStackMapping{
		ByCategory: map[resource.Category]StackType{
			// Compute categories - mostly passthrough, but serverless consolidates
			resource.CategoryCompute:    StackTypePassthrough,
			resource.CategoryContainer:  StackTypePassthrough,
			resource.CategoryServerless: StackTypeCompute,
			resource.CategoryKubernetes: StackTypePassthrough,

			// Storage categories
			resource.CategoryObjectStorage: StackTypeStorage,
			resource.CategoryBlockStorage:  StackTypePassthrough, // Block storage stays with VMs
			resource.CategoryFileStorage:   StackTypeStorage,

			// Database categories
			resource.CategorySQLDatabase:   StackTypeDatabase,
			resource.CategoryNoSQLDatabase: StackTypeDatabase,
			resource.CategoryCache:         StackTypeCache,

			// Messaging categories
			resource.CategoryQueue:  StackTypeMessaging,
			resource.CategoryPubSub: StackTypeMessaging,
			resource.CategoryStream: StackTypeMessaging,

			// Networking categories - generally passthrough
			resource.CategoryLoadBalancer: StackTypePassthrough,
			resource.CategoryCDN:          StackTypePassthrough,
			resource.CategoryDNS:          StackTypePassthrough,
			resource.CategoryAPIGateway:   StackTypePassthrough,
			resource.CategoryVPC:          StackTypePassthrough,

			// Security categories
			resource.CategoryAuth:        StackTypeAuth,
			resource.CategorySecrets:     StackTypeSecrets,
			resource.CategoryIAM:         StackTypePassthrough, // IAM doesn't map to self-hosted
			resource.CategoryFirewall:    StackTypePassthrough,
			resource.CategoryCertificate: StackTypePassthrough,

			// Monitoring categories
			resource.CategoryMonitoring: StackTypeObservability,
			resource.CategoryLogging:    StackTypeObservability,
			resource.CategoryTracing:    StackTypeObservability,

			// Aggregate categories
			resource.CategoryMessaging:  StackTypeMessaging,
			resource.CategoryNetworking: StackTypePassthrough,
			resource.CategorySecurity:   StackTypeSecrets,
			resource.CategoryIdentity:   StackTypeAuth,
		},

		ByType: map[string]StackType{
			// ═══════════════════════════════════════════════════════
			// AWS Type Overrides
			// ═══════════════════════════════════════════════════════

			// Auth overrides (from Security category)
			"aws_cognito_user_pool":        StackTypeAuth,
			"aws_cognito_identity_pool":    StackTypeAuth,
			"aws_cognito_user_pool_client": StackTypeAuth,

			// Cache overrides (already in category, but explicit)
			"aws_elasticache_cluster":           StackTypeCache,
			"aws_elasticache_replication_group": StackTypeCache,

			// Observability overrides
			"aws_cloudwatch_log_group":     StackTypeObservability,
			"aws_cloudwatch_metric_alarm":  StackTypeObservability,
			"aws_cloudwatch_dashboard":     StackTypeObservability,
			"aws_cloudwatch_event_rule":    StackTypeObservability, // EventBridge for observability
			"aws_cloudwatch_event_target":  StackTypeObservability,
			"aws_xray_sampling_rule":       StackTypeObservability,
			"aws_xray_encryption_config":   StackTypeObservability,

			// Secrets overrides
			"aws_secretsmanager_secret":         StackTypeSecrets,
			"aws_secretsmanager_secret_version": StackTypeSecrets,
			"aws_ssm_parameter":                 StackTypeSecrets,

			// ═══════════════════════════════════════════════════════
			// GCP Type Overrides
			// ═══════════════════════════════════════════════════════

			// Auth overrides
			"google_identity_platform_config":               StackTypeAuth,
			"google_identity_platform_tenant":               StackTypeAuth,
			"google_identity_platform_default_supported_idp_config": StackTypeAuth,

			// Cache overrides
			"google_redis_instance": StackTypeCache,

			// Observability overrides
			"google_monitoring_alert_policy":      StackTypeObservability,
			"google_monitoring_notification_channel": StackTypeObservability,
			"google_monitoring_uptime_check_config": StackTypeObservability,
			"google_monitoring_dashboard":         StackTypeObservability,
			"google_logging_metric":               StackTypeObservability,
			"google_logging_project_sink":         StackTypeObservability,

			// Secrets overrides
			"google_secret_manager_secret":         StackTypeSecrets,
			"google_secret_manager_secret_version": StackTypeSecrets,

			// ═══════════════════════════════════════════════════════
			// Azure Type Overrides
			// ═══════════════════════════════════════════════════════

			// Auth overrides
			"azurerm_aadb2c_directory": StackTypeAuth,
			"azuread_application":      StackTypeAuth,
			"azuread_service_principal": StackTypeAuth,

			// Cache overrides
			"azurerm_redis_cache":         StackTypeCache,
			"azurerm_redis_enterprise_cluster": StackTypeCache,

			// Observability overrides
			"azurerm_monitor_action_group":       StackTypeObservability,
			"azurerm_monitor_metric_alert":       StackTypeObservability,
			"azurerm_monitor_activity_log_alert": StackTypeObservability,
			"azurerm_monitor_scheduled_query_rules_alert": StackTypeObservability,
			"azurerm_log_analytics_workspace":    StackTypeObservability,
			"azurerm_application_insights":       StackTypeObservability,
			"azurerm_monitor_diagnostic_setting": StackTypeObservability,

			// Secrets overrides
			"azurerm_key_vault":        StackTypeSecrets,
			"azurerm_key_vault_secret": StackTypeSecrets,
			"azurerm_key_vault_key":    StackTypeSecrets,
		},

		PassthroughTypes: map[string]bool{
			// ═══════════════════════════════════════════════════════
			// AWS Passthrough Types
			// ═══════════════════════════════════════════════════════

			// Compute - stays as individual services
			"aws_instance":              true, // EC2
			"aws_spot_instance_request": true,
			"aws_launch_template":       true,
			"aws_autoscaling_group":     true,

			// Container orchestration - stays as individual services
			"aws_ecs_service":          true,
			"aws_ecs_task_definition":  true,
			"aws_ecs_cluster":          true,
			"aws_eks_cluster":          true,
			"aws_eks_node_group":       true,

			// Networking - handled separately
			"aws_vpc":                     true,
			"aws_subnet":                  true,
			"aws_security_group":          true,
			"aws_lb":                      true,
			"aws_lb_listener":             true,
			"aws_lb_target_group":         true,
			"aws_api_gateway_rest_api":    true,
			"aws_route53_zone":            true,
			"aws_route53_record":          true,
			"aws_cloudfront_distribution": true,

			// Block storage - stays with compute
			"aws_ebs_volume":     true,
			"aws_volume_attachment": true,

			// ═══════════════════════════════════════════════════════
			// GCP Passthrough Types
			// ═══════════════════════════════════════════════════════

			// Compute
			"google_compute_instance":          true,
			"google_compute_instance_template": true,
			"google_compute_instance_group_manager": true,

			// Container orchestration
			"google_container_cluster":   true, // GKE
			"google_container_node_pool": true,
			"google_cloud_run_service":   true,

			// Networking
			"google_compute_network":           true,
			"google_compute_subnetwork":        true,
			"google_compute_firewall":          true,
			"google_compute_backend_service":   true,
			"google_compute_url_map":           true,
			"google_compute_target_http_proxy": true,
			"google_compute_global_forwarding_rule": true,
			"google_dns_managed_zone":          true,
			"google_dns_record_set":            true,

			// Block storage
			"google_compute_disk":       true,
			"google_compute_attached_disk": true,

			// ═══════════════════════════════════════════════════════
			// Azure Passthrough Types
			// ═══════════════════════════════════════════════════════

			// Compute
			"azurerm_linux_virtual_machine":     true,
			"azurerm_windows_virtual_machine":   true,
			"azurerm_virtual_machine":           true,
			"azurerm_virtual_machine_scale_set": true,

			// Container orchestration
			"azurerm_kubernetes_cluster":   true, // AKS
			"azurerm_container_group":      true,
			"azurerm_container_registry":   true,

			// Networking
			"azurerm_virtual_network":       true,
			"azurerm_subnet":                true,
			"azurerm_network_security_group": true,
			"azurerm_lb":                    true,
			"azurerm_application_gateway":  true,
			"azurerm_dns_zone":              true,
			"azurerm_dns_a_record":          true,
			"azurerm_cdn_profile":           true,
			"azurerm_frontdoor":             true,

			// Block storage
			"azurerm_managed_disk": true,
		},
	}
}

// GetStackType returns the stack type for a given resource.
// Resolution order:
// 1. Check if type is in PassthroughTypes -> StackTypePassthrough
// 2. Check if type is in ByType overrides -> use override
// 3. Check resource category in ByCategory -> use category mapping
// 4. Default to StackTypePassthrough
func (m *ResourceStackMapping) GetStackType(res *resource.Resource) StackType {
	if res == nil {
		return StackTypePassthrough
	}

	typeStr := res.Type.String()

	// Check passthrough first
	if m.IsPassthrough(res) {
		return StackTypePassthrough
	}

	// Check type-specific override
	if stackType, ok := m.ByType[typeStr]; ok {
		return stackType
	}

	// Check category mapping
	category := res.Type.GetCategory()
	if stackType, ok := m.ByCategory[category]; ok {
		return stackType
	}

	// Default to passthrough
	return StackTypePassthrough
}

// IsPassthrough returns true if the resource should not be consolidated.
// Passthrough resources become individual Docker services.
func (m *ResourceStackMapping) IsPassthrough(res *resource.Resource) bool {
	if res == nil {
		return true
	}

	typeStr := res.Type.String()
	return m.PassthroughTypes[typeStr]
}

// GetStackTypeForString returns the stack type for a resource type string.
// This is useful when you have just the type string without a full resource.
func (m *ResourceStackMapping) GetStackTypeForString(resourceType string) StackType {
	// Check passthrough first
	if m.PassthroughTypes[resourceType] {
		return StackTypePassthrough
	}

	// Check type-specific override
	if stackType, ok := m.ByType[resourceType]; ok {
		return stackType
	}

	// Try to infer category from resource type
	resType := resource.Type(resourceType)
	category := resType.GetCategory()
	if stackType, ok := m.ByCategory[category]; ok {
		return stackType
	}

	return StackTypePassthrough
}

// AddTypeOverride adds or updates a type-specific mapping.
func (m *ResourceStackMapping) AddTypeOverride(resourceType string, stackType StackType) {
	if m.ByType == nil {
		m.ByType = make(map[string]StackType)
	}
	m.ByType[resourceType] = stackType
}

// AddCategoryMapping adds or updates a category mapping.
func (m *ResourceStackMapping) AddCategoryMapping(category resource.Category, stackType StackType) {
	if m.ByCategory == nil {
		m.ByCategory = make(map[resource.Category]StackType)
	}
	m.ByCategory[category] = stackType
}

// AddPassthroughType marks a resource type as passthrough.
func (m *ResourceStackMapping) AddPassthroughType(resourceType string) {
	if m.PassthroughTypes == nil {
		m.PassthroughTypes = make(map[string]bool)
	}
	m.PassthroughTypes[resourceType] = true
}

// RemovePassthroughType removes a resource type from passthrough list.
func (m *ResourceStackMapping) RemovePassthroughType(resourceType string) {
	delete(m.PassthroughTypes, resourceType)
}

// GetTypesForStack returns all resource types that map to a given stack type.
func (m *ResourceStackMapping) GetTypesForStack(stackType StackType) []string {
	var types []string

	// Add types from ByType
	for t, st := range m.ByType {
		if st == stackType && !m.PassthroughTypes[t] {
			types = append(types, t)
		}
	}

	return types
}

// GetCategoriesForStack returns all categories that map to a given stack type.
func (m *ResourceStackMapping) GetCategoriesForStack(stackType StackType) []resource.Category {
	var categories []resource.Category

	for cat, st := range m.ByCategory {
		if st == stackType {
			categories = append(categories, cat)
		}
	}

	return categories
}

// Clone creates a deep copy of the mapping.
func (m *ResourceStackMapping) Clone() *ResourceStackMapping {
	clone := &ResourceStackMapping{
		ByCategory:       make(map[resource.Category]StackType),
		ByType:           make(map[string]StackType),
		PassthroughTypes: make(map[string]bool),
	}

	for k, v := range m.ByCategory {
		clone.ByCategory[k] = v
	}
	for k, v := range m.ByType {
		clone.ByType[k] = v
	}
	for k, v := range m.PassthroughTypes {
		clone.PassthroughTypes[k] = v
	}

	return clone
}
