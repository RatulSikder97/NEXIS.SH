package agents

import (
	"errors"
	"fmt"

	"github.com/xeipuuv/gojsonschema"

	"github.com/nexis-eco/nexis/services/control-plane/internal/domain"
)

// Validate is the helper each agent's schema.go calls. SchemaJSON is the
// JSON-Schema document as a Go string; payloadJSON is the LLM's response
// body. Wraps ErrAgentSchemaMismatch on failure so the LLMClient retry loop
// recognises it.
func Validate(schemaJSON, payloadJSON string) (map[string]any, error) {
	schemaLoader := gojsonschema.NewStringLoader(schemaJSON)
	docLoader := gojsonschema.NewStringLoader(payloadJSON)
	result, err := gojsonschema.Validate(schemaLoader, docLoader)
	if err != nil {
		return nil, errors.Join(domain.ErrAgentSchemaMismatch, err)
	}
	if !result.Valid() {
		var first string
		if errs := result.Errors(); len(errs) > 0 {
			first = errs[0].String()
		}
		return nil, fmt.Errorf("%w: %s", domain.ErrAgentSchemaMismatch, first)
	}
	return DecodeJSONOrError(payloadJSON)
}
