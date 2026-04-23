package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"thyris-sz/internal/auth"
	"thyris-sz/internal/cache"
	"thyris-sz/internal/models"
	"thyris-sz/internal/repository"
)

// ReloadCache manually clears all application caches
func ReloadCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !isAdminAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Clear caches
	cache.ClearCache(cache.KeyPatterns)
	cache.ClearCache(cache.KeyAllowlist)
	cache.ClearCache(cache.KeyBlocklist)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","message":"All caches cleared"}`))
}

// UpdatePatternPolicy allows admin to update pattern-level thresholds
// POST /admin/patterns/policy
func UpdatePatternPolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !isAdminAuthorized(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		PatternID      uint     `json:"pattern_id"`
		BlockThreshold *float64 `json:"block_threshold"`
		AllowThreshold *float64 `json:"allow_threshold"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	pattern, err := repository.GetPatternByID(req.PatternID)
	if err != nil || pattern == nil {
		http.Error(w, "Pattern not found", http.StatusNotFound)
		return
	}

	pattern.BlockThreshold = req.BlockThreshold
	pattern.AllowThreshold = req.AllowThreshold

	if err := repository.UpdatePattern(pattern); err != nil {
		http.Error(w, "Failed to update pattern", http.StatusInternalServerError)
		return
	}

	// Invalidate caches so policy is applied immediately
	cache.ClearCache(cache.KeyPatterns)

	resp := map[string]interface{}{
		"status": "ok",
		"pattern": models.Pattern{
			Model:          pattern.Model,
			Name:           pattern.Name,
			Category:       pattern.Category,
			BlockThreshold: pattern.BlockThreshold,
			AllowThreshold: pattern.AllowThreshold,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func isAdminAuthorized(r *http.Request) bool {
	if auth.HasPermission(r, "cache:admin") || auth.HasPermission(r, "*") {
		return true
	}
	adminKey := strings.TrimSpace(os.Getenv("ADMIN_API_KEY"))
	if adminKey == "" {
		return false
	}
	return strings.TrimSpace(r.Header.Get("X-ADMIN-KEY")) == adminKey
}
