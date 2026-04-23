package unit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"thyris-sz/internal/auth"
)

func TestAuthMiddleware_PublicPathBypass(t *testing.T) {
	auth.Init(auth.AuthConfig{
		Enabled:            true,
		TokenPermissions:   "token_detect=detect:read",
		PublicPaths:        "/healthz,/ready",
		RequireBearerToken: true,
	})

	h := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for public path, got %d", rr.Code)
	}
}

func TestAuthMiddleware_RejectsMissingBearer(t *testing.T) {
	auth.Init(auth.AuthConfig{
		Enabled:            true,
		TokenPermissions:   "token_detect=detect:read",
		PublicPaths:        "/healthz,/ready",
		RequireBearerToken: true,
	})

	h := auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/detect", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing bearer token, got %d", rr.Code)
	}
}

func TestAuthRequirePermission_AllowsScopedToken(t *testing.T) {
	auth.Init(auth.AuthConfig{
		Enabled:            true,
		TokenPermissions:   "token_detect=detect:read",
		PublicPaths:        "/healthz,/ready",
		RequireBearerToken: true,
	})

	finalHandler := auth.RequirePermission("detect:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h := auth.Middleware(finalHandler)

	req := httptest.NewRequest(http.MethodPost, "/detect", nil)
	req.Header.Set("Authorization", "Bearer token_detect")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for token with detect:read, got %d", rr.Code)
	}
}

func TestAuthRequirePermission_ForbiddenWithoutPermission(t *testing.T) {
	auth.Init(auth.AuthConfig{
		Enabled:            true,
		TokenPermissions:   "token_detect=detect:read",
		PublicPaths:        "/healthz,/ready",
		RequireBearerToken: true,
	})

	finalHandler := auth.RequirePermission("patterns:admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h := auth.Middleware(finalHandler)

	req := httptest.NewRequest(http.MethodPost, "/patterns", nil)
	req.Header.Set("Authorization", "Bearer token_detect")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for token without patterns:admin, got %d", rr.Code)
	}
}

func TestAuthMiddleware_AdminKeyCompatibility(t *testing.T) {
	auth.Init(auth.AuthConfig{
		Enabled:            true,
		TokenPermissions:   "",
		AdminAPIKey:        "test-admin-key",
		PublicPaths:        "/healthz,/ready",
		RequireBearerToken: false,
	})

	finalHandler := auth.RequirePermission("cache:admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	h := auth.Middleware(finalHandler)

	req := httptest.NewRequest(http.MethodPost, "/admin/reload", nil)
	req.Header.Set("X-ADMIN-KEY", "test-admin-key")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid admin key compatibility path, got %d", rr.Code)
	}
}
