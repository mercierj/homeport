package cutover

import "testing"

func TestBuildPreviewCreatesDNSAndHealthChecks(t *testing.T) {
	preview := BuildPreview(PreviewInput{BundleID: "b1", Domain: "example.com", TargetIP: "203.0.113.10"})
	if len(preview.DNSChanges) != 1 || preview.DNSChanges[0].NewValue != "203.0.113.10" {
		t.Fatalf("unexpected dns changes: %#v", preview.DNSChanges)
	}
	if len(preview.PostChecks) != 1 || preview.PostChecks[0].Endpoint != "https://example.com/health" {
		t.Fatalf("unexpected post checks: %#v", preview.PostChecks)
	}
}

func TestBuildPreviewWarnsWhenMissingInputs(t *testing.T) {
	preview := BuildPreview(PreviewInput{})
	if len(preview.Warnings) != 2 {
		t.Fatalf("warnings = %#v", preview.Warnings)
	}
}
