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

// Invite is a pending org membership offer. The token sent to the invitee is
// a one-time secret; only its SHA-256 hash is persisted. The row stays in the
// table after claim (ClaimedAt non-nil) so audit + admin UIs can show who has
// been invited and whether they accepted.
type Invite struct {
	TokenHash     []byte
	OrgID         string
	Email         string
	Role          Role
	InviterUserID string
	ExpiresAt     time.Time
	ClaimedAt     *time.Time
}

// InviteInfo is the public-facing summary returned by GetInviteInfo — used
// by the unauthenticated /v1/invites/{token} landing endpoint so the web app
// can render "You've been invited to join {org} as {role}" before the user
// commits to a password.
type InviteInfo struct {
	OrgID        string
	OrgName      string
	OrgSlug      string
	Role         Role
	InviterEmail string
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

	// Invites — Phase 3 member onboarding flow. The token returned by
	// IssueInvite is the raw value embedded in the magic link; only its hash
	// is persisted. GetInviteInfo + ClaimInvite are called from the public
	// /v1/invites/{token}{,/claim} endpoints, so they take a raw token rather
	// than a Principal.
	IssueInvite(ctx context.Context, p Principal, email string, role Role) (token string, err error)
	GetInviteInfo(ctx context.Context, token string) (InviteInfo, error)
	ClaimInvite(ctx context.Context, token, password string) (SessionToken, error)
	ListInvites(ctx context.Context, p Principal) ([]Invite, error)
	// RevokeInvite identifies the row by the hex-encoded token_hash that the
	// admin saw in the list response — the raw token is one-time and not
	// stored, so we can't reuse it here.
	RevokeInvite(ctx context.Context, p Principal, tokenHashHex string) error
}
