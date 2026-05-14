// Package handler — projects HTTP surface.
//
// Seven endpoints under /v1/workspaces/{ws_id}/projects + /v1/projects/{id}.
// All sit behind RequireAuth + RLS; the mutating routes additionally sit
// behind RequireRole(owner, admin). The audit row for every mutation is
// emitted from the usecase layer so a future scripted/SDK caller picks up
// the same logging without a duplicate write at the HTTP boundary.
package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
	"github.com/nexis-eco/nexis/services/control-plane/internal/platform/config"
	"github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/dto"
	appmw "github.com/nexis-eco/nexis/services/control-plane/internal/transport/http/middleware"
	"github.com/nexis-eco/nexis/services/control-plane/internal/usecase"
)

// ProjectsService is the narrow surface the handlers consume. The concrete
// *usecase.ProjectsService satisfies it; declaring an interface here keeps
// the handler trivially fakeable.
type ProjectsService interface {
	Create(ctx context.Context, princ domain.Principal, in usecase.CreateProjectInput) (domain.Project, error)
	Update(ctx context.Context, princ domain.Principal, projectID string, in usecase.UpdateProjectInput) (domain.Project, error)
	Archive(ctx context.Context, princ domain.Principal, projectID string) error
	Get(ctx context.Context, princ domain.Principal, projectID string) (domain.Project, error)
	List(ctx context.Context, princ domain.Principal, workspaceID string) ([]domain.Project, error)
	UpdatePolicy(ctx context.Context, princ domain.Principal, projectID string, policy domain.RecoveryPolicy) (domain.Project, error)
}

// ProjectsCreate wires POST /v1/workspaces/{ws_id}/projects. Owner|Admin.
//
// cfg routes the unknown-error arm through safeErrorMessage so dev surfaces
// the underlying cause and staging/prod return only "internal server error".
// The 4xx mappings in mapProjectError are user-actionable and stay as-is.
func ProjectsCreate(svc ProjectsService, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsID := chi.URLParam(r, "ws_id")
		if wsID == "" {
			writeError(w, http.StatusBadRequest, "workspace_id required")
			return
		}
		var req dto.CreateProjectReq
		if !decodeBody(w, r, &req) {
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name required")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		in := usecase.CreateProjectInput{
			WorkspaceID: wsID,
			Name:        req.Name,
			Description: req.Description,
			Environment: domain.Environment(req.Environment),
			OwnerUserID: req.OwnerUserID,
			Selectors:   dtoSelectorsToDomain(req.Selectors),
			SLO:         dtoSLOToDomain(req.SLO),
		}
		if req.Policy != nil {
			policy := dtoPolicyToDomain(*req.Policy)
			in.Policy = &policy
		}
		p, err := svc.Create(r.Context(), princ, in)
		if err != nil {
			mapProjectErrorSafe(w, err, cfg, "projects.create")
			return
		}
		writeJSON(w, http.StatusCreated, toProjectResp(p))
	}
}

// ProjectsListByWorkspace wires GET /v1/workspaces/{ws_id}/projects. Any
// authenticated principal in the workspace's org may read.
func ProjectsListByWorkspace(svc ProjectsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		wsID := chi.URLParam(r, "ws_id")
		if wsID == "" {
			writeError(w, http.StatusBadRequest, "workspace_id required")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		rows, err := svc.List(r.Context(), princ, wsID)
		if err != nil {
			mapProjectError(w, err)
			return
		}
		out := make([]dto.ProjectResp, 0, len(rows))
		for _, p := range rows {
			out = append(out, toProjectResp(p))
		}
		httpJSON(w, http.StatusOK, out)
	}
}

// ProjectsGet wires GET /v1/projects/{id}. Cross-tenant ids return 404.
func ProjectsGet(svc ProjectsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		p, err := svc.Get(r.Context(), princ, id)
		if err != nil {
			mapProjectError(w, err)
			return
		}
		httpJSON(w, http.StatusOK, toProjectResp(p))
	}
}

// ProjectsPatch wires PATCH /v1/projects/{id}. Owner|Admin.
func ProjectsPatch(svc ProjectsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		var req dto.UpdateProjectReq
		if !decodeBody(w, r, &req) {
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		in := usecase.UpdateProjectInput{
			Name:        req.Name,
			Description: req.Description,
			OwnerUserID: req.OwnerUserID,
		}
		if req.Environment != nil {
			env := domain.Environment(*req.Environment)
			in.Environment = &env
		}
		if req.Selectors != nil {
			sel := dtoSelectorsToDomain(*req.Selectors)
			in.Selectors = &sel
		}
		if req.SLO != nil {
			slo := dtoSLOToDomain(*req.SLO)
			in.SLO = &slo
		}
		updated, err := svc.Update(r.Context(), princ, id, in)
		if err != nil {
			mapProjectError(w, err)
			return
		}
		httpJSON(w, http.StatusOK, toProjectResp(updated))
	}
}

// ProjectsArchive wires DELETE /v1/projects/{id}. Owner|Admin. Idempotent;
// 204 even when the row is already archived.
func ProjectsArchive(svc ProjectsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		if err := svc.Archive(r.Context(), princ, id); err != nil {
			mapProjectError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ProjectsGetPolicy wires GET /v1/projects/{id}/recovery-policy. Returns just
// the policy subdocument so the dashboard's policy editor doesn't have to
// pull the full project object.
func ProjectsGetPolicy(svc ProjectsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		p, err := svc.Get(r.Context(), princ, id)
		if err != nil {
			mapProjectError(w, err)
			return
		}
		httpJSON(w, http.StatusOK, toRecoveryPolicyResp(p.Policy))
	}
}

// ProjectsPutPolicy wires PUT /v1/projects/{id}/recovery-policy. Owner|Admin.
// Replaces the entire policy object — partial-field policy updates can be
// done by GETting first and resending.
func ProjectsPutPolicy(svc ProjectsService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "id required")
			return
		}
		var req dto.RecoveryPolicy
		if !decodeBody(w, r, &req) {
			return
		}
		princ, _ := appmw.PrincipalFrom(r.Context())
		updated, err := svc.UpdatePolicy(r.Context(), princ, id, dtoPolicyToDomain(req))
		if err != nil {
			mapProjectError(w, err)
			return
		}
		httpJSON(w, http.StatusOK, toRecoveryPolicyResp(updated.Policy))
	}
}

// mapProjectError converts a domain/usecase error to an HTTP status + body.
// Sentinel errors (ErrCapExceeded, ErrIntegrationRequired, ...) map to
// specific 4xx codes; everything else lands on 500 with the legacy generic
// "internal error" message. Callers that thread a config.Config should use
// mapProjectErrorSafe instead so dev surfaces the underlying cause.
func mapProjectError(w http.ResponseWriter, err error) {
	mapProjectErrorImpl(w, err, nil, "projects")
}

// mapProjectErrorSafe is the env-aware variant. The unknown-error arm routes
// through safeErrorMessage so dev exposes the cause; staging/prod return only
// "internal server error".
func mapProjectErrorSafe(w http.ResponseWriter, err error, cfg config.Config, op string) {
	mapProjectErrorImpl(w, err, &cfg, op)
}

// mapProjectErrorImpl is the shared body. cfg may be nil; when nil the
// unknown arm falls back to the legacy "internal error" string.
func mapProjectErrorImpl(w http.ResponseWriter, err error, cfg *config.Config, op string) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, usecase.ErrCapExceeded):
		writeError(w, http.StatusForbidden, "project cap exceeded")
	case errors.Is(err, usecase.ErrIntegrationRequired):
		writeError(w, http.StatusBadRequest, "required integration not connected")
	case errors.Is(err, usecase.ErrInvalidEnvironment):
		writeError(w, http.StatusBadRequest, "invalid environment")
	case errors.Is(err, usecase.ErrInvalidSelectors):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, usecase.ErrSlugAllocationFailed):
		writeError(w, http.StatusConflict, "slug allocation failed")
	default:
		if cfg != nil {
			writeError(w, http.StatusInternalServerError, safeErrorMessage(err, *cfg, op))
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// toProjectResp converts a domain.Project to its wire shape.
func toProjectResp(p domain.Project) dto.ProjectResp {
	out := dto.ProjectResp{
		ID:             p.ID,
		OrgID:          p.OrgID,
		WorkspaceID:    p.WorkspaceID,
		Name:           p.Name,
		Slug:           p.Slug,
		Description:    p.Description,
		Environment:    string(p.Environment),
		OwnerUserID:    p.OwnerUserID,
		Selectors:      domainSelectorsToDTO(p.Selectors),
		RecoveryPolicy: toRecoveryPolicyResp(p.Policy),
		SLO:            domainSLOToDTO(p.SLO),
		CreatedAt:      p.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      p.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if p.ArchivedAt != nil {
		out.ArchivedAt = p.ArchivedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func toRecoveryPolicyResp(p domain.RecoveryPolicy) dto.RecoveryPolicy {
	approvers := p.ApproverUserIDs
	if approvers == nil {
		approvers = []string{}
	}
	return dto.RecoveryPolicy{
		AutoMergeLowSeverity:    p.AutoMergeLowSeverity,
		AutoMergeMediumSeverity: p.AutoMergeMediumSeverity,
		MediumCountdownSeconds:  p.MediumCountdownSeconds,
		KillSwitchEnabled:       p.KillSwitchEnabled,
		ApproverUserIDs:         approvers,
		MaxConcurrentRecoveries: p.MaxConcurrentRecoveries,
		RollbackOnSLOBreach:     p.RollbackOnSLOBreach,
	}
}

func domainSelectorsToDTO(s domain.ProjectSelectors) dto.ProjectSelectors {
	return dto.ProjectSelectors{
		GitHubRepo:                  s.GitHubRepo,
		GitHubInstallationID:        s.GitHubInstallationID,
		GitHubDefaultBranch:         s.GitHubDefaultBranch,
		SentryOrganizationSlug:      s.SentryOrganizationSlug,
		SentryProjectSlug:           s.SentryProjectSlug,
		ArgoCDServerURL:             s.ArgoCDServerURL,
		ArgoCDAppName:               s.ArgoCDAppName,
		ArgoCDProject:               s.ArgoCDProject,
		PagerDutyServiceID:          s.PagerDutyServiceID,
		PagerDutyEscalationPolicyID: s.PagerDutyEscalationPolicyID,
		DatadogServiceTag:           s.DatadogServiceTag,
		DatadogEnvTag:               s.DatadogEnvTag,
		SlackChannelID:              s.SlackChannelID,
	}
}

func dtoSelectorsToDomain(s dto.ProjectSelectors) domain.ProjectSelectors {
	return domain.ProjectSelectors{
		GitHubRepo:                  s.GitHubRepo,
		GitHubInstallationID:        s.GitHubInstallationID,
		GitHubDefaultBranch:         s.GitHubDefaultBranch,
		SentryOrganizationSlug:      s.SentryOrganizationSlug,
		SentryProjectSlug:           s.SentryProjectSlug,
		ArgoCDServerURL:             s.ArgoCDServerURL,
		ArgoCDAppName:               s.ArgoCDAppName,
		ArgoCDProject:               s.ArgoCDProject,
		PagerDutyServiceID:          s.PagerDutyServiceID,
		PagerDutyEscalationPolicyID: s.PagerDutyEscalationPolicyID,
		DatadogServiceTag:           s.DatadogServiceTag,
		DatadogEnvTag:               s.DatadogEnvTag,
		SlackChannelID:              s.SlackChannelID,
	}
}

func domainSLOToDTO(s domain.SLOTarget) dto.SLOTarget {
	return dto.SLOTarget{
		AvailabilityTarget: s.AvailabilityTarget,
		LatencyP95Ms:       s.LatencyP95Ms,
		ErrorRatePct:       s.ErrorRatePct,
	}
}

func dtoSLOToDomain(s dto.SLOTarget) domain.SLOTarget {
	return domain.SLOTarget{
		AvailabilityTarget: s.AvailabilityTarget,
		LatencyP95Ms:       s.LatencyP95Ms,
		ErrorRatePct:       s.ErrorRatePct,
	}
}

func dtoPolicyToDomain(p dto.RecoveryPolicy) domain.RecoveryPolicy {
	approvers := p.ApproverUserIDs
	if approvers == nil {
		approvers = []string{}
	}
	return domain.RecoveryPolicy{
		AutoMergeLowSeverity:    p.AutoMergeLowSeverity,
		AutoMergeMediumSeverity: p.AutoMergeMediumSeverity,
		MediumCountdownSeconds:  p.MediumCountdownSeconds,
		KillSwitchEnabled:       p.KillSwitchEnabled,
		ApproverUserIDs:         approvers,
		MaxConcurrentRecoveries: p.MaxConcurrentRecoveries,
		RollbackOnSLOBreach:     p.RollbackOnSLOBreach,
	}
}

