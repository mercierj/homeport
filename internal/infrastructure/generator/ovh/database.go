// Package ovh generates Terraform configurations for OVHcloud deployments.
package ovh

// OVH managed database engines
const (
	EnginePostgreSQL = "postgresql"
	EngineMySQL      = "mysql"
	EngineMongoDB    = "mongodb"
	EngineRedis      = "redis"
	EngineKafka      = "kafka"
	EngineCassandra  = "cassandra"
	EngineM3DB       = "m3db"
	EngineOpensearch = "opensearch"
	EngineGrafana    = "grafana"
)

// OVH database plans
const (
	PlanEssential  = "essential"  // Single node, development
	PlanBusiness   = "business"   // HA, production
	PlanEnterprise = "enterprise" // Advanced HA, large scale
)
