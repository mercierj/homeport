package httputil

import (
	"encoding/json"
	"io"
	"net/http"
)

// MaxJSONBodySize is the maximum size for JSON request bodies (1MB).
const MaxJSONBodySize = 1 << 20

// DecodeJSON decodes JSON from the request body into the provided value.
// It enforces a maximum body size to prevent memory exhaustion attacks.
// If decoding fails, it writes an appropriate error response and returns false.
// If decoding succeeds, it returns true.
//
// Usage:
//
//	var req MyRequest
//	if !httputil.DecodeJSON(w, r, &req) {
//	    return // error response already written
//	}
func DecodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	return DecodeJSONWithLimit(w, r, v, MaxJSONBodySize)
}

// DecodeJSONWithLimit decodes JSON with a custom size limit.
func DecodeJSONWithLimit(w http.ResponseWriter, r *http.Request, v interface{}, maxSize int64) bool {
	// Limit the body size to prevent memory exhaustion
	limitedBody := io.LimitReader(r.Body, maxSize+1)

	decoder := json.NewDecoder(limitedBody)
	if err := decoder.Decode(v); err != nil {
		InvalidJSON(w, r, err)
		return false
	}

	// Check if there was more data than allowed
	// Try to read one more byte to detect truncation
	var extra [1]byte
	if n, _ := limitedBody.Read(extra[:]); n > 0 {
		RequestTooLarge(w, r, maxSize)
		return false
	}

	return true
}
