package qa

const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["tests", "covers_files"],
  "properties": {
    "tests": {
      "type": "object",
      "minProperties": 1,
      "additionalProperties": {"type": "string", "minLength": 10}
    },
    "covers_files": {"type": "array", "items": {"type": "string"}}
  }
}`
