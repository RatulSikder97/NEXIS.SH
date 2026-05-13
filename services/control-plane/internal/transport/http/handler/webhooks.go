package handler

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/db"
)

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
func Webhook(reg *integration.Registry, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := chi.URLParam(r, "provider")
		orgID := chi.URLParam(r, "org_id")
		p, ok := reg.Get(domain.IntegrationProvider(providerName))
		if !ok {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
			return
		}
		// Copy headers into a string→string map. Each adapter only reads its
		// own signature header (X-Hub-Signature-256, Sentry-Hook-Signature,
		// …), so last-wins via Get is fine.
		hdrs := make(map[string]string, len(r.Header))
		for k := range r.Header {
			hdrs[k] = r.Header.Get(k)
		}

		ctx := r.Context()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			slog.Default().Error("webhook: begin tx", "err", err)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
			return
		}

		err = p.HandleWebhook(db.WithTx(ctx, tx), orgID, hdrs, body)
		if err != nil {
			// HMAC failures are the canonical 401 signal — every adapter
			// returns an error containing "HMAC mismatch" or "missing
			// signature" on auth failure. Map both to 401 so the client can
			// distinguish from a transient 500.
			if isHMACError(err) {
				httpJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			slog.Default().Error("webhook handler", "provider", providerName, "err", err)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

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
func WebhookByQuery(provider domain.IntegrationProvider, reg *integration.Registry, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		orgID := r.URL.Query().Get("org")
		if orgID == "" {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": "missing org query parameter"})
			return
		}
		p, ok := reg.Get(provider)
		if !ok {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": "read body"})
			return
		}
		hdrs := make(map[string]string, len(r.Header))
		for k := range r.Header {
			hdrs[k] = r.Header.Get(k)
		}

		ctx := r.Context()
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			slog.Default().Error("webhook_q: begin tx", "err", err)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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
			return
		}

		err = p.HandleWebhook(db.WithTx(ctx, tx), orgID, hdrs, body)
		if err != nil {
			if isHMACError(err) {
				httpJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
				return
			}
			slog.Default().Error("webhook_q handler",
				"provider", string(provider), "err", err)
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

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
