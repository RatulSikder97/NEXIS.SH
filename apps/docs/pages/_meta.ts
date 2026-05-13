// Phase 8 — Top-level navigation order for the docs site.
//
// Nextra v3 reads this file to seed the sidebar. The key is the MDX file
// name (without the extension); the value is the title shown in the
// sidebar. Order is preserved.
const meta = {
  index: "Introduction",
  install: "Install",
  integrations: "Integrations",
  "agent-reference": "Agent reference",
  runbook: "Runbook",
  architecture: "Architecture",
} as const;

export default meta;
