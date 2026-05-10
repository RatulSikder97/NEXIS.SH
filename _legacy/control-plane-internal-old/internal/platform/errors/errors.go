// Package errors implements RFC 9457 Problem Details responses + error helpers.
package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
)

// Kind classifies an error for HTTP status mapping + audit.
type Kind string

const (
	KindBadRequest      Kind = "bad_request"
	KindUnauthorized    Kind = "unauthorized"
	KindForbidden       Kind = "forbidden"
	KindNotFound        Kind = "not_found"
	KindConflict        Kind = "conflict"
	KindUnprocessable   Kind = "unprocessable"
	KindRateLimited     Kind = "rate_limited"
	KindInternal        Kind = "internal"
	KindUnavailable     Kind = "unavailable"
)

// E is the canonical error type returned across services.
type E struct {
	Kind    Kind
	Message string
	Detail  string
	Cause   error
}

func (e *E) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *E) Unwrap() error { return e.Cause }

// New constructs a new typed error.
func New(kind Kind, message string) *E {
	return &E{Kind: kind, Message: message}
}

// Wrap adds cause context.
func Wrap(kind Kind, message string, cause error) *E {
	return &E{Kind: kind, Message: message, Cause: cause}
}

// status maps Kind → HTTP status.
func status(k Kind) int {
	switch k {
	case KindBadRequest:
		return http.StatusBadRequest
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindUnprocessable:
		return http.StatusUnprocessableEntity
	case KindRateLimited:
		return http.StatusTooManyRequests
	case KindUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// ProblemDetails is the RFC 9457 wire envelope.
type ProblemDetails struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// Write renders an error as RFC 9457 problem JSON.
// 5xx errors are logged with cause; 4xx are not (client error).
func Write(w http.ResponseWriter, r *http.Request, err error, requestID string) {
	var e *E
	if !errors.As(err, &e) {
		e = Wrap(KindInternal, "internal server error", err)
	}
	st := status(e.Kind)
	if st >= 500 {
		slog.Error("request failed",
			"err", e.Error(),
			"path", r.URL.Path,
			"method", r.Method,
			"request_id", requestID,
		)
	}
	pd := ProblemDetails{
		Type:      "about:blank",
		Title:     string(e.Kind),
		Status:    st,
		Detail:    e.Message,
		Instance:  r.URL.Path,
		RequestID: requestID,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(st)
	_ = json.NewEncoder(w).Encode(pd)
}
