// byg-mock-openai is a test-only upstream for the BYG examples. It retains
// no prompt content: inspection exposes only a request counter, byte count,
// SHA-256 digest, and whether a TSZ mask placeholder was observed.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
)

type observation struct {
	Sequence               int    `json:"sequence"`
	Bytes                  int    `json:"bytes"`
	SHA256                 string `json:"sha256"`
	Masked                 bool   `json:"masked"`
	ContainsSyntheticEmail bool   `json:"contains_synthetic_email"`
}

type server struct {
	mu   sync.RWMutex
	last observation
}

func main() {
	s := &server{}
	http.HandleFunc("/v1/chat/completions", s.chat)
	http.HandleFunc("/inspect", s.inspect)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func (s *server) chat(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		http.Error(w, `{"error":"request too large"}`, http.StatusRequestEntityTooLarge)
		return
	}
	digest := sha256.Sum256(body)
	s.mu.Lock()
	s.last = observation{
		Sequence: s.last.Sequence + 1,
		Bytes:    len(body),
		SHA256:   hex.EncodeToString(digest[:]),
		Masked:   strings.Contains(string(body), "EMAIL_"),
		// This boolean is safe to expose: it identifies only the checked-in
		// synthetic example fixture, never a value received from a user.
		ContainsSyntheticEmail: strings.Contains(string(body), "demo.user@example.test"),
	}
	s.mu.Unlock()
	content := os.Getenv("BYG_MOCK_RESPONSE_CONTENT")
	if content == "" {
		content = "safe mock response"
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-kind-mock", "object": "chat.completion", "choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": content}}}})
}

func (s *server) inspect(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	result := s.last
	s.mu.RUnlock()
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}
