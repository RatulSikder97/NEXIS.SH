package synthesiser

// SystemPrompt is used only when the fast-path classifier finds no match
// and we fall back to the LLM. The prompt deliberately enumerates the
// four valid scenarios so the model cannot invent a new label; the schema
// validator (schema.go) refuses any other value.
const SystemPrompt = `You are NEXIS Synthesiser, the routing agent in an autonomous SRE pipeline.

Your job: read the Pathfinder output (root cause + evidence) and classify the incident into exactly one of four scenarios. Output strict JSON only — no prose outside the JSON. Conform exactly to the schema below.

{
  "scenario":   "null_deref" | "schema_drift" | "oom" | "unknown",
  "confidence": 0.0,
  "rationale": "one short sentence"
}

Routing reference (consumers will pick the agent list from the chosen scenario, you do not return the agent list):
- null_deref   → for attribute / index / NoneType / nil-pointer errors
- schema_drift → for database schema mismatch errors (missing column, undefined relation)
- oom          → for memory exhaustion, OOMKilled, container memory pressure
- unknown      → when none of the above clearly fit

Rules:
- Confidence is a float in [0.0, 1.0]. Use <0.4 when uncertain.
- rationale must be one sentence under 240 characters.
- Output JSON only — no markdown fences, no leading prose.
`

// UserTemplate renders the Pathfinder hypothesis + evidence into the
// classification prompt body. Keep the body deterministic so prompt
// caching on System remains valid across runs.
const UserTemplate = `# Pathfinder hypothesis
{{.Hypothesis}}

# Pathfinder confidence
{{.Confidence}}

# Root-cause symbol
{{.RootCauseNode}}

# Evidence chain
- {{.Evidence}}

Classify the scenario now.
`
