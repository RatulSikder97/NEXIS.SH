// Package http hosts the gitops service's HTTP surface. The router exposes:
//
//   GET  /healthz                — liveness probe, always 200
//   POST /v1/gitops/open-pr      — protected by bearer token; opens a PR.
//
// Bearer auth uses a single shared secret (GITOPS_AUTH_TOKEN) — the control
// plane sends "Authorization: Bearer <token>" on every call. Production
// rotates the secret via env redeploy; there is no per-org credential.
package http

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nexis-eco/nexis/services/gitops/internal/domain"
	"github.com/nexis-eco/nexis/services/gitops/internal/usecase"
)

// Deps bundles the dependencies the router needs. Constructed in
// cmd/server/main.go.
type Deps struct {
	OpenPR    *usecase.OpenPRUsecase
	AuthToken string
}

// New returns a chi.Router with all routes wired up.
func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": "gitops",
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(bearerAuth(d.AuthToken))
		r.Post("/v1/gitops/open-pr", openPR(d.OpenPR))
	})
	return r
}

// bearerAuth returns a middleware that requires "Authorization: Bearer
// <token>". Uses subtle.ConstantTimeCompare to avoid timing-leak based
// token enumeration. An empty configured token rejects every request (we
// fail closed rather than expose an open endpoint).
func bearerAuth(token string) func(http.Handler) http.Handler {
	expect := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(expect) == 0 {
				http.Error(w, "gitops auth not configured", http.StatusInternalServerError)
				return
			}
			hdr := r.Header.Get("Authorization")
			if !strings.HasPrefix(hdr, "Bearer ") {
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			supplied := []byte(strings.TrimPrefix(hdr, "Bearer "))
			if subtle.ConstantTimeCompare(supplied, expect) != 1 {
				http.Error(w, "bearer mismatch", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// openPR is the POST /v1/gitops/open-pr handler. Reads the JSON body, runs
// the usecase, encodes the response. Status codes:
//
//   - 201 on success
//   - 400 on bad request shape or unsupported diff
//   - 412 when no GitHub installation exists for the org
//   - 500 on any other error
func openPR(uc *usecase.OpenPRUsecase) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req domain.PROpenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		out, err := uc.Run(r.Context(), req)
		if err != nil {
			http.Error(w, err.Error(), classify(err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(out)
	}
}

// classify maps a usecase error to an HTTP status code. The function is
// allocation-free + does not depend on errors.Is for non-sentinel errors —
// substring matching is acceptable here because the error shapes are
// constructed in this repo and won't drift unexpectedly.
func classify(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "installation not connected"):
		return http.StatusPreconditionFailed
	case strings.Contains(msg, "unsupported diff shape"),
		strings.Contains(msg, "required"),
		strings.Contains(msg, "must be owner/name"),
		strings.Contains(msg, "context mismatch"):
		return http.StatusBadRequest
	case errors.Is(err, http.ErrNotSupported):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
