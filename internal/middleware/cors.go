package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

var corsConfig = struct {
	enabled        bool
	allowedOrigins map[string]bool
	allowedMethods string
	allowedHeaders string
	maxAge         string
}{
	enabled:        true,
	allowedOrigins: map[string]bool{},
	allowedMethods: "GET,POST,OPTIONS",
	allowedHeaders: "Content-Type,Authorization",
	maxAge:         "300",
}

// InitCORS loads runtime CORS settings.
func InitCORS(enabled bool, originsCSV, methodsCSV, headersCSV, maxAge string) {
	allowed := map[string]bool{}
	for _, v := range strings.Split(originsCSV, ",") {
		origin := strings.TrimSpace(v)
		if origin != "" {
			allowed[origin] = true
		}
	}

	corsConfig.enabled = enabled
	corsConfig.allowedOrigins = allowed
	if strings.TrimSpace(methodsCSV) != "" {
		corsConfig.allowedMethods = methodsCSV
	}
	if strings.TrimSpace(headersCSV) != "" {
		corsConfig.allowedHeaders = headersCSV
	}
	if _, err := strconv.Atoi(strings.TrimSpace(maxAge)); err == nil {
		corsConfig.maxAge = strings.TrimSpace(maxAge)
	}
}

// CORS enforces a fail-secure CORS policy.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !corsConfig.enabled {
			next.ServeHTTP(w, r)
			return
		}

		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		if !corsConfig.allowedOrigins[origin] {
			http.Error(w, "CORS origin is not allowed", http.StatusForbidden)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", corsConfig.allowedMethods)
		w.Header().Set("Access-Control-Allow-Headers", corsConfig.allowedHeaders)
		w.Header().Set("Access-Control-Max-Age", corsConfig.maxAge)

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
