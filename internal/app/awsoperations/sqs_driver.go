package awsoperations

import "context"

type SQSDriver struct{ backend QueuesBackend }

func NewSQSDriver(b QueuesBackend) *SQSDriver { return &SQSDriver{backend: b} }
func (*SQSDriver) Service() ServiceKey        { return ServiceSQS }
func (*SQSDriver) Capabilities(w Workspace) []Capability {
	return append([]Capability(nil), w.Services[ServiceSQS].Capabilities...)
}
func (d *SQSDriver) List(ctx context.Context, w Workspace) ([]any, error) {
	if _, err := serviceState(w, ServiceSQS, CapabilityList); err != nil {
		return nil, err
	}
	bound := bindingsFor(w, ServiceSQS)
	result := make([]any, 0)
	for _, binding := range bound {
		queues, err := d.backend.List(ctx, binding.LocalStackID)
		if err != nil {
			return nil, err
		}
		for _, queue := range queues {
			if queue.Name == binding.LocalResourceID {
				queue.ImportedResourceID = binding.ImportedResourceID
				queue.Region = binding.Region
				queue.Tags = binding.Tags
				queue.LocalStackID = binding.LocalStackID
				result = append(result, queue)
			}
		}
	}
	return result, nil
}
func (d *SQSDriver) Messages(ctx context.Context, w Workspace, id, status string) ([]MessageRecord, error) {
	binding, err := serviceBinding(w, ServiceSQS, id, CapabilityRead)
	if err != nil {
		return nil, err
	}
	return d.backend.Messages(ctx, binding.LocalStackID, binding.LocalResourceID, status)
}
func (d *SQSDriver) Retry(ctx context.Context, w Workspace, id, messageID string) error {
	binding, err := serviceBinding(w, ServiceSQS, id, CapabilityRetry)
	if err != nil {
		return err
	}
	return d.backend.Retry(ctx, binding.LocalStackID, binding.LocalResourceID, messageID)
}
func (d *SQSDriver) Delete(ctx context.Context, w Workspace, id, messageID string) error {
	binding, err := serviceBinding(w, ServiceSQS, id, CapabilityDelete)
	if err != nil {
		return err
	}
	return d.backend.Delete(ctx, binding.LocalStackID, binding.LocalResourceID, messageID)
}
func (d *SQSDriver) Purge(ctx context.Context, w Workspace, id, status string) (int64, error) {
	binding, err := serviceBinding(w, ServiceSQS, id, CapabilityPurge)
	if err != nil {
		return 0, err
	}
	return d.backend.Purge(ctx, binding.LocalStackID, binding.LocalResourceID, status)
}
