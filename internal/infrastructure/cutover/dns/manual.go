// Package dns provides DNS provider implementations for cutover operations.
package dns

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/cutover"
)

// ManualProvider is a DNS provider that generates manual instructions
// instead of making actual API calls.
type ManualProvider struct {
	// changes tracks applied changes for rollback.
	changes []*cutover.DNSChange
}

// NewManualProvider creates a new manual DNS provider.
func NewManualProvider() *ManualProvider {
	return &ManualProvider{
		changes: make([]*cutover.DNSChange, 0),
	}
}

// Name returns the provider name.
func (p *ManualProvider) Name() string {
	return "manual"
}

// ListRecords is not supported for manual provider.
func (p *ManualProvider) ListRecords(ctx context.Context, domain string) ([]*cutover.DNSRecord, error) {
	return nil, fmt.Errorf("manual provider does not support listing records")
}

// GetRecord is not supported for manual provider.
func (p *ManualProvider) GetRecord(ctx context.Context, domain, recordID string) (*cutover.DNSRecord, error) {
	return nil, fmt.Errorf("manual provider does not support getting records")
}

// CreateRecord marks a record as created (user must do this manually).
func (p *ManualProvider) CreateRecord(ctx context.Context, change *cutover.DNSChange) error {
	change.Status = cutover.DNSChangeStatusApplied
	now := time.Now()
	change.AppliedAt = &now
	p.changes = append(p.changes, change)
	return nil
}

// UpdateRecord marks a record as updated (user must do this manually).
func (p *ManualProvider) UpdateRecord(ctx context.Context, change *cutover.DNSChange) error {
	change.Status = cutover.DNSChangeStatusApplied
	now := time.Now()
	change.AppliedAt = &now
	p.changes = append(p.changes, change)
	return nil
}

// DeleteRecord marks a record as deleted (user must do this manually).
func (p *ManualProvider) DeleteRecord(ctx context.Context, domain, recordID string) error {
	return nil
}

// ValidateCredentials always succeeds for manual provider.
func (p *ManualProvider) ValidateCredentials(ctx context.Context) error {
	return nil
}

// GetAppliedChanges returns changes that have been marked as applied.
func (p *ManualProvider) GetAppliedChanges() []*cutover.DNSChange {
	return p.changes
}

// GenerateInstructions creates human-readable instructions for DNS changes.
func (p *ManualProvider) GenerateInstructions(changes []*cutover.DNSChange) []string {
	instructions := make([]string, 0)

	if len(changes) == 0 {
		return instructions
	}

	instructions = append(instructions, "DNS CHANGES REQUIRED")
	instructions = append(instructions, "====================")
	instructions = append(instructions, "")
	instructions = append(instructions, "Please make the following DNS changes manually:")
	instructions = append(instructions, "")

	for i, change := range changes {
		instructions = append(instructions, fmt.Sprintf("%d. %s Record: %s", i+1, change.RecordType, change.FullName()))
		instructions = append(instructions, fmt.Sprintf("   Current Value: %s", change.OldValue))
		instructions = append(instructions, fmt.Sprintf("   New Value:     %s", change.NewValue))
		instructions = append(instructions, fmt.Sprintf("   TTL:           %d seconds", change.TTL))
		instructions = append(instructions, "")
	}

	instructions = append(instructions, "After making these changes, wait for DNS propagation.")
	instructions = append(instructions, "You can verify propagation using: dig +short <hostname>")

	return instructions
}

// GenerateRollbackInstructions creates rollback instructions.
func (p *ManualProvider) GenerateRollbackInstructions(changes []*cutover.DNSChange) []string {
	instructions := make([]string, 0)

	if len(changes) == 0 {
		return instructions
	}

	instructions = append(instructions, "DNS ROLLBACK REQUIRED")
	instructions = append(instructions, "=====================")
	instructions = append(instructions, "")
	instructions = append(instructions, "To rollback, revert the following DNS changes:")
	instructions = append(instructions, "")

	for i, change := range changes {
		instructions = append(instructions, fmt.Sprintf("%d. %s Record: %s", i+1, change.RecordType, change.FullName()))
		instructions = append(instructions, fmt.Sprintf("   Current Value: %s", change.NewValue))
		instructions = append(instructions, fmt.Sprintf("   Revert To:     %s", change.OldValue))
		instructions = append(instructions, "")
	}

	return instructions
}
