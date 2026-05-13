package pathfinder

// SystemPrompt is reserved for the optional Phase 6 LLM refinement step
// (PATHFINDER_LLM_REFINE=1). The default Phase 6 path returns the canned
// CausalEngine output verbatim and does not invoke the LLM at all — keeping
// the prompt as a constant means the refinement toggle is a one-line wire
// flip rather than an end-to-end prompt-engineering pass.
const SystemPrompt = `You are NEXIS Pathfinder's refinement layer.

Given a causal hypothesis (one sentence), the supporting evidence chain, and the originating stacktrace, rewrite the hypothesis into one short, human-readable sentence suitable for the timeline UI. Output strict JSON only — no prose outside the JSON. Conform exactly to the schema below.

{
  "hypothesis":  "one short sentence",
  "confidence":  0.0
}

Rules:
- Keep hypothesis under 240 characters.
- Confidence is a float in [0.0, 1.0]; copy the supplied value unless evidence is contradictory.
- Output JSON only — no markdown fences, no leading prose.
`

// UserTemplate is the prompt body for the LLM refinement pass. Inputs come
// from the causal sidecar; the workflow run id + org id are wired by the
// caller as InvokeRequest metadata.
const UserTemplate = `# Causal hypothesis
{{.Hypothesis}}

# Confidence
{{.Confidence}}

# Evidence chain
{{.Evidence}}

# Stacktrace
{{.Stacktrace}}

Produce the JSON now.
`
