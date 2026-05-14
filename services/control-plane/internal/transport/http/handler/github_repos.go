package handler

import (
	"encoding/json"
	"net/http"

	"github.com/nexis-eco/nexis/services/control-plane/internal/adapter/integration"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// GitHubReposList wires GET /v1/integrations/github/repos. Returns the
// repositories the org's connected GitHub App installation has access to.
// Used by the project-wizard repo dropdown.
func GitHubReposList(reg *integration.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if reg == nil || reg.GitHub == nil {
			writeError(w, http.StatusServiceUnavailable, "github provider not configured")
			return
		}
		repos, err := reg.GitHub.ListReposForOrg(r.Context(), princ)
		if err != nil {
			writeError(w, http.StatusBadGateway, "github: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(repos)
	}
}
