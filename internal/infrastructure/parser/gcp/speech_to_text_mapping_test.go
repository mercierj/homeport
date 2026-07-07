package gcp

import (
	"testing"

	"github.com/homeport/homeport/internal/domain/resource"
)

func TestSpeechToTextTerraformTypesMapToResources(t *testing.T) {
	tests := map[string]resource.Type{
		"google_speech_custom_class": resource.TypeSpeechCustomClass,
		"google_speech_phrase_set":   resource.TypeSpeechPhraseSet,
	}
	for input, want := range tests {
		if got := mapGCPTerraformType(input); got != want {
			t.Fatalf("%s maps to %s, want %s", input, got, want)
		}
	}
}

func TestSpeechToTextDeploymentManagerTypesMapToResources(t *testing.T) {
	tests := map[string]resource.Type{
		"speech.v1.customclass": resource.TypeSpeechCustomClass,
		"speech.v1.phraseset":   resource.TypeSpeechPhraseSet,
	}
	for input, want := range tests {
		if got := mapDMTypeToResourceType(input); got != want {
			t.Fatalf("%s maps to %s, want %s", input, got, want)
		}
	}
}
