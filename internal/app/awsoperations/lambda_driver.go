package awsoperations

import "context"

type LambdaDriver struct{ backend FunctionsBackend }

func NewLambdaDriver(b FunctionsBackend) *LambdaDriver { return &LambdaDriver{backend: b} }
func (*LambdaDriver) Service() ServiceKey              { return ServiceLambda }
func (*LambdaDriver) Capabilities(w Workspace) []Capability {
	return append([]Capability(nil), w.Services[ServiceLambda].Capabilities...)
}
func (d *LambdaDriver) List(ctx context.Context, w Workspace) ([]any, error) {
	if _, err := serviceState(w, ServiceLambda, CapabilityList); err != nil {
		return nil, err
	}
	functions, err := d.backend.List(ctx)
	if err != nil {
		return nil, err
	}
	bound := map[string]ResourceBinding{}
	for _, binding := range bindingsFor(w, ServiceLambda) {
		bound[binding.LocalResourceID] = binding
	}
	result := make([]any, 0)
	for _, function := range functions {
		if binding, ok := bound[function.ID]; ok {
			result = append(result, lambdaWithMetadata(function, binding))
		}
	}
	return result, nil
}
func (d *LambdaDriver) Get(ctx context.Context, w Workspace, id string) (*FunctionRecord, error) {
	binding, err := serviceBinding(w, ServiceLambda, id, CapabilityRead)
	if err != nil {
		return nil, err
	}
	result, err := d.backend.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return pointerToLambdaWithMetadata(result, binding), nil
}

func pointerToLambdaWithMetadata(record *FunctionRecord, binding ResourceBinding) *FunctionRecord {
	result := lambdaWithMetadata(*record, binding)
	return &result
}
func lambdaWithMetadata(record FunctionRecord, binding ResourceBinding) FunctionRecord {
	record.ImportedResourceID = binding.ImportedResourceID
	record.Region = binding.Region
	record.Tags = binding.Tags
	record.LocalStackID = binding.LocalStackID
	return record
}

func (d *LambdaDriver) Update(ctx context.Context, w Workspace, id string, in FunctionInput) (*FunctionRecord, error) {
	binding, err := serviceBinding(w, ServiceLambda, id, CapabilityUpdate)
	if err != nil {
		return nil, err
	}
	result, err := d.backend.Update(ctx, id, in)
	if err != nil {
		return nil, err
	}
	return pointerToLambdaWithMetadata(result, binding), nil
}
func (d *LambdaDriver) Delete(ctx context.Context, w Workspace, id string) error {
	if _, err := serviceBinding(w, ServiceLambda, id, CapabilityDelete); err != nil {
		return err
	}
	return d.backend.Delete(ctx, id)
}
func (d *LambdaDriver) Invoke(ctx context.Context, w Workspace, id string, payload []byte) (*InvocationRecord, error) {
	if _, err := serviceBinding(w, ServiceLambda, id, CapabilityInvoke); err != nil {
		return nil, err
	}
	return d.backend.Invoke(ctx, id, payload)
}
func (d *LambdaDriver) Logs(ctx context.Context, w Workspace, id string) ([]LogRecord, error) {
	if _, err := serviceBinding(w, ServiceLambda, id, CapabilityLogs); err != nil {
		return nil, err
	}
	return d.backend.Logs(ctx, id)
}
