// Package handler — integration HTTP surface.
//
// Phase 3 wires four endpoints under /v1/integrations: list, connect,
// disconnect, and a dev-only mock-install for GitHub that fakes the App
// installation step end-to-end inside a single signed-in browser session.
// All four sit behind RequireAuth + RLS; the mutating three additionally sit
// behind RequireRole(owner, admin). See server.go for the routing layout.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// toResp converts a domain.Connection into the JSON wire shape. Status is
// stringified verbatim — the IntegrationStatus constants already mirror the
// enum constraint on the table.
func toResp(c domain.Connection) dto.IntegrationResp {
	return dto.IntegrationResp{
		Provider:       string(c.Provider),
		Status:         string(c.Status),
		InstallationID: c.InstallationID,
		Metadata:       c.Metadata,
		LastError:      c.LastError,
		CreatedAt:      c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// IntegrationsList wires GET /v1/integrations. Returns the caller's org's
// integration rows in stable provider order (the repo already orders by
// provider). The body is always an array — never null — so callers can iterate
// without a nil check.
func IntegrationsList(reg *integration.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		rows, err := reg.List(r.Context(), princ)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]dto.IntegrationResp, 0, len(rows))
		for _, c := range rows {
			out = append(out, toResp(c))
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// IntegrationsConnect wires POST /v1/integrations/{provider}/connect. The body
// is a free-form map fed to the adapter's Connect; each adapter validates its
// own required keys (github wants installation_id, sentry wants webhook_secret).
// On success an audit row is appended with the installation_id metadata.
//
// appCfg is the process-wide config — used only to route the 400-arm
// adapter error through safeErrorMessage so prod doesn't leak adapter
// internals (e.g. raw HMAC mismatch strings, decode hints) to the wire.
func IntegrationsConnect(reg *integration.Registry, aud domain.AuditWriter, appCfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		providerName := chi.URLParam(r, "provider")
		p, ok := reg.Get(domain.IntegrationProvider(providerName))
		if !ok {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
			return
		}
		var connectCfg dto.ConnectReq
		if r.ContentLength > 0 {
			var raw map[string]any
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				httpJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
				return
			}
			if inner, ok := raw["config"].(map[string]any); ok {
				connectCfg = dto.ConnectReq(inner)
			} else {
				connectCfg = dto.ConnectReq(raw)
			}
		}
		if connectCfg == nil {
			connectCfg = dto.ConnectReq{}
		}
		c, err := p.Connect(r.Context(), princ, connectCfg)
		if err != nil {
			httpJSON(w, http.StatusBadRequest, map[string]string{"error": safeErrorMessage(err, appCfg, "integrations.connect", "provider", providerName)})
			return
		}
		if aud != nil {
			meta := map[string]any{"installation_id": c.InstallationID}
			if err := aud.Write(r.Context(), princ, "integration.connected", providerName, meta); err != nil {
				slog.Default().Error("audit write", "action", "integration.connected", "err", err)
			}
		}
		httpJSON(w, http.StatusOK, toResp(c))
	}
}

// IntegrationsDisconnect wires DELETE /v1/integrations/{provider}. Idempotent
// — the repo's Delete returns nil even if no row existed, so we always emit
// 204 on a successful auth. The audit row is appended regardless.
//
// appCfg routes the 500-arm error through safeErrorMessage so prod
// returns "internal server error" instead of the raw cause.
func IntegrationsDisconnect(reg *integration.Registry, aud domain.AuditWriter, appCfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		providerName := chi.URLParam(r, "provider")
		p, ok := reg.Get(domain.IntegrationProvider(providerName))
		if !ok {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
			return
		}
		if err := p.Disconnect(r.Context(), princ); err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": safeErrorMessage(err, appCfg, "integrations.disconnect", "provider", providerName)})
			return
		}
		if aud != nil {
			if err := aud.Write(r.Context(), princ, "integration.disconnected", providerName, nil); err != nil {
				slog.Default().Error("audit write", "action", "integration.disconnected", "err", err)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// GitHubMockInstall wires GET /v1/integrations/github/mock_install. In dev only
// (cfg.AppEnv == "dev"), it Connects a synthetic installation derived from the
// caller's org_id and 302s back to the console so the operator can verify the
// end-to-end Connect → list path without a real GitHub App. Audited with
// via=mock so the row can be filtered out of compliance reports if needed.
func GitHubMockInstall(reg *integration.Registry, aud domain.AuditWriter, appBaseURL, appEnv string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if appEnv != "dev" {
			httpJSON(w, http.StatusForbidden, map[string]string{"error": "mock install disabled outside dev"})
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		p, ok := reg.Get(domain.IntegrationGitHub)
		if !ok {
			httpJSON(w, http.StatusNotFound, map[string]string{"error": "github provider not configured"})
			return
		}
		// princ.OrgID is a uuid string; first 8 chars give a stable id without
		// leaking the full org id in the installation field.
		idSuffix := princ.OrgID
		if len(idSuffix) > 8 {
			idSuffix = idSuffix[:8]
		}
		installID := "mock-" + idSuffix
		c, err := p.Connect(r.Context(), princ, map[string]any{"installation_id": installID})
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if aud != nil {
			meta := map[string]any{"installation_id": c.InstallationID, "via": "mock"}
			if err := aud.Write(r.Context(), princ, "integration.connected", "github", meta); err != nil {
				slog.Default().Error("audit write", "action", "integration.connected", "err", err)
			}
		}
		http.Redirect(w, r, appBaseURL+"/console/integrations?installed=github", http.StatusFound)
	}
}

