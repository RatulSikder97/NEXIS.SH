package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

// maxWebhookPayloadBytes is the soft cap on payload bodies that get logged to
// webhook_deliveries.payload. Bodies bigger than this are still processed by
// the adapter but only the first N bytes are persisted so the audit row stays
// manageable. Matches repo.payloadCap conceptually; the bytes-cap here runs
// before the JSON re-marshal so we never even parse a giant body for logging.
const maxWebhookPayloadBytes = 32 * 1024

// payloadEventType extracts a short event-type label from the request +
// headers + body for the webhook_deliveries.event_type column. Provider
// adapters use different conventions (X-GitHub-Event, sentry payload.type,
// Datadog event_type, etc.), so we sniff the most common header first and
// fall back to a "<provider>.webhook" label so the column is never empty.
func payloadEventType(provider string, hdrs map[string]string, body []byte) string {
	// Common header names across providers.
	if v := hdrs["X-Github-Event"]; v != "" {
		return v
	}
	if v := hdrs["X-Gitlab-Event"]; v != "" {
		return v
	}
	if v := hdrs["Sentry-Hook-Resource"]; v != "" {
		return v
	}
	if v := hdrs["X-Pagerduty-Webhook-Type"]; v != "" {
		return v
	}
	if v := hdrs["X-Datadog-Event-Type"]; v != "" {
		return v
	}
	// Cheap body sniff: many providers expose a top-level `event` or `type`
	// field. We deliberately limit the unmarshal target to a tiny map so we
	// don't allocate the full payload twice.
	var probe struct {
		Event string `json:"event"`
		Type  string `json:"type"`
		Action string `json:"action"`
	}
	if len(body) > 0 && len(body) < 256*1024 {
		_ = json.Unmarshal(body, &probe)
		if probe.Event != "" {
			return probe.Event
		}
		if probe.Type != "" {
			return probe.Type
		}
		if probe.Action != "" {
			return probe.Action
		}
	}
	return provider + ".webhook"
}

// clientIPFromRequest extracts the best-effort caller IP. Honours
// X-Forwarded-For when present (last hop on the proxy chain — the closest
// non-private address — is what we want for the audit row) and falls back to
// RemoteAddr. Returns "" when nothing parses.
func clientIPFromRequest(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(parts[i])
			if ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// bodyAsJSONForAudit tries to JSON-parse body into a generic map. When the
// payload is too large or non-JSON, returns a synthetic envelope so the
// audit row carries SOMETHING about the body shape.
func bodyAsJSONForAudit(body []byte) map[string]any {
	clip := body
	if len(clip) > maxWebhookPayloadBytes {
		clip = clip[:maxWebhookPayloadBytes]
	}
	var out map[string]any
	if err := json.Unmarshal(clip, &out); err == nil && out != nil {
		return out
	}
	// Non-JSON or malformed — preserve the head as a string so engineers can
	// see what arrived.
	return map[string]any{
		"raw_head_utf8": string(clip),
		"truncated":     len(body) > len(clip),
	}
}

// logDelivery is the shared tail-end of every webhook handler. It records
// one webhook_deliveries row for the just-finished request. nil-safe — when
// deliveries is nil the function is a no-op so the existing dev/no-pool boot
// path still works.
func logDelivery(
	ctx context.Context, //nolint:revive
	deliveries *repo.WebhookDeliveriesRepo,
	orgID, provider, eventType, status string,
	latency time.Duration,
	body []byte,
	hdrs map[string]string,
	sourceIP, errMsg string,
) {
	if deliveries == nil {
		return
	}
	d := repo.WebhookDelivery{
		OrgID:       orgID,
		Provider:    provider,
		EventType:   eventType,
		Status:      status,
		LatencyMs:   int(latency / time.Millisecond),
		PayloadSize: len(body),
		SourceIP:    sourceIP,
		Headers:     hdrs,
		Payload:     bodyAsJSONForAudit(body),
		Error:       errMsg,
	}
	if err := deliveries.Insert(ctx, d); err != nil {
		slog.Default().Warn("webhook_deliveries insert", "provider", provider, "err", err)
	}
}

// Webhook is the public ingest for provider webhooks. The URL carries the
// provider name and the destination org_id so the adapter knows where to
// charge persisted rows; there is no session cookie or bearer — the integrity
// of the call is established by the per-tenant HMAC inside each adapter's
// HandleWebhook.
//
// Pool argument is the admin pool (DATABASE_URL). We open a tx, pin
// app.current_org_id to the URL org_id, and thread the tx into ctx so the
// adapter's repo writes (Get to read the secret; IncidentSink.Insert in the
// sentry adapter) honour RLS even though the request has no Principal.
//
// Status mapping:
//   - 200 on success
//   - 401 on any error containing the substring "HMAC mismatch" — that's the
//     canonical signal from every adapter's signature check
//   - 404 if the provider segment doesn't match a registered adapter
//   - 500 for any other adapter error (DB unreachable, decrypt failure, …)
//
// deliveries (optional) records every attempt — verified, rejected,
// processed, failed — for the operator-facing /v1/integrations/webhooks log.
// When nil the function still works (the dev no-pool boot path).
func Webhook(reg *integration.Registry, pool *pgxpool.Pool, deliveries *repo.WebhookDeliveriesRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		providerName := chi.URLParam(r, "provider")
		orgID := chi.URLParam(r, "org_id")
		sourceIP := clientIPFromRequest(r)

		// Header snapshot is built once so we can hand the same map to both
		// the adapter and the delivery audit row.
		hdrs := make(map[string]string, len(r.Header))
		for k := range r.Header {
			hdrs[k] = r.Header.Get(k)
		}

		body, berr := io.ReadAll(r.Body)
		if berr != nil {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
			logDelivery(r.Context(), deliveries, orgID, providerName,
				"unknown", "failed", time.Since(started), nil, hdrs, sourceIP, berr.Error())
			return
		}
		eventType := payloadEventType(providerName, hdrs, body)

		p, ok := reg.Get(domain.IntegrationProvider(providerName))
		if !ok {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
			logDelivery(r.Context(), deliveries, orgID, providerName,
				eventType, "rejected", time.Since(started), body, hdrs, sourceIP, "unknown provider")
			return
		}

		ctx := r.Context()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			slog.Default().Error("webhook: begin tx", "err", err)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			logDelivery(ctx, deliveries, orgID, providerName,
				eventType, "failed", time.Since(started), body, hdrs, sourceIP, err.Error())
			return
		}
		defer func() {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				slog.Default().Error("webhook: rollback", "err", rbErr)
			}
		}()

		if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
			slog.Default().Error("webhook: set org_id", "err", err, "org_id", orgID)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			logDelivery(ctx, deliveries, orgID, providerName,
				eventType, "failed", time.Since(started), body, hdrs, sourceIP, err.Error())
			return
		}

		txCtx := db.WithTx(ctx, tx)
		hErr := p.HandleWebhook(txCtx, orgID, hdrs, body)
		if hErr != nil {
			// HMAC failures are the canonical 401 signal — every adapter
			// returns an error containing "HMAC mismatch" or "missing
			// signature" on auth failure. Map both to 401 so the client can
			// distinguish from a transient 500.
			if isHMACError(hErr) {
				httpJSON(w, http.StatusUnauthorized, map[string]string{"error": hErr.Error()})
				logDelivery(ctx, deliveries, orgID, providerName,
					eventType, "rejected", time.Since(started), body, hdrs, sourceIP, hErr.Error())
				return
			}
			slog.Default().Error("webhook handler", "provider", providerName, "err", hErr)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": hErr.Error()})
			logDelivery(ctx, deliveries, orgID, providerName,
				eventType, "failed", time.Since(started), body, hdrs, sourceIP, hErr.Error())
			return
		}

		// Record the successful delivery row INSIDE the same tx so RLS picks
		// up the GUC we set above. Falls back to a post-commit insert if
		// deliveries is wired with its own admin pool route (the repo handles
		// either).
		logDelivery(txCtx, deliveries, orgID, providerName,
			eventType, "processed", time.Since(started), body, hdrs, sourceIP, "")

		if err := tx.Commit(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Default().Error("webhook: commit", "err", err)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit"})
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// WebhookByQuery is the variant of Webhook for providers whose webhook URL is
// configured by the customer in their own dashboard (Datadog, PagerDuty). The
// provider segment is wired statically at mount time; the destination org_id
// comes from the `?org=<org_id>` query string so a single mount can fan out
// to every tenant without per-tenant routes.
//
// Behaviour is otherwise identical to Webhook: same admin-pool tx, same
// app.current_org_id pinning via set_config(...,true), same HMAC-mismatch →
// 401 mapping. Missing `?org` returns 404 — without a target org the adapter
// has no key to look up and we want this to look like a routing failure to
// the upstream so it doesn't keep retrying.
func WebhookByQuery(provider domain.IntegrationProvider, reg *integration.Registry, pool *pgxpool.Pool, deliveries *repo.WebhookDeliveriesRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		sourceIP := clientIPFromRequest(r)
		providerName := string(provider)
		orgID := r.URL.Query().Get("org")
		hdrs := make(map[string]string, len(r.Header))
		for k := range r.Header {
			hdrs[k] = r.Header.Get(k)
		}

		if orgID == "" {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": "missing org query parameter"})
			logDelivery(r.Context(), deliveries, "", providerName,
				providerName+".webhook", "rejected", time.Since(started), nil, hdrs, sourceIP, "missing org")
			return
		}
		body, berr := io.ReadAll(r.Body)
		if berr != nil {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
			logDelivery(r.Context(), deliveries, orgID, providerName,
				providerName+".webhook", "failed", time.Since(started), nil, hdrs, sourceIP, berr.Error())
			return
		}
		eventType := payloadEventType(providerName, hdrs, body)

		p, ok := reg.Get(provider)
		if !ok {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
			logDelivery(r.Context(), deliveries, orgID, providerName,
				eventType, "rejected", time.Since(started), body, hdrs, sourceIP, "unknown provider")
			return
		}

		ctx := r.Context()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			slog.Default().Error("webhook_q: begin tx", "err", err)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			logDelivery(ctx, deliveries, orgID, providerName,
				eventType, "failed", time.Since(started), body, hdrs, sourceIP, err.Error())
			return
		}
		defer func() {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				slog.Default().Error("webhook_q: rollback", "err", rbErr)
			}
		}()

		if _, err := tx.Exec(ctx, "SELECT set_config('app.current_org_id', $1, true)", orgID); err != nil {
			slog.Default().Error("webhook_q: set org_id", "err", err, "org_id", orgID)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			logDelivery(ctx, deliveries, orgID, providerName,
				eventType, "failed", time.Since(started), body, hdrs, sourceIP, err.Error())
			return
		}

		txCtx := db.WithTx(ctx, tx)
		hErr := p.HandleWebhook(txCtx, orgID, hdrs, body)
		if hErr != nil {
			if isHMACError(hErr) {
				httpJSON(w, http.StatusUnauthorized, map[string]string{"error": hErr.Error()})
				logDelivery(ctx, deliveries, orgID, providerName,
					eventType, "rejected", time.Since(started), body, hdrs, sourceIP, hErr.Error())
				return
			}
			slog.Default().Error("webhook_q handler",
				"provider", providerName, "err", hErr)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": hErr.Error()})
			logDelivery(ctx, deliveries, orgID, providerName,
				eventType, "failed", time.Since(started), body, hdrs, sourceIP, hErr.Error())
			return
		}

		logDelivery(txCtx, deliveries, orgID, providerName,
			eventType, "processed", time.Since(started), body, hdrs, sourceIP, "")

		if err := tx.Commit(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			slog.Default().Error("webhook_q: commit", "err", err)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "commit"})
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

// isHMACError matches the canonical signature-failure errors returned by each
// integration adapter. Keeping the matcher loose (substring) lets new adapters
// adopt the same phrasing without a wiring change here. The exact strings come
// from github/sentry/argocd/datadog/pagerduty provider.go's HandleWebhook.
func isHMACError(err error) bool {
	s := err.Error()
	return containsAny(s, []string{
		"HMAC mismatch",
		"missing X-Hub-Signature",
		"missing Sentry-Hook-Signature",
		"missing argocd webhook signature",
		"missing X-Datadog-Signature",
		"missing webhook signature",
		"missing X-PagerDuty-Signature",
	})
}

// containsAny returns true if any of the targets appears as a substring of s.
func containsAny(s string, targets []string) bool {
	for _, t := range targets {
		if len(t) == 0 {
			continue
		}
		if indexOfSub(s, t) >= 0 {
			return true
		}
	}
	return false
}

func indexOfSub(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
