# Secret Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace in-memory bundle secret resolution with restart-safe encrypted persistence and runbook integration.

**Architecture:** Keep secret values out of bundle JSON responses. Store encrypted secret values in `.homeport/secrets.json` using an app-local key file. `ProvideSecrets` and `PullSecrets` write through the store, and runbook credentials pass only when required secrets have non-empty stored values.

**Tech Stack:** Go stdlib `crypto/aes`, `crypto/cipher`, `crypto/rand`, existing bundle handler, existing runbook service.

---

## Files

- Create: `internal/app/secrets/store.go`
- Create: `internal/app/secrets/store_test.go`
- Modify: `internal/api/handlers/bundle.go`
- Modify: `internal/api/handlers/bundle_runbook_test.go`
- Modify: `web/src/components/MigrationWizard/steps/SecretsStep.tsx`

## Task 1: Add encrypted store

- [ ] Create `internal/app/secrets/store.go`:

```go
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type Store struct {
	baseDir string
	mu      sync.Mutex
}

type state struct {
	Values map[string]map[string]string `json:"values"`
}

func NewStore(baseDir string) *Store {
	if baseDir == "" {
		baseDir = "."
	}
	return &Store{baseDir: baseDir}
}

func (s *Store) Put(bundleID, name, value string) error {
	if bundleID == "" || name == "" {
		return fmt.Errorf("bundle id and secret name are required")
	}
	st, err := s.load()
	if err != nil {
		return err
	}
	encrypted, err := s.encrypt([]byte(value))
	if err != nil {
		return err
	}
	s.mu.Lock()
	if st.Values[bundleID] == nil {
		st.Values[bundleID] = map[string]string{}
	}
	st.Values[bundleID][name] = encrypted
	s.mu.Unlock()
	return s.save(st)
}

func (s *Store) Get(bundleID, name string) (string, bool, error) {
	st, err := s.load()
	if err != nil {
		return "", false, err
	}
	s.mu.Lock()
	encrypted := st.Values[bundleID][name]
	s.mu.Unlock()
	if encrypted == "" {
		return "", false, nil
	}
	plain, err := s.decrypt(encrypted)
	if err != nil {
		return "", false, err
	}
	return string(plain), true, nil
}

func (s *Store) path() string {
	return filepath.Join(s.baseDir, ".homeport", "secrets.json")
}

func (s *Store) keyPath() string {
	return filepath.Join(s.baseDir, ".homeport", "secrets.key")
}

func (s *Store) load() (state, error) {
	st := state{Values: map[string]map[string]string{}}
	data, err := os.ReadFile(s.path())
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, err
	}
	if st.Values == nil {
		st.Values = map[string]map[string]string{}
	}
	return st, nil
}

func (s *Store) save(st state) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(s.path(), data, 0o600)
}

func (s *Store) key() ([]byte, error) {
	data, err := os.ReadFile(s.keyPath())
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(s.keyPath()), 0o700); err != nil {
		return nil, err
	}
	return key, os.WriteFile(s.keyPath(), key, 0o600)
}

func (s *Store) encrypt(plain []byte) (string, error) {
	key, err := s.key()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plain, nil)), nil
}

func (s *Store) decrypt(encoded string) ([]byte, error) {
	key, err := s.key()
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, fmt.Errorf("encrypted value is too short")
	}
	return gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
}
```

- [ ] Create `internal/app/secrets/store_test.go`:

```go
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
```

- [ ] Run `go test ./internal/app/secrets -run Store`.
Expected: pass.

## Task 2: Wire bundle handler to the store

- [ ] Modify `internal/api/handlers/bundle.go`:
  - Add `secretStore *appsecrets.Store` to `BundleHandler`.
  - Initialize it with `appsecrets.NewStore(".")`.
  - In `ProvideSecrets`, replace writes to `h.secretValues` with `h.secretStore.Put(bundleID, k, v)`.
  - In the missing/resolved loop, call `h.secretStore.Get(bundleID, secret.Name)` and trim the returned value.
  - Keep `secretValues` only if tests still need it; otherwise delete it and its mutex.

- [ ] Add regression to `internal/api/handlers/bundle_runbook_test.go`:

```go
func TestProvideSecretsSurvivesHandlerRestart(t *testing.T) {
	previousDir, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	handler := NewBundleHandler()
	handler.bundles["bundle-1"] = &BundleInfo{ID: "bundle-1", Secrets: []*SecretRef{{Name: "DB_PASSWORD", Required: true}}}
	if err := apprunbook.NewService(".").Save(buildBundleRunbook("bundle-1", true, handler.bundles["bundle-1"].Secrets)); err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBufferString(`{"secrets":{"DB_PASSWORD":"secret"}}`)
	req := httptest.NewRequest(http.MethodPost, "/bundles/bundle-1/secrets", body)
	rec := httptest.NewRecorder()
	router := chi.NewRouter()
	router.Post("/bundles/{bundleId}/secrets", handler.ProvideSecrets)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	restarted := NewBundleHandler()
	restarted.bundles["bundle-1"] = handler.bundles["bundle-1"]
	req = httptest.NewRequest(http.MethodPost, "/bundles/bundle-1/secrets", bytes.NewBufferString(`{"secrets":{}}`))
	rec = httptest.NewRecorder()
	router = chi.NewRouter()
	router.Post("/bundles/{bundleId}/secrets", restarted.ProvideSecrets)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("unexpected response after restart: %d %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] Run `go test ./internal/api/handlers -run 'ProvideSecrets|BundleRunbook'`.
Expected: pass.

## Task 3: Show persisted state in the UI

- [ ] Modify `web/src/components/MigrationWizard/steps/SecretsStep.tsx`:
  - Remove debug `console.log` lines around required/provided secrets.
  - After a successful `provideSecrets`, set a persisted session patch from Plan 12: `{ secrets_resolved: result.success, current_step: result.success ? 'deploy' : 'secrets' }`.
  - Keep the current local `secretValues` map only for form inputs; never display returned secret values.

- [ ] Run `cd web && ./node_modules/.bin/tsc -b`.
Expected: pass.

## Task 4: Commit

- [ ] Run `gofmt -w internal/app/secrets internal/api/handlers/bundle.go internal/api/handlers/bundle_runbook_test.go`.
- [ ] Run `go test ./internal/app/secrets ./internal/api/handlers`.
- [ ] Run `cd web && ./node_modules/.bin/tsc -b`.
- [ ] Commit:

```bash
git add internal/app/secrets internal/api/handlers/bundle.go internal/api/handlers/bundle_runbook_test.go web/src/components/MigrationWizard/steps/SecretsStep.tsx
git commit -m "feat: persist bundle secrets securely"
```

