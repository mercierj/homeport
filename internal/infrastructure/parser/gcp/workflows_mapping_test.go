package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestWorkflowsTerraformTypeMapsToResource(t *testing.T) {
	if got := mapGCPTerraformType("google_workflows_workflow"); got != resource.TypeWorkflowsWorkflow {
		t.Fatalf("google_workflows_workflow maps to %s, want %s", got, resource.TypeWorkflowsWorkflow)
	}
}

func TestWorkflowsDeploymentManagerTypeMapsToResource(t *testing.T) {
	if got := mapDMTypeToResourceType("workflows.v1.workflow"); got != resource.TypeWorkflowsWorkflow {
		t.Fatalf("workflows.v1.workflow maps to %s, want %s", got, resource.TypeWorkflowsWorkflow)
	}
}
