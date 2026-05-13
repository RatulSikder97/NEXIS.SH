"""
NEXIS — Project progress presentation (9-minute slot).
Focus: overall progress, architecture, DB schema, work done.
Output: docs/overview/NEXIS_presentation.pptx
"""
from pathlib import Path
from pptx import Presentation
from pptx.util import Inches, Pt, Emu
from pptx.dml.color import RGBColor
from pptx.enum.shapes import MSO_SHAPE
from pptx.enum.text import PP_ALIGN, MSO_ANCHOR

HERE = Path(__file__).parent

# Brand palette (matches docs/PROJECT_PLAN.md §4.1)
NAVY     = RGBColor(0x0B, 0x12, 0x20)
BRAND    = RGBColor(0x3B, 0x82, 0xF6)
ACCENT   = RGBColor(0xBF, 0xCF, 0xE8)
MUTED    = RGBColor(0xF1, 0xF5, 0xF9)
MUTED_FG = RGBColor(0x64, 0x74, 0x8B)
BORDER   = RGBColor(0xE2, 0xE8, 0xF0)
SUCCESS  = RGBColor(0x10, 0xB9, 0x81)
WARN     = RGBColor(0xF5, 0x9E, 0x0B)
DANGER   = RGBColor(0xEF, 0x44, 0x44)
WHITE    = RGBColor(0xFF, 0xFF, 0xFF)
INK      = RGBColor(0x1E, 0x29, 0x3B)

prs = Presentation()
prs.slide_width  = Inches(13.333)
prs.slide_height = Inches(7.5)
BLANK = prs.slide_layouts[6]
TOTAL = 11

# -------- helpers --------
def text(slide, x, y, w, h, t, *, size=14, bold=False, italic=False, color=NAVY,
         align=PP_ALIGN.LEFT, font="Inter", anchor=MSO_ANCHOR.TOP):
    tb = slide.shapes.add_textbox(x, y, w, h)
    tf = tb.text_frame
    tf.margin_left = tf.margin_right = Emu(0)
    tf.margin_top = tf.margin_bottom = Emu(0)
    tf.word_wrap = True
    tf.vertical_anchor = anchor
    lines = t.split("\n")
    for i, ln in enumerate(lines):
        p = tf.paragraphs[0] if i == 0 else tf.add_paragraph()
        p.alignment = align
        r = p.add_run()
        r.text = ln
        r.font.name = font
        r.font.size = Pt(size)
        r.font.bold = bold
        r.font.italic = italic
        r.font.color.rgb = color
    return tb


def rect(slide, x, y, w, h, fill, line=None, line_w=0.6, shape=MSO_SHAPE.ROUNDED_RECTANGLE, adj=0.06):
    sh = slide.shapes.add_shape(shape, x, y, w, h)
    if shape == MSO_SHAPE.ROUNDED_RECTANGLE:
        sh.adjustments[0] = adj
    sh.fill.solid(); sh.fill.fore_color.rgb = fill
    if line is None: sh.line.fill.background()
    else:
        sh.line.color.rgb = line; sh.line.width = Pt(line_w)
    sh.shadow.inherit = False
    return sh


def line(slide, x1, y1, x2, y2, color=BORDER, w=0.6):
    cn = slide.shapes.add_connector(1, x1, y1, x2, y2)
    cn.line.color.rgb = color
    cn.line.width = Pt(w)
    return cn


def chrome(slide, page, section=""):
    bg = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, 0, 0, prs.slide_width, prs.slide_height)
    bg.fill.solid(); bg.fill.fore_color.rgb = WHITE; bg.line.fill.background()
    rect(slide, Inches(0), Inches(0), prs.slide_width, Inches(0.04), BRAND,
         shape=MSO_SHAPE.RECTANGLE)
    text(slide, Inches(0.5), Inches(0.18), Inches(2), Inches(0.4),
         "NEXIS", size=13, bold=True)
    text(slide, Inches(1.25), Inches(0.21), Inches(7), Inches(0.4),
         "Project progress review", size=11, color=MUTED_FG, italic=True)
    text(slide, Inches(9.5), Inches(0.18), Inches(3.3), Inches(0.4),
         section, size=11, color=MUTED_FG, align=PP_ALIGN.RIGHT)
    text(slide, Inches(12.0), Inches(7.05), Inches(1.2), Inches(0.3),
         f"{page} / {TOTAL}", size=10, color=MUTED_FG, align=PP_ALIGN.RIGHT)
    line(slide, Inches(0.5), Inches(7.02), Inches(12.83), Inches(7.02), BORDER, 0.4)


def head(slide, title, sub=None):
    text(slide, Inches(0.5), Inches(0.75), Inches(12.3), Inches(0.7),
         title, size=28, bold=True, color=NAVY, font="Inter Tight")
    if sub:
        text(slide, Inches(0.5), Inches(1.4), Inches(12.3), Inches(0.4),
             sub, size=14, color=MUTED_FG, italic=True)


# ==============================================================
# 1. TITLE
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 1, "Title")
rect(s, Inches(0.5), Inches(2.4), Inches(0.18), Inches(2.5), BRAND, shape=MSO_SHAPE.RECTANGLE)
text(s, Inches(0.85), Inches(2.3), Inches(12), Inches(0.6),
     "Project progress review", size=18, color=MUTED_FG, italic=True, font="Inter Tight")
text(s, Inches(0.85), Inches(2.7), Inches(12), Inches(1.1),
     "NEXIS", size=64, bold=True, color=NAVY, font="Inter Tight")
text(s, Inches(0.85), Inches(3.85), Inches(12), Inches(0.6),
     "Multi-tenant agentic fault-recovery platform", size=22, color=NAVY, font="Inter Tight")
text(s, Inches(0.85), Inches(4.45), Inches(12), Inches(0.5),
     "Architecture · database schema · phase progress",
     size=16, color=BRAND, italic=True, font="Inter Tight")

# Bottom metadata strip
rect(s, Inches(0.85), Inches(5.6), Inches(11.65), Inches(1.0), MUTED, BORDER, 0.5)
text(s, Inches(1.1), Inches(5.75), Inches(11), Inches(0.35),
     "Author", size=10, color=MUTED_FG, bold=True)
text(s, Inches(1.1), Inches(6.05), Inches(11), Inches(0.4),
     "Ratul Sikder", size=15, color=NAVY, font="Inter Tight", bold=True)
text(s, Inches(5.5), Inches(5.75), Inches(11), Inches(0.35),
     "Reporting period", size=10, color=MUTED_FG, bold=True)
text(s, Inches(5.5), Inches(6.05), Inches(11), Inches(0.4),
     "2026-05-10 → 2026-05-14", size=15, color=NAVY, font="Inter Tight", bold=True)
text(s, Inches(9.5), Inches(5.75), Inches(11), Inches(0.35),
     "Status", size=10, color=MUTED_FG, bold=True)
text(s, Inches(9.5), Inches(6.05), Inches(11), Inches(0.4),
     "Phase 8 shipped · Phase 9 in flight", size=15, color=SUCCESS, font="Inter Tight", bold=True)

# ==============================================================
# 2. SCOPE — what NEXIS does, at a glance (no marketing)
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 2, "Scope")
head(s, "Project scope",
     "A multi-tenant SaaS that runs a 9-agent recovery pipeline per incident, with human approval on risky patches")

cards = [
    ("Inputs",
     ["Sentry / ArgoCD / OTel webhooks",
      "GitHub repo (via App)",
      "Per-tenant API keys + RLS"]),
    ("Pipeline",
     ["9 LLM agents on Temporal",
      "Sandbox-validated patches",
      "Severity-routed approval gate"]),
    ("Outputs",
     ["Pull request on tenant repo",
      "Audit-logged decision trail",
      "Token + cost ledger"]),
]
for i, (h, bullets) in enumerate(cards):
    x = Inches(0.5 + i * 4.3)
    rect(s, x, Inches(2.2), Inches(4.0), Inches(4.4), WHITE, BORDER, 0.5)
    rect(s, x, Inches(2.2), Inches(4.0), Inches(0.5), BRAND)
    text(s, x, Inches(2.32), Inches(4.0), Inches(0.4),
         h, size=15, bold=True, color=WHITE, align=PP_ALIGN.CENTER, font="Inter Tight")
    for j, b in enumerate(bullets):
        yy = 2.95 + j * 0.55
        rect(s, x + Inches(0.3), Inches(yy + 0.12), Inches(0.1), Inches(0.1), BRAND)
        text(s, x + Inches(0.55), Inches(yy), Inches(3.4), Inches(0.45),
             b, size=12, color=NAVY)

# Strip
rect(s, Inches(0.5), Inches(6.85), Inches(12.3), Inches(0.3), MUTED, shape=MSO_SHAPE.RECTANGLE)
text(s, Inches(0.5), Inches(6.85), Inches(12.3), Inches(0.3),
     "Three deployable services + one Python sidecar + one Next.js frontend.   "
     "All tenant data row-level isolated in Postgres.",
     size=11, italic=True, color=MUTED_FG, align=PP_ALIGN.CENTER)

# ==============================================================
# 3. TECH STACK
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 3, "Tech stack")
head(s, "Tech stack",
     "Same code path runs in docker-compose (dev) and ECS Fargate (cloud)")

stack = [
    ("Backend",   [
        ("Go 1.22",          "control-plane · validator · gitops"),
        ("Chi",              "HTTP router + middleware chain"),
        ("Temporal",         "workflow orchestration · replay-deterministic"),
        ("sqlc + pgx",       "compile-time-checked SQL"),
        ("Python 3.12",      "DoWhy causal-inference sidecar (gRPC)"),
    ]),
    ("Data + Infra", [
        ("Postgres 16 + pgvector", "34 tables · RLS FORCE on every tenant table"),
        ("Neo4j 5",          "code dependency graph for Pathfinder"),
        ("Redis 7",          "rate-limit + cache"),
        ("MinIO / S3",       "envelope-encrypted patches + audit"),
        ("Temporal dev-svr", "Postgres-backed history"),
    ]),
    ("Frontend + Ops", [
        ("Next.js 16.2.2",   "landing + console · LIGHT default"),
        ("shadcn · Tremor · Monaco", "primitives · charts · diff viewer"),
        ("OTel → Grafana stack",    "Prometheus · Loki · Tempo"),
        ("docker-compose",   "16 services for full local parity"),
        ("GitHub Actions",   "lint · typecheck · test · build"),
    ]),
]
for i, (h, rows) in enumerate(stack):
    x = Inches(0.5 + i * 4.3)
    rect(s, x, Inches(2.2), Inches(4.0), Inches(4.7), WHITE, BORDER, 0.5)
    rect(s, x, Inches(2.2), Inches(0.12), Inches(4.7), BRAND, shape=MSO_SHAPE.RECTANGLE)
    text(s, x + Inches(0.2), Inches(2.3), Inches(3.7), Inches(0.4),
         h, size=14, bold=True, color=NAVY, font="Inter Tight")
    for j, (k, v) in enumerate(rows):
        yy = 2.85 + j * 0.78
        text(s, x + Inches(0.2), Inches(yy), Inches(3.7), Inches(0.35),
             k, size=12, bold=True, color=BRAND, font="JetBrains Mono")
        text(s, x + Inches(0.2), Inches(yy + 0.34), Inches(3.7), Inches(0.4),
             v, size=10, color=MUTED_FG)

# ==============================================================
# 4. SYSTEM ARCHITECTURE — embed microservices.png
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 4, "Architecture")
head(s, "System architecture",
     "Modular monolith for the agent plane + two carved-out services for security boundaries")

img = HERE / "microservices.png"
if img.exists():
    s.shapes.add_picture(str(img), Inches(5.4), Inches(1.85), height=Inches(5.2))

# Left: annotation key
ann = [
    ("control-plane",
     "Go modular monolith.  HTTP + Temporal worker + 9-agent plane in one binary.  "
     "Internal packages enforced by go-arch-lint."),
    ("validator",
     "Carved-out service.  Runs untrusted patch inside  --network=none --read-only  container."),
    ("gitops",
     "Carved-out service.  Sole holder of the GitHub App private key.  10-min JWT TTL."),
    ("causal-inference",
     "Python gRPC sidecar at :8090 for DoWhy backdoor adjustment.  Stateless."),
    ("Data plane",
     "Postgres + Neo4j + Redis + MinIO + Temporal — all on the same docker network."),
]
yy = 1.95
for h, body in ann:
    rect(s, Inches(0.5), Inches(yy), Inches(0.1), Inches(0.85), BRAND, shape=MSO_SHAPE.RECTANGLE)
    text(s, Inches(0.72), Inches(yy), Inches(4.6), Inches(0.35),
         h, size=13, bold=True, color=NAVY, font="Inter Tight")
    text(s, Inches(0.72), Inches(yy + 0.32), Inches(4.6), Inches(0.55),
         body, size=10, color=MUTED_FG)
    yy += 0.95

# ==============================================================
# 5. DATABASE SCHEMA — clustered overview (built natively)
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 5, "DB schema")
head(s, "Database schema — 34 tables across 8 domains",
     "Postgres 16 + pgvector.  All tenant tables FORCE row-level security on org_id.")

# Group definitions: (header_color, title, x, y, w, h, tables[])
groups = [
    (BRAND,   "Identity & Tenancy",        0.5,  2.05, 3.05, 2.05,
     ["organizations", "users", "org_members", "sessions",
      "magic_tokens", "api_keys", "org_invites", "workspaces"]),
    (SUCCESS, "Workflow & Agents",         3.7,  2.05, 3.05, 2.05,
     ["workflow_runs", "activity_events", "agent_runs",
      "patch_candidates", "validation_runs", "root_cause_reports",
      "deployments"]),
    (WARN,    "Approval & Audit",          6.9,  2.05, 3.05, 2.05,
     ["approval_decisions", "approval_policies", "slack_notifications",
      "audit_log", "audit_anchors", "audit.audit_events"]),
    (DANGER,  "Integrations & Sources",   10.1,  2.05, 2.75, 2.05,
     ["integrations", "incidents_raw", "incidents", "connectors", "services"]),

    (BRAND,   "LLM Spine",                 0.5,  4.25, 3.05, 2.05,
     ["token_budgets", "token_ledger", "code_embeddings",
      "prompt_versions", "memory_documents", "feedback_examples"]),
    (SUCCESS, "Evaluation",                3.7,  4.25, 3.05, 2.05,
     ["eval_runs", "eval_transcripts", "nasa_tlx_responses"]),
    (WARN,    "Billing",                   6.9,  4.25, 3.05, 2.05,
     ["payment_methods", "invoices", "usage_records",
      "entitlements", "stripe_events_processed"]),
    (DANGER,  "Org Structure",            10.1,  4.25, 2.75, 2.05,
     ["teams", "team_members", "invite_codes",
      "clerk_webhook_events", "waitlist"]),
]
for color, title, x_in, y_in, w_in, h_in, tables in groups:
    x, y = Inches(x_in), Inches(y_in)
    w, h = Inches(w_in), Inches(h_in)
    rect(s, x, y, w, h, WHITE, BORDER, 0.5)
    rect(s, x, y, w, Inches(0.32), color)
    text(s, x + Inches(0.15), y + Inches(0.05), w - Inches(0.3), Inches(0.3),
         title, size=11, bold=True, color=WHITE, font="Inter Tight")
    text(s, x + Inches(2.0), y + Inches(0.05), w - Inches(2.1), Inches(0.3),
         f"{len(tables)} tables", size=9, color=WHITE,
         align=PP_ALIGN.RIGHT, italic=True)
    # tables list, two-column inside the card
    cols_per_row = 2
    row_h = (h_in - 0.4) / ((len(tables) + cols_per_row - 1) // cols_per_row + 0.001)
    row_h = min(row_h, 0.28)
    for i, tname in enumerate(tables):
        col = i % cols_per_row
        row = i // cols_per_row
        cx = x + Inches(0.15) + Inches(col * (w_in - 0.3) / cols_per_row)
        cy = y + Inches(0.4 + row * row_h)
        rect(s, cx, cy + Inches(row_h*0.35), Inches(0.06), Inches(0.06), color, shape=MSO_SHAPE.OVAL)
        text(s, cx + Inches(0.13), cy, Inches((w_in - 0.3)/cols_per_row - 0.15), Inches(row_h),
             tname, size=9, color=NAVY, font="JetBrains Mono")

# Bottom legend
rect(s, Inches(0.5), Inches(6.45), Inches(12.3), Inches(0.5), MUTED, BORDER, 0.4)
text(s, Inches(0.7), Inches(6.55), Inches(12), Inches(0.35),
     "Latest migration: 0020_phase8_public_beta · 25 up-migrations on control-plane · all RLS-tested in CI",
     size=11, italic=True, color=MUTED_FG)

# ==============================================================
# 6. ERD — relationships between core entities (simplified)
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 6, "ERD")
head(s, "Entity-relationship diagram — core entities",
     "Every tenant entity FK to organizations via org_id, enforced at the DB layer")

# Build a compact ERD: organizations at top, fans down
def ent(x_in, y_in, w_in, h_in, name, color):
    x, y = Inches(x_in), Inches(y_in)
    w, h = Inches(w_in), Inches(h_in)
    rect(s, x, y, w, h, WHITE, color, 1.1)
    rect(s, x, y, w, Inches(0.32), color)
    text(s, x, y + Inches(0.04), w, Inches(0.3),
         name, size=11, bold=True, color=WHITE,
         align=PP_ALIGN.CENTER, font="Inter Tight")
    return (x, y, w, h)

def rel(a, b, mark="1..N", color=BORDER):
    ax, ay, aw, ah = a; bx, by, bw, bh = b
    # connect closest centers vertically
    x1 = ax + aw / 2; y1 = ay + ah
    x2 = bx + bw / 2; y2 = by
    if y2 < y1:    # b is above
        y1 = ay; y2 = by + bh
    cn = line(s, x1, y1, x2, y2, color, 0.9)

# org root
org = ent(5.65, 1.95, 2.0, 0.65, "organizations", NAVY)

# row 2: identity + workspace anchors
usr  = ent(0.6,  3.05, 1.8, 0.55, "users",          BRAND)
om   = ent(2.6,  3.05, 1.8, 0.55, "org_members",    BRAND)
api  = ent(4.6,  3.05, 1.8, 0.55, "api_keys",       BRAND)
wsp  = ent(6.6,  3.05, 1.8, 0.55, "workspaces",     SUCCESS)
intg = ent(8.6,  3.05, 1.8, 0.55, "integrations",   DANGER)
inc  = ent(10.6, 3.05, 1.8, 0.55, "incidents_raw",  DANGER)

for e in [usr, om, api, wsp, intg, inc]:
    rel(org, e)

# row 3: workflow cluster (off workspaces)
wfr = ent(5.7, 4.15, 1.9, 0.55, "workflow_runs", SUCCESS)
rel(wsp, wfr)

# row 4: per-run children
act = ent(0.6,  5.25, 1.9, 0.55, "activity_events",   SUCCESS)
agr = ent(2.7,  5.25, 1.9, 0.55, "agent_runs",        SUCCESS)
tkl = ent(4.8,  5.25, 1.9, 0.55, "token_ledger",      BRAND)
apd = ent(6.9,  5.25, 1.9, 0.55, "approval_decisions",WARN)
pcd = ent(9.0,  5.25, 1.9, 0.55, "patch_candidates",  SUCCESS)
vrn = ent(11.1, 5.25, 1.9, 0.55, "validation_runs",   SUCCESS)
for e in [act, agr, tkl, apd, pcd, vrn]:
    rel(wfr, e)

# row 5: billing + eval cluster (off org)
inv = ent(0.6,  6.35, 1.9, 0.55, "invoices",          WARN)
pm  = ent(2.7,  6.35, 1.9, 0.55, "payment_methods",   WARN)
usg = ent(4.8,  6.35, 1.9, 0.55, "usage_records",     WARN)
evr = ent(6.9,  6.35, 1.9, 0.55, "eval_runs",         BRAND)
emb = ent(9.0,  6.35, 1.9, 0.55, "code_embeddings",   BRAND)
tlx = ent(11.1, 6.35, 1.9, 0.55, "nasa_tlx_resp.",    BRAND)
for e in [inv, pm, usg, evr, emb, tlx]:
    rel(org, e, color=ACCENT)
# usage records also fk → workspaces
rel(wsp, usg, color=ACCENT)

# legend
text(s, Inches(0.5), Inches(2.7), Inches(5), Inches(0.3),
     "Lines = foreign-key relationships  ·  arrows simplified to vertical",
     size=10, color=MUTED_FG, italic=True)

# ==============================================================
# 7. RECOVERY PIPELINE — Temporal DAG (compact)
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 7, "Pipeline")
head(s, "RecoveryPipeline workflow — Temporal-orchestrated DAG",
     "Sequential L2 → sequential L1 → parallel DevOps∥DataEng → approval join → validate → PR")

def dag_node(x_in, y_in, label, kind="agent"):
    x, y = Inches(x_in), Inches(y_in)
    w, h = Inches(1.6), Inches(0.75)
    fill = {"agent": MUTED, "gate": ACCENT, "io": WHITE}[kind]
    ln_c = {"agent": BORDER, "gate": BRAND, "io": SUCCESS}[kind]
    rect(s, x, y, w, h, fill, ln_c, 1.0 if kind != "agent" else 0.5)
    text(s, x, y + Inches(0.18), w, Inches(0.5),
         label, size=11, bold=True, color=NAVY, align=PP_ALIGN.CENTER, font="Inter Tight")
    return (x, y, w, h)

def conn(a, b, color=BRAND):
    ax, ay, aw, ah = a; bx, by, bw, bh = b
    line(s, ax + aw, ay + ah/2, bx, by + bh/2, color, 1.2)

# Row 1 — L2 + L1
y1 = 2.45
n1 = dag_node(0.5,  y1, "Sentinel\nDetect")
n2 = dag_node(2.45, y1, "Pathfinder\nDiagnose")
n3 = dag_node(4.4,  y1, "Synthesiser\nPlan")
n4 = dag_node(6.35, y1, "Architect\nSolution")
n5 = dag_node(8.3,  y1, "Backend\nCodegen")
n6 = dag_node(10.25,y1, "QA\nTestGen")
for a, b in zip([n1,n2,n3,n4,n5],[n2,n3,n4,n5,n6]): conn(a,b)

# Row 2 — fan-out parallel, gate, IO
y2 = 4.05
n7 = dag_node(3.2,  y2, "DevOps\nPipeline")
n8 = dag_node(5.15, y2, "DataEng\nMigrations")
n9 = dag_node(7.1,  y2, "ApprovalGate\nRoute", kind="gate")
n10= dag_node(9.05, y2, "validator\nsandbox", kind="io")
n11= dag_node(11.0, y2, "gitops\nopen PR", kind="io")

# fan-out from QA to parallel branch
qa_right = (n6[0] + n6[2], n6[1] + n6[3]/2)
line(s, qa_right[0], qa_right[1], n7[0], n7[1] + n7[3]/2, BRAND, 1.1)
line(s, qa_right[0], qa_right[1], n8[0], n8[1] + n8[3]/2, BRAND, 1.1)
# join into gate
line(s, n7[0]+n7[2], n7[1]+n7[3]/2, n9[0], n9[1]+n9[3]/2, BRAND, 1.1)
line(s, n8[0]+n8[2], n8[1]+n8[3]/2, n9[0], n9[1]+n9[3]/2, BRAND, 1.1)
conn(n9, n10); conn(n10, n11)

# Annotations
rect(s, Inches(0.5), Inches(5.4), Inches(6.0), Inches(1.55), MUTED, BORDER, 0.5)
text(s, Inches(0.75), Inches(5.5), Inches(5.5), Inches(0.3),
     "Severity routing at ApprovalGate", size=12, bold=True, color=NAVY)
text(s, Inches(0.75), Inches(5.85), Inches(5.5), Inches(1),
     "low    → auto-approved\nmedium → 120 s signal/timer race\nhigh   → human required (no timer)",
     size=11, color=NAVY, font="JetBrains Mono")

rect(s, Inches(6.7), Inches(5.4), Inches(6.1), Inches(1.55), MUTED, BORDER, 0.5)
text(s, Inches(6.95), Inches(5.5), Inches(5.5), Inches(0.3),
     "Activity timeouts", size=12, bold=True, color=NAVY)
text(s, Inches(6.95), Inches(5.85), Inches(5.5), Inches(1),
     "stub        30 s start-to-close · 3 retries\nLLM-bound   5 min  · 2 retries\nrecorder    5 s    · 3 retries",
     size=11, color=NAVY, font="JetBrains Mono")

# ==============================================================
# 8. PHASE PROGRESS TIMELINE
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 8, "Progress")
head(s, "Phase progress — 8 of 9 phases shipped to main",
     "Two-day autonomous build sprint · all eight phases on main as of 2026-05-13")

phases = [
    ("Phase 1",   "Foundations + landing",       "shipped", "Next.js + docker-compose + Caddy TLS"),
    ("Phase 2",   "Auth + RLS + OTel",           "shipped", "WorkOS stub + 34-table RLS + Grafana stack"),
    ("Phase 3",   "Console + integrations",      "shipped", "8 surfaces + GitHub App + Sentry webhook"),
    ("Phase 3.5", "Workspaces + billing",        "shipped", "Onboarding wizard + Stripe-style mock"),
    ("Phase 4",   "Temporal + sandbox",          "shipped", "RecoveryPipeline DAG + validator sandbox"),
    ("Phase 5",   "L1 agents + LLM spine",       "shipped", "Architect/Backend/QA/DevOps/DataEng + pgvector"),
    ("Phase 6",   "L2 + Approval + GitOps",      "shipped · MVP", "Sentinel + Pathfinder + Synthesiser + gate"),
    ("Phase 7",   "WorkOS + Stripe wiring",      "scaffolded",    "Real-provider gated behind env flags"),
    ("Phase 8",   "Public beta + docs",          "shipped",       "Nextra docs + Shepherd tour + NASA-TLX"),
    ("Phase 9",   "Thesis + pilot + defence",    "in flight",     "Plan exists · activated this week"),
]

# Vertical rail
rail_x = 2.4
line(s, Inches(rail_x), Inches(2.15), Inches(rail_x), Inches(6.85), BORDER, 1.5)
for i, (tag, name, status, note) in enumerate(phases):
    y_in = 2.15 + i * 0.48
    is_active = status == "in flight"
    is_done   = "shipped" in status
    is_scaff  = status == "scaffolded"
    dot_color = SUCCESS if is_done else (BRAND if is_active else WARN)
    # dot
    rect(s, Inches(rail_x - 0.07), Inches(y_in + 0.05), Inches(0.14), Inches(0.14),
         dot_color, shape=MSO_SHAPE.OVAL)
    # tag
    text(s, Inches(0.5), Inches(y_in), Inches(1.7), Inches(0.3),
         tag, size=11, bold=True, color=NAVY, font="Inter Tight", align=PP_ALIGN.RIGHT)
    # name
    text(s, Inches(rail_x + 0.25), Inches(y_in), Inches(4.5), Inches(0.3),
         name, size=11, bold=True, color=NAVY, font="Inter Tight")
    # status pill
    sx, sy = Inches(rail_x + 4.85), Inches(y_in + 0.03)
    rect(s, sx, sy, Inches(1.5), Inches(0.25), dot_color)
    text(s, sx, sy, Inches(1.5), Inches(0.25),
         status, size=9, bold=True, color=WHITE,
         align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    # note
    text(s, Inches(rail_x + 6.5), Inches(y_in), Inches(6.3), Inches(0.3),
         note, size=10, color=MUTED_FG, italic=True)

# ==============================================================
# 9. BUILD STATS
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 9, "Build stats")
head(s, "Build stats — snapshot",
     "Snapshot of the main branch as of 2026-05-14")

stats = [
    ("9",        "cooperating LLM agents",        BRAND),
    ("34",       "Postgres tables (RLS FORCE)",   BRAND),
    ("25",       "control-plane migrations",      BRAND),
    ("4",        "backend services",              SUCCESS),
    ("287",      "Go source files",               SUCCESS),
    ("47k",      "lines of Go",                   SUCCESS),
    ("232",      "TS / TSX files (web + ui)",     WARN),
    ("64",       "Go test files",                 WARN),
    ("16",       "docker-compose services",       DANGER),
    ("52",       "commits this sprint",           DANGER),
    ("8 / 9",    "phases shipped",                NAVY),
    ("2 days",   "autonomous build sprint",       NAVY),
]
cols, rows = 4, 3
cw = 3.05; ch = 1.55
pad_x = 0.5; pad_y = 2.05
for i, (big, lbl, color) in enumerate(stats):
    col = i % cols; row = i // cols
    x = Inches(pad_x + col * cw)
    y = Inches(pad_y + row * ch)
    rect(s, x, y, Inches(cw - 0.1), Inches(ch - 0.1), WHITE, BORDER, 0.5)
    rect(s, x, y, Inches(0.12), Inches(ch - 0.1), color)
    text(s, x + Inches(0.25), y + Inches(0.2), Inches(cw - 0.5), Inches(0.7),
         big, size=34, bold=True, color=color, font="Inter Tight")
    text(s, x + Inches(0.25), y + Inches(0.95), Inches(cw - 0.5), Inches(0.4),
         lbl, size=11, color=MUTED_FG)

# ==============================================================
# 10. WORK DONE THIS PERIOD — recent commits / surfaces
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 10, "Work done")
head(s, "Work shipped this reporting period",
     "Highlights from the last 52 commits — surfaces, plumbing, observability")

groups = [
    ("Console + UX",
     ["28-provider integration catalog with brand logos",
      "Live-demo scenario library + recharts dashboards",
      "Onboarding wizard with provisioning animation",
      "Shepherd in-app tour + NASA-TLX widget"]),
    ("Backend + agents",
     ["L1 agents (5) calling LLM Provider iface",
      "L2 agents (Sentinel, Pathfinder, Synthesiser)",
      "ApprovalGate signal/timer race + Slack notifier",
      "L1 stub fallback for keyless demos"]),
    ("Platform + ops",
     ["RLS on every tenant table + CI fuzzer",
      "Audit hash-chain + nightly anchor",
      "OTel pipeline → Grafana / Loki / Tempo",
      "System-health page wired to real probes"]),
    ("Data + persistence",
     ["20 forward migrations · 34 tables",
      "pgvector RAG corpus for Backend agent",
      "Token ledger + monthly budget enforcer",
      "First-class self-healing projects schema"]),
]
for i, (h, items) in enumerate(groups):
    row = i // 2; col = i % 2
    x = Inches(0.5 + col * 6.3)
    y = Inches(2.1 + row * 2.45)
    rect(s, x, y, Inches(6.1), Inches(2.3), WHITE, BORDER, 0.5)
    rect(s, x, y, Inches(0.16), Inches(2.3), BRAND)
    text(s, x + Inches(0.35), y + Inches(0.15), Inches(5.7), Inches(0.4),
         h, size=14, bold=True, color=NAVY, font="Inter Tight")
    for j, it in enumerate(items):
        yy = y + Inches(0.6 + j * 0.4)
        rect(s, x + Inches(0.4), yy + Inches(0.13), Inches(0.08), Inches(0.08),
             SUCCESS, shape=MSO_SHAPE.OVAL)
        text(s, x + Inches(0.6), yy, Inches(5.4), Inches(0.4),
             it, size=11, color=NAVY)

# ==============================================================
# 11. RISKS + NEXT STEPS
# ==============================================================
s = prs.slides.add_slide(BLANK)
chrome(s, 11, "Next")
head(s, "Outstanding work · risks · next steps",
     "Phase 9 = thesis chapters 1–4 + pilot tenant activation + viva preparation")

# Two columns: Outstanding/Next vs Risks
rect(s, Inches(0.5), Inches(2.1), Inches(6.1), Inches(4.4), WHITE, BORDER, 0.5)
rect(s, Inches(0.5), Inches(2.1), Inches(0.16), Inches(4.4), SUCCESS)
text(s, Inches(0.8), Inches(2.25), Inches(5.6), Inches(0.4),
     "Next steps (Phase 9)", size=14, bold=True, color=NAVY, font="Inter Tight")
next_items = [
    "Provision real OpenAI + WorkOS + Stripe keys (env-gated)",
    "Investigate causal-inference container unhealthy state",
    "Recruit 5 pilot tenants from waitlist (target end-of-month)",
    "Run 30-fault × 5-seed × 2-provider eval benchmark",
    "Begin NASA-TLX user study (ethics approved Week 0)",
    "Draft thesis chapters 1–4 (intro · system · eval · discussion)",
    "Defence-rehearsal slot booked at Phase 6 completion",
]
for j, it in enumerate(next_items):
    yy = Inches(2.75 + j * 0.5)
    rect(s, Inches(0.85), yy + Inches(0.16), Inches(0.1), Inches(0.1), SUCCESS, shape=MSO_SHAPE.OVAL)
    text(s, Inches(1.05), yy, Inches(5.5), Inches(0.4),
         it, size=11, color=NAVY)

rect(s, Inches(6.75), Inches(2.1), Inches(6.1), Inches(4.4), WHITE, BORDER, 0.5)
rect(s, Inches(6.75), Inches(2.1), Inches(0.16), Inches(4.4), WARN)
text(s, Inches(7.05), Inches(2.25), Inches(5.6), Inches(0.4),
     "Open risks", size=14, bold=True, color=NAVY, font="Inter Tight")
risks = [
    ("Scope creep",            "Phase scope contractual · backlog otherwise"),
    ("LLM cost runaway",       "Pre-call budget + Ollama path mitigates"),
    ("Validator escape",       "Modal swap in Phase 7 · sandbox network=none"),
    ("Multi-tenant leak",      "RLS FORCE + CI fuzzer covers every table"),
    ("Design-partner ghosting","Oversubscribe to 5 partners; live demo as fallback"),
    ("Defence scheduling",     "12-week lead booked · Phase 9 buffer absorbs slip"),
]
for j, (k, v) in enumerate(risks):
    yy = Inches(2.75 + j * 0.6)
    rect(s, Inches(7.1), yy + Inches(0.18), Inches(0.1), Inches(0.1), WARN, shape=MSO_SHAPE.OVAL)
    text(s, Inches(7.3), yy, Inches(5.5), Inches(0.3),
         k, size=11, bold=True, color=NAVY, font="Inter Tight")
    text(s, Inches(7.3), yy + Inches(0.28), Inches(5.5), Inches(0.3),
         v, size=10, color=MUTED_FG)

# Bottom closing strip
rect(s, Inches(0.5), Inches(6.65), Inches(12.3), Inches(0.4), ACCENT, BRAND, 0.6, shape=MSO_SHAPE.RECTANGLE)
text(s, Inches(0.5), Inches(6.65), Inches(12.3), Inches(0.4),
     "Repo: github.com/.../nexis-eco   ·   local: app.nexis.local   ·   spec: docs/PROJECT_PLAN.md",
     size=11, color=NAVY, italic=True, align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)

# Save
out = HERE / "NEXIS_presentation.pptx"
prs.save(out)
print(f"Saved {out}")
print(f"Slides: {len(prs.slides)}")
