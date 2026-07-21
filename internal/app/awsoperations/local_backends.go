package awsoperations

import (
	"context"

	"github.com/homeport/homeport/internal/app/functions"
	"github.com/homeport/homeport/internal/app/queues"
)

// NewFunctionsBackend adapts the local Homeport functions service. It is the
// only production implementation used by the AWS operations driver.
func NewFunctionsBackend(service *functions.Service) FunctionsBackend {
	return functionsBackend{service: service}
}

type functionsBackend struct{ service *functions.Service }

func (b functionsBackend) List(ctx context.Context) ([]FunctionRecord, error) {
	items, err := b.service.ListFunctions(ctx, nil)
	if err != nil {
		return nil, err
	}
	result := make([]FunctionRecord, 0, len(items))
	for _, item := range items {
		result = append(result, functionRecord(item))
	}
	return result, nil
}
func (b functionsBackend) Get(ctx context.Context, id string) (*FunctionRecord, error) {
	item, err := b.service.GetFunction(ctx, id)
	if err != nil {
		return nil, err
	}
	result := functionRecord(*item)
	return &result, nil
}
func (b functionsBackend) Create(ctx context.Context, input FunctionInput) (*FunctionRecord, error) {
	item, err := b.service.CreateFunction(ctx, functionConfig(input))
	if err != nil {
		return nil, err
	}
	result := functionRecord(*item)
	return &result, nil
}
func (b functionsBackend) Update(ctx context.Context, id string, input FunctionInput) (*FunctionRecord, error) {
	item, err := b.service.UpdateFunction(ctx, id, functionConfig(input))
	if err != nil {
		return nil, err
	}
	result := functionRecord(*item)
	return &result, nil
}
func (b functionsBackend) Delete(ctx context.Context, id string) error {
	return b.service.DeleteFunction(ctx, id)
}
func (b functionsBackend) Invoke(ctx context.Context, id string, payload []byte) (*InvocationRecord, error) {
	item, err := b.service.InvokeFunction(ctx, id, payload)
	if err != nil {
		return nil, err
	}
	return &InvocationRecord{RequestID: item.RequestID, StatusCode: item.StatusCode, Body: item.Body, DurationMS: item.DurationMS, Logs: item.Logs, Error: item.Error}, nil
}
func (b functionsBackend) Logs(ctx context.Context, id string) ([]LogRecord, error) {
	items, err := b.service.GetFunctionLogs(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	result := make([]LogRecord, 0, len(items))
	for _, item := range items {
		result = append(result, LogRecord{Timestamp: item.Timestamp, Level: item.Level, Message: item.Message, RequestID: item.RequestID})
	}
	return result, nil
}
func functionConfig(input FunctionInput) functions.FunctionConfig {
	return functions.FunctionConfig{Name: input.Name, Runtime: input.Runtime, Handler: input.Handler, MemoryMB: input.MemoryMB, TimeoutSeconds: input.TimeoutSeconds, Environment: input.Environment, Description: input.Description}
}
func functionRecord(item functions.FunctionInfo) FunctionRecord {
	return FunctionRecord{ID: item.ID, Name: item.Name, Runtime: item.Runtime, Handler: item.Handler, MemoryMB: item.MemoryMB, TimeoutSeconds: item.TimeoutSeconds, Environment: item.Environment, Description: item.Description, Status: string(item.Status), InvocationCount: item.InvocationCount, LastInvoked: item.LastInvoked, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

// NewQueuesBackend adapts Homeport's local Redis-backed queue service.
func NewQueuesBackend(service *queues.Service) QueuesBackend { return queuesBackend{service: service} }

type queuesBackend struct{ service *queues.Service }

func (b queuesBackend) List(ctx context.Context, stackID string) ([]QueueRecord, error) {
	items, err := b.service.ListQueues(ctx, stackID)
	if err != nil {
		return nil, err
	}
	result := make([]QueueRecord, 0, len(items))
	for _, item := range items {
		result = append(result, QueueRecord{Name: item.Name, PendingCount: item.PendingCount, ActiveCount: item.ActiveCount, CompletedCount: item.CompletedCount, FailedCount: item.FailedCount, TotalCount: item.TotalCount, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
	}
	return result, nil
}
func (b queuesBackend) Messages(ctx context.Context, stackID, queueName, status string) ([]MessageRecord, error) {
	items, err := b.service.ListMessages(ctx, stackID, queueName, queues.MessageStatus(status), 1000, 0)
	if err != nil {
		return nil, err
	}
	result := make([]MessageRecord, 0, len(items))
	for _, item := range items {
		result = append(result, MessageRecord{ID: item.ID, QueueName: item.QueueName, Status: string(item.Status), Data: item.Data, Attempts: item.Attempts, MaxAttempts: item.MaxAttempts, Error: item.Error, CreatedAt: item.CreatedAt, ProcessedAt: item.ProcessedAt, CompletedAt: item.CompletedAt, FailedAt: item.FailedAt})
	}
	return result, nil
}
func (b queuesBackend) Retry(ctx context.Context, stackID, queueName, messageID string) error {
	return b.service.RetryMessage(ctx, stackID, queueName, messageID)
}
func (b queuesBackend) Delete(ctx context.Context, stackID, queueName, messageID string) error {
	return b.service.DeleteMessage(ctx, stackID, queueName, messageID)
}
func (b queuesBackend) Purge(ctx context.Context, stackID, queueName, status string) (int64, error) {
	return b.service.PurgeQueue(ctx, stackID, queueName, queues.MessageStatus(status))
}
