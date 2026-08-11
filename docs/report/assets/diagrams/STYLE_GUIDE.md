# NEXIS Diagram Style Guide (v2 — professional, restrained, icon-based)

Every diagram in this set must follow this guide EXACTLY so the full set reads
as one coherent, professionally-produced system, not nine mismatched styles.

## Palette — restrained, almost monochrome
- Page background: `#ffffff` (white). Never colored page backgrounds.
- Ink (borders, text, icon strokes): `#1f2937` (slate-800). This is the ONLY
  color used for 90% of the diagram — borders, arrows, body text, icons.
- Box fill: `#ffffff` with a `#1f2937` 1.4px stroke, OR (for a subtle group
  boundary only, e.g. a swimlane or layer band) a very light neutral fill
  `#f4f5f7` — never more than ONE fill tone per diagram besides white.
- ONE accent color per diagram, used SPARINGLY and only for the single most
  important element (e.g. the human-approval node, or the primary data-flow
  arrow): accent blue `#2563eb`. Do not introduce a second accent color. Do
  NOT color-code every box by category (no rainbow of blue/green/orange/
  purple boxes — that is explicitly the mistake being corrected).
- A single warning/critical accent MAY be used only where the diagram's own
  content is literally about a failure/rollback state: `#b91c1c` (a muted
  brick red), used for at most 1–2 elements in the whole diagram.
- Text color always `#1f2937` on white, or white on a filled dark box if a
  box is ever fully filled (avoid fully-filled boxes in general — prefer
  outline boxes, they read as more professional and print better).

## Typography
- Font family: `Helvetica, Arial, sans-serif` everywhere (matches the report
  body). No decorative fonts.
- Diagram title (top of canvas): 20px bold.
- Section / swimlane label: 13px bold, ALL CAPS, letter-spacing 0.5px,
  color `#6b7280` (muted gray) — used only for group headers, not content.
- Box title (component/technology name): 12.5px bold, full product name,
  NEVER an abbreviation (see naming rules below).
- Box subtitle / description line: 10.5px regular, `#4b5563`.
- Arrow / flow label: 10px regular, `#4b5563`, placed with a small white
  halo/background rectangle behind it so it never visually collides with a
  line it crosses.
- Footnote / caption line at the bottom of the canvas: 11px, `#6b7280`.

## Naming rules — full names only, no abbreviations, ever
Always spell out the full, official product/technology name. Reference list
(use exactly these strings):
- "Next.js" (not "NJS"), "React", "TypeScript" (not "TS"), "Tailwind CSS"
- "Go" — write "Go (Golang)" on first mention per diagram if space allows,
  otherwise "Go"
- "Python"
- "PostgreSQL" (never "Postgres" or "PG")
- "Neo4j"
- "Redis"
- "MinIO" (object storage) — may add "(Amazon S3–compatible)" as a subtitle
- "Temporal" (workflow orchestration)
- "Docker" / "Podman" — when referring to the runtime NEXIS actually used,
  label it "Docker (Podman-compatible runtime)"
- "GitHub" (not "GH"), "GitHub App", "GitHub Actions"
- "Slack"
- "OpenAI", "Ollama" — under a group labeled "Large Language Model Provider"
  (not "LLM")
- "Argo CD" (GitOps continuous delivery) — not "ArgoCD" run together
- "Terraform" (Infrastructure as Code)
- "OpenTelemetry", "Prometheus", "Grafana", "Loki", "Tempo"
- "Stripe" (Billing), "Sentry", "Datadog", "PagerDuty", "WorkOS"
- Generic terms to also expand on first use per diagram: "Application
  Programming Interface (API)", "Role-Based Access Control (RBAC)",
  "Row-Level Security (RLS)", "Single Sign-On (SSO)",
  "Multi-Factor Authentication (MFA)", "Large Language Model (LLM)" is
  itself fine to spell out once then abbreviate within ONE diagram if space
  is tight, but the FIRST occurrence must be the full phrase.

## Icons
- Source: `icon-library.svg` in this same directory defines a `<symbol>` for
  every icon needed (database, graph, cache, bucket/object-storage,
  workflow, container, git, chat, brain/AI, shield, terraform, observability,
  chart, card/billing, cloud, bell/notification, lock, code, terminal,
  check, person, robot, server, service/gear, browser, webapp).
- **Critical technical note**: `rsvg-convert` does NOT resolve `<use
  href="other-file.svg#id">` across files. Every diagram SVG must COPY the
  entire `<defs>...</defs>` block from `icon-library.svg` verbatim into its
  own `<defs>` section, then reference icons locally via
  `<use href="#i-database" .../>`.
- Render icons at 24–30px, stroke-colored `currentColor` (so wrap each
  `<use>` in a `<g color="#1f2937">` or set `color` attribute directly on
  the `<use>` element) — never filled solid, always the thin line-art style
  already defined in the library.
- Every icon MUST sit directly beside or above its full technology name —
  never an icon with no label, never a label with no icon for a named
  technology box.
- Do not attempt to reproduce exact trademarked logos (no literal GitHub
  octocat, Slack hashtag-swirl, Docker whale silhouette, etc.) — use the
  library's clean generic-category glyphs instead paired with the correct
  full product name in text. This keeps every diagram visually consistent
  and avoids uneven-quality hand-drawn logo approximations.

## Layout rules
- Generous padding: minimum 24px between any two boxes, minimum 40px margin
  from canvas edge.
- Prefer orthogonal (right-angle) arrow routing over diagonal lines wherever
  the layout allows it — diagonal lines are acceptable only for actor-to-
  usecase connectors in UML diagrams (that is the standard notation).
  Arrowheads: consistent small filled triangle, 8px, `#1f2937` (or the
  diagram's single accent color if the arrow IS the highlighted flow).
- NEVER let text touch or cross a box border or another line. If a label is
  long, wrap it onto a second `<text>` line rather than letting it overflow
  its box or the canvas.
- Group related boxes inside a large outline rectangle with a small ALL-CAPS
  section label in the top-left corner of that rectangle (not centered,
  not overlapping the boxes inside it).
- After drawing, ALWAYS render with `rsvg-convert -w <W> -h <H> file.svg -o
  file.png` and use the Read tool on the resulting PNG to visually inspect
  it yourself before considering the diagram done. Check specifically for:
  text cut off at the canvas edge, text overlapping a box border or another
  text block, arrows that visually cross through unrelated text, and any
  element rendered outside the visible canvas. Fix and re-render until none
  of these problems are visible. This self-check step is mandatory, not
  optional.

## File conventions
- One `.svg` source + one rendered `.png` per diagram, both in
  `docs/report/assets/diagrams/`.
- Canvas size: choose whatever width/height comfortably fits the content at
  the padding rules above — do not compress content into an undersized
  canvas. A tall or wide canvas is fine; cramped content is not.
- Title as a `<text>` element at the top of the canvas, matching the exact
  "Figure: ..." caption wording that will appear under it in the report
  (confirm the exact caption text against the chapter-writing brief so the
  in-diagram title and the report caption say the same thing).
