package handler_test

// Coverage for /v1/me/preferences GET + PATCH. Uses the local provider's
// MemStore so the persistence path is a real round-trip without Postgres.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// newAuthForPrefs constructs a local.Provider over the in-memory store with
// a single seeded user so the preferences handler has something to read /
// write against.
func newAuthForPrefs(t *testing.T) (*local.Provider, domain.Principal) {
	t.Helper()
	store := local.NewMemStore()
	provider := local.New(local.Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
		Clock:         func() time.Time { return time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC) },
	})
	ctx := context.Background()
	res, err := provider.Signup(ctx, domain.SignupInput{
		Email: "alice@example.com", Password: "passw0rd!", OrgName: "AliceCo",
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	princ := domain.Principal{
		UserID: res.User.ID,
		OrgID:  res.Org.ID,
		Role:   domain.RoleOwner,
	}
	return provider, princ
}

// TestGetPreferences_EmptyDefault — first call returns "{}" (empty object).
func TestGetPreferences_EmptyDefault(t *testing.T) {
	auth, princ := newAuthForPrefs(t)
	h := handler.GetPreferences(auth)
	req := httptest.NewRequest(http.MethodGet, "/v1/me/preferences", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var p map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p) != 0 {
		t.Fatalf("expected empty prefs, got %+v", p)
	}
}

// TestPatchPreferences_HappyPath — write then read back returns the same
// payload.
func TestPatchPreferences_HappyPath(t *testing.T) {
	auth, princ := newAuthForPrefs(t)
	patch := handler.PatchPreferences(auth, noopAuditWriter{})
	get := handler.GetPreferences(auth)

	body, _ := json.Marshal(map[string]any{
		"theme":            "dark",
		"sidebar_collapsed": true,
	})
	req := httptest.NewRequest(http.MethodPatch, "/v1/me/preferences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	patch.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("patch status: %d body=%s", rec.Code, rec.Body.String())
	}

	// Read back.
	greq := httptest.NewRequest(http.MethodGet, "/v1/me/preferences", nil)
	greq = greq.WithContext(appmw.WithPrincipal(greq.Context(), princ))
	grec := httptest.NewRecorder()
	get.ServeHTTP(grec, greq)
	if grec.Code != http.StatusOK {
		t.Fatalf("get status: %d", grec.Code)
	}
	var got map[string]any
	if err := json.NewDecoder(grec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["theme"] != "dark" {
		t.Fatalf("theme: %v", got["theme"])
	}
	if got["sidebar_collapsed"] != true {
		t.Fatalf("sidebar_collapsed: %v", got["sidebar_collapsed"])
	}
}

// TestPatchPreferences_BadJSONIs400 — malformed body returns 400.
func TestPatchPreferences_BadJSONIs400(t *testing.T) {
	auth, princ := newAuthForPrefs(t)
	patch := handler.PatchPreferences(auth, noopAuditWriter{})
	req := httptest.NewRequest(http.MethodPatch, "/v1/me/preferences", bytes.NewReader([]byte("{not")))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	patch.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rec.Code)
	}
}

// TestPatchPreferences_InvalidThemeIs400 — the theme field has a fixed
// allowlist.
func TestPatchPreferences_InvalidThemeIs400(t *testing.T) {
	auth, princ := newAuthForPrefs(t)
	patch := handler.PatchPreferences(auth, noopAuditWriter{})
	body, _ := json.Marshal(map[string]any{"theme": "rainbow"})
	req := httptest.NewRequest(http.MethodPatch, "/v1/me/preferences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	patch.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid theme") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestPatchPreferences_OtherKeysPassThrough — keys other than "theme" are
// accepted verbatim.
func TestPatchPreferences_OtherKeysPassThrough(t *testing.T) {
	auth, princ := newAuthForPrefs(t)
	patch := handler.PatchPreferences(auth, noopAuditWriter{})
	body, _ := json.Marshal(map[string]any{
		"custom_field":     "custom_value",
		"numeric_setting":  42,
	})
	req := httptest.NewRequest(http.MethodPatch, "/v1/me/preferences", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	patch.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}
