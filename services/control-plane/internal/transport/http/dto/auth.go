// Package dto holds wire-level request/response shapes for HTTP handlers.
// Keeping these in their own package lets clients (the web app, integration
// tests) depend on them without dragging in transport-specific code.
package dto

// SignupReq is the payload accepted by POST /v1/auth/signup.
type SignupReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	OrgName  string `json:"org_name"`
}

// LoginReq is the payload accepted by POST /v1/auth/login.
type LoginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	MFACode  string `json:"mfa_code,omitempty"`
}

// MagicReq is the payload accepted by POST /v1/auth/magic.
type MagicReq struct {
	Email   string `json:"email"`
	Purpose string `json:"purpose"`
}

// AuthResp is the success body returned on Signup / Login / magic-link
// consumption. The session cookie is also set; the body lets API consumers
// extract user/org ids without re-decoding the JWT.
type AuthResp struct {
	UserID    string `json:"user_id"`
	OrgID     string `json:"org_id"`
	ExpiresAt string `json:"expires_at"`
}

// ErrorResp is the canonical error envelope returned for non-2xx responses.
type ErrorResp struct {
	Error string `json:"error"`
}

// MFAEnrollResp is the body returned by POST /v1/auth/mfa/enroll. The QR is
// returned as a data URL so the web client can render it directly. The
// recovery_codes slice is reserved for Phase 3; today we always emit an empty
// slice rather than null so clients can rely on shape.
//
// Secret is the raw base32 TOTP secret. It is already trivially extractable
// from the otpauth:// URL embedded in the QR PNG, so exposing it here adds no
// real attack surface — but it lets deterministic E2E tests generate a TOTP
// code without parsing a QR image. The handler never logs this value; clients
// (including production web UIs) MUST treat it as one-time use and never store
// it server-side or in browser storage.
type MFAEnrollResp struct {
	QRDataURL     string   `json:"qr_data_url"`
	Secret        string   `json:"secret"`
	RecoveryCodes []string `json:"recovery_codes"`
}

// MFAVerifyReq is the payload accepted by POST /v1/auth/mfa/verify.
type MFAVerifyReq struct {
	Code string `json:"code"`
}

// CreateAPIKeyReq is the payload accepted by POST /v1/apikeys.
type CreateAPIKeyReq struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
}

// APIKeyCreatedResp is the body returned by POST /v1/apikeys. The plaintext
// is included here exactly once at creation and cannot be re-derived.
type APIKeyCreatedResp struct {
	ID        string   `json:"id"`
	Prefix    string   `json:"prefix"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	Plaintext string   `json:"plaintext_once"`
}

// APIKeyResp is one entry in the response from GET /v1/apikeys. CreatedAt and
// LastUsedAt are RFC3339 timestamps; LastUsedAt is the empty string when the
// key has never been used.
type APIKeyResp struct {
	ID         string   `json:"id"`
	Prefix     string   `json:"prefix"`
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt string   `json:"last_used_at"`
}

// MeUser is the embedded user portion of MeResp.
type MeUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// MeOrg is the embedded org portion of MeResp.
type MeOrg struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// MeResp is the body returned by GET /v1/me — the resolved actor identity.
type MeResp struct {
	User MeUser `json:"user"`
	Org  MeOrg  `json:"org"`
	Role string `json:"role"`
}
