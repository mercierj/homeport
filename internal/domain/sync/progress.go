package sync

import (
	"fmt"
	"time"
)

// Progress tracks the progress of a sync operation.
// It provides metrics for bytes transferred, items processed, and time estimates.
type Progress struct {
	// TaskID identifies which task this progress belongs to.
	TaskID string `json:"task_id"`
	// BytesTotal is the total number of bytes to transfer.
	BytesTotal int64 `json:"bytes_total"`
	// BytesDone is the number of bytes already transferred.
	BytesDone int64 `json:"bytes_done"`
	// ItemsTotal is the total number of items to sync (rows, files, keys, etc.).
	ItemsTotal int64 `json:"items_total"`
	// ItemsDone is the number of items already synced.
	ItemsDone int64 `json:"items_done"`
	// StartedAt is when the sync operation started.
	StartedAt time.Time `json:"started_at"`
	// UpdatedAt is when progress was last updated.
	UpdatedAt time.Time `json:"updated_at"`
	// EstimatedETA is the estimated time until completion.
	EstimatedETA time.Duration `json:"estimated_eta"`
	// Speed is the current transfer rate in bytes per second.
	Speed float64 `json:"speed"`
	// Message is a human-readable status message.
	Message string `json:"message,omitempty"`
	// Phase describes the current phase of the operation (e.g., "scanning", "transferring", "verifying").
	Phase string `json:"phase,omitempty"`
	// CurrentItem is the name/identifier of the item currently being processed.
	CurrentItem string `json:"current_item,omitempty"`
	// Errors counts the number of errors encountered (may still continue with retries).
	Errors int `json:"errors,omitempty"`
	// Warnings counts the number of warnings encountered.
	Warnings int `json:"warnings,omitempty"`
}

// NewProgress creates a new progress tracker for a task.
func NewProgress(taskID string) *Progress {
	now := time.Now()
	return &Progress{
		TaskID:    taskID,
		StartedAt: now,
		UpdatedAt: now,
	}
}

// Percentage returns the completion percentage based on bytes.
// Returns 0 if BytesTotal is 0 to avoid division by zero.
func (p *Progress) Percentage() float64 {
	if p.BytesTotal == 0 {
		return 0
	}
	return float64(p.BytesDone) / float64(p.BytesTotal) * 100
}

// ItemPercentage returns the completion percentage based on items.
// Returns 0 if ItemsTotal is 0 to avoid division by zero.
func (p *Progress) ItemPercentage() float64 {
	if p.ItemsTotal == 0 {
		return 0
	}
	return float64(p.ItemsDone) / float64(p.ItemsTotal) * 100
}

// CalculateETA calculates the estimated time to completion based on current speed.
// Returns 0 if no progress has been made or speed is too low.
func (p *Progress) CalculateETA() time.Duration {
	if p.BytesDone == 0 || p.Speed <= 0 {
		return 0
	}
	remaining := p.BytesTotal - p.BytesDone
	if remaining <= 0 {
		return 0
	}
	secondsRemaining := float64(remaining) / p.Speed
	return time.Duration(secondsRemaining * float64(time.Second))
}

// CalculateSpeed calculates the average transfer speed based on elapsed time.
// Updates the Speed field and returns the calculated value.
func (p *Progress) CalculateSpeed() float64 {
	elapsed := time.Since(p.StartedAt).Seconds()
	if elapsed <= 0 {
		p.Speed = 0
		return 0
	}
	p.Speed = float64(p.BytesDone) / elapsed
	return p.Speed
}

// Update updates the progress with new values and recalculates derived metrics.
func (p *Progress) Update(bytesDone, itemsDone int64, message string) {
	p.BytesDone = bytesDone
	p.ItemsDone = itemsDone
	p.Message = message
	p.UpdatedAt = time.Now()
	p.CalculateSpeed()
	p.EstimatedETA = p.CalculateETA()
}

// IncrementBytes adds to the bytes done counter.
func (p *Progress) IncrementBytes(bytes int64) {
	p.BytesDone += bytes
	p.UpdatedAt = time.Now()
	p.CalculateSpeed()
	p.EstimatedETA = p.CalculateETA()
}

// IncrementItems adds to the items done counter.
func (p *Progress) IncrementItems(items int64) {
	p.ItemsDone += items
	p.UpdatedAt = time.Now()
}

// SetTotals sets the total bytes and items to be processed.
func (p *Progress) SetTotals(bytesTotal, itemsTotal int64) {
	p.BytesTotal = bytesTotal
	p.ItemsTotal = itemsTotal
	p.UpdatedAt = time.Now()
}

// SetPhase updates the current operation phase.
func (p *Progress) SetPhase(phase string) {
	p.Phase = phase
	p.UpdatedAt = time.Now()
}

// SetCurrentItem updates the item currently being processed.
func (p *Progress) SetCurrentItem(item string) {
	p.CurrentItem = item
	p.UpdatedAt = time.Now()
}

// RecordError increments the error counter.
func (p *Progress) RecordError() {
	p.Errors++
	p.UpdatedAt = time.Now()
}

// RecordWarning increments the warning counter.
func (p *Progress) RecordWarning() {
	p.Warnings++
	p.UpdatedAt = time.Now()
}

// ElapsedTime returns how long the operation has been running.
func (p *Progress) ElapsedTime() time.Duration {
	return time.Since(p.StartedAt)
}

// IsComplete returns true if all bytes have been transferred.
func (p *Progress) IsComplete() bool {
	return p.BytesTotal > 0 && p.BytesDone >= p.BytesTotal
}

// RemainingBytes returns the number of bytes left to transfer.
func (p *Progress) RemainingBytes() int64 {
	remaining := p.BytesTotal - p.BytesDone
	if remaining < 0 {
		return 0
	}
	return remaining
}

// RemainingItems returns the number of items left to process.
func (p *Progress) RemainingItems() int64 {
	remaining := p.ItemsTotal - p.ItemsDone
	if remaining < 0 {
		return 0
	}
	return remaining
}

// FormatBytes converts bytes to a human-readable string (KB, MB, GB, etc.).
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatSpeed converts bytes per second to a human-readable string.
func FormatSpeed(bytesPerSec float64) string {
	return FormatBytes(int64(bytesPerSec)) + "/s"
}

// FormatDuration converts a duration to a human-readable string.
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}

// String returns a human-readable summary of the progress.
func (p *Progress) String() string {
	pct := p.Percentage()
	if p.BytesTotal == 0 {
		return fmt.Sprintf("[%s] %s - %d items done",
			p.Phase, p.Message, p.ItemsDone)
	}
	return fmt.Sprintf("[%s] %.1f%% - %s/%s @ %s - ETA: %s",
		p.Phase,
		pct,
		FormatBytes(p.BytesDone),
		FormatBytes(p.BytesTotal),
		FormatSpeed(p.Speed),
		FormatDuration(p.EstimatedETA))
}

// Clone creates a copy of the progress for thread-safe reading.
func (p *Progress) Clone() *Progress {
	if p == nil {
		return nil
	}
	return &Progress{
		TaskID:       p.TaskID,
		BytesTotal:   p.BytesTotal,
		BytesDone:    p.BytesDone,
		ItemsTotal:   p.ItemsTotal,
		ItemsDone:    p.ItemsDone,
		StartedAt:    p.StartedAt,
		UpdatedAt:    p.UpdatedAt,
		EstimatedETA: p.EstimatedETA,
		Speed:        p.Speed,
		Message:      p.Message,
		Phase:        p.Phase,
		CurrentItem:  p.CurrentItem,
		Errors:       p.Errors,
		Warnings:     p.Warnings,
	}
}

// ProgressCallback is a function type for receiving progress updates.
type ProgressCallback func(progress *Progress)

// ProgressReporter provides a convenient way to send progress updates.
type ProgressReporter struct {
	progress *Progress
	channel  chan<- Progress
	callback ProgressCallback
}

// NewProgressReporter creates a new progress reporter.
// Either channel or callback can be nil if not needed.
func NewProgressReporter(taskID string, channel chan<- Progress, callback ProgressCallback) *ProgressReporter {
	return &ProgressReporter{
		progress: NewProgress(taskID),
		channel:  channel,
		callback: callback,
	}
}

// Report sends the current progress to the channel and/or callback.
func (r *ProgressReporter) Report() {
	if r.channel != nil {
		// Send a copy to avoid race conditions
		r.channel <- *r.progress.Clone()
	}
	if r.callback != nil {
		r.callback(r.progress.Clone())
	}
}

// SetTotals sets the total bytes and items and reports progress.
func (r *ProgressReporter) SetTotals(bytesTotal, itemsTotal int64) {
	r.progress.SetTotals(bytesTotal, itemsTotal)
	r.Report()
}

// SetPhase updates the phase and reports progress.
func (r *ProgressReporter) SetPhase(phase string) {
	r.progress.SetPhase(phase)
	r.Report()
}

// Update updates progress and reports it.
func (r *ProgressReporter) Update(bytesDone, itemsDone int64, message string) {
	r.progress.Update(bytesDone, itemsDone, message)
	r.Report()
}

// IncrementBytes adds bytes and reports progress.
func (r *ProgressReporter) IncrementBytes(bytes int64) {
	r.progress.IncrementBytes(bytes)
	r.Report()
}

// IncrementItems adds items and reports progress.
func (r *ProgressReporter) IncrementItems(items int64) {
	r.progress.IncrementItems(items)
	r.Report()
}

// SetCurrentItem updates the current item and reports progress.
func (r *ProgressReporter) SetCurrentItem(item string) {
	r.progress.SetCurrentItem(item)
	r.Report()
}

// Error records an error and reports progress.
func (r *ProgressReporter) Error(message string) {
	r.progress.RecordError()
	r.progress.Message = message
	r.Report()
}

// Warning records a warning and reports progress.
func (r *ProgressReporter) Warning(message string) {
	r.progress.RecordWarning()
	r.progress.Message = message
	r.Report()
}

// GetProgress returns the current progress.
func (r *ProgressReporter) GetProgress() *Progress {
	return r.progress.Clone()
}
