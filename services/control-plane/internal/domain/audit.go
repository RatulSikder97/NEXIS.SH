package domain

import "context"

// AuditWriter is the port that mutation handlers use to append a tamper-evident
// row to audit_log. Implementations live in internal/adapter/audit/* (see
// HMACWriter for the production chain-hashed implementation).
//
// Write must be safe to call inside an RLS-scoped per-request transaction (the
// tx is read from ctx via db.FromCtx) as well as outside one — for endpoints
// like signup/login which run BEFORE a principal exists in ctx, the writer
// falls back to its owning pool.
type AuditWriter interface {
	Write(ctx context.Context, p Principal, action, target string, metadata map[string]any) error
}
