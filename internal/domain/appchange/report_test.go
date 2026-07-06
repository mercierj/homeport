package appchange

import "testing"

func TestRequiresActionIgnoresAdapterOnlyChanges(t *testing.T) {
	report := Report{Changes: []Change{{Service: "S3", Mode: ModeAdapter, AdapterURL: "http://minio:9000"}}}
	if report.RequiresAction() {
		t.Fatal("adapter-only report should not require customer code action")
	}
}

func TestRequiresActionDetectsGeneratedPatch(t *testing.T) {
	report := Report{Changes: []Change{{Service: "Cloud Storage", Mode: ModeGeneratedPatch, File: ".env"}}}
	if !report.RequiresAction() {
		t.Fatal("generated patch should require customer action")
	}
}
