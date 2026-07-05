package cli

import (
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
)

func TestValidateStrictClickBamFailsOnManualMapperText(t *testing.T) {
	result := mapper.NewMappingResult("api")
	result.AddManualStep("Update application code")

	err := validateStrictClickBam([]*mapper.MappingResult{result})
	if err == nil {
		t.Fatal("validateStrictClickBam() error = nil, want strict failure")
	}
	if !strings.Contains(err.Error(), "unresolved manual text") {
		t.Fatalf("error = %q, want unresolved manual text", err.Error())
	}
}

func TestValidateStrictClickBamAllowsMappedResultWithoutManualText(t *testing.T) {
	result := mapper.NewMappingResult("api")

	if err := validateStrictClickBam([]*mapper.MappingResult{result}); err != nil {
		t.Fatalf("validateStrictClickBam() error = %v, want nil", err)
	}
}
