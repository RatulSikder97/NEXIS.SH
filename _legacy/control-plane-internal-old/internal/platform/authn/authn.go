// Package authn verifies inbound credentials (Clerk session JWT or NEXIS API key)
// and attaches user/org/role context.
//
// Two paths:
//   - Browser request → Clerk session cookie or Authorization: Bearer <clerk-jwt>
//   - Programmatic    → Authorization: Bearer <api-key starting with nxs_>
package authn

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	platerrors "nexis/backend/internal/platform/errors"
	"nexis/backend/internal/platform/httpserver"
)

// Verifier resolves a JWT or API key into an Identity.
type Verifier struct {
	ClerkJWKSURL    string                 // unused in this initial impl — placeholder for prod JWKS fetch
	ClerkAudience   string
	APIKeyResolver  func(ctx context.Context, prefix, fullKey string) (Identity, error)
	JWTKeyfunc      jwt.Keyfunc            // for Clerk verify; tests inject a fake
}

// Identity is what a verified request carries.
type Identity struct {
	UserID    string
	OrgID     string
	Role      string
	Source    string // "clerk" | "api_key"
	APIKeyID  string // when source = api_key
}

// Middleware enforces authentication on the wrapped handler.
// Use OptionalMiddleware for routes that should resolve identity if present but not require it.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return v.handle(next, true)
}

// Optional resolves identity if present but does not reject anonymous.
func (v *Verifier) Optional(next http.Handler) http.Handler {
	return v.handle(next, false)
}

func (v *Verifier) handle(next http.Handler, required bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ident, err := v.resolve(r)
		if err != nil {
			if required {
				platerrors.Write(w, r, err, httpserver.RequestIDFromContext(r.Context()))
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		ctx = httpserver.WithUser(ctx, ident.UserID)
		ctx = httpserver.WithOrg(ctx, ident.OrgID)
		ctx = httpserver.WithRole(ctx, ident.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (v *Verifier) resolve(r *http.Request) (Identity, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		// fall back to Clerk session cookie
		if c, err := r.Cookie("__session"); err == nil && c.Value != "" {
			return v.verifyClerkJWT(r.Context(), c.Value)
		}
		return Identity{}, platerrors.New(platerrors.KindUnauthorized, "missing credentials")
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return Identity{}, platerrors.New(platerrors.KindUnauthorized, "malformed Authorization header")
	}
	tok := parts[1]
	if strings.HasPrefix(tok, "nxs_") {
		if v.APIKeyResolver == nil {
			return Identity{}, platerrors.New(platerrors.KindUnauthorized, "api key auth not configured")
		}
		prefix := tok
		if len(tok) > 12 {
			prefix = tok[:12]
		}
		return v.APIKeyResolver(r.Context(), prefix, tok)
	}
	return v.verifyClerkJWT(r.Context(), tok)
}

func (v *Verifier) verifyClerkJWT(ctx context.Context, raw string) (Identity, error) {
	if v.JWTKeyfunc == nil {
		// In dev / when Clerk JWKS isn't wired, accept unsigned JSON in raw — rejected in prod.
		return Identity{}, platerrors.New(platerrors.KindUnauthorized, "jwt verification not configured")
	}
	tok, err := jwt.Parse(raw, v.JWTKeyfunc, jwt.WithIssuedAt())
	if err != nil || !tok.Valid {
		return Identity{}, platerrors.Wrap(platerrors.KindUnauthorized, "invalid token", err)
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return Identity{}, platerrors.New(platerrors.KindUnauthorized, "invalid token claims")
	}
	if v.ClerkAudience != "" {
		if aud, _ := claims["aud"].(string); aud != "" && aud != v.ClerkAudience {
			return Identity{}, platerrors.New(platerrors.KindUnauthorized, "token audience mismatch")
		}
	}
	id := Identity{Source: "clerk"}
	if s, ok := claims["sub"].(string); ok {
		id.UserID = s
	}
	if s, ok := claims["org_id"].(string); ok {
		id.OrgID = s
	}
	if s, ok := claims["role"].(string); ok {
		id.Role = s
	}
	if id.UserID == "" {
		return Identity{}, platerrors.New(platerrors.KindUnauthorized, "missing sub claim")
	}
	return id, nil
}

// HashAPIKey returns a bcrypt hash for storage.
func HashAPIKey(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// VerifyAPIKey compares plain key to bcrypt hash.
func VerifyAPIKey(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// errMalformed is exposed for tests.
var errMalformed = errors.New("malformed authorization header")
