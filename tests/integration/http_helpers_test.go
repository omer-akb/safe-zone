package integration

import (
	"io"
	"net/http"
	"os"
	"strings"
)

func applyTestAuthHeaders(req *http.Request) {
	if req == nil {
		return
	}

	if token := strings.TrimSpace(os.Getenv("TSZ_TEST_BEARER_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if adminKey := strings.TrimSpace(os.Getenv("TSZ_TEST_ADMIN_KEY")); adminKey != "" {
		req.Header.Set("X-ADMIN-KEY", adminKey)
	}
}

func postJSONWithAuth(client *http.Client, url string, body io.Reader) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyTestAuthHeaders(req)
	return client.Do(req)
}
