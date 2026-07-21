// Package awsoperations contains the post-cutover read model for AWS resources.
package awsoperations

import "time"

type ServiceKey string

const (
	ServiceLambda ServiceKey = "lambda"
	ServiceSQS    ServiceKey = "sqs"
)

type ServiceStatus string

const (
	ServiceStatusAvailable   ServiceStatus = "available"
	ServiceStatusUnavailable ServiceStatus = "unavailable"
	ServiceStatusDegraded    ServiceStatus = "degraded"
)

type Capability string

const (
	CapabilityList   Capability = "list"
	CapabilityRead   Capability = "read"
	CapabilityCreate Capability = "create"
	CapabilityUpdate Capability = "update"
	CapabilityDelete Capability = "delete"
	CapabilityInvoke Capability = "invoke"
	CapabilityLogs   Capability = "logs"
	CapabilityPurge  Capability = "purge"
	CapabilityRetry  Capability = "retry"
)

type ResourceBinding struct {
	ImportedResourceID string            `json:"imported_resource_id"`
	Service            ServiceKey        `json:"service"`
	LocalResourceID    string            `json:"local_resource_id"`
	LocalStackID       string            `json:"local_stack_id"`
	Name               string            `json:"name"`
	Region             string            `json:"region,omitempty"`
	Tags               map[string]string `json:"tags,omitempty"`
}

// LocalResourceBinding is produced by trusted deployment/cutover code after a
// local resource exists. It is deliberately not an HTTP request model.
type LocalResourceBinding struct {
	ImportedResourceID string
	LocalResourceID    string
	LocalStackID       string
}

type Workspace struct {
	ID                 string                      `json:"id"`
	DiscoveryID        string                      `json:"discovery_id"`
	Name               string                      `json:"name"`
	Provider           string                      `json:"provider"`
	CutoverCompletedAt time.Time                   `json:"cutover_completed_at"`
	Services           map[ServiceKey]ServiceState `json:"services"`
	Bindings           []ResourceBinding           `json:"bindings"`
}

type ServiceState struct {
	Status       ServiceStatus `json:"status"`
	Capabilities []Capability  `json:"capabilities"`
	Reason       string        `json:"reason,omitempty"`
}
