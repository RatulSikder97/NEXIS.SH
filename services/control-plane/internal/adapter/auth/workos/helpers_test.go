package workos

// Coverage for the workos Provider's pure helpers. The ConsumeOAuthCode
// path needs the HTTP client + a wrapped Local provider; that lives under
// integration tests. Here we cover the pure helpers + the ErrNotImplemented
// stubs.

import (
	"context"
	"errors"
	"testing"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// TestNew_ReturnsProviderWithDefaults — constructor returns a non-nil
// Provider; default HTTP client is wired when none supplied.
func TestNew_ReturnsProviderWithDefaults(t *testing.T) {
	p := New(Config{APIKey: "test", ClientID: "client"})
	if p == nil {
		t.Fatalf("nil provider")
	}
	if p.Name() != "workos" {
		t.Fatalf("name: %q", p.Name())
	}
}

// TestDisplayNameFromUser walks every input variant.
func TestDisplayNameFromUser(t *testing.T) {
	cases := []struct {
		name      string
		user      workosUser
		want      string
	}{
		{
			name: "full_name",
			user: workosUser{FirstName: "Alice", LastName: "Anderson"},
			want: "Alice Anderson's org",
		},
		{
			name: "first_only",
			user: workosUser{FirstName: "Alice"},
			want: "Alice's org",
		},
		{
			name: "last_only",
			user: workosUser{LastName: "Anderson"},
			want: "Anderson's org",
		},
		{
			name: "email_fallback",
			user: workosUser{Email: "alice@x.com"},
			want: "alice@x.com's org",
		},
		{
			name: "no_data_default",
			user: workosUser{},
			want: "WorkOS Org",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := displayNameFromUser(tc.user)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// TestTruncate — happy + boundary cases.
func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello"},
		{"", 5, ""},
		{"abc", 0, ""},
	}
	for _, tc := range cases {
		got := truncate(tc.in, tc.n)
		if got != tc.want {
			t.Fatalf("truncate(%q, %d): got %q want %q", tc.in, tc.n, got, tc.want)
		}
	}
}

// TestProvider_UnimplementedReturnsErrNotImplemented — every
// non-OAuth-code method returns the sentinel so callers can fall back to
// the local provider.
func TestProvider_UnimplementedReturnsErrNotImplemented(t *testing.T) {
	p := New(Config{})
	ctx := context.Background()

	if _, err := p.Signup(ctx, domain.SignupInput{}); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("Signup: %v", err)
	}
	if _, err := p.Login(ctx, domain.LoginInput{}); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("Login: %v", err)
	}
	if err := p.IssueMagicLink(ctx, "", ""); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("IssueMagicLink: %v", err)
	}
	if _, err := p.ConsumeMagicLink(ctx, ""); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("ConsumeMagicLink: %v", err)
	}
	if _, _, err := p.EnrollMFA(ctx, ""); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("EnrollMFA: %v", err)
	}
	if err := p.VerifyMFA(ctx, "", ""); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("VerifyMFA: %v", err)
	}
	if err := p.DisableMFA(ctx, ""); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("DisableMFA: %v", err)
	}
	if _, err := p.CreateAPIKey(ctx, domain.Principal{}, "", nil); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if _, err := p.ListAPIKeys(ctx, domain.Principal{}); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if err := p.RevokeAPIKey(ctx, domain.Principal{}, ""); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	if _, err := p.VerifyAPIKey(ctx, ""); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("VerifyAPIKey: %v", err)
	}
	if _, err := p.IssueInvite(ctx, domain.Principal{}, "", domain.RoleMember); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("IssueInvite: %v", err)
	}
	if _, err := p.GetInviteInfo(ctx, ""); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("GetInviteInfo: %v", err)
	}
	if _, err := p.ClaimInvite(ctx, "", ""); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("ClaimInvite: %v", err)
	}
	if _, err := p.ListInvites(ctx, domain.Principal{}); !errors.Is(err, domain.ErrNotImplemented) {
		t.Fatalf("ListInvites: %v", err)
	}
}
