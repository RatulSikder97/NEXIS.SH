package local

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// newTestProvider builds a Provider wired against an in-memory Store + test
// mailer, with a fixed clock so tests are deterministic. The returned memStore
// has a back-reference to the mailer so lastMagicToken() works.
func newTestProvider(t *testing.T) (*Provider, *memStore) {
	t.Helper()
	store := newMemStore()
	mailer := &testMailer{}
	store.mailer = mailer
	clk := func() time.Time { return time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC) }
	p := New(Config{
		Store:         store,
		SessionSecret: []byte("test-secret-not-for-prod-12345678"),
		Mailer:        mailer,
		BaseURL:       "http://localhost:3000",
		Clock:         clk,
	})
	return p, store
}

func TestProvider_Name(t *testing.T) {
	p, _ := newTestProvider(t)
	if p.Name() != "local" {
		t.Fatalf("Name() = %q, want local", p.Name())
	}
}

func TestProvider_Signup_CreatesOrgAndOwnerMembership(t *testing.T) {
	p, store := newTestProvider(t)
	res, err := p.Signup(context.Background(), domain.SignupInput{
		Email: "alice@example.com", Password: "correct horse battery staple",
		OrgName: "Alice Co",
	})
	if err != nil {
		t.Fatalf("Signup: %v", err)
	}
	if res.User.Email != "alice@example.com" {
		t.Errorf("email = %q", res.User.Email)
	}
	if res.Org.Name != "Alice Co" {
		t.Errorf("org name = %q", res.Org.Name)
	}
	if res.Org.Slug != "alice-co" {
		t.Errorf("slug = %q, want alice-co", res.Org.Slug)
	}
	if res.Org.OwnerUserID != res.User.ID {
		t.Errorf("owner_user_id mismatch: %q vs %q", res.Org.OwnerUserID, res.User.ID)
	}
	if res.Session.Token == "" {
		t.Error("empty session token")
	}
	if res.Session.ExpiresAt.IsZero() {
		t.Error("zero session expiry")
	}
	got, err := store.getMembership(res.Org.ID, res.User.ID)
	if err != nil {
		t.Fatalf("getMembership: %v", err)
	}
	if got != domain.RoleOwner {
		t.Errorf("role = %q", got)
	}
}

func TestProvider_Login_RejectsWrongPassword(t *testing.T) {
	p, _ := newTestProvider(t)
	_, err := p.Signup(context.Background(), domain.SignupInput{
		Email: "a@b.com", Password: "right", OrgName: "X",
	})
	if err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	_, err = p.Login(context.Background(), domain.LoginInput{Email: "a@b.com", Password: "wrong"})
	if err == nil || !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
}

func TestProvider_Login_RejectsUnknownEmail(t *testing.T) {
	p, _ := newTestProvider(t)
	_, err := p.Login(context.Background(), domain.LoginInput{Email: "ghost@example.com", Password: "x"})
	if err == nil || !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("want ErrInvalidCredentials, got %v", err)
	}
}

func TestProvider_Login_Success(t *testing.T) {
	p, _ := newTestProvider(t)
	res, err := p.Signup(context.Background(), domain.SignupInput{
		Email: "a@b.com", Password: "secret", OrgName: "X",
	})
	if err != nil {
		t.Fatalf("setup signup: %v", err)
	}
	st, err := p.Login(context.Background(), domain.LoginInput{Email: "a@b.com", Password: "secret"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if st.Token == "" {
		t.Fatal("empty token after login")
	}
	princ, err := p.VerifyToken(context.Background(), st.Token)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if princ.UserID != res.User.ID || princ.OrgID != res.Org.ID {
		t.Errorf("principal mismatch: %+v", princ)
	}
}

func TestProvider_VerifyToken_RoundTrips(t *testing.T) {
	p, _ := newTestProvider(t)
	res, err := p.Signup(context.Background(), domain.SignupInput{
		Email: "a@b.com", Password: "x", OrgName: "X",
	})
	if err != nil {
		t.Fatalf("Signup: %v", err)
	}
	princ, err := p.VerifyToken(context.Background(), res.Session.Token)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if princ.UserID != res.User.ID {
		t.Errorf("userID mismatch: %q vs %q", princ.UserID, res.User.ID)
	}
	if princ.OrgID != res.Org.ID {
		t.Errorf("orgID mismatch: %q vs %q", princ.OrgID, res.Org.ID)
	}
	if princ.Role != domain.RoleOwner {
		t.Errorf("role = %q, want owner", princ.Role)
	}
	if princ.SessionID == "" {
		t.Error("empty SessionID in principal")
	}
}

func TestProvider_VerifyToken_RejectsRevoked(t *testing.T) {
	p, _ := newTestProvider(t)
	res, err := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	if err != nil {
		t.Fatalf("Signup: %v", err)
	}
	princ, err := p.VerifyToken(context.Background(), res.Session.Token)
	if err != nil {
		t.Fatalf("first VerifyToken: %v", err)
	}
	if err := p.Logout(context.Background(), princ.SessionID); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	_, err = p.VerifyToken(context.Background(), res.Session.Token)
	if err == nil || !errors.Is(err, domain.ErrSessionRevoked) {
		t.Fatalf("want ErrSessionRevoked, got %v", err)
	}
}

func TestProvider_VerifyToken_RejectsTamperedSignature(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	tampered := res.Session.Token + "x"
	_, err := p.VerifyToken(context.Background(), tampered)
	if err == nil {
		t.Fatal("want error for tampered token, got nil")
	}
}

// --- Magic-link ---

func TestProvider_MagicLink_RoundTrip(t *testing.T) {
	p, store := newTestProvider(t)
	res, err := p.Signup(context.Background(), domain.SignupInput{
		Email: "a@b.com", Password: "x", OrgName: "X",
	})
	if err != nil {
		t.Fatalf("Signup: %v", err)
	}
	if err := p.IssueMagicLink(context.Background(), "a@b.com", "login"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	token := store.lastMagicToken()
	if token == "" {
		t.Fatal("no token captured")
	}
	st, err := p.ConsumeMagicLink(context.Background(), token)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	princ, err := p.VerifyToken(context.Background(), st.Token)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if princ.UserID != res.User.ID {
		t.Errorf("userID mismatch")
	}
}

func TestProvider_MagicLink_RejectsReuse(t *testing.T) {
	p, store := newTestProvider(t)
	_, err := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	if err != nil {
		t.Fatalf("Signup: %v", err)
	}
	if err := p.IssueMagicLink(context.Background(), "a@b.com", "login"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	token := store.lastMagicToken()
	if _, err := p.ConsumeMagicLink(context.Background(), token); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if _, err := p.ConsumeMagicLink(context.Background(), token); err == nil {
		t.Fatal("want error on reuse, got nil")
	}
}

func TestProvider_MagicLink_UnknownEmailNoOp(t *testing.T) {
	p, store := newTestProvider(t)
	// Don't sign up — just call directly. The mailer should NOT be invoked.
	if err := p.IssueMagicLink(context.Background(), "ghost@example.com", "login"); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if store.mailer.lastLink != "" {
		t.Errorf("mailer fired for unknown email: %q", store.mailer.lastLink)
	}
}

// --- MFA ---

func TestProvider_MFA_EnrollAndVerify(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	qr, secret, err := p.EnrollMFA(context.Background(), res.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(qr) == 0 {
		t.Error("empty QR")
	}
	if secret == "" {
		t.Error("empty secret")
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if err := p.VerifyMFA(context.Background(), res.User.ID, code); err != nil {
		t.Fatalf("verify: %v", err)
	}
	u, err := p.store.GetUser(context.Background(), res.User.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !u.MFAEnabled {
		t.Error("MFA not enabled after verify")
	}
}

func TestProvider_Login_RequiresMFAWhenEnabled(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	_, secret, err := p.EnrollMFA(context.Background(), res.User.ID)
	if err != nil {
		t.Fatalf("EnrollMFA: %v", err)
	}
	code, _ := totp.GenerateCode(secret, time.Now())
	if err := p.VerifyMFA(context.Background(), res.User.ID, code); err != nil {
		t.Fatalf("VerifyMFA: %v", err)
	}

	// No MFA code → ErrMFARequired
	_, err = p.Login(context.Background(), domain.LoginInput{Email: "a@b.com", Password: "x"})
	if !errors.Is(err, domain.ErrMFARequired) {
		t.Fatalf("want ErrMFARequired, got %v", err)
	}

	// With code → success
	code2, _ := totp.GenerateCode(secret, time.Now())
	_, err = p.Login(context.Background(), domain.LoginInput{Email: "a@b.com", Password: "x", MFACode: code2})
	if err != nil {
		t.Fatalf("login with MFA: %v", err)
	}

	// With bogus code → ErrMFAInvalid
	_, err = p.Login(context.Background(), domain.LoginInput{Email: "a@b.com", Password: "x", MFACode: "000000"})
	if !errors.Is(err, domain.ErrMFAInvalid) {
		t.Fatalf("want ErrMFAInvalid, got %v", err)
	}
}

func TestProvider_DisableMFA_ClearsState(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	_, secret, _ := p.EnrollMFA(context.Background(), res.User.ID)
	code, _ := totp.GenerateCode(secret, time.Now())
	if err := p.VerifyMFA(context.Background(), res.User.ID, code); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := p.DisableMFA(context.Background(), res.User.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	u, _ := p.store.GetUser(context.Background(), res.User.ID)
	if u.MFAEnabled || u.MFASecret != "" {
		t.Errorf("MFA not cleared: %+v", u)
	}
}

// --- API keys ---

func TestProvider_APIKey_CreateAndVerify(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	princ := domain.Principal{UserID: res.User.ID, OrgID: res.Org.ID, Role: domain.RoleOwner}
	c, err := p.CreateAPIKey(context.Background(), princ, "ci-token", []string{"read"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.Plaintext, "nx_live_") {
		t.Errorf("prefix: %q", c.Plaintext)
	}
	if c.Key.Prefix != c.Plaintext[:8] {
		t.Errorf("stored prefix mismatch: %q vs %q", c.Key.Prefix, c.Plaintext[:8])
	}
	if c.Key.OrgID != res.Org.ID {
		t.Errorf("orgID mismatch")
	}

	got, err := p.VerifyAPIKey(context.Background(), c.Plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if got.OrgID != res.Org.ID {
		t.Errorf("orgID mismatch on verify")
	}
	if got.Role != domain.RoleOwner {
		t.Errorf("role = %q", got.Role)
	}
}

func TestProvider_APIKey_RevokeRejects(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	princ := domain.Principal{UserID: res.User.ID, OrgID: res.Org.ID, Role: domain.RoleOwner}
	c, _ := p.CreateAPIKey(context.Background(), princ, "x", nil)
	if err := p.RevokeAPIKey(context.Background(), princ, c.Key.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	_, err := p.VerifyAPIKey(context.Background(), c.Plaintext)
	if err == nil {
		t.Fatal("want error on revoked key")
	}
}

func TestProvider_APIKey_List(t *testing.T) {
	p, _ := newTestProvider(t)
	res, _ := p.Signup(context.Background(), domain.SignupInput{Email: "a@b.com", Password: "x", OrgName: "X"})
	princ := domain.Principal{UserID: res.User.ID, OrgID: res.Org.ID, Role: domain.RoleOwner}
	for _, name := range []string{"k1", "k2", "k3"} {
		if _, err := p.CreateAPIKey(context.Background(), princ, name, nil); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	keys, err := p.ListAPIKeys(context.Background(), princ)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 3 {
		t.Errorf("len(keys) = %d, want 3", len(keys))
	}
}
