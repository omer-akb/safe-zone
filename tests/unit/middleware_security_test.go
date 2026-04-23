package unit

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"thyris-sz/internal/middleware"
)

func TestInputValidation_RejectsNonJSONContentType(t *testing.T) {
	middleware.SetValidateContentType(true)

	h := middleware.InputValidation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/detect", strings.NewReader(`{"text":"hello"}`))
	req.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-json content-type, got %d", rr.Code)
	}
}

func TestInputValidation_RejectsInvalidJSON(t *testing.T) {
	middleware.SetValidateContentType(true)

	h := middleware.InputValidation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/detect", strings.NewReader(`{"text":`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid json, got %d", rr.Code)
	}
}

func TestRequestLimits_Returns413ForLargePayload(t *testing.T) {
	middleware.InitRequestLimits(middleware.RequestLimitsConfig{
		MaxBodyBytes:         16,
		DetectTimeoutSeconds: 30,
		ChatTimeoutSeconds:   300,
	})
	middleware.SetValidateContentType(true)

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := middleware.RequestLimits(16)(middleware.InputValidation(final))

	large := `{"text":"12345678901234567890"}`
	req := httptest.NewRequest(http.MethodPost, "/detect", strings.NewReader(large))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for oversized payload, got %d", rr.Code)
	}
}

func TestCORS_DeniesDisallowedOrigin(t *testing.T) {
	middleware.InitCORS(true, "https://allowed.example.com", "GET,POST,OPTIONS", "Content-Type,Authorization", "300")

	h := middleware.CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/detect", nil)
	req.Header.Set("Origin", "https://blocked.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed cors origin, got %d", rr.Code)
	}
}

func TestSecurityHeaders_SetsHeaders(t *testing.T) {
	h := middleware.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("expected X-Frame-Options=DENY")
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options=nosniff")
	}
}

func TestRateLimit_TooManyRequests(t *testing.T) {
	middleware.InitRateLimit(middleware.RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 1,
		Burst:             0,
		DetectPerMinute:   1,
		ChatPerMinute:     1,
		PatternsPerMinute: 1,
		AdminPerMinute:    1,
	})

	h := middleware.RateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := bytes.NewBufferString(`{"text":"hello"}`)
	req1 := httptest.NewRequest(http.MethodPost, "/detect", body)
	req1.Header.Set("Authorization", "Bearer token-a")
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("expected first request to pass, got %d", rr1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/detect", strings.NewReader(`{"text":"hello"}`))
	req2.Header.Set("Authorization", "Bearer token-a")
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to be rate-limited, got %d", rr2.Code)
	}
}
