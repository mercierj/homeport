package deploy

import (
	"context"
	"fmt"
	"time"
)

type Deployer interface {
	Deploy(ctx context.Context, d *Deployment) error
	GetPhases() []string
}

type Service struct {
	manager       *Manager
	localDeployer Deployer
	sshDeployer   Deployer
}

func NewService() *Service {
	return &Service{
		manager:       NewManager(),
		localDeployer: NewLocalDeployer(),
		sshDeployer:   NewSSHDeployer(),
	}
}

func (s *Service) Manager() *Manager {
	return s.manager
}

func (s *Service) StartDeployment(target string, config interface{}) (*Deployment, error) {
	d := s.manager.CreateDeployment(target, config)

	var deployer Deployer
	switch target {
	case "local":
		deployer = s.localDeployer
	case "ssh":
		deployer = s.sshDeployer
	default:
		return nil, fmt.Errorf("unknown target: %s", target)
	}

	phases := deployer.GetPhases()
	d.TotalPhases = len(phases)

	go s.runDeployment(d, deployer)

	return d, nil
}

func (s *Service) runDeployment(d *Deployment, deployer Deployer) {
	d.mu.Lock()
	d.Status = StatusRunning
	d.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Monitor for cancellation
	go func() {
		<-d.cancel
		cancel()
	}()

	err := deployer.Deploy(ctx, d)

	d.mu.Lock()
	if err != nil {
		d.Status = StatusFailed
		d.Error = err.Error()
		d.Emit(Event{
			Type: EventError,
			Data: ErrorEvent{
				Message:     err.Error(),
				Phase:       fmt.Sprintf("Phase %d", d.CurrentPhase),
				Recoverable: true,
			},
		})
	} else if d.Status != StatusCancelled {
		d.Status = StatusCompleted
	}
	d.mu.Unlock()

	// Close all subscriber channels
	d.mu.Lock()
	for _, ch := range d.subscribers {
		close(ch)
	}
	d.subscribers = nil
	d.mu.Unlock()
}

func (s *Service) CancelDeployment(id string) error {
	d := s.manager.GetDeployment(id)
	if d == nil {
		return fmt.Errorf("deployment not found: %s", id)
	}
	d.Cancel()
	return nil
}

func (s *Service) RetryDeployment(id string) (*Deployment, error) {
	old := s.manager.GetDeployment(id)
	if old == nil {
		return nil, fmt.Errorf("deployment not found: %s", id)
	}
	return s.StartDeployment(old.Target, old.Config)
}

// EmitPhase emits phase updates to all subscribers
func EmitPhase(d *Deployment, phase string, index int) {
	d.mu.Lock()
	d.CurrentPhase = index
	d.mu.Unlock()

	d.Emit(Event{
		Type: EventPhase,
		Data: PhaseEvent{
			Phase: phase,
			Index: index,
			Total: d.TotalPhases,
		},
	})
}

// EmitLog emits log messages to all subscribers
func EmitLog(d *Deployment, level, message string) {
	d.Emit(Event{
		Type: EventLog,
		Data: LogEvent{
			Timestamp: time.Now().Format(time.RFC3339),
			Level:     level,
			Message:   message,
		},
	})
}

// EmitProgress emits progress updates to all subscribers
func EmitProgress(d *Deployment, percent int) {
	d.Emit(Event{
		Type: EventProgress,
		Data: ProgressEvent{Percent: percent},
	})
}
