package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// RequestLimitsConfig stores defaults for request limits and endpoint timeouts.
type RequestLimitsConfig struct {
	MaxBodyBytes            int64
	DetectTimeoutSeconds    int
	ChatTimeoutSeconds      int
	HTTPReadTimeoutSeconds  int
	HTTPWriteTimeoutSeconds int
	HTTPIdleTimeoutSeconds  int
}

var limitsCfg = RequestLimitsConfig{
	MaxBodyBytes:         10 * 1024 * 1024,
	DetectTimeoutSeconds: 30,
	ChatTimeoutSeconds:   300,
}

// InitRequestLimits stores runtime request-limit configuration.
func InitRequestLimits(cfg RequestLimitsConfig) {
	if cfg.MaxBodyBytes > 0 {
		limitsCfg.MaxBodyBytes = cfg.MaxBodyBytes
	}
	if cfg.DetectTimeoutSeconds > 0 {
		limitsCfg.DetectTimeoutSeconds = cfg.DetectTimeoutSeconds
	}
	if cfg.ChatTimeoutSeconds > 0 {
		limitsCfg.ChatTimeoutSeconds = cfg.ChatTimeoutSeconds
	}
}

// RequestLimits enforces maximum body size and per-endpoint timeout budgets.
func RequestLimits(maxBodyBytes int64) func(http.Handler) http.Handler {
	if maxBodyBytes <= 0 {
		maxBodyBytes = limitsCfg.MaxBodyBytes
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldLimitBody(r.Method) {
				r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
			}

			timeout := timeoutForPath(r.URL.Path)
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			rw := &timeoutAwareWriter{ResponseWriter: w}
			next.ServeHTTP(rw, r.WithContext(ctx))

			if errors.Is(ctx.Err(), context.DeadlineExceeded) && !rw.wroteHeader {
				http.Error(w, "Request timed out", http.StatusGatewayTimeout)
			}
		})
	}
}

func shouldLimitBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

func timeoutForPath(path string) time.Duration {
	switch {
	case path == "/detect":
		return time.Duration(limitsCfg.DetectTimeoutSeconds) * time.Second
	case strings.HasPrefix(path, "/v1/chat/completions"):
		return time.Duration(limitsCfg.ChatTimeoutSeconds) * time.Second
	default:
		return time.Duration(limitsCfg.DetectTimeoutSeconds) * time.Second
	}
}

type timeoutAwareWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *timeoutAwareWriter) WriteHeader(statusCode int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *timeoutAwareWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}
