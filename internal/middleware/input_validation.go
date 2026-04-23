package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

var validateContentType = true

// SetValidateContentType allows toggling content-type checks at runtime.
func SetValidateContentType(enabled bool) {
	validateContentType = enabled
}

// InputValidation validates Content-Type and request JSON body for write operations.
func InputValidation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isWriteMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		if validateContentType {
			contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
			if contentType == "" || !strings.HasPrefix(contentType, "application/json") {
				http.Error(w, "Content-Type must be application/json", http.StatusBadRequest)
				return
			}
		}

		if r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, "Payload too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		if len(bytes.TrimSpace(bodyBytes)) == 0 {
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			next.ServeHTTP(w, r)
			return
		}

		var payload interface{}
		if err := json.Unmarshal(bodyBytes, &payload); err != nil {
			http.Error(w, "Invalid JSON request body", http.StatusBadRequest)
			return
		}

		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		next.ServeHTTP(w, r)
	})
}

func isWriteMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}
