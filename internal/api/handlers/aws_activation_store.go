package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/homeport/homeport/internal/app/awsoperations"
)

// awsActivationStore persists trusted bindings and pending activations outside
// browser/SSE state. It is deliberately only writable by trusted cutover code.
type awsActivationStore struct {
	mu       sync.Mutex
	path     string
	bindings map[string][]awsoperations.LocalResourceBinding
	plans    map[string]awsoperations.ActivationInput
}
type awsActivationStoreData struct {
	Bindings map[string][]awsoperations.LocalResourceBinding `json:"bindings"`
	Plans    map[string]awsoperations.ActivationInput        `json:"plans"`
}

func newAWSActivationStore(path string) (*awsActivationStore, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".homeport", "aws-pending-activations.json")
	}
	s := &awsActivationStore{path: path, bindings: map[string][]awsoperations.LocalResourceBinding{}, plans: map[string]awsoperations.ActivationInput{}}
	if data, err := os.ReadFile(path); err == nil {
		var saved awsActivationStoreData
		if err := json.Unmarshal(data, &saved); err != nil {
			return nil, fmt.Errorf("read AWS activation store: %w", err)
		}
		s.bindings, s.plans = saved.Bindings, saved.Plans
		if s.bindings == nil {
			s.bindings = map[string][]awsoperations.LocalResourceBinding{}
		}
		if s.plans == nil {
			s.plans = map[string]awsoperations.ActivationInput{}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *awsActivationStore) putBindings(key string, values []awsoperations.LocalResourceBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[key] = append([]awsoperations.LocalResourceBinding(nil), values...)
	return s.persist()
}
func (s *awsActivationStore) getBindings(key string) []awsoperations.LocalResourceBinding {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]awsoperations.LocalResourceBinding(nil), s.bindings[key]...)
}
func (s *awsActivationStore) putPlan(id string, input awsoperations.ActivationInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.plans[id] = input
	return s.persist()
}
func (s *awsActivationStore) getPlan(id string) (awsoperations.ActivationInput, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	input, ok := s.plans[id]
	return input, ok
}
func (s *awsActivationStore) deletePlan(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.plans, id)
	return s.persist()
}
func (s *awsActivationStore) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(awsActivationStoreData{Bindings: s.bindings, Plans: s.plans})
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}
