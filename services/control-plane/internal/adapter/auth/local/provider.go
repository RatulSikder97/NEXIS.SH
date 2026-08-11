// Package local implements the AuthProvider port using bcrypt for passwords,
// TOTP for MFA, signed JWTs for session tokens, and a Store-backed persistence
// layer. The production wiring uses a pgx-backed Store; tests use memStore.
package local

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// isUniqueViolation returns true if err is a Postgres UNIQUE constraint
// violation (SQLSTATE 23505). Used to drive slug-suffix retry on signup.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	// memStore returns plain errors with the substring "duplicate" / "unique"
	// — match defensively so tests work without pulling pg-specific symbols.
	s := err.Error()
	return strings.Contains(s, "23505") ||
		strings.Contains(strings.ToLower(s), "duplicate") ||
		strings.Contains(strings.ToLower(s), "unique")
}

// inviteTTL is how long a freshly-issued invite remains claimable. Mirrors the
// 7-day window the spec calls out; constant rather than configurable to keep
// the surface tight.
const inviteTTL = 7 * 24 * time.Hour

// defaultSessionTTL is the lifetime of a freshly-issued session token. Overridable
// via Config.SessionTTL for tests that want shorter or fixed-clock semantics.
const defaultSessionTTL = 7 * 24 * time.Hour

// magicTokenTTL is how long a magic link remains redeemable after issuance.
const magicTokenTTL = 15 * time.Minute

// resetTokenTTL is how long a password-reset link remains redeemable. Longer
// than a magic link (the user has to context-switch to their inbox and pick a
// new password) but still short enough to bound the exposure of a leaked link.
const resetTokenTTL = 30 * time.Minute

// minPasswordLen mirrors the signup form's minLength=8. Enforced on reset so
// account recovery can't downgrade a credential below the signup bar.
const minPasswordLen = 8

// lastSeenTouchInterval throttles the last_seen_at write on VerifyToken so a
// busy dashboard doesn't turn every API call into a session UPDATE.
const lastSeenTouchInterval = 5 * time.Minute

// Config wires the Provider's dependencies. Fields with sensible defaults can
// be left zero.
type Config struct {
	Store         Store
	SessionSecret []byte
	Mailer        Mailer        // optional; defaults to no-op when nil (mostly for tests)
	BaseURL       string        // used to build magic-link URLs
	SessionTTL    time.Duration // optional; defaults to 7d
	Clock         func() time.Time
}

// Provider implements domain.AuthProvider on top of Config.Store.
type Provider struct {
	store         Store
	sessionSecret []byte
	mailer        Mailer
	baseURL       string
	sessionTTL    time.Duration
	clock         func() time.Time
}

// New constructs a Provider from cfg, filling defaults for nil/zero fields.
func New(cfg Config) *Provider {
	ttl := cfg.SessionTTL
	if ttl == 0 {
		ttl = defaultSessionTTL
	}
	clk := cfg.Clock
	if clk == nil {
		clk = func() time.Time { return time.Now().UTC() }
	}
	mailer := cfg.Mailer
	if mailer == nil {
		mailer = noopMailer{}
	}
	return &Provider{
		store:         cfg.Store,
		sessionSecret: cfg.SessionSecret,
		mailer:        mailer,
		baseURL:       cfg.BaseURL,
		sessionTTL:    ttl,
		clock:         clk,
	}
}

// Name returns the provider identifier used by the factory + logs.
func (p *Provider) Name() string { return "local" }

// --- Signup / Login / VerifyToken / Logout ---------------------------------

// Signup hashes the password, creates a user + org + owner membership + session,
// and returns a signed JWT. Order is: validate email is free → create org
// (with slug-suffix retry on collision) → create user → membership → session.
// This ordering avoids orphan rows when org-slug collides without requiring
// a transaction across Store calls.
func (p *Provider) Signup(ctx context.Context, in domain.SignupInput) (domain.SignupResult, error) {
	if in.Email == "" || in.Password == "" || in.OrgName == "" {
		return domain.SignupResult{}, fmt.Errorf("signup: %w", domain.ErrInvalidCredentials)
	}

	// Reject duplicate email up-front with a meaningful error code. The
	// caller (HTTP handler) maps this to 409 Conflict; without this check
	// the user would see the raw pg unique-violation message.
	if existing, err := p.store.GetUserByEmail(ctx, in.Email); err == nil && existing != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: %w", domain.ErrConflict)
	}

	hash, err := hashPassword(in.Password)
	if err != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: hash password: %w", err)
	}

	now := p.clock().UTC()
	userID := uuid.NewString()
	orgID := uuid.NewString()
	sessionID := uuid.NewString()

	// Create org FIRST with slug-suffix retry. If this fails we haven't
	// created any other rows yet.
	baseSlug := slugify(in.OrgName)
	org := domain.Organization{
		ID:          orgID,
		Name:        in.OrgName,
		Slug:        baseSlug,
		OwnerUserID: userID,
		CreatedAt:   now,
	}
	var createErr error
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			org.Slug = fmt.Sprintf("%s-%d", baseSlug, attempt+1)
		}
		createErr = p.store.CreateOrganization(ctx, &org)
		if createErr == nil {
			break
		}
		if !isUniqueViolation(createErr) {
			break
		}
	}
	if createErr != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: create org: %w", createErr)
	}

	user := domain.User{
		ID:           userID,
		Email:        in.Email,
		PasswordHash: hash,
		CreatedAt:    now,
	}
	if err := p.store.CreateUser(ctx, &user); err != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: create user: %w", err)
	}

	if err := p.store.CreateMembership(ctx, orgID, userID, domain.RoleOwner); err != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: create membership: %w", err)
	}

	session := domain.Session{
		ID:        sessionID,
		UserID:    userID,
		OrgID:     orgID,
		CreatedAt: now,
		ExpiresAt: now.Add(p.sessionTTL),
		UserAgent: in.UserAgent,
		IP:        in.IP,
	}
	if err := p.store.CreateSession(ctx, &session); err != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: create session: %w", err)
	}

	tok, err := p.signJWT(sessionID, userID, orgID, domain.RoleOwner, now, session.ExpiresAt)
	if err != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: sign jwt: %w", err)
	}

	return domain.SignupResult{
		User:    user,
		Org:     org,
		Session: domain.SessionToken{Token: tok, ExpiresAt: session.ExpiresAt},
	}, nil
}

// Login authenticates a user with email+password (+TOTP if MFA is enabled).
func (p *Provider) Login(ctx context.Context, in domain.LoginInput) (domain.SessionToken, error) {
	user, err := p.store.GetUserByEmail(ctx, in.Email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.SessionToken{}, domain.ErrInvalidCredentials
		}
		return domain.SessionToken{}, fmt.Errorf("login: lookup user: %w", err)
	}
	if err := verifyPassword(user.PasswordHash, in.Password); err != nil {
		return domain.SessionToken{}, domain.ErrInvalidCredentials
	}

	if user.MFAEnabled {
		if in.MFACode == "" {
			return domain.SessionToken{}, domain.ErrMFARequired
		}
		if !verifyTOTP(user.MFASecret, in.MFACode) {
			return domain.SessionToken{}, domain.ErrMFAInvalid
		}
	}

	return p.issueSessionForUserWithDevice(ctx, user.ID, in.UserAgent, in.IP)
}

// VerifyToken parses + validates a JWT and confirms the backing session is
// neither revoked nor expired.
func (p *Provider) VerifyToken(ctx context.Context, token string) (domain.Principal, error) {
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return p.sessionSecret, nil
	}, jwt.WithTimeFunc(p.clock))
	if err != nil || !parsed.Valid {
		return domain.Principal{}, fmt.Errorf("verify token: %w", domain.ErrInvalidCredentials)
	}

	sid, _ := claims["sid"].(string)
	if sid == "" {
		return domain.Principal{}, domain.ErrInvalidCredentials
	}

	session, err := p.store.GetSession(ctx, sid)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Principal{}, domain.ErrSessionRevoked
		}
		return domain.Principal{}, fmt.Errorf("verify token: load session: %w", err)
	}
	if session.RevokedAt != nil {
		return domain.Principal{}, domain.ErrSessionRevoked
	}
	if p.clock().After(session.ExpiresAt) {
		return domain.Principal{}, domain.ErrSessionExpired
	}

	// Best-effort last-seen touch, throttled so a busy dashboard doesn't
	// turn every request into an UPDATE. Errors are swallowed: the touch is
	// telemetry for the sessions UI, never a reason to fail auth.
	now := p.clock()
	if session.LastSeenAt == nil || now.Sub(*session.LastSeenAt) >= lastSeenTouchInterval {
		_ = p.store.TouchSession(ctx, session.ID, now)
	}

	role, _ := claims["role"].(string)
	if role == "" {
		// Fall back to looking up membership directly if the JWT didn't carry one.
		_, r, err := p.store.GetMembership(ctx, session.UserID)
		if err != nil {
			return domain.Principal{}, fmt.Errorf("verify token: lookup membership: %w", err)
		}
		role = string(r)
	}

	return domain.Principal{
		UserID:    session.UserID,
		OrgID:     session.OrgID,
		Role:      domain.Role(role),
		SessionID: session.ID,
	}, nil
}

// Logout revokes a session by its server-side ID.
func (p *Provider) Logout(ctx context.Context, sessionID string) error {
	return p.store.RevokeSession(ctx, sessionID)
}

// --- Magic-link ------------------------------------------------------------

// IssueMagicLink mints a single-use redirect token, persists its SHA-256 hash,
// and emails the plaintext token to the caller. Unknown emails are silently
// accepted to avoid leaking account existence.
func (p *Provider) IssueMagicLink(ctx context.Context, email, purpose string) error {
	user, err := p.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil // silent; don't leak existence
		}
		return fmt.Errorf("magic link: lookup user: %w", err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("magic link: rand: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	h := sha256.Sum256([]byte(plaintext))
	expires := p.clock().Add(magicTokenTTL)

	if err := p.store.CreateMagicToken(ctx, h[:], user.ID, purpose, expires); err != nil {
		return fmt.Errorf("magic link: persist: %w", err)
	}

	link := p.baseURL + "/auth/verify?token=" + plaintext
	if err := p.mailer.SendMagicLink(ctx, email, link); err != nil {
		return fmt.Errorf("magic link: send: %w", err)
	}
	return nil
}

// ConsumeMagicLink redeems a previously issued token, marking it used, and
// returns a fresh session token bound to the same user.
func (p *Provider) ConsumeMagicLink(ctx context.Context, token string) (domain.SessionToken, error) {
	h := sha256.Sum256([]byte(token))
	userID, _, expires, usedAt, err := p.store.GetMagicToken(ctx, h[:])
	if err != nil {
		return domain.SessionToken{}, fmt.Errorf("consume magic link: %w", err)
	}
	if usedAt != nil {
		return domain.SessionToken{}, fmt.Errorf("consume magic link: token already used")
	}
	if p.clock().After(expires) {
		return domain.SessionToken{}, fmt.Errorf("consume magic link: token expired")
	}
	if err := p.store.MarkMagicTokenUsed(ctx, h[:]); err != nil {
		return domain.SessionToken{}, fmt.Errorf("consume magic link: mark used: %w", err)
	}
	return p.issueSessionForUser(ctx, userID)
}

// --- Password reset (Phase 9) ----------------------------------------------

// RequestPasswordReset mints a single-use reset token, persists its SHA-256
// hash, and emails the plaintext link. Unknown emails are silently accepted
// so the endpoint can't be used to probe for accounts — same contract as
// IssueMagicLink.
func (p *Provider) RequestPasswordReset(ctx context.Context, email string) error {
	user, err := p.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil // silent; don't leak existence
		}
		return fmt.Errorf("password reset: lookup user: %w", err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("password reset: rand: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	h := sha256.Sum256([]byte(plaintext))
	expires := p.clock().Add(resetTokenTTL)

	if err := p.store.CreatePasswordResetToken(ctx, h[:], user.ID, expires); err != nil {
		return fmt.Errorf("password reset: persist: %w", err)
	}

	link := p.baseURL + "/reset-password?token=" + plaintext
	if err := p.mailer.SendPasswordReset(ctx, email, link); err != nil {
		return fmt.Errorf("password reset: send: %w", err)
	}
	return nil
}

// ResetPassword redeems a reset token: validate → consume-once → replace the
// bcrypt hash → revoke every outstanding session for the user. All token
// failures (unknown, expired, already consumed) wrap ErrInvalidCredentials so
// the handler can collapse them into one non-leaky "invalid or expired" reply.
func (p *Provider) ResetPassword(ctx context.Context, token, newPassword string) error {
	if token == "" {
		return fmt.Errorf("reset password: token required: %w", domain.ErrInvalidCredentials)
	}
	if len(newPassword) < minPasswordLen {
		return fmt.Errorf("reset password: password too short: %w", domain.ErrInvalidCredentials)
	}

	h := sha256.Sum256([]byte(token))
	userID, expires, consumedAt, err := p.store.GetPasswordResetToken(ctx, h[:])
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("reset password: unknown token: %w", domain.ErrInvalidCredentials)
		}
		return fmt.Errorf("reset password: lookup token: %w", err)
	}
	if consumedAt != nil {
		return fmt.Errorf("reset password: token already used: %w", domain.ErrInvalidCredentials)
	}
	if p.clock().After(expires) {
		return fmt.Errorf("reset password: token expired: %w", domain.ErrInvalidCredentials)
	}

	// Consume BEFORE updating the credential: if the update fails the token
	// is burned, which fails safe (user requests a fresh link) rather than
	// leaving a replayable token behind.
	if err := p.store.MarkPasswordResetConsumed(ctx, h[:]); err != nil {
		return fmt.Errorf("reset password: consume: %w", domain.ErrInvalidCredentials)
	}

	hash, err := hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("reset password: hash: %w", err)
	}
	if err := p.store.UpdateUserPassword(ctx, userID, hash); err != nil {
		return fmt.Errorf("reset password: update credential: %w", err)
	}

	// A reset usually means the old credential is suspect — kill every open
	// session so a hijacker can't ride an existing cookie past the reset.
	if err := p.store.RevokeSessionsForUser(ctx, userID); err != nil {
		return fmt.Errorf("reset password: revoke sessions: %w", err)
	}
	return nil
}

// --- Sessions (Phase 9) -----------------------------------------------------

// ListSessions returns the user's live sessions, newest first. "Current" is a
// transport concern — the handler compares each id to the principal's.
func (p *Provider) ListSessions(ctx context.Context, userID string) ([]domain.Session, error) {
	return p.store.ListSessionsByUser(ctx, userID, p.clock())
}

// RevokeSession revokes one of the user's own sessions. Foreign or unknown
// session ids both come back as ErrNotFound so the endpoint never confirms
// that a guessed id exists.
func (p *Provider) RevokeSession(ctx context.Context, userID, sessionID string) error {
	if sessionID == "" {
		return domain.ErrNotFound
	}
	session, err := p.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.UserID != userID {
		return domain.ErrNotFound
	}
	if session.RevokedAt != nil {
		return nil // idempotent — already revoked
	}
	return p.store.RevokeSession(ctx, sessionID)
}

// --- OAuth (Phase 7) -------------------------------------------------------

// ConsumeOAuthCode is the local stub for the WorkOS callback handshake. It
// produces a deterministic user "oauth-stub@nexis.local" so the dev path can
// exercise the same /v1/auth/workos/callback handler / cookie code without a
// WorkOS account. On first run it creates the org + user + membership; on
// subsequent runs it reuses the existing rows and only mints a new session.
//
// `code` is accepted but unused — any non-empty value is treated as a valid
// stub exchange. The handler is responsible for state (CSRF) verification
// before calling this method.
func (p *Provider) ConsumeOAuthCode(ctx context.Context, code string) (domain.SignupResult, error) {
	if code == "" {
		return domain.SignupResult{}, fmt.Errorf("oauth: code required: %w", domain.ErrInvalidCredentials)
	}
	const stubEmail = "oauth-stub@nexis.local"
	const stubOrgName = "OAuth Stub Org"
	return p.upsertOAuthUser(ctx, stubEmail, stubOrgName)
}

// upsertOAuthUser finds-or-creates a user by email and (if newly created) an
// org with the supplied display name. Then issues a session for the resulting
// user. Used by both the local-stub ConsumeOAuthCode path and the WorkOS
// provider's real callback flow.
//
// Exported via the OAuthUpserter helper below so the WorkOS adapter can
// delegate to the same persistence flow without duplicating the org-bootstrap
// logic.
func (p *Provider) upsertOAuthUser(ctx context.Context, email, orgDisplayName string) (domain.SignupResult, error) {
	if email == "" {
		return domain.SignupResult{}, fmt.Errorf("oauth: email required: %w", domain.ErrInvalidCredentials)
	}
	now := p.clock().UTC()

	// Existing user? Reuse their org + role + mint a session. We do not touch
	// the password hash — OAuth users may not have one. The session mint path
	// is identical to ConsumeMagicLink.
	if existing, err := p.store.GetUserByEmail(ctx, email); err == nil && existing != nil {
		orgID, role, err := p.store.GetMembership(ctx, existing.ID)
		if err != nil {
			return domain.SignupResult{}, fmt.Errorf("oauth: lookup membership: %w", err)
		}
		org, err := p.store.GetOrganization(ctx, orgID)
		if err != nil {
			return domain.SignupResult{}, fmt.Errorf("oauth: lookup org: %w", err)
		}
		session := domain.Session{
			ID:        uuid.NewString(),
			UserID:    existing.ID,
			OrgID:     orgID,
			CreatedAt: now,
			ExpiresAt: now.Add(p.sessionTTL),
		}
		if err := p.store.CreateSession(ctx, &session); err != nil {
			return domain.SignupResult{}, fmt.Errorf("oauth: create session: %w", err)
		}
		tok, err := p.signJWT(session.ID, existing.ID, orgID, role, now, session.ExpiresAt)
		if err != nil {
			return domain.SignupResult{}, fmt.Errorf("oauth: sign jwt: %w", err)
		}
		return domain.SignupResult{
			User:    *existing,
			Org:     *org,
			Session: domain.SessionToken{Token: tok, ExpiresAt: session.ExpiresAt},
		}, nil
	}

	// New user — mirror the Signup flow: org first (with slug-suffix retry on
	// collision), then user, membership, session. Password hash is left empty
	// because the OAuth provider owns the credential.
	userID := uuid.NewString()
	orgID := uuid.NewString()
	if orgDisplayName == "" {
		orgDisplayName = email + "'s org"
	}
	baseSlug := slugify(orgDisplayName)
	org := domain.Organization{
		ID:          orgID,
		Name:        orgDisplayName,
		Slug:        baseSlug,
		OwnerUserID: userID,
		CreatedAt:   now,
	}
	var createErr error
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			org.Slug = fmt.Sprintf("%s-%d", baseSlug, attempt+1)
		}
		createErr = p.store.CreateOrganization(ctx, &org)
		if createErr == nil {
			break
		}
		if !isUniqueViolation(createErr) {
			break
		}
	}
	if createErr != nil {
		return domain.SignupResult{}, fmt.Errorf("oauth: create org: %w", createErr)
	}
	user := domain.User{
		ID:        userID,
		Email:     email,
		CreatedAt: now,
	}
	if err := p.store.CreateUser(ctx, &user); err != nil {
		return domain.SignupResult{}, fmt.Errorf("oauth: create user: %w", err)
	}
	if err := p.store.CreateMembership(ctx, orgID, userID, domain.RoleOwner); err != nil {
		return domain.SignupResult{}, fmt.Errorf("oauth: create membership: %w", err)
	}
	session := domain.Session{
		ID:        uuid.NewString(),
		UserID:    userID,
		OrgID:     orgID,
		CreatedAt: now,
		ExpiresAt: now.Add(p.sessionTTL),
	}
	if err := p.store.CreateSession(ctx, &session); err != nil {
		return domain.SignupResult{}, fmt.Errorf("oauth: create session: %w", err)
	}
	tok, err := p.signJWT(session.ID, userID, orgID, domain.RoleOwner, now, session.ExpiresAt)
	if err != nil {
		return domain.SignupResult{}, fmt.Errorf("oauth: sign jwt: %w", err)
	}
	return domain.SignupResult{
		User:    user,
		Org:     org,
		Session: domain.SessionToken{Token: tok, ExpiresAt: session.ExpiresAt},
	}, nil
}

// UpsertOAuthUser is the exported entrypoint the WorkOS adapter calls to share
// the find-or-create flow without duplicating session-mint logic. It is the
// same code path the local ConsumeOAuthCode stub uses.
func (p *Provider) UpsertOAuthUser(ctx context.Context, email, orgDisplayName string) (domain.SignupResult, error) {
	return p.upsertOAuthUser(ctx, email, orgDisplayName)
}

// --- MFA -------------------------------------------------------------------

// EnrollMFA generates a TOTP secret and QR code for a user; the secret is
// stored with mfa_enabled=false until VerifyMFA confirms the code.
func (p *Provider) EnrollMFA(ctx context.Context, userID string) ([]byte, string, error) {
	user, err := p.store.GetUser(ctx, userID)
	if err != nil {
		return nil, "", fmt.Errorf("enroll mfa: lookup user: %w", err)
	}
	secret, qr, err := generateMFASecret(user.Email)
	if err != nil {
		return nil, "", fmt.Errorf("enroll mfa: generate: %w", err)
	}
	if err := p.store.UpdateUserMFASecret(ctx, userID, secret); err != nil {
		return nil, "", fmt.Errorf("enroll mfa: persist: %w", err)
	}
	return qr, secret, nil
}

// VerifyMFA confirms a TOTP code matches the user's secret and flips
// mfa_enabled to true.
func (p *Provider) VerifyMFA(ctx context.Context, userID, code string) error {
	user, err := p.store.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("verify mfa: lookup user: %w", err)
	}
	if user.MFASecret == "" {
		return domain.ErrMFAInvalid
	}
	if !verifyTOTP(user.MFASecret, code) {
		return domain.ErrMFAInvalid
	}
	return p.store.UpdateUserMFAEnabled(ctx, userID, true)
}

// DisableMFA clears the MFA secret and disables MFA for the user.
func (p *Provider) DisableMFA(ctx context.Context, userID string) error {
	return p.store.ClearUserMFA(ctx, userID)
}

// --- API keys --------------------------------------------------------------

// CreateAPIKey mints a fresh API key bound to the principal's org. The plaintext
// is returned to the caller exactly once; only the SHA-256 hash is persisted.
func (p *Provider) CreateAPIKey(ctx context.Context, princ domain.Principal, name string, scopes []string) (domain.APIKeyCreated, error) {
	plaintext, prefix, hash, err := generateAPIKey()
	if err != nil {
		return domain.APIKeyCreated{}, fmt.Errorf("create api key: %w", err)
	}
	key := domain.APIKey{
		ID:        uuid.NewString(),
		OrgID:     princ.OrgID,
		UserID:    princ.UserID,
		Prefix:    prefix,
		Name:      name,
		Scopes:    scopes,
		CreatedAt: p.clock().UTC(),
	}
	if err := p.store.CreateAPIKey(ctx, &key, hash); err != nil {
		return domain.APIKeyCreated{}, fmt.Errorf("create api key: persist: %w", err)
	}
	return domain.APIKeyCreated{Key: key, Plaintext: plaintext}, nil
}

// ListAPIKeys returns every API key bound to the principal's org. Plaintext is
// never re-derivable.
func (p *Provider) ListAPIKeys(ctx context.Context, princ domain.Principal) ([]domain.APIKey, error) {
	return p.store.ListAPIKeysByOrg(ctx, princ.OrgID)
}

// RevokeAPIKey marks a key revoked. Subsequent VerifyAPIKey calls will reject it.
func (p *Provider) RevokeAPIKey(ctx context.Context, princ domain.Principal, id string) error {
	return p.store.RevokeAPIKey(ctx, princ.OrgID, id)
}

// GetUser resolves a User by id. Used by the /v1/me handler to enrich the
// principal with email + verification state. Returns ErrNotFound for unknown ids.
func (p *Provider) GetUser(ctx context.Context, id string) (domain.User, error) {
	u, err := p.store.GetUser(ctx, id)
	if err != nil {
		return domain.User{}, err
	}
	return *u, nil
}

// GetUserPreferences returns the persisted preferences blob for the user, or
// an empty map when none have been set. The local Provider thinly wraps its
// Store so the http handler can satisfy its prefsUpdater type-assertion
// without coupling to the storage shape.
func (p *Provider) GetUserPreferences(ctx context.Context, userID string) (map[string]any, error) {
	return p.store.GetUserPreferences(ctx, userID)
}

// UpdateUserPreferences overwrites the user's preferences with prefs. The UI
// always sends the full object so there is no merge.
func (p *Provider) UpdateUserPreferences(ctx context.Context, userID string, prefs map[string]any) error {
	return p.store.UpdateUserPreferences(ctx, userID, prefs)
}

// GetOrg resolves an Organization by id. Used by the /v1/me handler. Returns
// ErrNotFound for unknown ids.
func (p *Provider) GetOrg(ctx context.Context, id string) (domain.Organization, error) {
	o, err := p.store.GetOrganization(ctx, id)
	if err != nil {
		return domain.Organization{}, err
	}
	return *o, nil
}

// VerifyAPIKey resolves a bearer plaintext to a Principal, rejecting revoked keys.
func (p *Provider) VerifyAPIKey(ctx context.Context, key string) (domain.Principal, error) {
	hash := hashAPIKey(key)
	k, err := p.store.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		return domain.Principal{}, fmt.Errorf("verify api key: %w", err)
	}
	if k.RevokedAt != nil {
		return domain.Principal{}, fmt.Errorf("verify api key: revoked")
	}
	_, role, err := p.store.GetMembership(ctx, k.UserID)
	if err != nil {
		return domain.Principal{}, fmt.Errorf("verify api key: lookup membership: %w", err)
	}
	return domain.Principal{
		UserID: k.UserID,
		OrgID:  k.OrgID,
		Role:   role,
	}, nil
}

// --- Invites (Phase 3) -----------------------------------------------------

// IssueInvite mints a 32-byte token, persists its SHA-256 hash + the invite
// metadata, and emails a magic link to the invitee. Returns the raw token so
// the caller can derive a token_prefix for the audit row; the full token never
// leaves the server beyond the email.
//
// Pre-check: if the email already maps to a user that is a member of the
// caller's org, return ErrConflict — quietly creating a duplicate invite would
// confuse operators reading the list. New users (or existing users not yet in
// the org) get an invite that the claim flow upgrades to a membership.
func (p *Provider) IssueInvite(ctx context.Context, princ domain.Principal, email string, role domain.Role) (string, error) {
	if email == "" {
		return "", fmt.Errorf("invite: email required: %w", domain.ErrInvalidCredentials)
	}
	if role != domain.RoleAdmin && role != domain.RoleMember {
		return "", fmt.Errorf("invite: role must be admin|member: %w", domain.ErrInvalidCredentials)
	}

	// Conflict check: existing user already in this org?
	if existing, err := p.store.GetUserByEmail(ctx, email); err == nil && existing != nil {
		if orgID, _, err := p.store.GetMembership(ctx, existing.ID); err == nil && orgID == princ.OrgID {
			return "", fmt.Errorf("invite: user already in org: %w", domain.ErrConflict)
		}
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("invite: rand: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	h := sha256.Sum256([]byte(plaintext))

	inv := domain.Invite{
		TokenHash:     h[:],
		OrgID:         princ.OrgID,
		Email:         email,
		Role:          role,
		InviterUserID: princ.UserID,
		ExpiresAt:     p.clock().Add(inviteTTL),
	}
	if err := p.store.CreateInvite(ctx, &inv); err != nil {
		return "", fmt.Errorf("invite: persist: %w", err)
	}

	link := p.baseURL + "/invites/" + plaintext
	if err := p.mailer.SendMagicLink(ctx, email, link); err != nil {
		return "", fmt.Errorf("invite: send: %w", err)
	}
	return plaintext, nil
}

// GetInviteInfo resolves an invite token to the public-facing summary used by
// the claim landing page. Returns ErrNotFound on bad/expired/claimed tokens —
// the web UI funnels both into the same "this invite isn't usable" message.
func (p *Provider) GetInviteInfo(ctx context.Context, token string) (domain.InviteInfo, error) {
	if token == "" {
		return domain.InviteInfo{}, domain.ErrNotFound
	}
	h := sha256.Sum256([]byte(token))
	inv, err := p.store.GetInvite(ctx, h[:])
	if err != nil {
		return domain.InviteInfo{}, err
	}
	if inv.ClaimedAt != nil {
		return domain.InviteInfo{}, fmt.Errorf("invite: already claimed: %w", domain.ErrConflict)
	}
	if p.clock().After(inv.ExpiresAt) {
		return domain.InviteInfo{}, fmt.Errorf("invite: expired: %w", domain.ErrNotFound)
	}
	org, err := p.store.GetOrganization(ctx, inv.OrgID)
	if err != nil {
		return domain.InviteInfo{}, fmt.Errorf("invite: load org: %w", err)
	}
	inviter, err := p.store.GetUser(ctx, inv.InviterUserID)
	if err != nil {
		return domain.InviteInfo{}, fmt.Errorf("invite: load inviter: %w", err)
	}
	return domain.InviteInfo{
		OrgID:        org.ID,
		OrgName:      org.Name,
		OrgSlug:      org.Slug,
		Role:         inv.Role,
		InviterEmail: inviter.Email,
	}, nil
}

// ClaimInvite redeems an invite token, creating a user (if the email isn't
// already registered) or upgrading the existing user into the org with the
// invite's role. Returns a signed JWT for the new session.
func (p *Provider) ClaimInvite(ctx context.Context, token, password string) (domain.SessionToken, error) {
	if token == "" {
		return domain.SessionToken{}, domain.ErrNotFound
	}
	h := sha256.Sum256([]byte(token))
	inv, err := p.store.GetInvite(ctx, h[:])
	if err != nil {
		return domain.SessionToken{}, fmt.Errorf("claim invite: %w", err)
	}
	if inv.ClaimedAt != nil {
		return domain.SessionToken{}, fmt.Errorf("claim invite: already claimed: %w", domain.ErrConflict)
	}
	if p.clock().After(inv.ExpiresAt) {
		return domain.SessionToken{}, fmt.Errorf("claim invite: expired: %w", domain.ErrNotFound)
	}

	// Find or create the user. If an existing user is found we silently
	// accept any password (the invite proves email control) — same as the
	// magic-link flow. Brand-new users must supply a non-empty password.
	now := p.clock().UTC()
	var userID string
	if existing, err := p.store.GetUserByEmail(ctx, inv.Email); err == nil && existing != nil {
		userID = existing.ID
	} else {
		if password == "" {
			return domain.SessionToken{}, fmt.Errorf("claim invite: password required for new user: %w", domain.ErrInvalidCredentials)
		}
		hash, err := hashPassword(password)
		if err != nil {
			return domain.SessionToken{}, fmt.Errorf("claim invite: hash password: %w", err)
		}
		userID = uuid.NewString()
		u := domain.User{ID: userID, Email: inv.Email, PasswordHash: hash, CreatedAt: now}
		if err := p.store.CreateUser(ctx, &u); err != nil {
			return domain.SessionToken{}, fmt.Errorf("claim invite: create user: %w", err)
		}
	}

	if err := p.store.CreateMembership(ctx, inv.OrgID, userID, inv.Role); err != nil {
		return domain.SessionToken{}, fmt.Errorf("claim invite: membership: %w", err)
	}
	if err := p.store.MarkInviteClaimed(ctx, h[:]); err != nil {
		return domain.SessionToken{}, fmt.Errorf("claim invite: mark used: %w", err)
	}

	session := domain.Session{
		ID:        uuid.NewString(),
		UserID:    userID,
		OrgID:     inv.OrgID,
		CreatedAt: now,
		ExpiresAt: now.Add(p.sessionTTL),
	}
	if err := p.store.CreateSession(ctx, &session); err != nil {
		return domain.SessionToken{}, fmt.Errorf("claim invite: session: %w", err)
	}
	tok, err := p.signJWT(session.ID, userID, inv.OrgID, inv.Role, now, session.ExpiresAt)
	if err != nil {
		return domain.SessionToken{}, fmt.Errorf("claim invite: sign jwt: %w", err)
	}
	return domain.SessionToken{Token: tok, ExpiresAt: session.ExpiresAt}, nil
}

// ListInvites delegates to the store. The handler layer filters/orders.
func (p *Provider) ListInvites(ctx context.Context, princ domain.Principal) ([]domain.Invite, error) {
	return p.store.ListInvites(ctx, princ.OrgID)
}

// RevokeInvite deletes the row identified by the hex-encoded token_hash. The
// admin saw this hex value in the list response; the raw token is one-time and
// not retrievable here.
func (p *Provider) RevokeInvite(ctx context.Context, _ domain.Principal, tokenHashHex string) error {
	raw, err := hex.DecodeString(tokenHashHex)
	if err != nil {
		return fmt.Errorf("revoke invite: bad hex: %w", domain.ErrInvalidCredentials)
	}
	return p.store.DeleteInvite(ctx, raw)
}

// --- internal helpers ------------------------------------------------------

func (p *Provider) issueSessionForUser(ctx context.Context, userID string) (domain.SessionToken, error) {
	return p.issueSessionForUserWithDevice(ctx, userID, "", "")
}

// issueSessionForUserWithDevice is issueSessionForUser plus optional device
// metadata (user agent + client IP) stamped onto the session row for the
// Phase 9 sessions UI. Non-browser paths pass empty strings.
func (p *Provider) issueSessionForUserWithDevice(ctx context.Context, userID, userAgent, ip string) (domain.SessionToken, error) {
	orgID, role, err := p.store.GetMembership(ctx, userID)
	if err != nil {
		return domain.SessionToken{}, fmt.Errorf("issue session: lookup membership: %w", err)
	}
	now := p.clock().UTC()
	session := domain.Session{
		ID:        uuid.NewString(),
		UserID:    userID,
		OrgID:     orgID,
		CreatedAt: now,
		ExpiresAt: now.Add(p.sessionTTL),
		UserAgent: userAgent,
		IP:        ip,
	}
	if err := p.store.CreateSession(ctx, &session); err != nil {
		return domain.SessionToken{}, fmt.Errorf("issue session: persist: %w", err)
	}
	tok, err := p.signJWT(session.ID, userID, orgID, role, now, session.ExpiresAt)
	if err != nil {
		return domain.SessionToken{}, fmt.Errorf("issue session: sign jwt: %w", err)
	}
	return domain.SessionToken{Token: tok, ExpiresAt: session.ExpiresAt}, nil
}

func (p *Provider) signJWT(sid, uid, oid string, role domain.Role, iat, exp time.Time) (string, error) {
	claims := jwt.MapClaims{
		"sid":  sid,
		"uid":  uid,
		"oid":  oid,
		"role": string(role),
		"iat":  iat.Unix(),
		"exp":  exp.Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString(p.sessionSecret)
}

// slugify produces a stable kebab-case slug for org names. Falls back to a
// uuid suffix if the input would yield an empty slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-' || r == '_':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		out = "org-" + uuid.NewString()[:8]
	}
	return out
}

// noopMailer is used when Config.Mailer is nil — useful in test setups that
// don't care about the link emission.
type noopMailer struct{}

func (noopMailer) SendMagicLink(_ context.Context, _, _ string) error     { return nil }
func (noopMailer) SendPasswordReset(_ context.Context, _, _ string) error { return nil }
