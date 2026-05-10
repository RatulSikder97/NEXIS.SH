// Package audit emits append-only audit events to the audit.audit_events table.
//
// Every state-changing service-layer call MUST log an audit event in the SAME
// transaction as the mutation. This is enforced by code review + a future linter.
package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"nexis/backend/internal/platform/httpserver"
)

// ActorKind classifies who took the action.
type ActorKind string

const (
	ActorUser     ActorKind = "user"
	ActorSystem   ActorKind = "system"
	ActorWorkflow ActorKind = "workflow"
	ActorAPIKey   ActorKind = "api_key"
	ActorAuth     ActorKind = "auth"
)

// Event is the audit log row.
type Event struct {
	OrgID        string
	ActorUserID  string
	ActorKind    ActorKind
	Action       string
	ResourceKind string
	ResourceID   string
	Payload      interface{}
	IP           string
	UserAgent    string
	OccurredAt   time.Time
}

// EnrichFromRequest populates the IP / UA / actor fields from the http.Request context.
func EnrichFromRequest(e *Event, r *http.Request) {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	if e.OrgID == "" {
		e.OrgID = httpserver.OrgIDFromContext(r.Context())
	}
	if e.ActorUserID == "" {
		e.ActorUserID = httpserver.UserIDFromContext(r.Context())
	}
	if e.ActorKind == "" {
		if e.ActorUserID != "" {
			e.ActorKind = ActorUser
		} else {
			e.ActorKind = ActorSystem
		}
	}
	if e.IP == "" {
		e.IP = r.RemoteAddr
	}
	if e.UserAgent == "" {
		e.UserAgent = r.UserAgent()
	}
}

// Write inserts an audit event using the supplied tx.
// Pass tx=nil to insert outside a tx (e.g. for system events with no co-mutation).
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgx.CommandTag, error)
}

func Write(ctx context.Context, db DB, e Event) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	payloadJSON, _ := json.Marshal(e.Payload)
	_, err := db.Exec(ctx, `
		INSERT INTO audit.audit_events
			(org_id, actor_user_id, actor_kind, action, resource_kind, resource_id, payload, ip, user_agent, occurred_at)
		VALUES
			(NULLIF($1,'')::uuid, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, NULLIF($8,'')::inet, $9, $10)
	`, e.OrgID, e.ActorUserID, string(e.ActorKind), e.Action, e.ResourceKind, e.ResourceID, payloadJSON, e.IP, e.UserAgent, e.OccurredAt)
	return err
}
