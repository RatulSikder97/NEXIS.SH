package handler_test

// Coverage for the integrations handler: List, Connect (404 path),
// Disconnect (404 path), GitHubMockInstall (env gate). The happy paths
// of Connect / Disconnect require a real integration adapter; those
// live under tests/integration.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/keyvault"
	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/repo"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
)

// integrationFactory builds a registry with stub deps so the handlers can
// be exercised without launching adapters.
func integrationFactory(t *testing.T) *integration.Registry {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	kv, err := keyvault.NewLocal(key)
	if err != nil {
		t.Fatalf("kv: %v", err)
	}
	return integration.NewRegistry(integration.Deps{
		Repo:         &repo.IntegrationsRepo{},
		KV:           kv,
		IncidentSink: nil,
	})
}

// TestIntegrationsConnect_UnknownProviderIs404 — connecting to a
// non-existent provider returns 404.
func TestIntegrationsConnect_UnknownProviderIs404(t *testing.T) {
	reg := integrationFactory(t)
	h := handler.IntegrationsConnect(reg, noopAuditWriter{}, config.Config{})
	r := chi.NewRouter()
	r.Post("/v1/integrations/{provider}/connect", h)
	req := withPrincipalReq(http.MethodPost, "/v1/integrations/not-a-provider/connect", `{}`, "org-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestIntegrationsConnect_BadJSONIs400 — malformed body returns 400.
func TestIntegrationsConnect_BadJSONIs400(t *testing.T) {
	reg := integrationFactory(t)
	h := handler.IntegrationsConnect(reg, noopAuditWriter{}, config.Config{})
	r := chi.NewRouter()
	r.Post("/v1/integrations/{provider}/connect", h)
	req := httptest.NewRequest(http.MethodPost, "/v1/integrations/github/connect", bytes.NewReader([]byte("{not-json")))
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = 9
	req = withPrincipalReq(http.MethodPost, "/v1/integrations/github/connect", `{not-json`, "org-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestIntegrationsDisconnect_UnknownProviderIs404 — same 404 contract for
// disconnect.
func TestIntegrationsDisconnect_UnknownProviderIs404(t *testing.T) {
	reg := integrationFactory(t)
	h := handler.IntegrationsDisconnect(reg, noopAuditWriter{}, config.Config{})
	r := chi.NewRouter()
	r.Delete("/v1/integrations/{provider}", h)
	req := withPrincipalReq(http.MethodDelete, "/v1/integrations/not-a-provider", "", "org-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestGitHubMockInstall_NonDevReturns403 — the mock-install endpoint must
// refuse to fire outside dev.
func TestGitHubMockInstall_NonDevReturns403(t *testing.T) {
	reg := integrationFactory(t)
	h := handler.GitHubMockInstall(reg, noopAuditWriter{}, "https://example.com", "prod")
	req := withPrincipalReq(http.MethodGet, "/v1/integrations/github/mock_install", "", "org-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "mock install disabled outside dev") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}
