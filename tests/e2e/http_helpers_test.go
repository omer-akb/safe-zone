package e2e

import (
	"net/http"
	"os"
	"strings"
)

func testAdminKey() string {
	if v := strings.TrimSpace(os.Getenv("TSZ_TEST_ADMIN_KEY")); v != "" {
		return v
	}
	return "test-admin-key"
}

func applyE2EAuthHeaders(req *http.Request) {
	if req == nil {
		return
	}

	if token := strings.TrimSpace(os.Getenv("TSZ_TEST_BEARER_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if key := strings.TrimSpace(os.Getenv("TSZ_TEST_ADMIN_KEY")); key != "" {
		req.Header.Set("X-ADMIN-KEY", key)
	}
}
