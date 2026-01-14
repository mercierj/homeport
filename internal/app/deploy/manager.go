// Package deploy provides deployment management functionality.
package deploy

import (
	"sync"

	"github.com/google/uuid"
)

type DeploymentStatus string

const (
	StatusPending   DeploymentStatus = "pending"
	StatusRunning   DeploymentStatus = "running"
	StatusCompleted DeploymentStatus = "completed"
	StatusFailed    DeploymentStatus = "failed"
	StatusCancelled DeploymentStatus = "cancelled"
)

type Deployment struct {
	ID           string
	Status       DeploymentStatus
	Target       string // "local" or "ssh"
	Config       interface{}
	CurrentPhase int
	TotalPhases  int
	Error        string
	subscribers  []chan Event
	mu           sync.RWMutex
	cancel       chan struct{}
	cancelled    bool
}

type Manager struct {
	deployments map[string]*Deployment
	mu          sync.RWMutex
}

func NewManager() *Manager {
	return &Manager{
		deployments: make(map[string]*Deployment),
	}
}

func (m *Manager) CreateDeployment(target string, config interface{}) *Deployment {
	id := uuid.New().String()
	d := &Deployment{
		ID:          id,
		Status:      StatusPending,
		Target:      target,
		Config:      config,
		subscribers: make([]chan Event, 0),
		cancel:      make(chan struct{}),
	}
	m.mu.Lock()
	m.deployments[id] = d
	m.mu.Unlock()
	return d
}

func (m *Manager) GetDeployment(id string) *Deployment {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.deployments[id]
}

func (d *Deployment) Subscribe() chan Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch := make(chan Event, 100)
	d.subscribers = append(d.subscribers, ch)
	return ch
}

func (d *Deployment) Unsubscribe(ch chan Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, sub := range d.subscribers {
		if sub == ch {
			d.subscribers = append(d.subscribers[:i], d.subscribers[i+1:]...)
			close(ch)
			break
		}
	}
}

func (d *Deployment) Emit(event Event) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, ch := range d.subscribers {
		select {
		case ch <- event:
		default: // drop if buffer full
		}
	}
}

func (d *Deployment) Cancel() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancelled {
		return
	}
	d.cancelled = true
	close(d.cancel)
	d.Status = StatusCancelled
}

func (d *Deployment) IsCancelled() bool {
	select {
	case <-d.cancel:
		return true
	default:
		return false
	}
}
