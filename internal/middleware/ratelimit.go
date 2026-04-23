package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

type RateLimitConfig struct {
	Enabled           bool
	RequestsPerSecond int
	Burst             int
	DetectPerMinute   int
	ChatPerMinute     int
	PatternsPerMinute int
	AdminPerMinute    int
}

var rlState = struct {
	sync.Mutex
	cfg       RateLimitConfig
	global    *globalCounter
	perKeyMap map[string]*windowCounter
}{
	cfg: RateLimitConfig{
		Enabled:           false,
		RequestsPerSecond: 100,
		Burst:             1000,
		DetectPerMinute:   1000,
		ChatPerMinute:     100,
		PatternsPerMinute: 50,
		AdminPerMinute:    10,
	},
	perKeyMap: map[string]*windowCounter{},
}

type windowCounter struct {
	count     int
	windowEnd time.Time
}

type globalCounter struct {
	count     int
	windowEnd time.Time
}

func InitRateLimit(cfg RateLimitConfig) {
	rlState.Lock()
	defer rlState.Unlock()

	rlState.cfg = cfg
	if rlState.cfg.RequestsPerSecond <= 0 {
		rlState.cfg.RequestsPerSecond = 100
	}
	if rlState.cfg.Burst <= 0 {
		rlState.cfg.Burst = 1000
	}
	rlState.global = &globalCounter{count: 0, windowEnd: time.Now().Add(time.Second)}
	if rlState.perKeyMap == nil {
		rlState.perKeyMap = map[string]*windowCounter{}
	}
}

func RateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rlState.Lock()
		cfg := rlState.cfg
		rlState.Unlock()

		if !cfg.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		if !allowGlobal(cfg) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		limit := perMinuteLimitForPath(cfg, r.URL.Path)
		if limit <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		key := clientRateKey(r)
		if !allowPerMinute(key+":"+r.URL.Path, limit) {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func perMinuteLimitForPath(cfg RateLimitConfig, path string) int {
	switch {
	case path == "/detect":
		return cfg.DetectPerMinute
	case strings.HasPrefix(path, "/v1/chat/completions"):
		return cfg.ChatPerMinute
	case strings.HasPrefix(path, "/patterns"):
		return cfg.PatternsPerMinute
	case strings.HasPrefix(path, "/admin"):
		return cfg.AdminPerMinute
	default:
		return 0
	}
}

func clientRateKey(r *http.Request) string {
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); auth != "" {
		return auth
	}
	if apiKey := strings.TrimSpace(r.Header.Get("X-API-Key")); apiKey != "" {
		return apiKey
	}
	return r.RemoteAddr
}

func allowPerMinute(key string, limit int) bool {
	now := time.Now()
	rlState.Lock()
	defer rlState.Unlock()

	counter, ok := rlState.perKeyMap[key]
	if !ok || now.After(counter.windowEnd) {
		rlState.perKeyMap[key] = &windowCounter{count: 1, windowEnd: now.Add(time.Minute)}
		return true
	}

	if counter.count >= limit {
		return false
	}

	counter.count++
	return true
}

func allowGlobal(cfg RateLimitConfig) bool {
	if cfg.RequestsPerSecond <= 0 {
		return true
	}

	rlState.Lock()
	defer rlState.Unlock()

	now := time.Now()
	if rlState.global == nil || now.After(rlState.global.windowEnd) {
		rlState.global = &globalCounter{count: 1, windowEnd: now.Add(time.Second)}
		return true
	}

	maxInWindow := cfg.RequestsPerSecond + cfg.Burst
	if maxInWindow <= 0 {
		maxInWindow = cfg.RequestsPerSecond
	}
	if rlState.global.count >= maxInWindow {
		return false
	}

	rlState.global.count++
	return true
}
