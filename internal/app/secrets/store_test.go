package secrets

import (
	"os"
	"strings"
	"testing"
)

func TestStorePersistsEncryptedSecret(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	if err := store.Put("bundle-1", "DB_PASSWORD", "secret"); err != nil {
		t.Fatal(err)
	}
	value, ok, err := NewStore(dir).Get("bundle-1", "DB_PASSWORD")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || value != "secret" {
		t.Fatalf("value = %q ok = %v", value, ok)
	}
	data, err := os.ReadFile(dir + "/.homeport/secrets.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatalf("secret stored in plaintext: %s", data)
	}
}
