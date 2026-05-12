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
)
