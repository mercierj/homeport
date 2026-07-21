package compat

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	compatazure "github.com/homeport/homeport/internal/app/compat/azure"
	compatgcp "github.com/homeport/homeport/internal/app/compat/gcp"
)

type Adapter interface {
	http.Handler
	Provider() string
	Service() string
	Routes() []string
	TargetEnv() map[string]string
	ConformanceChecks() []string
}

type Registry struct {
	adapters map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
}

func NewDefaultRegistry() *Registry {
	registry := NewRegistry()
	for _, adapter := range []Adapter{
		compataws.NewALBAdapter(),
		compataws.NewComprehendAdapter(),
		compataws.NewS3Adapter(),
		compataws.NewDynamoDBAdapter(),
		NativeAdapter("aws", "redis", map[string]string{
			"REDIS_HOST":               "redis",
			"REDIS_PORT":               "6379",
			"REDIS_TLS":                "false",
			"REDIS_PASSWORD":           "${REDIS_PASSWORD}",
			"HOMEPORT_COMPAT_BACKEND":  "redis",
			"HOMEPORT_COMPAT_PROTOCOL": "redis",
		}, "set-key", "get-key"),
		NativeAdapter("aws", "ssm", map[string]string{
			"HOMEPORT_COMPAT_BACKEND": "vault",
			"AWS_ENDPOINT_URL_SSM":    "http://homeport:8080/api/v1/compat/aws/ssm",
		}, "get-parameter"),
		compataws.NewSQSAdapter(),
		compataws.NewSNSAdapter(),
		compataws.NewKinesisAdapter(),
		compataws.NewSecretsAdapter(),
		compataws.NewKMSAdapter(),
		compataws.NewCloudWatchLogsAdapter(),
		compataws.NewLambdaAdapter(),
		compataws.NewEventBridgeAdapter(),
		compataws.NewACMAdapter(),
		compataws.NewSESAdapter(),
		compataws.NewCognitoAdapter(),
		compataws.NewECSAdapter(),
		compataws.NewAPIGatewayAdapter(),
		compataws.NewEFSAdapter(),
		compataws.NewEKSAdapter(),
		compataws.NewIAMAdapter(),
		compataws.NewECRAdapter(),
		compataws.NewStepFunctionsAdapter(),
		compataws.NewCodeBuildAdapter(),
		compataws.NewAppSyncAdapter(),
		compatgcp.NewPubSubAdapter(),
		compatazure.NewServiceBusAdapter(),
	} {
		mustRegister(registry, adapter)
	}
	return registry
}

func (r *Registry) Register(adapter Adapter) error {
	key := adapterKey(adapter.Provider(), adapter.Service())
	if _, exists := r.adapters[key]; exists {
		return fmt.Errorf("compat adapter already registered: %s", key)
	}
	r.adapters[key] = adapter
	return nil
}

func (r *Registry) Get(provider, service string) (Adapter, error) {
	key := adapterKey(provider, service)
	adapter, ok := r.adapters[key]
	if !ok {
		return nil, fmt.Errorf("unknown compat adapter %s", key)
	}
	return adapter, nil
}

func (r *Registry) List() []Adapter {
	keys := make([]string, 0, len(r.adapters))
	for key := range r.adapters {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	adapters := make([]Adapter, 0, len(keys))
	for _, key := range keys {
		adapters = append(adapters, r.adapters[key])
	}
	return adapters
}

func NativeAdapter(provider, service string, env map[string]string, checks ...string) Adapter {
	return nativeAdapter{provider: provider, service: service, env: env, checks: checks}
}

type nativeAdapter struct {
	provider string
	service  string
	env      map[string]string
	checks   []string
}

func (a nativeAdapter) Provider() string { return a.provider }
func (a nativeAdapter) Service() string  { return a.service }
func (a nativeAdapter) Routes() []string { return nil }
func (a nativeAdapter) TargetEnv() map[string]string {
	env := make(map[string]string, len(a.env))
	for key, value := range a.env {
		env[key] = value
	}
	return env
}
func (a nativeAdapter) ConformanceChecks() []string {
	return append([]string(nil), a.checks...)
}
func (a nativeAdapter) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "native-compatible backend: no proxy required", http.StatusNotImplemented)
}

func mustRegister(registry *Registry, adapter Adapter) {
	if err := registry.Register(adapter); err != nil {
		panic(err)
	}
}

func adapterKey(provider, service string) string {
	return strings.ToLower(provider) + "/" + strings.ToLower(service)
}
