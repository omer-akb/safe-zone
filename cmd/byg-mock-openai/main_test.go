package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestChatServesSSEFixtureEventByEvent(t *testing.T) {
	fixture := "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n" +
		"data: [DONE]\n\n"
	path := filepath.Join(t.TempDir(), "response.sse")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BYG_MOCK_RESPONSE_MODE", "sse")
	t.Setenv("BYG_MOCK_SSE_FIXTURE", path)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	(&server{}).chat(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("content-type"); got != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
	if got := recorder.Body.String(); got != fixture {
		t.Fatalf("body = %q, want fixture %q", got, fixture)
	}
}

func TestSSEFixtureEventsPreservesTrailingNonEventBytes(t *testing.T) {
	fixture := []byte("data: one\n\ntrailing")
	events := sseFixtureEvents(fixture)
	if len(events) != 2 || string(events[0]) != "data: one\n\n" || string(events[1]) != "trailing" {
		t.Fatalf("sseFixtureEvents(%q) = %#v", fixture, events)
	}
}

func TestSSEFixtureEventsSupportsCRLFEventBoundaries(t *testing.T) {
	fixture := []byte("data: one\r\n\r\ndata: two\r\n\r\n")
	events := sseFixtureEvents(fixture)
	if len(events) != 2 || string(events[0]) != "data: one\r\n\r\n" || string(events[1]) != "data: two\r\n\r\n" {
		t.Fatalf("sseFixtureEvents(%q) = %#v", fixture, events)
	}
}
