package aws

import (
	"net/http"
	"sort"
	"strings"
	"sync"
)

type SecretsAdapter struct {
	mu      sync.Mutex
	secrets map[string]string
}

func NewSecretsAdapter() *SecretsAdapter {
	return &SecretsAdapter{secrets: make(map[string]string)}
}

func (SecretsAdapter) Provider() string { return "aws" }
func (SecretsAdapter) Service() string  { return "secretsmanager" }
func (SecretsAdapter) Routes() []string { return []string{"POST /compat/aws/secretsmanager"} }
func (SecretsAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_SECRETSMANAGER": "http://homeport:8080/api/v1/compat/aws/secretsmanager",
		"HOMEPORT_COMPAT_BACKEND":         "vault",
	}
}
func (SecretsAdapter) ConformanceChecks() []string {
	return []string{"list-secrets", "describe-secret", "get-secret-value"}
}
func (a *SecretsAdapter) PutSecret(name, value string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.secrets[name] = value
}

func (a *SecretsAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "GetSecretValue":
		name := secretName(stringValue(body["SecretId"]))
		value, ok := a.secrets[name]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "secret not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"ARN":          secretARN(r, name),
			"Name":         name,
			"SecretString": value,
			"VersionId":    "homeport",
		})
	case "DescribeSecret":
		name := secretName(stringValue(body["SecretId"]))
		if _, ok := a.secrets[name]; !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "secret not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ARN":  secretARN(r, name),
			"Name": name,
			"VersionIdsToStages": map[string][]string{
				"homeport": {"AWSCURRENT"},
			},
		})
	case "ListSecrets":
		names := make([]string, 0, len(a.secrets))
		for name := range a.secrets {
			names = append(names, name)
		}
		sort.Strings(names)
		secretList := make([]map[string]string, 0, len(names))
		for _, name := range names {
			secretList = append(secretList, map[string]string{
				"ARN":  secretARN(r, name),
				"Name": name,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"SecretList": secretList})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported Secrets Manager action: " + action})
	}
}

func secretName(secretID string) string {
	if after, ok := strings.CutPrefix(secretID, "arn:aws:secretsmanager:"); ok {
		if _, name, ok := strings.Cut(after, ":secret:"); ok {
			if base, _, ok := strings.Cut(name, "-"); ok {
				return base
			}
			return name
		}
	}
	return secretID
}

func secretARN(r *http.Request, name string) string {
	return "arn:aws:secretsmanager:us-east-1:000000000000:secret:" + name + "-homeport"
}
