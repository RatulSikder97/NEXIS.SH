package pathfinder

// SchemaJSON validates the Pathfinder agent's Structured output. The shape
// mirrors what the workflow stores on PipelineInput.PathfinderResult and
// what the synthesiser routing fast-path reads (scenario classification
// keys off the evidence_chain tokens defined by Phase 6 §7.2).
const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["root_cause_node", "hypothesis", "confidence", "evidence_chain"],
  "properties": {
    "root_cause_node": {"type": "string"},
    "hypothesis":      {"type": "string"},
    "confidence":      {"type": "number", "minimum": 0.0, "maximum": 1.0},
    "evidence_chain":  {"type": "array", "items": {"type": "string"}},
    "estimand_name":   {"type": "string"},
    "graph_available": {"type": "boolean"},
    "causal_available": {"type": "boolean"}
  }
}`

// SchemaRefineJSON validates the optional LLM-refined hypothesis output
// (PATHFINDER_LLM_REFINE=1). Returned text replaces the canned hypothesis;
// the rest of Structured stays untouched.
const SchemaRefineJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["hypothesis", "confidence"],
  "properties": {
    "hypothesis": {"type": "string", "maxLength": 480},
    "confidence": {"type": "number", "minimum": 0.0, "maximum": 1.0}
  }
}`
