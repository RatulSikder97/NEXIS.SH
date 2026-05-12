package handler

import (
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// httpJSON is the shared {code, body→json} helper used by handlers added in
// Phase 3 (integrations, invites, audit list). Existing Phase 2 handlers use
// writeJSON in auth.go — that one wraps via dto.ErrorResp envelopes. Both
// coexist deliberately; the older form is kept for compatibility with the
// dto-typed responses while httpJSON serves the newer map-typed payloads.
func httpJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Error("encode json", "err", err)
	}
}

// toHex is a thin alias for encoding/hex.EncodeToString so handlers can call it
// without an extra import. Used to surface invite token_hash values on the wire.
func toHex(b []byte) string { return hex.EncodeToString(b) }

// nullableTimeStr returns the RFC3339 form of t, or the empty string when t is
// nil. Used for nullable timestamp columns surfaced in JSON (claimed_at,
// last_used_at, etc.).
func nullableTimeStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
