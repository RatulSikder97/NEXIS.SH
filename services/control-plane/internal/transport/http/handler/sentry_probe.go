// Package handler — Sentry-webhook probe HTTP surface (Phase 8 — public beta).
//
// One endpoint: POST /v1/integrations/sentry/probe. BetterStack hits this
// every 30s with an HMAC-signed body so we can monitor the Sentry-ingest path
// end-to-end (signature verification + payload parsing) without needing a
// real Sentry org to be connected at the time of the probe.
//
// The shared dev-default secret is read from env GITHUB_WEBHOOK_SECRET — same
// fallback the GitHub adapter uses for its install probe in dev — but
// overridden by SENTRY_PROBE_SECRET when set. Production callers pass a
// 32-byte secret; the response is 200 + {ok: true} on valid signature and
// 401 otherwise.
//
// No auth on the route — the signature IS the auth.
package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
)

// sentryProbeResp mirrors the BetterStack-friendly shape: {ok: true, message:
// "..."}.
type sentryProbeResp struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// SentryProbe wires POST /v1/integrations/sentry/probe. The probe expects the
// header "Sentry-Hook-Signature: <hex(hmac-sha256(secret, body))>" — same
// header shape Sentry uses on its real webhooks so this code path exercises
// the same parser.
//
// The probe is intentionally permissive on payload: any body is accepted as
// long as the signature is correct.
func SentryProbe(secret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sig := r.Header.Get("Sentry-Hook-Signature")
		if sig == "" {
			writeJSON(w, http.StatusUnauthorized, sentryProbeResp{Message: "missing signature"})
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, sentryProbeResp{Message: "body read failed"})
			return
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(sig)) {
			writeJSON(w, http.StatusUnauthorized, sentryProbeResp{Message: "signature mismatch"})
			return
		}
		writeJSON(w, http.StatusOK, sentryProbeResp{OK: true, Message: "probe ok"})
	}
}
