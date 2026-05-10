// Package authz implements RBAC + ABAC access control.
//
// Allow(ctx, action, resource) is the only entry point. Routes never check
// roles directly — they call Allow with the action name + resource attributes.
package authz

import (
	"context"
	"fmt"

	platerrors "nexis/backend/internal/platform/errors"
	"nexis/backend/internal/platform/httpserver"
)

// Action enumerates everything that can be authorised.
type Action string

const (
	ActionIncidentRead    Action = "incident:read"
	ActionIncidentApprove Action = "incident:approve"
	ActionIncidentReject  Action = "incident:reject"
	ActionPolicyManage    Action = "policy:manage"
	ActionConnectorManage Action = "connector:manage"
	ActionMemberManage    Action = "member:manage"
	ActionAuditRead       Action = "audit:read"
	ActionAPIKeyManage    Action = "apikey:manage"
	ActionAgentRead       Action = "agent:read"
	ActionDemoRun         Action = "demo:run"
	ActionOrgManage       Action = "org:manage"
)

// Resource carries attributes for ABAC scoping.
type Resource struct {
	OrgID     string
	ServiceID string // for service-scoped reviewers
	OwnerUser string // for self-scoped actions
}

// Allow returns nil if permitted, else a Forbidden error.
//
// Default policy is closed: unknown role/action → deny.
func Allow(ctx context.Context, action Action, res Resource) error {
	role := httpserver.RoleFromContext(ctx)
	ctxOrg := httpserver.OrgIDFromContext(ctx)
	if res.OrgID == "" {
		res.OrgID = ctxOrg
	}
	if ctxOrg != "" && res.OrgID != ctxOrg {
		return platerrors.New(platerrors.KindForbidden,
			fmt.Sprintf("cross-org access denied for %s", action))
	}
	if !permitted(role, action, res, ctx) {
		return platerrors.New(platerrors.KindForbidden,
			fmt.Sprintf("role %q not permitted for %s", role, action))
	}
	return nil
}

func permitted(role string, action Action, res Resource, ctx context.Context) bool {
	switch role {
	case "owner", "admin":
		return true
	case "sre":
		switch action {
		case ActionOrgManage:
			return false
		}
		return true
	case "engineer":
		switch action {
		case ActionIncidentRead, ActionAuditRead, ActionAgentRead, ActionDemoRun:
			return true
		case ActionIncidentApprove, ActionIncidentReject:
			// engineers can approve their own services only — caller must set res.ServiceID
			return res.ServiceID != "" // service-scope check happens at the data layer too
		}
	case "reviewer":
		switch action {
		case ActionIncidentRead, ActionAuditRead, ActionAgentRead:
			return true
		case ActionIncidentApprove, ActionIncidentReject:
			return res.ServiceID != ""
		}
	case "viewer":
		switch action {
		case ActionIncidentRead, ActionAuditRead, ActionAgentRead:
			return true
		}
	case "bot":
		// API-key scope set at key issuance — verifier surface adapts caller scopes
		// directly into ctx; this default keeps bots read-only here.
		switch action {
		case ActionIncidentRead, ActionAgentRead:
			return true
		}
	}
	_ = ctx
	return false
}
