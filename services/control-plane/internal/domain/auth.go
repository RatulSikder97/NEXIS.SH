package domain

import (
	"context"
	"time"
)

// Role represents the role a user holds within an organization.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// User is the authenticated identity. PasswordHash is bcrypt; MFASecret is a base32 TOTP secret.
type User struct {
	ID              string
	Email           string
	PasswordHash    string
	MFASecret       string
	MFAEnabled      bool
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
}

// Organization is the tenant. Owner is captured for fast lookup and audit attribution.
type Organization struct {
	ID          string
	Name        string
	Slug        string
	OwnerUserID string
	CreatedAt   time.Time
}

// Session is a server-side handle for a JWT — checked on every VerifyToken.
type Session struct {
	ID        string
	UserID    string
	OrgID     string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// Principal is the resolved actor for a request — populated by auth middleware,
// read by handlers, used for RLS SET LOCAL and audit attribution.
type Principal struct {
	UserID    string
	OrgID     string
	Role      Role
	SessionID string
}

// SignupInput is the user-facing signup payload.
type SignupInput struct {
	Email    string
	Password string
	OrgName  string
}

// SignupResult bundles everything the caller needs after a successful signup.
type SignupResult struct {
	User    User
	Org     Organization
	Session SessionToken
}

// LoginInput is the credential bundle for password login.
type LoginInput struct {
	Email    string
	Password string
	MFACode  string // optional; required if user has MFA enabled
}

// SessionToken wraps a signed JWT and its expiry.
type SessionToken struct {
	Token     string // signed JWT — opaque to caller
	ExpiresAt time.Time
}

// APIKey is a long-lived bearer credential for headless callers.
type APIKey struct {
	ID         string
	OrgID      string
	UserID     string
	Prefix     string
	Name       string
	Scopes     []string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// APIKeyCreated is the one-time return value of CreateAPIKey.
// Plaintext is returned exactly once at creation and never persisted.
type APIKeyCreated struct {
	Key       APIKey
	Plaintext string // shown once at creation
}

// AuthProvider is the single port the auth usecases depend on. Adapters in
// internal/adapter/auth/* (local, workos) implement it.
type AuthProvider interface {
	Name() string

	Signup(ctx context.Context, in SignupInput) (SignupResult, error)
	Login(ctx context.Context, in LoginInput) (SessionToken, error)

	// VerifyToken parses + validates a session token (JWT) and returns the
	// Principal. Includes a server-side revocation check (sessions table).
	VerifyToken(ctx context.Context, token string) (Principal, error)

	// Logout marks the session revoked. The token continues to verify
	// cryptographically until expiry but VerifyToken will reject it.
	Logout(ctx context.Context, sessionID string) error

	IssueMagicLink(ctx context.Context, email, purpose string) error
	ConsumeMagicLink(ctx context.Context, token string) (SessionToken, error)

	EnrollMFA(ctx context.Context, userID string) (qrPNG []byte, secret string, err error)
	VerifyMFA(ctx context.Context, userID, code string) error
	DisableMFA(ctx context.Context, userID string) error

	CreateAPIKey(ctx context.Context, p Principal, name string, scopes []string) (APIKeyCreated, error)
	ListAPIKeys(ctx context.Context, p Principal) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, p Principal, id string) error

	// VerifyAPIKey is called by middleware when the request carries
	// "Authorization: Bearer nx_live_...". Returns a Principal on success.
	VerifyAPIKey(ctx context.Context, key string) (Principal, error)

	// GetUser resolves a User by id. Used by the /v1/me handler.
	GetUser(ctx context.Context, id string) (User, error)
	// GetOrg resolves an Organization by id. Used by the /v1/me handler.
	GetOrg(ctx context.Context, id string) (Organization, error)
}
