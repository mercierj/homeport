package clouddeploy

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestUnzipTerraformWritesMainTF(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("terraform/main.tf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("terraform {}")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := unzipTerraform(buf.Bytes(), dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "terraform", "main.tf")); err != nil {
		t.Fatal(err)
	}
}
