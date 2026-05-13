// Package handler — workspace HTTP surface.
//
// Six endpoints under /v1/workspaces, all behind RequireAuth + RLS. The SSE
// stream verifies workspace ownership BEFORE upgrading to text/event-stream
// so callers without access get a clean 404 rather than a hanging connection.
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
)

// toWorkspaceResp converts a domain.Workspace into the JSON wire shape.
// ReadyAt is the empty string when the workspace is still provisioning so
// clients can detect the transition without a null check.
func toWorkspaceResp(w domain.Workspace) dto.WorkspaceResp {
	resp := dto.WorkspaceResp{
		ID:               w.ID,
		OrgID:            w.OrgID,
		Name:             w.Name,
		Slug:             w.Slug,
		Region:           w.Region,
		Status:           string(w.Status),
		StatusMessage:    w.StatusMessage,
		ProvisioningStep: w.ProvisioningStep,
		CreatedAt:        w.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:        w.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if w.ReadyAt != nil {
		resp.ReadyAt = w.ReadyAt.UTC().Format(time.RFC3339)
	}
	return resp
}

// WorkspaceRegions wires GET /v1/workspaces/regions. Returns the static
// catalog. Kept behind RequireAuth so unauthenticated callers don't get a
// free read of our region inventory.
func WorkspaceRegions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpJSON(w, http.StatusOK, domain.Regions)
	}
}

// WorkspacesList wires GET /v1/workspaces. Always emits an array (never null)
// so the client can iterate without a nil check.
func WorkspacesList(svc domain.WorkspaceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		princ, _ := appmw.PrincipalFrom(r.Context())
		rows, err := svc.List(r.Context(), princ)
		if err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]dto.WorkspaceResp, 0, len(rows))
		for _, x := range rows {
			out = append(out, toWorkspaceResp(x))
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// WorkspaceCreate wires POST /v1/workspaces. Returns 202 because the
// workspace is still mid-provisioning when we respond; clients should follow
// the SSE stream at /v1/workspaces/{id}/events to await readiness.
func WorkspaceCreate(svc domain.WorkspaceService, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req dto.CreateWorkspaceReq
		if !decodeBody(w, r, &req) {
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name required")
			return
		}
		if req.Region == "" {
			writeError(w, http.StatusBadRequest, "region required")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		ws, err := svc.Create(r.Context(), princ, req.Name, req.Region)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		auditWrite(r, aud, princ, "workspace.created", ws.ID, map[string]any{
			"name":   ws.Name,
			"region": ws.Region,
		})
		writeJSON(w, http.StatusAccepted, toWorkspaceResp(ws))
	}
}

// WorkspaceGet wires GET /v1/workspaces/{id}. Returns 404 on not-found or
// cross-org access; the service uses (org_id, id) under RLS so a foreign id
// is indistinguishable from a missing one.
func WorkspaceGet(svc domain.WorkspaceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		princ, _ := appmw.PrincipalFrom(r.Context())
		ws, err := svc.Get(r.Context(), princ, id)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		httpJSON(w, http.StatusOK, toWorkspaceResp(ws))
	}
}

// WorkspaceSuspend wires DELETE /v1/workspaces/{id}. Idempotent; 204 even
// when the workspace is already suspended.
func WorkspaceSuspend(svc domain.WorkspaceService, aud domain.AuditWriter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		princ, _ := appmw.PrincipalFrom(r.Context())
		// Verify ownership before mutating — the underlying repo does a bare
		// UPDATE WHERE id=$1 (no org filter) so we depend on the Get pre-check
		// for tenant scoping.
		if _, err := svc.Get(r.Context(), princ, id); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := svc.Suspend(r.Context(), princ, id); err != nil {
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		auditWrite(r, aud, princ, "workspace.suspended", id, map[string]any{})
		w.WriteHeader(http.StatusNoContent)
	}
}

// WorkspaceEvents wires GET /v1/workspaces/{id}/events. Streams provisioning
// frames as SSE until the terminal "ready" or "error" event, or the client
// disconnects. We verify ownership BEFORE upgrading so unauthorised callers
// get a clean 404 rather than a hanging connection.
//
// Headers: text/event-stream + no-cache + X-Accel-Buffering: no so nginx /
// proxies don't buffer the stream. A 15-second keep-alive comment is emitted
// to keep intermediate proxies from idling the socket shut.
func WorkspaceEvents(svc domain.WorkspaceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		princ, _ := appmw.PrincipalFrom(r.Context())
		ws, err := svc.Get(r.Context(), princ, id)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			httpJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// http.NewResponseController unwraps middleware-wrapped writers (chi's
		// timeout writer in particular doesn't implement Flusher directly).
		// Cancel any upstream write deadline so the 60s Timeout middleware
		// doesn't sever the stream mid-provisioning.
		rc := http.NewResponseController(w)
		_ = rc.SetWriteDeadline(time.Time{})

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		if err := rc.Flush(); err != nil {
			// Underlying transport doesn't support flushing — give up before
			// the client hangs. Headers are already committed so we can't
			// switch to a JSON error envelope, but at least we won't block.
			slog.Default().Error("sse: flush after WriteHeader", "err", err)
			return
		}

		// If the workspace is already terminal, emit a single frame and return.
		// Otherwise tail the broker.
		if ws.Status == domain.WSReady || ws.Status == domain.WSError {
			finalStatus := "ready"
			if ws.Status == domain.WSError {
				finalStatus = "error"
			}
			label := "Workspace ready"
			if ws.Status == domain.WSError {
				label = "Provisioning failed"
			}
			payload, _ := json.Marshal(domain.ProvisioningStep{
				Step:     ws.ProvisioningStep,
				Label:    label,
				Progress: 1.0,
				Status:   finalStatus,
				Message:  ws.StatusMessage,
				TS:       time.Now(),
			})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			_ = rc.Flush()
			return
		}

		events := svc.Events(r.Context(), id)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				payload, err := json.Marshal(ev)
				if err != nil {
					slog.Default().Error("encode provisioning step", "err", err)
					continue
				}
				if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
					return
				}
				_ = rc.Flush()
				if ev.Status == "ready" || ev.Status == "error" {
					return
				}
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				_ = rc.Flush()
			}
		}
	}
}
