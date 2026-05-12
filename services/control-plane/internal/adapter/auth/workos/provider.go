// Package workos is the WorkOS-backed AuthProvider — currently a stub that
// returns domain.ErrNotImplemented for every method. Phase 7 fills it in once
// WorkOS credentials and the AuthKit migration are scoped.
package workos

import (
	"context"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Config carries the WorkOS-specific configuration. Empty for now.
type Config struct{}

// Provider is the WorkOS AuthProvider implementation.
type Provider struct{}

// New constructs the stub Provider. cfg is accepted but unused until Phase 7.
func New(_ Config) *Provider { return &Provider{} }

// Name returns the provider identifier surfaced in factory + logs.
func (*Provider) Name() string { return "workos" }

func (*Provider) Signup(_ context.Context, _ domain.SignupInput) (domain.SignupResult, error) {
	return domain.SignupResult{}, domain.ErrNotImplemented
}

func (*Provider) Login(_ context.Context, _ domain.LoginInput) (domain.SessionToken, error) {
	return domain.SessionToken{}, domain.ErrNotImplemented
}

func (*Provider) VerifyToken(_ context.Context, _ string) (domain.Principal, error) {
	return domain.Principal{}, domain.ErrNotImplemented
}

func (*Provider) Logout(_ context.Context, _ string) error {
	return domain.ErrNotImplemented
}

func (*Provider) IssueMagicLink(_ context.Context, _, _ string) error {
	return domain.ErrNotImplemented
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

func (*Provider) GetUser(_ context.Context, _ string) (domain.User, error) {
	return domain.User{}, domain.ErrNotImplemented
}

func (*Provider) GetOrg(_ context.Context, _ string) (domain.Organization, error) {
	return domain.Organization{}, domain.ErrNotImplemented
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
