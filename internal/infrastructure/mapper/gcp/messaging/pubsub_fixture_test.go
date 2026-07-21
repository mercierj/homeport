package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestPubSubFixtureMapsToNATSJetStreamAdapterPath(t *testing.T) {
	result, err := NewPubSubMapper().Map(context.Background(), &resource.AWSResource{
		ID:   "topic-1",
		Type: resource.TypePubSubTopic,
		Name: "events",
		Config: map[string]interface{}{
			"name":                     "events",
			"message_ordering_enabled": true,
		},
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	if result.DockerService == nil || result.DockerService.Image != "nats:2.10-alpine" {
		t.Fatalf("DockerService = %#v", result.DockerService)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("ManualSteps = %#v, want generated NATS JetStream handoff", result.ManualSteps)
	}
	appEnv := string(result.Configs["config/pubsub/app-change.env"])
	if !strings.Contains(appEnv, "APP_CHANGE_MODE=adapter") || !strings.Contains(appEnv, "HOMEPORT_COMPAT_BACKEND=nats-jetstream") {
		t.Fatalf("app-change env missing NATS JetStream adapter target:\n%s", appEnv)
	}
}
