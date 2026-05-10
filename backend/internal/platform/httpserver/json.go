package httpserver

import (
	"encoding/json"
	"net/http"

	"nexis/backend/internal/platform/errors"
)

// WriteJSON marshals v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// DecodeJSON decodes the request body. Returns a typed errors.E on failure.
func DecodeJSON(r *http.Request, v interface{}) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.Wrap(errors.KindBadRequest, "invalid json body", err)
	}
	return nil
}
