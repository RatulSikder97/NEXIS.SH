package local

// Phase 9 coverage — password reset + session management on the local
// provider, MemStore-backed.
//
// NOTE: these tests use a real-time clock (time.Now) rather than the fixed
// 2026-05-12 clock in newTestProvider, because VerifyToken round-trips JWTs
// whose exp claim is validated by jwt/v5 against wall-clock time.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// newLiveClockProvider builds a Provider against MemStore with a mutable
// clock that starts at time.Now so JWT exp validation holds.
func newLiveClockProvider(t *testing.T) (*Provider, *MemStore, *TestMailer, *time.Time) {
	t.Helper()
	store := NewMemStore()
	mailer := &TestMailer{}
	store.mailer = mailer
	now := time.Now().UTC()
	clockAt := &now
	p := New(Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        mailer,
		BaseURL:       "http://localhost:3000",
		Clock:         func() time.Time { return *clockAt },
	})
	return p, store, mailer, clockAt
}

// resetTokenFromLink extracts the plaintext token from the emailed link
// ("http://localhost:3000/reset-password?token=<tok>").
func resetTokenFromLink(t *testing.T, link string) string {
	t.Helper()
	const marker = "token="
	i := strings.Index(link, marker)
	if i < 0 {
		t.Fatalf("no token in link %q", link)
	}
	return link[i+len(marker):]
}

func TestPasswordReset_RoundTrip(t *testing.T) {
	p, _, mailer, _ := newLiveClockProvider(t)
	ctx := context.Background()
	if _, err := p.Signup(ctx, domain.SignupInput{Email: "a@b.com", Password: "old-password", OrgName: "X"}); err != nil {
		t.Fatalf("setup signup: %v", err)
	}

	if err := p.RequestPasswordReset(ctx, "a@b.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	if !strings.Contains(mailer.lastLink, "/reset-password?token=") {
		t.Fatalf("link shape: %q", mailer.lastLink)
	}
	tok := resetTokenFromLink(t, mailer.lastLink)

	if err := p.ResetPassword(ctx, tok, "brand-new-password"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	// Old password must no longer work; the new one must.
	if _, err := p.Login(ctx, domain.LoginInput{Email: "a@b.com", Password: "old-password"}); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("old password still valid: %v", err)
	}
	if _, err := p.Login(ctx, domain.LoginInput{Email: "a@b.com", Password: "brand-new-password"}); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
}

func TestPasswordReset_UnknownEmailIsSilent(t *testing.T) {
	p, _, mailer, _ := newLiveClockProvider(t)
	if err := p.RequestPasswordReset(context.Background(), "ghost@example.com"); err != nil {
		t.Fatalf("unknown email must be silent, got %v", err)
	}
	if mailer.lastLink != "" {
		t.Fatalf("mailer fired for unknown email: %q", mailer.lastLink)
	}
}

func TestPasswordReset_TokenIsSingleUse(t *testing.T) {
	p, _, mailer, _ := newLiveClockProvider(t)
	ctx := context.Background()
	if _, err := p.Signup(ctx, domain.SignupInput{Email: "a@b.com", Password: "old-password", OrgName: "X"}); err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	if err := p.RequestPasswordReset(ctx, "a@b.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	tok := resetTokenFromLink(t, mailer.lastLink)
	if err := p.ResetPassword(ctx, tok, "first-new-password"); err != nil {
		t.Fatalf("first ResetPassword: %v", err)
	}
	err := p.ResetPassword(ctx, tok, "second-new-password")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("replay must fail with ErrInvalidCredentials, got %v", err)
	}
}

func TestPasswordReset_ExpiredTokenRejected(t *testing.T) {
	p, _, mailer, clockAt := newLiveClockProvider(t)
	ctx := context.Background()
	if _, err := p.Signup(ctx, domain.SignupInput{Email: "a@b.com", Password: "old-password", OrgName: "X"}); err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	if err := p.RequestPasswordReset(ctx, "a@b.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	tok := resetTokenFromLink(t, mailer.lastLink)

	*clockAt = clockAt.Add(resetTokenTTL + time.Minute)
	err := p.ResetPassword(ctx, tok, "brand-new-password")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expired token must fail with ErrInvalidCredentials, got %v", err)
	}
}

func TestPasswordReset_RejectsShortPassword(t *testing.T) {
	p, _, mailer, _ := newLiveClockProvider(t)
	ctx := context.Background()
	if _, err := p.Signup(ctx, domain.SignupInput{Email: "a@b.com", Password: "old-password", OrgName: "X"}); err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	if err := p.RequestPasswordReset(ctx, "a@b.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	tok := resetTokenFromLink(t, mailer.lastLink)
	err := p.ResetPassword(ctx, tok, "short")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("short password must fail, got %v", err)
	}
	// The token must survive the failed attempt (validation precedes consume).
	if err := p.ResetPassword(ctx, tok, "long-enough-password"); err != nil {
		t.Fatalf("token burned by failed validation: %v", err)
	}
}

func TestPasswordReset_RevokesAllSessions(t *testing.T) {
	p, _, mailer, _ := newLiveClockProvider(t)
	ctx := context.Background()
	res, err := p.Signup(ctx, domain.SignupInput{Email: "a@b.com", Password: "old-password", OrgName: "X"})
	if err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	if _, err := p.Login(ctx, domain.LoginInput{Email: "a@b.com", Password: "old-password"}); err != nil {
		t.Fatalf("setup login: %v", err)
	}
	sessions, err := p.ListSessions(ctx, res.User.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 live sessions before reset, got %d", len(sessions))
	}

	if err := p.RequestPasswordReset(ctx, "a@b.com"); err != nil {
		t.Fatalf("RequestPasswordReset: %v", err)
	}
	tok := resetTokenFromLink(t, mailer.lastLink)
	if err := p.ResetPassword(ctx, tok, "brand-new-password"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}

	sessions, err = p.ListSessions(ctx, res.User.ID)
	if err != nil {
		t.Fatalf("ListSessions after reset: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions must all be revoked after reset, got %d live", len(sessions))
	}
	// The pre-reset JWT must now fail server-side revocation checks.
	if _, err := p.VerifyToken(ctx, res.Session.Token); !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("pre-reset token must be revoked, got %v", err)
	}
}

func TestListSessions_ReturnsLiveOnlyNewestFirst(t *testing.T) {
	p, _, _, clockAt := newLiveClockProvider(t)
	ctx := context.Background()
	res, err := p.Signup(ctx, domain.SignupInput{
		Email: "a@b.com", Password: "pw-longenough", OrgName: "X",
		UserAgent: "TestBrowser/1.0", IP: "203.0.113.7",
	})
	if err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	*clockAt = clockAt.Add(time.Minute)
	if _, err := p.Login(ctx, domain.LoginInput{Email: "a@b.com", Password: "pw-longenough", UserAgent: "OtherBrowser/2.0"}); err != nil {
		t.Fatalf("setup login: %v", err)
	}

	sessions, err := p.ListSessions(ctx, res.User.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("want 2 sessions, got %d", len(sessions))
	}
	if !sessions[0].CreatedAt.After(sessions[1].CreatedAt) {
		t.Fatalf("ordering: want newest first, got %v then %v", sessions[0].CreatedAt, sessions[1].CreatedAt)
	}
	if sessions[0].UserAgent != "OtherBrowser/2.0" {
		t.Fatalf("newest session user agent = %q", sessions[0].UserAgent)
	}
	if sessions[1].UserAgent != "TestBrowser/1.0" || sessions[1].IP != "203.0.113.7" {
		t.Fatalf("signup session metadata = %q / %q", sessions[1].UserAgent, sessions[1].IP)
	}

	// Revoke the older one — the list shrinks to the newest.
	if err := p.RevokeSession(ctx, res.User.ID, sessions[1].ID); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	sessions, err = p.ListSessions(ctx, res.User.ID)
	if err != nil {
		t.Fatalf("ListSessions after revoke: %v", err)
	}
	if len(sessions) != 1 || sessions[0].UserAgent != "OtherBrowser/2.0" {
		t.Fatalf("after revoke: %+v", sessions)
	}
}

func TestRevokeSession_ForeignSessionIsNotFound(t *testing.T) {
	p, _, _, _ := newLiveClockProvider(t)
	ctx := context.Background()
	alice, err := p.Signup(ctx, domain.SignupInput{Email: "alice@b.com", Password: "pw-longenough", OrgName: "A"})
	if err != nil {
		t.Fatalf("setup alice: %v", err)
	}
	bob, err := p.Signup(ctx, domain.SignupInput{Email: "bob@b.com", Password: "pw-longenough", OrgName: "B"})
	if err != nil {
		t.Fatalf("setup bob: %v", err)
	}
	aliceSessions, err := p.ListSessions(ctx, alice.User.ID)
	if err != nil || len(aliceSessions) != 1 {
		t.Fatalf("alice sessions: %v %d", err, len(aliceSessions))
	}

	// Bob tries to revoke Alice's session — 404-shaped, not permitted.
	if err := p.RevokeSession(ctx, bob.User.ID, aliceSessions[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign revoke must be ErrNotFound, got %v", err)
	}
	// Alice's session is untouched.
	still, err := p.ListSessions(ctx, alice.User.ID)
	if err != nil || len(still) != 1 {
		t.Fatalf("alice sessions after foreign revoke: %v %d", err, len(still))
	}
	// Unknown id → ErrNotFound too.
	if err := p.RevokeSession(ctx, bob.User.ID, "no-such-session"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown id must be ErrNotFound, got %v", err)
	}
}

func TestVerifyToken_TouchesLastSeen(t *testing.T) {
	p, _, _, clockAt := newLiveClockProvider(t)
	ctx := context.Background()
	res, err := p.Signup(ctx, domain.SignupInput{Email: "a@b.com", Password: "pw-longenough", OrgName: "X"})
	if err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	if _, err := p.VerifyToken(ctx, res.Session.Token); err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	sessions, err := p.ListSessions(ctx, res.User.ID)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions: %v %d", err, len(sessions))
	}
	if sessions[0].LastSeenAt == nil {
		t.Fatal("LastSeenAt not stamped by VerifyToken")
	}
	first := *sessions[0].LastSeenAt

	// Within the throttle window a second verify must NOT move last_seen.
	*clockAt = clockAt.Add(time.Minute)
	if _, err := p.VerifyToken(ctx, res.Session.Token); err != nil {
		t.Fatalf("second VerifyToken: %v", err)
	}
	sessions, _ = p.ListSessions(ctx, res.User.ID)
	if !sessions[0].LastSeenAt.Equal(first) {
		t.Fatalf("last_seen moved within throttle window: %v → %v", first, *sessions[0].LastSeenAt)
	}

	// Past the throttle interval it advances.
	*clockAt = clockAt.Add(lastSeenTouchInterval)
	if _, err := p.VerifyToken(ctx, res.Session.Token); err != nil {
		t.Fatalf("third VerifyToken: %v", err)
	}
	sessions, _ = p.ListSessions(ctx, res.User.ID)
	if !sessions[0].LastSeenAt.After(first) {
		t.Fatalf("last_seen did not advance past throttle: %v", *sessions[0].LastSeenAt)
	}
}
