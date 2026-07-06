package aws

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func decodeAWSAction(r *http.Request) (string, map[string]any, error) {
	target := r.Header.Get("X-Amz-Target")
	if target != "" {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return "", nil, err
		}
		parts := strings.Split(target, ".")
		return parts[len(parts)-1], body, nil
	}
	if err := r.ParseForm(); err != nil {
		return "", nil, err
	}
	body := make(map[string]any, len(r.Form))
	for key, values := range r.Form {
		if len(values) > 0 {
			body[key] = values[0]
		}
	}
	return stringValue(body["Action"]), body, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.0")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func stringValue(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	default:
		return ""
	}
}

func mapValue(v any) map[string]string {
	out := map[string]string{}
	switch value := v.(type) {
	case map[string]string:
		return value
	case map[string]any:
		for key, item := range value {
			out[key] = stringValue(item)
		}
	}
	return out
}

func mergeStringMap(dst, src map[string]string) {
	for key, value := range src {
		dst[key] = value
	}
}
