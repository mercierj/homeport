// Package ovh provides utility functions for OVH Cloud generator.
package ovh

// ResourceProperties provides a wrapper to access resource properties.
type ResourceProperties map[string]interface{}

// GetString returns a string property or empty string.
func (p ResourceProperties) GetString(key string) string {
	if val, ok := p[key].(string); ok {
		return val
	}
	return ""
}

// GetInt returns an int property or 0.
func (p ResourceProperties) GetInt(key string) int {
	switch val := p[key].(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	}
	return 0
}

// GetBool returns a bool property or false.
func (p ResourceProperties) GetBool(key string) bool {
	if val, ok := p[key].(bool); ok {
		return val
	}
	return false
}
