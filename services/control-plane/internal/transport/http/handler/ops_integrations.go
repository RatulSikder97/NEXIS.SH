package handler

// Operator-facing integration surfaces:
//
//   GET  /v1/integrations/webhooks         — recent webhook_deliveries log
//   POST /v1/integrations/{provider}/probe — manual status refresh

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// IntegrationsWebhooks wires GET /v1/integrations/webhooks. Returns the most
// recent N deliveries for the caller's org, optionally filtered by
// `?provider=<name>`. Reuses parseLimitOffset from agents.go.
//
// nil-safe — when deliveries is unwired (dev/no-pool boot) the handler
// returns an empty page so the dashboard renders the empty state instead of
// 503.
func IntegrationsWebhooks(deliveries *repo.WebhookDeliveriesRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		f := repo.ListFilter{
			OrgID:    princ.OrgID,
			Provider: r.URL.Query().Get("provider"),
		}
		f.Limit, f.Offset = parseLimitOffset(r, 50, 200)

		rows, total, err := deliveries.ListByOrg(r.Context(), f)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]dto.WebhookDeliveryResp, 0, len(rows))
		for _, d := range rows {
			out = append(out, dto.WebhookDeliveryResp{
				ID:          d.ID,
				Provider:    d.Provider,
				EventType:   d.EventType,
				Status:      d.Status,
				LatencyMs:   d.LatencyMs,
				PayloadSize: d.PayloadSize,
				SourceIP:    d.SourceIP,
				Headers:     d.Headers,
				Payload:     d.Payload,
				Error:       d.Error,
				TS:          d.TS.UTC().Format(time.RFC3339Nano),
			})
		}
		writeJSON(w, http.StatusOK, dto.WebhookDeliveriesResp{Deliveries: out, Total: total})
	}
}

// IntegrationProbe wires POST /v1/integrations/{provider}/probe. Calls Status
// on the named adapter and returns the result inline so the dashboard can
// flip the row without a separate refresh.
//
// Audited as integration.probed. Owner|Admin only — already enforced by the
// router group; this handler does not re-check the role.
func IntegrationProbe(reg *integration.Registry, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		providerName := chi.URLParam(r, "provider")
		if reg == nil {
			writeError(w, http.StatusServiceUnavailable, "integrations not wired")
			return
		}
		p, ok := reg.Get(domain.IntegrationProvider(providerName))
		if !ok {
			writeError(w, http.StatusNotFound, "unknown provider")
			return
		}
		start := time.Now()
		c, err := p.Status(r.Context(), princ)
		latency := time.Since(start)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		auditWrite(r, aud, princ, "integration.probed", providerName, map[string]any{
			"latency_ms": latency.Milliseconds(),
			"status":     string(c.Status),
		})
		writeJSON(w, http.StatusOK, dto.IntegrationProbeResp{
			Provider:       string(c.Provider),
			Status:         string(c.Status),
			InstallationID: c.InstallationID,
			Metadata:       c.Metadata,
			LastError:      c.LastError,
			LatencyMs:      latency.Milliseconds(),
			ProbedAt:       time.Now().UTC().Format(time.RFC3339),
		})
	}
}
