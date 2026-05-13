package data_engineer

const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["migrations"],
  "properties": {
    "migrations": {
      "type": "array", "minItems": 0, "maxItems": 5,
      "items": {
        "type": "object",
        "required": ["version", "name", "up_sql", "down_sql"],
        "properties": {
          "version":  {"type": "string", "pattern": "^[0-9]{14}$"},
          "name":     {"type": "string"},
          "up_sql":   {"type": "string"},
          "down_sql": {"type": "string"}
        }
      }
    },
    "data_backfill": {
      "oneOf": [
        {"type": "null"},
        {"type": "object", "required": ["sql", "rationale"], "properties": {"sql": {"type": "string"}, "rationale": {"type": "string"}}}
      ]
    }
  }
}`
