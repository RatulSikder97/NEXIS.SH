package handler

// Pipeline patch diff endpoint — surfaces the unified diff an approver needs
// to look at BEFORE clicking Approve or Reject.
//
// Surface:
//
//	GET /v1/workspaces/{ws_id}/pipelines/{run_id}/patch?key=<patch_key>
//
// Owner|Admin only (mounted under the role-gated sub-group in server.go).
// Returns the decrypted diff body as text/plain so the FE can render it
// inline or pipe it into a diff viewer.
//
// Tenancy model:
//   - The principal's OrgID is the boundary. The handler resolves the run via
//     WorkflowService.Get which already enforces (org_id, run_id) at the
//     repo layer.
//   - The workspace_id URL param is double-checked against the run's
//     workspace_id so a leaked run id from another workspace can't pull a
//     patch under a different workspace's URL.
//   - The patch key is opaque to the handler — but the FE only ever has
//     keys that came from activity_events.payload.patch_key, which the
//     workflow stamps as "patches/{run_id}/<agent>.<step>.patch.enc". We
//     additionally enforce the "patches/<run_id>/" prefix so a malicious
//     caller can't request another run's patch via this endpoint.
//   - The bucket is derived from the run's OrgID (BucketForOrg), never
//     from the URL or query — eliminates the cross-tenant bucket-swap
//     attack vector.
//
// The patchstore.Get call decrypts via KeyVault before returning, so the
// response is plaintext diff (not the on-disk envelope ciphertext).

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// PipelinePatch wires GET /v1/workspaces/{ws_id}/pipelines/{run_id}/patch.
// The patch key is read from ?key=<patch_key>; the handler verifies tenancy
// then fetches + decrypts the object from the patchstore.
//
// Returns:
//
//	200 text/plain  — decrypted patch body
//	400             — missing/invalid key shape
//	401             — no principal
//	404             — run not found, wrong workspace, or patch object missing
//	500             — patchstore fetch / decrypt error
func PipelinePatch(svc domain.WorkflowService, ps domain.PatchStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, ok := appmw.PrincipalFrom(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if svc == nil || ps == nil {
			// Defensive — the route mount guards on both being non-nil
			// so this should not be reachable in production. Returning
			// 503 makes the misconfiguration visible in tests.
			writeError(w, http.StatusServiceUnavailable, "patchstore not wired")
			return
		}
		wsID := chi.URLParam(r, "ws_id")
		runID := chi.URLParam(r, "run_id")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing run_id")
			return
		}
		key := r.URL.Query().Get("key")
		if key == "" {
			writeError(w, http.StatusBadRequest, "missing key")
			return
		}
		// Patch keys are workflow-stamped as
		//   patches/{run_id}/<agent>.<step>.patch.enc
		// (activities.go:694). Enforce the prefix so the endpoint can
		// only resolve keys that belong to THIS run, not arbitrary
		// objects in the org bucket (logs/, reports/, etc.).
		wantPrefix := "patches/" + runID + "/"
		if !strings.HasPrefix(key, wantPrefix) {
			writeError(w, http.StatusBadRequest, "invalid key for run")
			return
		}
		// Reject path-traversal attempts inside the suffix. The bucket
		// is derived from OrgID so "../" can't escape the tenant; this
		// is belt-and-suspenders.
		if strings.Contains(key, "..") {
			writeError(w, http.StatusBadRequest, "invalid key")
			return
		}

		// Resolve the run — confirms (org, workspace) tenancy via the
		// repo's WHERE org_id=$1 filter inside Get.
		run, _, err := svc.Get(r.Context(), princ, runID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "lookup failed")
			return
		}
		if wsID != "" && run.WorkspaceID != wsID {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if run.OrgID != princ.OrgID {
			// Defence-in-depth — Get already filters on OrgID but a
			// future refactor could drop that and we'd silently leak
			// across tenants.
			writeError(w, http.StatusNotFound, "not found")
			return
		}

		bucket := domain.BucketForOrg(run.OrgID)
		body, err := ps.Get(r.Context(), bucket, key)
		if err != nil {
			// Surface NotFound from the patchstore as a 404 so the FE
			// can render an "expired/missing patch" message without
			// distinguishing storage vs. tenancy failures.
			writeError(w, http.StatusNotFound, "patch not found")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
}
