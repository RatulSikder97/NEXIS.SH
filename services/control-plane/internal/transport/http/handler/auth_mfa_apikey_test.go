package handler_test

// Coverage for the MFA and API-key handlers driven through the real
// local.Provider so the happy paths run end-to-end with no Postgres.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/handler"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// newAuthForMFA constructs a local provider + signed-in owner principal.
func newAuthForMFA(t *testing.T) (*local.Provider, domain.Principal) {
	t.Helper()
	store := local.NewMemStore()
	provider := local.New(local.Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        &local.TestMailer{},
		BaseURL:       "http://localhost:3000",
		Clock:         func() time.Time { return time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC) },
	})
	res, err := provider.Signup(context.Background(), domain.SignupInput{
		Email: "alice@example.com", Password: "passw0rd!", OrgName: "AliceCo",
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	princ := domain.Principal{
		UserID: res.User.ID, OrgID: res.Org.ID, Role: domain.RoleOwner,
	}
	return provider, princ
}

// TestMFAEnroll_ReturnsQRPngAndSecretInDev — dev mode exposes the raw
// secret so test harnesses can compute a TOTP code without decoding the
// PNG. Production hides the secret.
func TestMFAEnroll_ReturnsQRPngAndSecretInDev(t *testing.T) {
	auth, princ := newAuthForMFA(t)
	h := handler.MFAEnroll(auth, noopAuditWriter{}, config.Config{AppEnv: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/mfa/enroll", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		QRDataURL string `json:"qr_data_url"`
		Secret    string `json:"secret"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(resp.QRDataURL, "data:image/png;base64,") {
		t.Fatalf("qr data url shape: %q", resp.QRDataURL[:40])
	}
	if resp.Secret == "" {
		t.Fatalf("dev mode should expose secret")
	}
}

// TestMFAEnroll_HidesSecretInProd — prod mode must NOT surface the secret.
func TestMFAEnroll_HidesSecretInProd(t *testing.T) {
	auth, princ := newAuthForMFA(t)
	h := handler.MFAEnroll(auth, noopAuditWriter{}, config.Config{AppEnv: "prod"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/mfa/enroll", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Secret != "" {
		t.Fatalf("prod mode must NOT expose secret, got %q", resp.Secret)
	}
}

// TestMFAVerify_HappyPath — enroll, generate a valid TOTP code, verify
// returns 204.
func TestMFAVerify_HappyPath(t *testing.T) {
	auth, princ := newAuthForMFA(t)
	enroll := handler.MFAEnroll(auth, noopAuditWriter{}, config.Config{AppEnv: "dev"})
	verify := handler.MFAVerify(auth, noopAuditWriter{})

	// Enroll first.
	enrollReq := httptest.NewRequest(http.MethodPost, "/", nil)
	enrollReq = enrollReq.WithContext(appmw.WithPrincipal(enrollReq.Context(), princ))
	enrollRec := httptest.NewRecorder()
	enroll.ServeHTTP(enrollRec, enrollReq)
	var enrollResp struct {
		Secret string `json:"secret"`
	}
	_ = json.NewDecoder(enrollRec.Body).Decode(&enrollResp)
	if enrollResp.Secret == "" {
		t.Fatalf("enroll secret empty")
	}

	// Compute TOTP code from the secret using real time (verifyTOTP uses
	// time.Now() internally).
	code, err := totp.GenerateCode(enrollResp.Secret, time.Now())
	if err != nil {
		t.Fatalf("totp: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	verifyReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyReq = verifyReq.WithContext(appmw.WithPrincipal(verifyReq.Context(), princ))
	verifyRec := httptest.NewRecorder()
	verify.ServeHTTP(verifyRec, verifyReq)
	if verifyRec.Code != http.StatusNoContent {
		t.Fatalf("verify status: %d body=%s", verifyRec.Code, verifyRec.Body.String())
	}
}

// TestMFAVerify_BadCodeIs400 — wrong code returns 400.
func TestMFAVerify_BadCodeIs400(t *testing.T) {
	auth, princ := newAuthForMFA(t)
	enroll := handler.MFAEnroll(auth, noopAuditWriter{}, config.Config{AppEnv: "dev"})
	verify := handler.MFAVerify(auth, noopAuditWriter{})

	// Enroll so the user has a secret.
	enrollReq := httptest.NewRequest(http.MethodPost, "/", nil)
	enrollReq = enrollReq.WithContext(appmw.WithPrincipal(enrollReq.Context(), princ))
	enrollRec := httptest.NewRecorder()
	enroll.ServeHTTP(enrollRec, enrollReq)

	body, _ := json.Marshal(map[string]string{"code": "000000"})
	verifyReq := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyReq = verifyReq.WithContext(appmw.WithPrincipal(verifyReq.Context(), princ))
	verifyRec := httptest.NewRecorder()
	verify.ServeHTTP(verifyRec, verifyReq)
	if verifyRec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", verifyRec.Code)
	}
}

// TestAPIKeyCreate_HappyPath — POST /v1/apikeys returns 201 + plaintext key.
func TestAPIKeyCreate_HappyPath(t *testing.T) {
	auth, princ := newAuthForMFA(t)
	h := handler.APIKeyCreate(auth, noopAuditWriter{})
	body, _ := json.Marshal(map[string]any{"name": "CI key", "scopes": []string{"read"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Plaintext string `json:"plaintext_once"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Plaintext == "" {
		t.Fatalf("plaintext must be set on Create")
	}
	if resp.Name != "CI key" {
		t.Fatalf("name: %q", resp.Name)
	}
}

// TestAPIKeyCreate_MissingNameIs400 — empty name returns 400.
func TestAPIKeyCreate_MissingNameIs400(t *testing.T) {
	auth, princ := newAuthForMFA(t)
	h := handler.APIKeyCreate(auth, noopAuditWriter{})
	body, _ := json.Marshal(map[string]any{"scopes": []string{}})
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAPIKeyList_HappyPath — list returns the keys for the principal.
func TestAPIKeyList_HappyPath(t *testing.T) {
	auth, princ := newAuthForMFA(t)
	// Create two first.
	_, _ = auth.CreateAPIKey(context.Background(), princ, "key-1", []string{"read"})
	_, _ = auth.CreateAPIKey(context.Background(), princ, "key-2", []string{"write"})

	h := handler.APIKeyList(auth)
	req := httptest.NewRequest(http.MethodGet, "/v1/apikeys", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "key-1") || !strings.Contains(rec.Body.String(), "key-2") {
		t.Fatalf("body: %s", rec.Body.String())
	}
}

// TestMe_HappyPath — Me returns the principal's user + org info.
func TestMe_HappyPath(t *testing.T) {
	auth, princ := newAuthForMFA(t)
	h := handler.Me(auth, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	var me struct {
		User struct{ Email string `json:"email"` } `json:"user"`
		Org  struct{ Name string `json:"name"` }  `json:"org"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&me); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if me.User.Email != "alice@example.com" {
		t.Fatalf("email: %q", me.User.Email)
	}
	if me.Org.Name != "AliceCo" {
		t.Fatalf("org name: %q", me.Org.Name)
	}
}

// TestLogout_ClearsSessionCookie — Logout returns 204 + a clear-cookie header.
func TestLogout_ClearsSessionCookie(t *testing.T) {
	auth, princ := newAuthForMFA(t)
	h := handler.Logout(auth, noopAuditWriter{}, config.Config{AppEnv: "dev"})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	req = req.WithContext(appmw.WithPrincipal(req.Context(), princ))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d", rec.Code)
	}
	// Cookie should have MaxAge=-1.
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "nexis_session" && c.MaxAge == -1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("clear-cookie not found")
	}
}
