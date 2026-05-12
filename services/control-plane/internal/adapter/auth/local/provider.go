// Package local implements the AuthProvider port using bcrypt for passwords,
// TOTP for MFA, signed JWTs for session tokens, and a Store-backed persistence
// layer. The production wiring uses a pgx-backed Store; tests use memStore.
package local

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// defaultSessionTTL is the lifetime of a freshly-issued session token. Overridable
// via Config.SessionTTL for tests that want shorter or fixed-clock semantics.
const defaultSessionTTL = 7 * 24 * time.Hour

// magicTokenTTL is how long a magic link remains redeemable after issuance.
const magicTokenTTL = 15 * time.Minute

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
// and returns a signed JWT. All writes are issued through Store; this method
// does NOT wrap them in a transaction — Store implementations may choose to.
func (p *Provider) Signup(ctx context.Context, in domain.SignupInput) (domain.SignupResult, error) {
	if in.Email == "" || in.Password == "" || in.OrgName == "" {
		return domain.SignupResult{}, fmt.Errorf("signup: %w", domain.ErrInvalidCredentials)
	}

	hash, err := hashPassword(in.Password)
	if err != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: hash password: %w", err)
	}

	now := p.clock().UTC()
	userID := uuid.NewString()
	orgID := uuid.NewString()
	sessionID := uuid.NewString()

	user := domain.User{
		ID:           userID,
		Email:        in.Email,
		PasswordHash: hash,
		CreatedAt:    now,
	}
	if err := p.store.CreateUser(ctx, &user); err != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: create user: %w", err)
	}

	org := domain.Organization{
		ID:          orgID,
		Name:        in.OrgName,
		Slug:        slugify(in.OrgName),
		OwnerUserID: userID,
		CreatedAt:   now,
	}
	if err := p.store.CreateOrganization(ctx, &org); err != nil {
		return domain.SignupResult{}, fmt.Errorf("signup: create org: %w", err)
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

	return p.issueSessionForUser(ctx, user.ID)
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
	})
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

// --- internal helpers ------------------------------------------------------

func (p *Provider) issueSessionForUser(ctx context.Context, userID string) (domain.SessionToken, error) {
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

func (noopMailer) SendMagicLink(_ context.Context, _, _ string) error { return nil }
