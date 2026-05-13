package architect

const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["plan_steps", "affected_files", "risk_level"],
  "properties": {
    "plan_steps": {
      "type": "array", "minItems": 1, "maxItems": 5,
      "items": {
        "type": "object",
        "required": ["id", "description", "files", "rationale"],
        "properties": {
          "id":          {"type": "string"},
          "description": {"type": "string"},
          "files":       {"type": "array", "items": {"type": "string"}},
          "rationale":   {"type": "string"}
        }
      }
    },
    "affected_files": {"type": "array", "items": {"type": "string"}},
    "risk_level":     {"type": "string", "enum": ["low", "medium", "high"]}
  }
}`
