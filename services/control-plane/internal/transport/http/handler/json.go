package handler

import (
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
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

// safeErrorMessage returns an error string suitable to send back to a client.
// In dev (cfg.AppEnv == "dev") it surfaces the underlying err.Error() so
// engineers can debug locally without grepping logs; in staging/prod it
// returns a generic "internal server error" message so adapter / database
// internals never leak to the wire. The full error is always logged
// server-side via slog so the operator can still correlate.
//
// Pass op ("auth.login", "billing.add_pm", …) and any structured context
// (request id, user id, …) as variadic slog args — they show up alongside the
// error in the server log without ever reaching the client.
//
// Returns "internal server error" when err is nil (defensive — callers
// shouldn't invoke this on a happy path but the helper stays correct if they
// do).
func safeErrorMessage(err error, cfg config.Config, op string, ctx ...any) string {
	if err == nil {
		return "internal server error"
	}
	// Always log the real error so the operator can debug. Promote err into the
	// structured-log key "err" rather than appending it to the message — keeps
	// the slog handler's JSON shape intact.
	args := append([]any{"op", op, "err", err}, ctx...)
	slog.Default().Error("handler error", args...)

	if cfg.AppEnv == "dev" {
		return err.Error()
	}
	return "internal server error"
}
