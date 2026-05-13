package synthesiser

// SchemaJSON validates the Synthesiser agent's Structured output. Stored
// on PipelineInput.SynthesiserPlan and consumed by the workflow as the
// L1 invocation list driver. The selected_agents + skipped_agents slots
// hold the canonical agent name strings (snake_case) defined by
// domain.AgentName.
const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["scenario", "confidence", "selected_agents", "skipped_agents", "rationale"],
  "properties": {
    "scenario":              {"type": "string", "enum": ["null_deref", "schema_drift", "oom", "unknown"]},
    "confidence":            {"type": "number", "minimum": 0.0, "maximum": 1.0},
    "selected_agents":       {"type": "array", "minItems": 1, "items": {"type": "string"}},
    "skipped_agents":        {"type": "array", "items": {"type": "string"}},
    "rationale":             {"type": "string"},
    "estimated_duration_ms": {"type": "integer", "minimum": 0},
    "source":                {"type": "string", "enum": ["fast_path", "llm", "fallback"]}
  }
}`

// SchemaLLMJSON validates the LLM fallback's raw output before we lift
// scenario + confidence + rationale into the agent's final Structured map.
// The model never picks the agent list directly — that's a pure lookup
// against routingTable in provider.go.
const SchemaLLMJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["scenario", "confidence", "rationale"],
  "properties": {
    "scenario":   {"type": "string", "enum": ["null_deref", "schema_drift", "oom", "unknown"]},
    "confidence": {"type": "number", "minimum": 0.0, "maximum": 1.0},
    "rationale":  {"type": "string"}
  }
}`
