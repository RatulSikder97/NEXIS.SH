package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration/internal/httpx"
)

// newTestKey returns a freshly-generated RSA-2048 key wrapped in PKCS1 PEM —
// the same shape GitHub gives App owners when they download the .pem from the
// settings UI. Generating per-test keeps the suite hermetic.
func newTestKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, pemBytes
}

// newClientForTest builds a Client pointing at the supplied test server. The
// httpx.Client is configured with fast retries + a high rate limit so tests
// don't accidentally hit production-shaped delays.
func newClientForTest(t *testing.T, srv *httptest.Server, appID int64, pemBytes []byte) *Client {
	t.Helper()
	httpc := httpx.New(srv.Client(), httpx.Config{
		RatePerSec:  1000,
		Burst:       1000,
		MaxAttempts: 1,
	})
	c, err := NewClientWithBaseURL(AppCreds{AppID: appID, PrivateKeyPEM: pemBytes}, httpc, srv.URL)
	if err != nil {
		t.Fatalf("NewClientWithBaseURL: %v", err)
	}
	return c
}

func TestMintAppJWT_RS256(t *testing.T) {
	key, pemBytes := newTestKey(t)
	const appID int64 = 424242
	c, err := NewClient(AppCreds{AppID: appID, PrivateKeyPEM: pemBytes}, httpx.New(nil, httpx.Config{}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// Pin the issuer's clock so exp/iat values are deterministic. Use
	// time.Now()-relative anchoring (rather than a fixed date) so the parser
	// below — which validates exp against wall-clock — does not fail when the
	// suite runs on a date past 2026-05-13.
	now := time.Now().UTC().Truncate(time.Second)
	c.nowFunc = func() time.Time { return now }

	tokenStr, err := c.MintAppJWT()
	if err != nil {
		t.Fatalf("MintAppJWT: %v", err)
	}

	// Verify the algorithm is RS256 — anything else would be a signing
	// regression that GitHub would reject at runtime.
	parsed, err := jwt.Parse(tokenStr, func(tok *jwt.Token) (any, error) {
		if tok.Method.Alg() != "RS256" {
			return nil, fmt.Errorf("unexpected alg %q", tok.Method.Alg())
		}
		return &key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("jwt.Parse: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims type: %T", parsed.Claims)
	}
	if iss, _ := claims["iss"].(string); iss != fmt.Sprintf("%d", appID) {
		t.Errorf("iss = %q, want %d", iss, appID)
	}
	// jwt-go decodes numeric claims as float64.
	iat, _ := claims["iat"].(float64)
	exp, _ := claims["exp"].(float64)
	wantIat := now.Add(-60 * time.Second).Unix()
	wantExp := now.Add(10 * time.Minute).Unix()
	if int64(iat) != wantIat {
		t.Errorf("iat = %d, want %d", int64(iat), wantIat)
	}
	if int64(exp) != wantExp {
		t.Errorf("exp = %d, want %d", int64(exp), wantExp)
	}
}

func TestExchangeInstallationToken(t *testing.T) {
	_, pemBytes := newTestKey(t)
	const installID int64 = 12345
	tokenWant := "ghs_super_secret"
	expiresWant := time.Date(2026, 5, 13, 11, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantToken  string
		wantStatus int  // for APIError check
		wantErr    bool // for any error
	}{
		{
			name: "happy path",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				wantPath := fmt.Sprintf("/app/installations/%d/access_tokens", installID)
				if r.URL.Path != wantPath {
					t.Errorf("path = %s, want %s", r.URL.Path, wantPath)
				}
				if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "Bearer ") {
					t.Errorf("Authorization = %q, want Bearer ...", auth)
				}
				if v := r.Header.Get("X-GitHub-Api-Version"); v != "2022-11-28" {
					t.Errorf("api version = %q", v)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"token":      tokenWant,
					"expires_at": expiresWant.Format(time.RFC3339),
				})
			},
			wantToken: tokenWant,
		},
		{
			name: "401 unauthorized maps to APIError",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"bad credentials"}`))
			},
			wantStatus: 401,
			wantErr:    true,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)

			c := newClientForTest(t, srv, 1, pemBytes)
			tok, _, err := c.ExchangeInstallationToken(context.Background(), installID)

			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				var apiErr *APIError
				if !errors.As(err, &apiErr) {
					t.Fatalf("err = %v, want *APIError", err)
				}
				if apiErr.Status != tc.wantStatus {
					t.Errorf("status = %d, want %d", apiErr.Status, tc.wantStatus)
				}
				return
			}
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if tok != tc.wantToken {
				t.Errorf("token = %q, want %q", tok, tc.wantToken)
			}
		})
	}
}

func TestListInstallationRepos(t *testing.T) {
	_, pemBytes := newTestKey(t)
	body := `{
		"total_count": 2,
		"repositories": [
			{"full_name": "octocat/hello"},
			{"full_name": "octocat/world"}
		]
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/installation/repositories" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("per_page") != "30" {
			t.Errorf("per_page = %q, want 30", r.URL.Query().Get("per_page"))
		}
		if auth := r.Header.Get("Authorization"); !strings.HasPrefix(auth, "token ") {
			t.Errorf("Authorization = %q, want token ...", auth)
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	c := newClientForTest(t, srv, 1, pemBytes)
	repos, err := c.ListInstallationRepos(context.Background(), "tok")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got, want := len(repos), 2; got != want {
		t.Fatalf("len = %d, want %d", got, want)
	}
	if repos[0] != "octocat/hello" || repos[1] != "octocat/world" {
		t.Errorf("repos = %v", repos)
	}
}

func TestOpenPullRequest_HappyPath(t *testing.T) {
	_, pemBytes := newTestKey(t)
	const htmlURL = "https://github.com/octocat/hello/pull/42"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octocat/hello/pulls" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		var got pullRequestWire
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		want := pullRequestWire{
			Title: "fix: thing",
			Head:  "feature/fix",
			Base:  "main",
			Body:  "auto",
			Draft: true,
		}
		if got != want {
			t.Errorf("body = %+v, want %+v", got, want)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, fmt.Sprintf(`{"html_url":%q}`, htmlURL))
	}))
	t.Cleanup(srv.Close)

	c := newClientForTest(t, srv, 1, pemBytes)
	url, err := c.OpenPullRequest(context.Background(), "tok", "octocat", "hello", PullRequestReq{
		Title: "fix: thing",
		Head:  "feature/fix",
		Base:  "main",
		Body:  "auto",
		Draft: true,
	})
	if err != nil {
		t.Fatalf("OpenPullRequest: %v", err)
	}
	if url != htmlURL {
		t.Errorf("url = %q, want %q", url, htmlURL)
	}
}

func TestOpenPullRequest_422DuplicateBranch(t *testing.T) {
	_, pemBytes := newTestKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"message":"A pull request already exists for octocat:feature/fix."}`)
	}))
	t.Cleanup(srv.Close)

	c := newClientForTest(t, srv, 1, pemBytes)
	_, err := c.OpenPullRequest(context.Background(), "tok", "octocat", "hello", PullRequestReq{
		Title: "x",
		Head:  "feature/fix",
		Base:  "main",
	})
	if err == nil {
		t.Fatal("want error on 422")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.Status != 422 {
		t.Errorf("status = %d, want 422", apiErr.Status)
	}
	if !strings.Contains(apiErr.Message, "already exists") {
		t.Errorf("message = %q, want it to contain 'already exists'", apiErr.Message)
	}
}
