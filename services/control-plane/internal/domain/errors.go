// Package domain holds pure business types and the port interfaces
// the usecase layer depends on. Adapters in internal/adapter/* implement these.
//
// This package depends on NOTHING in internal/* — keep it pure.
package domain

import "errors"

var (
	ErrNotFound   = errors.New("not found")
	ErrForbidden  = errors.New("forbidden")
	ErrConflict   = errors.New("conflict")
	ErrUnknown    = errors.New("unknown")
	ErrLLMRequest = errors.New("llm request failed")

	// Auth-related errors (Phase 2)
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrMFARequired        = errors.New("mfa required")
	ErrMFAInvalid         = errors.New("mfa invalid")
	ErrSessionRevoked     = errors.New("session revoked")
	ErrSessionExpired     = errors.New("session expired")
	ErrNotImplemented     = errors.New("not implemented")

	// Phase 5 — token budget + agent + retrieval.
	ErrBudgetExceeded      = errors.New("token budget exceeded")
	ErrAgentSchemaMismatch = errors.New("agent output failed schema validation")
	ErrEmbeddingFailed     = errors.New("embedding provider failed")

	// Phase 6 — approval gate, graphstore, causal, gitops.
	ErrApprovalRejected      = errors.New("approval rejected")
	ErrApprovalTimeout       = errors.New("approval timeout")
	ErrPathfinderUnavailable = errors.New("pathfinder unavailable")
	ErrCausalUnavailable     = errors.New("causal engine unavailable")
	ErrGraphUnavailable      = errors.New("graph store unavailable")
	ErrGitOpsUnavailable     = errors.New("gitops service unavailable")
)
