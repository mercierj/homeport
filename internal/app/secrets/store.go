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
