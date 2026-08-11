// Package workos is the WorkOS-backed AuthProvider. Phase 7 keeps most of the
// surface unimplemented — WorkOS owns sign-up / sign-in / MFA via the hosted
// AuthKit UI, so the only method the dashboard actually hits is
// ConsumeOAuthCode (the server-side half of the redirect-back). Everything
// else returns ErrNotImplemented; callers who need password / MFA / API key
// management must fall back to the local provider until those flows are
// migrated.
package workos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/auth/local"
	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Config carries the WorkOS-specific configuration plus the inner Local
// provider used for user/session persistence. The Local provider's Store is
// the source of truth — WorkOS adds a thin token-exchange shim on top.
type Config struct {
	APIKey     string
	ClientID   string
	BaseURL    string // defaults to https://api.workos.com when empty
	HTTPClient *http.Client
	// Local is the wrapped local provider used for session minting + user
	// upsert. WorkOS is intentionally a façade over it so the JWT signing
	// path stays single-sourced; if Local is nil ConsumeOAuthCode errs.
	Local *local.Provider
}

// Provider is the WorkOS AuthProvider implementation.
type Provider struct {
	cfg    Config
	client *http.Client
}

// defaultWorkOSBaseURL is the production WorkOS API endpoint. Override via
// Config.BaseURL in tests.
const defaultWorkOSBaseURL = "https://api.workos.com"

// authCodeExchangePath is the WorkOS token-exchange path documented at
// https://workos.com/docs/reference/authkit/authenticate-with-code.
const authCodeExchangePath = "/user_management/authenticate"

// New constructs the Provider. APIKey may be empty in dev — calls that need
// the WorkOS API will fail at request time rather than at boot, mirroring
// the local provider's no-pool fallback.
func New(cfg Config) *Provider {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Provider{cfg: cfg, client: client}
}

// Name returns the provider identifier surfaced in factory + logs.
func (*Provider) Name() string { return "workos" }

// --- Implemented: OAuth code exchange --------------------------------------

// ConsumeOAuthCode exchanges the supplied authorization code for a WorkOS
// user, then delegates to the wrapped Local provider to upsert the user +
// mint a session. The Local provider's Store is the durable source of truth;
// WorkOS just contributes the email + organization name.
func (p *Provider) ConsumeOAuthCode(ctx context.Context, code string) (domain.SignupResult, error) {
	if p.cfg.Local == nil {
		return domain.SignupResult{}, fmt.Errorf("workos: local provider not wired (cannot persist OAuth user)")
	}
	if code == "" {
		return domain.SignupResult{}, fmt.Errorf("workos: code required: %w", domain.ErrInvalidCredentials)
	}
	if p.cfg.APIKey == "" || p.cfg.ClientID == "" {
		return domain.SignupResult{}, fmt.Errorf("workos: WORKOS_API_KEY and WORKOS_CLIENT_ID required")
	}

	user, err := p.exchangeCode(ctx, code)
	if err != nil {
		return domain.SignupResult{}, err
	}
	displayName := strings.TrimSpace(user.OrganizationName)
	if displayName == "" {
		displayName = displayNameFromUser(user)
	}
	return p.cfg.Local.UpsertOAuthUser(ctx, user.Email, displayName)
}

// workosUser is the subset of WorkOS's /user_management/authenticate response
// that we depend on. WorkOS returns more fields (id, profile_picture_url,
// last_sign_in_at, ...) which we intentionally ignore — extending later is
// additive.
type workosUser struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	FirstName        string `json:"first_name"`
	LastName         string `json:"last_name"`
	OrganizationName string `json:"organization_name"`
}

// exchangeCode posts to /user_management/authenticate and returns the
// resulting user record. WorkOS returns 4xx for invalid codes / replay; we
// surface those as domain.ErrInvalidCredentials so the handler can render the
// generic "sign-in failed" message.
func (p *Provider) exchangeCode(ctx context.Context, code string) (workosUser, error) {
	base := p.cfg.BaseURL
	if base == "" {
		base = defaultWorkOSBaseURL
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.APIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+authCodeExchangePath, strings.NewReader(form.Encode()))
	if err != nil {
		return workosUser{}, fmt.Errorf("workos: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return workosUser{}, fmt.Errorf("workos: exchange code: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// WorkOS error payloads include a `code` + `message` — surface the
		// status in the wrapped error so operators reading logs can tell
		// 401 (expired code) from 500 (WorkOS outage).
		return workosUser{}, fmt.Errorf("workos: exchange code: status=%d body=%q: %w", resp.StatusCode, truncate(string(body), 256), domain.ErrInvalidCredentials)
	}
	// WorkOS wraps the user under a "user" key in the response envelope.
	var env struct {
		User workosUser `json:"user"`
		// Some WorkOS responses surface the user fields at the top level
		// for legacy /sso/token endpoints; we tolerate both shapes.
		Email            string `json:"email"`
		ID               string `json:"id"`
		FirstName        string `json:"first_name"`
		LastName         string `json:"last_name"`
		OrganizationName string `json:"organization_name"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return workosUser{}, fmt.Errorf("workos: decode response: %w", err)
	}
	if env.User.Email == "" && env.Email != "" {
		env.User = workosUser{
			ID:               env.ID,
			Email:            env.Email,
			FirstName:        env.FirstName,
			LastName:         env.LastName,
			OrganizationName: env.OrganizationName,
		}
	}
	if env.User.Email == "" {
		return workosUser{}, errors.New("workos: exchange code: empty email in response")
	}
	return env.User, nil
}

// displayNameFromUser builds a sensible default org name from a WorkOS user.
// "{first} {last}'s org" → "{email}'s org" if both names are empty.
func displayNameFromUser(u workosUser) string {
	first := strings.TrimSpace(u.FirstName)
	last := strings.TrimSpace(u.LastName)
	full := strings.TrimSpace(first + " " + last)
	if full != "" {
		return full + "'s org"
	}
	if u.Email != "" {
		return u.Email + "'s org"
	}
	return "WorkOS Org"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// --- Unimplemented: every other AuthProvider method ------------------------
//
// The dashboard's only WorkOS-mode entry point is the callback handler.
// Everything else either runs against the local provider (when an operator
// sets ALLOW_STUB_WORKOS=1) or 501s.

func (*Provider) Signup(_ context.Context, _ domain.SignupInput) (domain.SignupResult, error) {
	return domain.SignupResult{}, domain.ErrNotImplemented
}

func (*Provider) Login(_ context.Context, _ domain.LoginInput) (domain.SessionToken, error) {
	return domain.SessionToken{}, domain.ErrNotImplemented
}

// VerifyToken delegates to the wrapped Local provider so the session cookie
// minted by ConsumeOAuthCode validates on subsequent requests. Without this
// delegation the very first GET /v1/me after the callback would 401.
func (p *Provider) VerifyToken(ctx context.Context, token string) (domain.Principal, error) {
	if p.cfg.Local == nil {
		return domain.Principal{}, domain.ErrNotImplemented
	}
	return p.cfg.Local.VerifyToken(ctx, token)
}

// Logout delegates to Local for the same reason VerifyToken does — the
// session row was created by Local.
func (p *Provider) Logout(ctx context.Context, sessionID string) error {
	if p.cfg.Local == nil {
		return domain.ErrNotImplemented
	}
	return p.cfg.Local.Logout(ctx, sessionID)
}

func (*Provider) IssueMagicLink(_ context.Context, _, _ string) error {
	return domain.ErrNotImplemented
}

// RequestPasswordReset / ResetPassword are not supported in WorkOS mode —
// WorkOS owns the credential lifecycle via its hosted UI, so a local reset
// would desync the two stores. Callers get ErrNotImplemented, same as the
// other credential-management stubs above.
func (*Provider) RequestPasswordReset(_ context.Context, _ string) error {
	return domain.ErrNotImplemented
}

func (*Provider) ResetPassword(_ context.Context, _, _ string) error {
	return domain.ErrNotImplemented
}

// ListSessions / RevokeSession delegate to Local for the same reason
// VerifyToken and Logout do — the session rows are created and owned by the
// wrapped local provider even in WorkOS mode.
func (p *Provider) ListSessions(ctx context.Context, userID string) ([]domain.Session, error) {
	if p.cfg.Local == nil {
		return nil, domain.ErrNotImplemented
	}
	return p.cfg.Local.ListSessions(ctx, userID)
}

func (p *Provider) RevokeSession(ctx context.Context, userID, sessionID string) error {
	if p.cfg.Local == nil {
		return domain.ErrNotImplemented
	}
	return p.cfg.Local.RevokeSession(ctx, userID, sessionID)
}

func (*Provider) ConsumeMagicLink(_ context.Context, _ string) (domain.SessionToken, error) {
	return domain.SessionToken{}, domain.ErrNotImplemented
}

func (*Provider) EnrollMFA(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", domain.ErrNotImplemented
}

func (*Provider) VerifyMFA(_ context.Context, _, _ string) error {
	return domain.ErrNotImplemented
}

func (*Provider) DisableMFA(_ context.Context, _ string) error {
	return domain.ErrNotImplemented
}

func (*Provider) CreateAPIKey(_ context.Context, _ domain.Principal, _ string, _ []string) (domain.APIKeyCreated, error) {
	return domain.APIKeyCreated{}, domain.ErrNotImplemented
}

func (*Provider) ListAPIKeys(_ context.Context, _ domain.Principal) ([]domain.APIKey, error) {
	return nil, domain.ErrNotImplemented
}

func (*Provider) RevokeAPIKey(_ context.Context, _ domain.Principal, _ string) error {
	return domain.ErrNotImplemented
}

func (*Provider) VerifyAPIKey(_ context.Context, _ string) (domain.Principal, error) {
	return domain.Principal{}, domain.ErrNotImplemented
}

// GetUser / GetOrg delegate to Local so /v1/me works after an OAuth sign-in.
func (p *Provider) GetUser(ctx context.Context, id string) (domain.User, error) {
	if p.cfg.Local == nil {
		return domain.User{}, domain.ErrNotImplemented
	}
	return p.cfg.Local.GetUser(ctx, id)
}

func (p *Provider) GetOrg(ctx context.Context, id string) (domain.Organization, error) {
	if p.cfg.Local == nil {
		return domain.Organization{}, domain.ErrNotImplemented
	}
	return p.cfg.Local.GetOrg(ctx, id)
}

// --- invites (Phase 3 stubs) ------------------------------------------------

func (*Provider) IssueInvite(_ context.Context, _ domain.Principal, _ string, _ domain.Role) (string, error) {
	return "", domain.ErrNotImplemented
}

func (*Provider) GetInviteInfo(_ context.Context, _ string) (domain.InviteInfo, error) {
	return domain.InviteInfo{}, domain.ErrNotImplemented
}

func (*Provider) ClaimInvite(_ context.Context, _, _ string) (domain.SessionToken, error) {
	return domain.SessionToken{}, domain.ErrNotImplemented
}

func (*Provider) ListInvites(_ context.Context, _ domain.Principal) ([]domain.Invite, error) {
	return nil, domain.ErrNotImplemented
}

func (*Provider) RevokeInvite(_ context.Context, _ domain.Principal, _ string) error {
	return domain.ErrNotImplemented
}
