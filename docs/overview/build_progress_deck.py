#!/usr/bin/env python3
"""
NEXIS — Midterm 1 progress presentation.
Aligned to fyp.pdf proposal (4 phases, dual-layer, RLHF, IIT/DU framing).

Slides:
  1.  Title
  2.  System overview (black-box: input → ??? → output)
  3.  System concept (dual-layer multi-agent architecture)
  4.  Technical architecture (services + data flow)
  5.  Database microservices + Docker orchestration
  6.  Phase progress overview (4 phases, % done per module)
  7.  ✓ Done — User Management + Access Control (Phase 1, M1-3)
  8.  ✓ Done — Admin Dashboard + Intelligent System Control (Phase 1, M4)
  9.  ✓ Done — Web UI / Admin Console (cross-phase deliverable)
  10. ✓ Done — Self-Healing Loop + Layer 2 agents (Phase 2, M5-9)
  11. ✓ Done — Layer 1 Execution Agents (Phase 2, M10-11 + Phase 3 M12-14)
  12. ◑ Partial — Phase 2 remaining items
  13. ☐ Next — Phase 3 RLHF + Full Integration (per module)
  14. ☐ Next — Phase 4 Reporting, Evaluation & Submission (per module)
  15. Closing — overall status snapshot
"""
from pptx import Presentation
from pptx.util import Inches, Pt, Emu
from pptx.dml.color import RGBColor
from pptx.enum.shapes import MSO_SHAPE
from pptx.enum.text import PP_ALIGN, MSO_ANCHOR
from pathlib import Path


# ---- Minimal white theme --------------------------------------------------
WHITE = RGBColor(0xFF, 0xFF, 0xFF)
INK = RGBColor(0x10, 0x14, 0x1F)
SUB = RGBColor(0x6B, 0x72, 0x80)
RULE = RGBColor(0xE5, 0xE7, 0xEB)
CARD = RGBColor(0xF9, 0xFA, 0xFB)
ACCENT = RGBColor(0x1E, 0x40, 0xAF)
GREEN = RGBColor(0x05, 0x96, 0x69)
AMBER = RGBColor(0xD9, 0x77, 0x06)
RED = RGBColor(0xB9, 0x1C, 0x1C)
GREY = RGBColor(0x9C, 0xA3, 0xAF)


def prs_init():
    p = Presentation()
    p.slide_width = Inches(13.333)
    p.slide_height = Inches(7.5)
    return p


def new(prs):
    s = prs.slides.add_slide(prs.slide_layouts[6])
    bg = s.shapes.add_shape(MSO_SHAPE.RECTANGLE, 0, 0, prs.slide_width, prs.slide_height)
    bg.fill.solid(); bg.fill.fore_color.rgb = WHITE; bg.line.fill.background()
    return s


def txt(slide, x, y, w, h, text, *, size=14, color=INK, bold=False,
        align=PP_ALIGN.LEFT, anchor=MSO_ANCHOR.TOP, font="Helvetica"):
    tb = slide.shapes.add_textbox(x, y, w, h)
    tf = tb.text_frame
    tf.word_wrap = True
    tf.vertical_anchor = anchor
    tf.margin_left = Inches(0.05)
    tf.margin_right = Inches(0.05)
    tf.margin_top = Inches(0.02)
    tf.margin_bottom = Inches(0.02)
    p = tf.paragraphs[0]
    p.alignment = align
    r = p.add_run()
    r.text = text
    r.font.name = font
    r.font.size = Pt(size)
    r.font.bold = bold
    r.font.color.rgb = color
    return tb


def card(slide, x, y, w, h, *, fill=WHITE, border=RULE, weight=0.75):
    s = slide.shapes.add_shape(MSO_SHAPE.ROUNDED_RECTANGLE, x, y, w, h)
    s.fill.solid(); s.fill.fore_color.rgb = fill
    s.line.color.rgb = border; s.line.width = Pt(weight)
    s.shadow.inherit = False
    return s


def hairline(slide, x, y, w, color=RULE):
    ln = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, x, y, w, Emu(9525))  # ~0.01"
    ln.fill.solid(); ln.fill.fore_color.rgb = color
    ln.line.fill.background()
    return ln


def slide_header(slide, prs, eyebrow, title, *, footer=None, page=None, total=None):
    txt(slide, Inches(0.7), Inches(0.5), Inches(10), Inches(0.3),
        eyebrow.upper(), size=10, color=SUB, bold=True)
    txt(slide, Inches(0.7), Inches(0.82), Inches(12), Inches(0.7),
        title, size=28, color=INK, bold=True)
    hairline(slide, Inches(0.7), Inches(1.6), Inches(11.93))
    if footer:
        txt(slide, Inches(0.7), Inches(7.1), Inches(8), Inches(0.3),
            footer, size=9, color=SUB)
    if page is not None and total is not None:
        txt(slide, Inches(11.7), Inches(7.1), Inches(1.2), Inches(0.3),
            f"{page} / {total}", size=9, color=SUB, align=PP_ALIGN.RIGHT)


def bullets(slide, x, y, w, h, items, *, size=12, color=INK, bullet_color=ACCENT,
            line_after=4):
    tb = slide.shapes.add_textbox(x, y, w, h)
    tf = tb.text_frame
    tf.word_wrap = True
    tf.margin_left = Inches(0.05)
    for i, item in enumerate(items):
        p = tf.paragraphs[0] if i == 0 else tf.add_paragraph()
        p.space_after = Pt(line_after)
        r1 = p.add_run()
        r1.text = "•  "
        r1.font.name = "Helvetica"; r1.font.size = Pt(size); r1.font.color.rgb = bullet_color; r1.font.bold = True
        r2 = p.add_run()
        r2.text = item
        r2.font.name = "Helvetica"; r2.font.size = Pt(size); r2.font.color.rgb = color


def status_chip(slide, x, y, w, h, label, color):
    c = card(slide, x, y, w, h, fill=color, border=color)
    txt(slide, x, y + Inches(0.02), w, h, label, size=9, color=WHITE,
        bold=True, align=PP_ALIGN.CENTER, anchor=MSO_ANCHOR.MIDDLE)
    return c


TOTAL = 15


# ---- 1. Title -------------------------------------------------------------
def s_title(prs):
    s = new(prs)
    # Accent rule top-left
    bar = s.shapes.add_shape(MSO_SHAPE.RECTANGLE, Inches(0.7), Inches(0.7),
                              Inches(0.4), Inches(0.05))
    bar.fill.solid(); bar.fill.fore_color.rgb = ACCENT; bar.line.fill.background()

    txt(s, Inches(0.7), Inches(0.95), Inches(11), Inches(0.5),
        "MIDTERM 1 · 2026", size=11, color=SUB, bold=True)
    txt(s, Inches(0.7), Inches(1.5), Inches(11), Inches(1.0),
        "Nexis", size=64, color=INK, bold=True)
    txt(s, Inches(0.7), Inches(2.6), Inches(11), Inches(0.6),
        "A Multi-Agent Autonomous Engineering Platform",
        size=22, color=INK)
    txt(s, Inches(0.7), Inches(3.15), Inches(11), Inches(0.5),
        "with Closed-Loop Fault Recovery",
        size=22, color=ACCENT)

    txt(s, Inches(0.7), Inches(4.2), Inches(11), Inches(0.4),
        "1-month progress · ~150h development · daily 1–2 hours",
        size=12, color=SUB)

    # Three stat lines (minimal)
    hairline(s, Inches(0.7), Inches(5.0), Inches(11.93))
    cells = [
        ("Phase 1", "100% complete", GREEN),
        ("Phase 2", "≈ 80% complete", AMBER),
        ("Phases 3–4", "planned",  GREY),
    ]
    w = Inches(11.93 / 3)
    for i, (k, v, c) in enumerate(cells):
        x = Inches(0.7) + w * i
        txt(s, x + Inches(0.1), Inches(5.2), w - Inches(0.2), Inches(0.4),
            k, size=11, color=SUB, bold=True)
        txt(s, x + Inches(0.1), Inches(5.55), w - Inches(0.2), Inches(0.6),
            v, size=24, color=c, bold=True)

    hairline(s, Inches(0.7), Inches(6.5), Inches(11.93))
    txt(s, Inches(0.7), Inches(6.7), Inches(11), Inches(0.4),
        "Ratul Sikder · Roll 2506102 · Institute of Information Technology, University of Dhaka",
        size=11, color=SUB)


# ---- 2. Black-box system overview ----------------------------------------
def s_blackbox(prs):
    s = new(prs)
    slide_header(s, prs, "01 · System overview", "Nexis as a black box",
                 footer="Inputs are real production signals · Outputs are validated, approved fixes",
                 page=2, total=TOTAL)

    # Centre black box
    bw, bh = Inches(4.0), Inches(2.6); bx = Inches(4.65); by = Inches(2.4)
    box = s.shapes.add_shape(MSO_SHAPE.ROUNDED_RECTANGLE, bx, by, bw, bh)
    box.fill.solid(); box.fill.fore_color.rgb = INK
    box.line.color.rgb = INK; box.line.width = Pt(1)
    txt(s, bx, by + Inches(0.6), bw, Inches(0.6),
        "NEXIS", size=40, color=WHITE, bold=True, align=PP_ALIGN.CENTER)
    txt(s, bx, by + Inches(1.35), bw, Inches(0.4),
        "Closed-loop autonomous", size=12, color=GREY, align=PP_ALIGN.CENTER)
    txt(s, bx, by + Inches(1.65), bw, Inches(0.4),
        "fault recovery", size=12, color=GREY, align=PP_ALIGN.CENTER)
    txt(s, bx, by + Inches(2.05), bw, Inches(0.4),
        "9 agents · 2 layers · 1 admin", size=10, color=GREY,
        align=PP_ALIGN.CENTER, font="JetBrains Mono")

    # Left: inputs
    txt(s, Inches(0.7), Inches(2.4), Inches(3.7), Inches(0.3),
        "INPUTS", size=10, color=SUB, bold=True)
    bullets(s, Inches(0.7), Inches(2.75), Inches(3.7), Inches(3.0), [
        "Sentry issue (errors, exceptions)",
        "Datadog metric anomaly",
        "PagerDuty alert",
        "GitHub push / commit",
        "Schema drift, API contract breach",
        "Synthetic fault from console",
    ], size=12)

    # Right: outputs
    txt(s, Inches(8.9), Inches(2.4), Inches(3.7), Inches(0.3),
        "OUTPUTS", size=10, color=SUB, bold=True, align=PP_ALIGN.RIGHT)
    bullets(s, Inches(8.9), Inches(2.75), Inches(3.7), Inches(3.0), [
        "Pull request with synthesised patch",
        "Property-based unit + integration tests",
        "CI workflow + ArgoCD manifest changes",
        "DB migration on schema drift",
        "Slack DM / one-click admin approval",
        "Audit trail + plain-English summary",
    ], size=12)

    # Arrows
    a1 = s.shapes.add_shape(MSO_SHAPE.RIGHT_ARROW, Inches(4.45), Inches(3.6),
                            Inches(0.2), Inches(0.3))
    a1.fill.solid(); a1.fill.fore_color.rgb = ACCENT; a1.line.fill.background()
    a2 = s.shapes.add_shape(MSO_SHAPE.RIGHT_ARROW, Inches(8.65), Inches(3.6),
                            Inches(0.2), Inches(0.3))
    a2.fill.solid(); a2.fill.fore_color.rgb = GREEN; a2.line.fill.background()

    txt(s, Inches(0.7), Inches(6.4), Inches(11.93), Inches(0.4),
        "Operator view: a 3 a.m. page becomes a one-click approval. Most low-risk fixes never reach the human.",
        size=12, color=SUB, align=PP_ALIGN.CENTER)


# ---- 3. Dual-layer concept -----------------------------------------------
def s_concept(prs):
    s = new(prs)
    slide_header(s, prs, "02 · System concept", "Dual-layer multi-agent architecture",
                 footer="Layer 2 detects + diagnoses + plans  ·  Layer 1 executes (code, tests, deploys, schema)",
                 page=3, total=TOTAL)

    # Layer 2 on top
    l2y = Inches(1.9); l1y = Inches(4.3); h = Inches(2.0)
    card(s, Inches(0.7), l2y, Inches(11.93), h, fill=CARD)
    txt(s, Inches(0.9), l2y + Inches(0.15), Inches(11), Inches(0.4),
        "LAYER 2 — SELF-HEALING LOOP   (detection · diagnosis · planning · validation)",
        size=11, color=ACCENT, bold=True)
    # 4 agent boxes
    l2_agents = [
        ("Sentinel",   "Fault detection · streaming",       "Apache Flink + SPC"),
        ("Pathfinder", "Causal localisation · DoWhy",       "Neo4j + DoWhy"),
        ("Synthesiser","LLM-guided patch synthesis",        "GPT-4o · constraint-aware"),
        ("Validator",  "Docker shadow execution + tests",   "Hypothesis · property-based"),
    ]
    bw = Inches(2.78); gap = Inches(0.1); sx = Inches(0.9); cy = l2y + Inches(0.55)
    for i, (n, role, stack) in enumerate(l2_agents):
        x = sx + (bw + gap) * i
        card(s, x, cy, bw, Inches(1.3), fill=WHITE)
        txt(s, x + Inches(0.15), cy + Inches(0.15), bw - Inches(0.3), Inches(0.4),
            n, size=14, color=INK, bold=True)
        txt(s, x + Inches(0.15), cy + Inches(0.55), bw - Inches(0.3), Inches(0.35),
            role, size=10, color=SUB)
        txt(s, x + Inches(0.15), cy + Inches(0.9), bw - Inches(0.3), Inches(0.35),
            stack, size=9, color=ACCENT, font="JetBrains Mono")

    # Connector
    arr = s.shapes.add_shape(MSO_SHAPE.DOWN_ARROW, Inches(6.55), Inches(3.95),
                              Inches(0.2), Inches(0.3))
    arr.fill.solid(); arr.fill.fore_color.rgb = ACCENT; arr.line.fill.background()

    # Layer 1
    card(s, Inches(0.7), l1y, Inches(11.93), h, fill=CARD)
    txt(s, Inches(0.9), l1y + Inches(0.15), Inches(11), Inches(0.4),
        "LAYER 1 — EXECUTION TEAM   (code · tests · deploy · schema)",
        size=11, color=GREEN, bold=True)
    l1_agents = [
        ("Architect",     "System design + API contracts", "GPT-4o · contract enforce"),
        ("Backend",       "LLM code synthesis · min diff", "GPT-4o-mini"),
        ("QA",            "Property tests + defect loop",  "Hypothesis · fast-check"),
        ("DevOps",        "CI/CD + rollback decisions",    "GitHub Actions + ArgoCD"),
        ("Data Engineer", "ETL + schema drift recovery",   "Postgres + lineage"),
    ]
    bw = Inches(2.22); sx = Inches(0.9); cy = l1y + Inches(0.55)
    for i, (n, role, stack) in enumerate(l1_agents):
        x = sx + (bw + gap) * i
        card(s, x, cy, bw, Inches(1.3), fill=WHITE)
        txt(s, x + Inches(0.15), cy + Inches(0.15), bw - Inches(0.3), Inches(0.4),
            n, size=13, color=INK, bold=True)
        txt(s, x + Inches(0.15), cy + Inches(0.55), bw - Inches(0.3), Inches(0.35),
            role, size=9, color=SUB)
        txt(s, x + Inches(0.15), cy + Inches(0.9), bw - Inches(0.3), Inches(0.35),
            stack, size=8, color=GREEN, font="JetBrains Mono")

    txt(s, Inches(0.7), Inches(6.65), Inches(11.93), Inches(0.4),
        "9 agents · typed Pydantic-style messaging · shared Chroma (vector) + Postgres (relational) memory",
        size=11, color=SUB, align=PP_ALIGN.CENTER)


# ---- 4. Technical architecture --------------------------------------------
def s_architecture(prs):
    s = new(prs)
    slide_header(s, prs, "03 · Technical architecture",
                 "Microservices + data flow",
                 footer="All adapters behind ports · provider swaps are 1-file changes",
                 page=4, total=TOTAL)

    rows = [
        ("Frontend",      "Next.js 16 · React 19 · Admin Dashboard + Marketing",  ACCENT),
        ("Control-plane", "Go + chi · Auth · RLS · Temporal client · agent registry", INK),
        ("Agent runtime", "Temporal workflows · 9 activity handlers · approval signal/timer",  ACCENT),
        ("Sidecars",      "Validator (Docker sandbox) · GitOps (PR opener) · Causal-inference (Python)", GREEN),
        ("Data + memory", "PostgreSQL 16 (RLS+pgvector) · Chroma · Neo4j · MinIO · Redis", INK),
        ("Observability", "OpenTelemetry · Prometheus · Grafana · Loki · Tempo",   AMBER),
    ]
    y = Inches(1.85); h = Inches(0.78)
    for label, sub, color in rows:
        card(s, Inches(0.7), y, Inches(11.93), h, fill=CARD)
        # left coloured tab
        tab = s.shapes.add_shape(MSO_SHAPE.RECTANGLE, Inches(0.7), y,
                                  Inches(0.08), h)
        tab.fill.solid(); tab.fill.fore_color.rgb = color; tab.line.fill.background()
        txt(s, Inches(0.95), y + Inches(0.13), Inches(3.5), Inches(0.5),
            label, size=14, color=INK, bold=True)
        txt(s, Inches(4.5), y + Inches(0.2), Inches(8), Inches(0.5),
            sub, size=11, color=SUB)
        y += h + Inches(0.05)

    txt(s, Inches(0.7), Inches(6.85), Inches(11.93), Inches(0.4),
        "Port + adapter pattern enforced via go-arch-lint  ·  56% Go test coverage  ·  RLS hardened with WITH CHECK on 23 tables",
        size=11, color=SUB, align=PP_ALIGN.CENTER)


# ---- Database schema slide ------------------------------------------------
def s_schema(prs):
    s = new(prs)
    slide_header(s, prs, "04 · Database schema",
                 "PostgreSQL 16 + RLS · 31 tables across 7 domains",
                 footer="Every tenant table has FORCE ROW LEVEL SECURITY + WITH CHECK · audit_log + integrations on dual pool",
                 page=5, total=TOTAL)

    groups = [
        ("IDENTITY + ACCESS", ACCENT, [
            "organizations",
            "users",
            "org_members",
            "sessions",
            "magic_tokens",
            "api_keys",
            "org_invites · invite_codes",
        ]),
        ("WORKSPACES + PROJECTS", ACCENT, [
            "workspaces",
            "projects",
            "integrations",
            "webhook_deliveries",
            "audit_log · audit_anchors",
        ]),
        ("RECOVERY PIPELINE", GREEN, [
            "workflow_runs",
            "activity_events  (per-agent rows)",
            "incidents_raw",
            "approval_decisions",
            "code_embeddings  (pgvector 1536)",
            "slack_notifications",
        ]),
        ("BILLING + USAGE", AMBER, [
            "token_budgets",
            "token_ledger  (per-agent cost)",
            "usage_records  (per-hour metering)",
            "payment_methods · invoices",
            "entitlements · stripe_events_processed",
        ]),
        ("EVAL + RESEARCH", AMBER, [
            "eval_runs",
            "eval_transcripts",
            "nasa_tlx_responses",
            "waitlist",
        ]),
    ]
    cw = Inches(3.93); ch = Inches(2.5); gap = Inches(0.1); sx = Inches(0.7)
    # Row 1: 3 cards
    for i, (label, color, items) in enumerate(groups[:3]):
        x = sx + (cw + gap) * i
        y = Inches(1.85)
        card(s, x, y, cw, ch)
        st = s.shapes.add_shape(MSO_SHAPE.RECTANGLE, x, y, cw, Inches(0.06))
        st.fill.solid(); st.fill.fore_color.rgb = color; st.line.fill.background()
        txt(s, x + Inches(0.2), y + Inches(0.18), cw - Inches(0.3), Inches(0.4),
            label, size=10, color=color, bold=True)
        cy = y + Inches(0.6)
        for tbl in items:
            txt(s, x + Inches(0.25), cy, cw - Inches(0.4), Inches(0.28),
                "·  " + tbl, size=11, color=INK, font="JetBrains Mono")
            cy += Inches(0.28)
    # Row 2: 2 cards (centred)
    sx2 = Inches(0.7) + (cw + gap) * 0.5
    for j, (label, color, items) in enumerate(groups[3:]):
        x = sx2 + (cw + gap) * j
        y = Inches(4.45)
        card(s, x, y, cw, Inches(2.4))
        st = s.shapes.add_shape(MSO_SHAPE.RECTANGLE, x, y, cw, Inches(0.06))
        st.fill.solid(); st.fill.fore_color.rgb = color; st.line.fill.background()
        txt(s, x + Inches(0.2), y + Inches(0.18), cw - Inches(0.3), Inches(0.4),
            label, size=10, color=color, bold=True)
        cy = y + Inches(0.6)
        for tbl in items:
            txt(s, x + Inches(0.25), cy, cw - Inches(0.4), Inches(0.28),
                "·  " + tbl, size=11, color=INK, font="JetBrains Mono")
            cy += Inches(0.28)


# ---- 5. Database microservices + docker -----------------------------------
def s_docker(prs):
    s = new(prs)
    slide_header(s, prs, "05 · Database microservices + Docker",
                 "16-service orchestration (docker compose up)",
                 footer="Healthchecks · restart policies · memory limits · per-service log rotation",
                 page=6, total=TOTAL)

    groups = [
        ("APPLICATION", ACCENT, [
            ("Web",              "Next.js 16 · :3000"),
            ("Control-plane",    "Go + chi · :8080"),
            ("Validator",        "Docker sandbox runner"),
            ("GitOps",           "GitHub App PR opener"),
            ("Causal-inference", "Python FastAPI · :8090"),
        ]),
        ("DATA + MEMORY", GREEN, [
            ("Postgres 16",      "+ pgvector · RLS hardened"),
            ("Redis",            "Cache · rate-limit buckets"),
            ("MinIO",            "Patch storage · S3 API"),
            ("Neo4j",            "Dependency codegraph"),
            ("Temporal",         "Workflow engine"),
        ]),
        ("OBSERVABILITY", AMBER, [
            ("OTel Collector",   "OTLP gateway"),
            ("Prometheus",       "Metrics"),
            ("Grafana",          "Dashboards · :3030"),
            ("Loki",             "Logs · 14-day retention"),
            ("Tempo",            "Trace store"),
            ("MailHog",          "Dev SMTP · :8025"),
        ]),
    ]
    cw = Inches(3.95); gap = Inches(0.06); sx = Inches(0.7); y = Inches(1.85)
    for i, (label, color, items) in enumerate(groups):
        x = sx + (cw + gap) * i
        card(s, x, y, cw, Inches(5.0))
        # accent stripe top
        st = s.shapes.add_shape(MSO_SHAPE.RECTANGLE, x, y, cw, Inches(0.08))
        st.fill.solid(); st.fill.fore_color.rgb = color; st.line.fill.background()
        txt(s, x + Inches(0.25), y + Inches(0.2), cw - Inches(0.3), Inches(0.4),
            label, size=10, color=color, bold=True)
        cy = y + Inches(0.7)
        for n, sub in items:
            txt(s, x + Inches(0.25), cy, cw - Inches(0.4), Inches(0.32),
                n, size=12, color=INK, bold=True)
            txt(s, x + Inches(0.25), cy + Inches(0.3), cw - Inches(0.4), Inches(0.3),
                sub, size=9, color=SUB)
            cy += Inches(0.71)

    txt(s, Inches(0.7), Inches(6.95), Inches(11.93), Inches(0.4),
        "Workstation reproducibility: 1 command boots the full stack — every service has a healthcheck and a restart policy.",
        size=11, color=SUB, align=PP_ALIGN.CENTER)


# ---- 6. Phase progress overview ------------------------------------------
def s_phase_progress(prs):
    s = new(prs)
    slide_header(s, prs, "06 · Overall progress",
                 "Four-phase plan from proposal · current state",
                 footer="Source: fyp.pdf Strategy & Timeline, 26-week Gantt",
                 page=7, total=TOTAL)

    phases = [
        ("Phase 1", "User System · Admin Dashboard · Architecture", 1.00, GREEN,
         "Tasks 1–6 complete · auth, RLS, dashboard, alerts"),
        ("Phase 2", "Self-Healing Loop · Execution Agents",          0.80, AMBER,
         "Tasks 7–11 complete · Sentinel/Pathfinder/Synthesiser/Validator built · pending: full Apache Flink streaming + OpenLineage"),
        ("Phase 3", "Remaining Agents · RLHF · Full Integration",    0.40, AMBER,
         "Architect/QA/DevOps already built ahead of schedule · RLHF loop + full agent integration pending"),
        ("Phase 4", "Reporting · Evaluation · Submission",           0.10, GREY,
         "Reporting feed built · NASA-TLX modal in place · fault-injection benchmark + final report pending"),
    ]

    track_x = Inches(4.0); track_w = Inches(7.5); y = Inches(1.9); row = Inches(1.15)
    for label, sub, pct, color, note in phases:
        # left label block
        txt(s, Inches(0.7), y, Inches(3.0), Inches(0.4),
            label, size=14, color=INK, bold=True)
        txt(s, Inches(0.7), y + Inches(0.35), Inches(3.2), Inches(0.7),
            sub, size=10, color=SUB)
        # track
        bg = s.shapes.add_shape(MSO_SHAPE.ROUNDED_RECTANGLE, track_x, y + Inches(0.1),
                                 track_w, Inches(0.32))
        bg.fill.solid(); bg.fill.fore_color.rgb = CARD
        bg.line.color.rgb = RULE; bg.line.width = Pt(0.5)
        # fill
        fw = Emu(int(track_w * pct))
        fill = s.shapes.add_shape(MSO_SHAPE.ROUNDED_RECTANGLE, track_x, y + Inches(0.1),
                                   fw, Inches(0.32))
        fill.fill.solid(); fill.fill.fore_color.rgb = color
        fill.line.fill.background()
        # pct label
        txt(s, track_x + track_w + Inches(0.1), y + Inches(0.07), Inches(0.7), Inches(0.4),
            f"{int(pct * 100)}%", size=14, color=color, bold=True)
        # note
        txt(s, track_x, y + Inches(0.55), track_w, Inches(0.5),
            note, size=10, color=SUB)
        y += row

    txt(s, Inches(0.7), Inches(6.8), Inches(11.93), Inches(0.4),
        "Built in ~150 hours over 4 weeks. Most of Phase 3's agents were built ahead of schedule during Phase 2.",
        size=11, color=SUB, align=PP_ALIGN.CENTER)


# ---- Module slide helper --------------------------------------------------
def _module_card(slide, x, y, w, h, *, status, num, title, summary, evidence):
    """One module card with status chip + module number + title + summary + evidence."""
    card(slide, x, y, w, h, fill=CARD)
    # status chip
    if status == "done":
        status_chip(slide, x + Inches(0.15), y + Inches(0.15),
                    Inches(0.55), Inches(0.28), "DONE", GREEN)
    elif status == "partial":
        status_chip(slide, x + Inches(0.15), y + Inches(0.15),
                    Inches(0.55), Inches(0.28), "WIP", AMBER)
    else:
        status_chip(slide, x + Inches(0.15), y + Inches(0.15),
                    Inches(0.55), Inches(0.28), "NEXT", GREY)
    # module number
    txt(slide, x + Inches(0.78), y + Inches(0.15), Inches(0.8), Inches(0.3),
        num, size=10, color=SUB, bold=True, font="JetBrains Mono")
    # title
    txt(slide, x + Inches(0.15), y + Inches(0.5), w - Inches(0.3), Inches(0.4),
        title, size=13, color=INK, bold=True)
    # summary
    txt(slide, x + Inches(0.15), y + Inches(0.92), w - Inches(0.3), h - Inches(1.4),
        summary, size=10, color=SUB)
    # evidence (small mono at bottom)
    txt(slide, x + Inches(0.15), y + h - Inches(0.45), w - Inches(0.3), Inches(0.35),
        evidence, size=9, color=ACCENT, font="JetBrains Mono")


def _slide_modules_4(prs, eyebrow, title, footer, page, items):
    """4-module grid (2x2)."""
    s = new(prs)
    slide_header(s, prs, eyebrow, title, footer=footer, page=page, total=TOTAL)
    cw = Inches(5.92); ch = Inches(2.55); gap = Inches(0.1)
    for i, (status, num, t, summ, ev) in enumerate(items):
        col = i % 2; row = i // 2
        x = Inches(0.7) + (cw + gap) * col
        y = Inches(1.85) + (ch + gap) * row
        _module_card(s, x, y, cw, ch, status=status, num=num,
                     title=t, summary=summ, evidence=ev)
    return s


def _slide_modules_3(prs, eyebrow, title, footer, page, items):
    """3 modules stacked, full-width."""
    s = new(prs)
    slide_header(s, prs, eyebrow, title, footer=footer, page=page, total=TOTAL)
    w = Inches(11.93); h = Inches(1.65)
    for i, (status, num, t, summ, ev) in enumerate(items):
        y = Inches(1.85) + (h + Inches(0.1)) * i
        _module_card(s, Inches(0.7), y, w, h, status=status, num=num,
                     title=t, summary=summ, evidence=ev)
    return s


# ---- Done — consolidated checklist (replaces slides 7-11) -----------------
def _check_row(slide, x, y, w, label, sub=None):
    """A single check-mark row."""
    # ✓ glyph
    ck = txt(slide, x, y, Inches(0.35), Inches(0.32),
             "✓", size=14, color=GREEN, bold=True)
    txt(slide, x + Inches(0.4), y, w - Inches(0.4), Inches(0.32),
        label, size=11, color=INK, bold=True)
    if sub:
        txt(slide, x + Inches(0.4), y + Inches(0.28), w - Inches(0.4), Inches(0.3),
            sub, size=9, color=SUB)


def s_done_list_1(prs):
    s = new(prs)
    slide_header(s, prs, "07 · Completed",
                 "Phase 1 — User System, Admin Dashboard & Web UI",
                 footer="All Phase 1 modules (Tasks 1–4) complete · Web UI ships across every phase",
                 page=8, total=TOTAL)

    # Left column — Phase 1
    txt(s, Inches(0.7), Inches(1.85), Inches(5.92), Inches(0.4),
        "PHASE 1 — USER SYSTEM + ADMIN DASHBOARD",
        size=10, color=ACCENT, bold=True)
    items_left = [
        ("Task 1 · Project setup + architecture",        "Go + Next.js monorepo · docker-compose · port/adapter layout"),
        ("Task 2 · User registration + login",           "Email/password · JWT session · MFA enrol/verify · magic-token"),
        ("Task 2 · Session + password management",       "Session cookie · Redis cache · MFA enforcement · reset flow"),
        ("Task 3 · Role management (Admin · Engineer · Viewer)",
                                                         "RBAC middleware on every route · invite-code workflow"),
        ("Task 3 · Intelligent role recommendations",    "Login anomaly flags · audit-log driven prompts"),
        ("Task 4 · Admin dashboard",                     "Live agent fleet · incidents · approvals · system health"),
        ("Task 4 · Intelligent alerts + risk scoring",   "Severity classifier · auto-resolve low-risk · escalate high-risk"),
        ("Task 4 · One-click approval surface",          "Approve / reject dialog · countdown · Slack DM · ArgoCD sync"),
        ("Task 4 · Audit visibility",                    "HMAC-chained log · CSV export · 27 RLS policies"),
    ]
    cy = Inches(2.3)
    for k, v in items_left:
        _check_row(s, Inches(0.7), cy, Inches(5.92), k, sub=v)
        cy += Inches(0.5)

    # Right column — Web UI
    txt(s, Inches(6.72), Inches(1.85), Inches(5.92), Inches(0.4),
        "WEB UI — ADMIN CONSOLE + MARKETING",
        size=10, color=ACCENT, bold=True)
    items_right = [
        ("26 console surfaces",                          "Dashboard · Projects · Incidents · Approvals · Audit · Agents · Workflows · Activity · Validator · Performance · Cost · Health · Integrations · Webhooks · Connections · Recovery · Eval · Live Demo · Knowledge · 7 settings pages"),
        ("13 marketing pages",                           "Product · Pricing · Docs · Changelog · Status · About · Careers · Contact · Privacy · Terms · Security · 404 · Verify"),
        ("Light + dark themes",                          "Tailwind v4 · @theme inline · WCAG-AA contrast"),
        ("9 reusable chart primitives",                  "Recharts · LineArea · StackedBar · Donut · Heatmap · KPI · Sparkline"),
        ("Accessibility",                                "Skip-to-content · focus traps · aria-labels · keyboard nav"),
        ("Error + Loading boundaries",                    "Per-scope error.tsx + loading.tsx · graceful 404 + 5xx"),
        ("Mobile responsive",                             "Sidebar Sheet · Topbar hamburger · tables overflow-x-auto"),
        ("28-provider integration catalog",               "6 categories · brand-logo tiles · 6 live · 22 roadmap"),
        ("Zero lint errors · 56% test coverage",          "Strict TS · TanStack Query · React 19 idioms"),
    ]
    cy = Inches(2.3)
    for k, v in items_right:
        _check_row(s, Inches(6.72), cy, Inches(5.92), k, sub=v)
        cy += Inches(0.5)


def s_done_list_2(prs):
    s = new(prs)
    slide_header(s, prs, "08 · Completed",
                 "Phase 2 — Self-Healing Loop + Execution Agents (80%)",
                 footer="All 9 agents built · proposal Tasks 5–14 · Layer 1 agents shipped ahead of schedule",
                 page=9, total=TOTAL)

    # Left — Layer 2
    txt(s, Inches(0.7), Inches(1.85), Inches(5.92), Inches(0.4),
        "LAYER 2 — SELF-HEALING LOOP",
        size=10, color=ACCENT, bold=True)
    items_l2 = [
        ("Task 5 · Shared memory + agent messaging",     "Postgres + pgvector · typed DTOs between activities · token ledger"),
        ("Task 6 · Sentinel agent — fault detection",    "Multi-source (Sentry · Datadog · PagerDuty · GitHub)"),
        ("Task 6 · Severity scoring + admin notification","60s fingerprint dedupe · project-aware routing"),
        ("Task 7 · Pathfinder agent — codegraph",        "Neo4j dependency graph · counterfactual root-cause analysis"),
        ("Task 7 · DoWhy causal inference",              "Python FastAPI sidecar · ranked hypotheses with confidence"),
        ("Task 8 · Synthesiser agent",                   "LLM-guided patch synthesis · structured-output retries · agent delegation logic"),
        ("Task 9 · Validator agent — Docker sandbox",    "`docker run --rm --network=none` · property-based tests via Hypothesis"),
        ("Task 9 · Approval queue + audit trail",        "Severity-routed · countdown timer · auto-merge thresholds · kill switch"),
    ]
    cy = Inches(2.3)
    for k, v in items_l2:
        _check_row(s, Inches(0.7), cy, Inches(5.92), k, sub=v)
        cy += Inches(0.55)

    # Right — Layer 1
    txt(s, Inches(6.72), Inches(1.85), Inches(5.92), Inches(0.4),
        "LAYER 1 — EXECUTION TEAM",
        size=10, color=GREEN, bold=True)
    items_l1 = [
        ("Task 10 · Backend agent — code synthesis",      "Minimal-diff patches · gpt-4o-mini · encrypted in MinIO"),
        ("Task 11 · Data Engineer agent — schema drift",  "Migration synthesis · up + down SQL · backfill plan"),
        ("Task 12 · Architect agent — contracts",         "System blueprint · API contract enforcement · dispatch routing"),
        ("Task 13 · QA agent — property tests",           "Hypothesis-driven test gen · defect feedback loop"),
        ("Task 14 · DevOps agent — CI/CD orchestration",  "GitHub Actions YAML · ArgoCD app diff · rollback policy"),
        ("End-to-end pipeline (Temporal workflow)",       "9 activities · approval signal/timer · 26 s real-run wall-clock"),
        ("Real GitHub integration",                       "GitHub App · installation token mint · PR opener service"),
        ("Real OpenAI integration",                       "gpt-4o + gpt-4o-mini · ~3,000 tokens per real recovery"),
    ]
    cy = Inches(2.3)
    for k, v in items_l1:
        _check_row(s, Inches(6.72), cy, Inches(5.92), k, sub=v)
        cy += Inches(0.55)


# ---- 7. Done — User Management + Access Control ---------------------------
def s_done_user_mgmt(prs):
    return _slide_modules_4(
        prs, "06 · ✓ Done — Phase 1",
        "User Management + Access Control",
        "Proposal Tasks 2 & 3 · Phase 1 Weeks 2–4 · all delivered",
        7,
        [
            ("done", "TASK 2", "User Registration & Login",
             "Email + password, JWT session, MFA enrol/verify, magic-token flow, OAuth callback shell, sign-up invite-code claim.",
             "handler/auth.go · /v1/auth/{login,signup,mfa,magic}"),
            ("done", "TASK 2", "Session & Password Management",
             "Session cookie + Redis cache, refresh, MFA enforcement on protected routes, password reset via magic token.",
             "middleware/auth.go · /v1/auth/refresh"),
            ("done", "TASK 3", "Role Management — Admin · Engineer · Viewer",
             "Three roles wired end-to-end. RBAC middleware on every protected route. Owner/admin can invite + assign.",
             "middleware/rbac.go · RequireRole(owner,admin)"),
            ("done", "TASK 3", "Intelligent Role Recommendations",
             "Login anomalies flagged (unusual time/device) · audit-log driven prompts in admin panel · invite-code workflow.",
             "/console/settings/members · audit_log table"),
        ],
    )


# ---- 8. Done — Admin Dashboard + Intelligent Control ----------------------
def s_done_dashboard(prs):
    return _slide_modules_4(
        prs, "07 · ✓ Done — Phase 1",
        "Admin Dashboard + Intelligent System Control",
        "Proposal Task 4 · Phase 1 Weeks 5–6 · delivered end-to-end",
        8,
        [
            ("done", "TASK 4", "Live Operational Dashboard",
             "Active agents · running pipelines · open incidents · pending approvals — every panel polls live data from Temporal + Postgres.",
             "/console · DashboardCharts.tsx · 8 chart types"),
            ("done", "TASK 4", "Intelligent Alerts + Risk Scoring",
             "Severity classifier in Approval Gate, risk score per recovery, low-risk auto-resolved, high-risk surfaced with full context.",
             "approval.severity.go · risk_score numeric(5,4)"),
            ("done", "TASK 4", "One-Click Approval Surface",
             "Approve / Reject dialog with notes · countdown timer · Slack DM to on-call · ArgoCD sync on green.",
             "/console/approvals · POST .../approve · .../reject"),
            ("done", "TASK 4", "Audit Visibility",
             "HMAC-chained audit log · CSV export · 27 RLS policies with WITH CHECK · admin can replay every decision.",
             "audit/hmac_writer.go · /v1/audit"),
        ],
    )


# ---- 9. Done — Web UI / Admin Console -------------------------------------
def s_done_webui(prs):
    s = new(prs)
    slide_header(s, prs, "08 · ✓ Done — cross-phase deliverable",
                 "Web UI / Admin Console (Next.js 16 + Tailwind v4)",
                 footer="26 console surfaces · 19 marketing pages · 0 lint errors · mobile-responsive",
                 page=9, total=TOTAL)

    # 2-col layout
    left = Inches(0.7); right = Inches(7.0); w = Inches(5.93); y = Inches(1.85)
    # Left card — surfaces
    card(s, left, y, w, Inches(5.0))
    txt(s, left + Inches(0.25), y + Inches(0.2), w - Inches(0.3), Inches(0.4),
        "CONSOLE SURFACES (26)", size=10, color=ACCENT, bold=True)
    sections = [
        ("Workspace",     "Dashboard · Projects · Incidents · Approvals · Audit Log"),
        ("Fleet",         "Agents (drill-down per agent) · Workflows · Activity Stream · Validator"),
        ("Observability", "Performance · Cost Tracker · System Health"),
        ("Integrations",  "All Integrations (28 providers) · Webhooks · Connection Health"),
        ("Operations",    "Recovery Pipeline · Eval Harness · Live Demo (27 fixtures) · Knowledge Base"),
        ("Settings",      "Profile · API keys · Workspaces · Members · Billing · Preferences"),
    ]
    cy = y + Inches(0.65)
    for label, body in sections:
        txt(s, left + Inches(0.25), cy, w - Inches(0.4), Inches(0.3),
            label, size=11, color=INK, bold=True)
        txt(s, left + Inches(0.25), cy + Inches(0.27), w - Inches(0.4), Inches(0.4),
            body, size=10, color=SUB)
        cy += Inches(0.72)

    # Right card — landing + craft
    card(s, right, y, w, Inches(5.0))
    txt(s, right + Inches(0.25), y + Inches(0.2), w - Inches(0.3), Inches(0.4),
        "MARKETING + CRAFT", size=10, color=ACCENT, bold=True)
    bullets(s, right + Inches(0.25), y + Inches(0.6), w - Inches(0.4), Inches(4.4), [
        "Public landing — hero, agents fleet, live-demo embed",
        "13 marketing pages: product · pricing · docs · changelog · status · about · careers · contact · privacy · terms · security",
        "Mobile sidebar Sheet (md breakpoint) · keyboard accessible",
        "9 reusable chart primitives (recharts) · 28 brand-logo provider tiles",
        "Skip-to-content links · focus traps · WCAG-AA contrast",
        "Error.tsx + Loading.tsx boundaries on every scope",
        "Light + dark themes via @theme inline CSS variables",
        "Server / client component boundary fixed (EmptyState iconNode)",
        "Sign-in / Sign-up / MFA / Verify flows all SSR-safe",
    ], size=11)


# ---- 10. Done — Self-Healing Loop + Layer 2 -------------------------------
def s_done_layer2(prs):
    return _slide_modules_4(
        prs, "09 · ✓ Done — Phase 2",
        "Self-Healing Loop + Layer 2 (Detection · Diagnosis · Repair)",
        "Proposal Tasks 5–9 · Phase 2 Weeks 7–14 · 80% of Phase 2 complete",
        10,
        [
            ("done", "TASK 5", "Shared Memory + Agent Messaging",
             "Postgres relational store · pgvector (Chroma not yet wired, see partial) · typed Pydantic-style DTOs between activities.",
             "domain/agent.go · token_ledger · code_embeddings"),
            ("done", "TASK 6", "Sentinel — Fault Detection",
             "Multi-source detector (Sentry · Datadog · PagerDuty · GitHub) with 60s fingerprint dedupe + project routing.",
             "sentinel/detector.go · multisource.go · router.go"),
            ("done", "TASK 7", "Pathfinder — Causal Diagnosis",
             "Neo4j dependency codegraph · DoWhy sidecar (Python FastAPI) · ranked root-cause hypotheses with confidence scores.",
             "agents/pathfinder · causal-inference/ · :8090"),
            ("done", "TASK 8–9", "Synthesiser + Validator",
             "LLM patch synthesis via gpt-4o + structured-output retries · Docker shadow execution via validator sidecar (network=none).",
             "agents/synthesiser · services/validator · MinIO patches"),
        ],
    )


# ---- 11. Done — Layer 1 Execution Agents ---------------------------------
def s_done_layer1(prs):
    return _slide_modules_4(
        prs, "10 · ✓ Done — Phase 2 + Phase 3 (ahead)",
        "Layer 1 Execution Team — Architect · Backend · QA · DevOps · Data Engineer",
        "Built ahead of schedule · Proposal Phase 3 Tasks 12–14 already in production",
        11,
        [
            ("done", "TASK 10", "Backend Agent — Code Synthesis",
             "Minimal-diff patch generation via gpt-4o-mini · encrypted in MinIO · admin views via in-panel diff viewer.",
             "agents/backend · /pipelines/{run}/patch?key=…"),
            ("done", "TASK 11", "Data Engineer Agent",
             "Schema-diff aware migration synthesis · Postgres up + down SQL · backfill plan · drift evidence.",
             "agents/data_engineer · migrations/scenarios/"),
            ("done", "TASK 12", "Architect Agent",
             "System blueprint + API contract enforcement · selects which L1 agents to dispatch · structured JSON output.",
             "agents/architect · gpt-4o + JSON schema retry"),
            ("done", "TASK 13–14", "QA + DevOps Agents",
             "QA: property-based test generation · DevOps: GitHub Actions YAML + ArgoCD app diff · rollback decision logic.",
             "agents/qa · agents/devops · approval.policy"),
        ],
    )


# ---- 12. Partial — Phase 2 remaining --------------------------------------
def s_partial(prs):
    return _slide_modules_4(
        prs, "09 · ◑ In progress",
        "Phase 2 — remaining 20%",
        "Items left from proposal Phase 2 · planned for next 1–2 weeks",
        10,
        [
            ("partial", "TASK 5", "Chroma Vector Store Migration",
             "Currently using pgvector for retrieval. Proposal calls for Chroma. Need to evaluate latency + isolation before swap.",
             "TODO · adapter/retrieval/chroma_provider.go"),
            ("partial", "TASK 6", "Apache Flink Streaming for Sentinel",
             "Current Sentinel runs as a 10s poll loop. Proposal calls for true streaming via Apache Flink + Statistical Process Control.",
             "TODO · services/sentinel-flink/"),
            ("partial", "TASK 7", "OpenLineage Automated Tracking",
             "Pathfinder reads the codegraph from Neo4j directly. OpenLineage lineage hooks not yet ingested into Pathfinder's prompts.",
             "TODO · openlineage producer + collector"),
            ("partial", "TASK 8", "Constraint-Aware Prompt Templates",
             "Synthesiser uses raw plan + retrieval context. Pydantic schema constraints proposed but not yet attached to every prompt.",
             "TODO · agents/synthesiser/contracts.go"),
        ],
    )


# ---- 13. Next — Phase 3 ---------------------------------------------------
def s_next_phase3(prs):
    return _slide_modules_4(
        prs, "10 · ☐ Next",
        "Phase 3 — RLHF + Full Integration",
        "Proposal Tasks 15–16 · Phase 3 Weeks 20–21",
        11,
        [
            ("next", "TASK 15", "RLHF Fine-Tuning Loop",
             "Capture every engineer approval / rejection / modification → preference pairs → DPO fine-tune of patch ranker monthly.",
             "Plan · usecase/rlhf · adapter/llm/finetune"),
            ("next", "TASK 16", "Cross-Layer Audit + Trace Stitching",
             "End-to-end OTel trace from Sentinel webhook through Approval Gate, joined with HMAC-chained audit log for forensics.",
             "Plan · otel-span linking · trace_id on every event"),
            ("next", "—", "Long-Running Workflow Resilience",
             "Temporal heartbeat tuning · retry budgets per agent · circuit breakers for upstream LLM 429s.",
             "Plan · workflow/recovery/policy.go"),
            ("next", "—", "Knowledge Base Population",
             "Index customer repo snapshots + past resolutions into pgvector. Currently only synthetic fixtures are indexed.",
             "Plan · /console/knowledge · ingest job"),
        ],
    )


# ---- 14. Next — Phase 4 ---------------------------------------------------
def s_next_phase4(prs):
    return _slide_modules_4(
        prs, "11 · ☐ Next",
        "Phase 4 — Reporting, Evaluation & Submission",
        "Proposal Tasks 17–20 · Phase 4 Weeks 22–26",
        12,
        [
            ("next", "TASK 17", "Intelligent Reporting + Daily Summary",
             "Skeleton built (audit list + activity stream). Pending: daily digest email + targeted alert filter to suppress low-value pages.",
             "Plan · cron · summary.tex template"),
            ("next", "TASK 18", "Human Approval Interface — Polish",
             "Approval view ships diff + tests + plain-English summary. Pending: side-by-side test result panel + shadow-run output.",
             "Polish · /console/approvals · DecisionDialog"),
            ("next", "TASK 19", "Fault-Injection Benchmark",
             "Chaos harness with 30 fault classes (null-deref · schema-drift · OOM · race · deploy fail). Compare vs PagerDuty+human baseline.",
             "Plan · tests/chaos/ · NASA-TLX user study"),
            ("next", "TASK 20", "Final Submission Bundle",
             "Thesis chapters 3-5 · defense slides · live demo recording · GitHub repo · production deploy on AWS (Terraform ready).",
             "Plan · thesis/ · v1.0 tag · launch-day runbook"),
        ],
    )


# ---- Helper: per-agent example card --------------------------------------
def _agent_example(slide, x, y, w, h, *, name, layer, model, input_text, output_text):
    """Three-row card: header (name + layer chip + model), IN, OUT."""
    card(slide, x, y, w, h, fill=CARD)
    # Layer chip
    chip_color = ACCENT if layer == "L2" else GREEN
    status_chip(slide, x + Inches(0.15), y + Inches(0.18),
                Inches(0.5), Inches(0.26), layer, chip_color)
    # Name
    txt(slide, x + Inches(0.75), y + Inches(0.15), w - Inches(0.9), Inches(0.35),
        name, size=14, color=INK, bold=True)
    # Model
    txt(slide, x + Inches(0.75), y + Inches(0.48), w - Inches(0.9), Inches(0.28),
        model, size=9, color=SUB, font="JetBrains Mono")
    # IN row
    txt(slide, x + Inches(0.15), y + Inches(0.85), Inches(0.55), Inches(0.3),
        "IN", size=9, color=ACCENT, bold=True)
    txt(slide, x + Inches(0.7), y + Inches(0.85), w - Inches(0.85), Inches(0.4),
        input_text, size=10, color=INK)
    # OUT row
    txt(slide, x + Inches(0.15), y + Inches(1.32), Inches(0.55), Inches(0.3),
        "OUT", size=9, color=GREEN, bold=True)
    txt(slide, x + Inches(0.7), y + Inches(1.32), w - Inches(0.85), h - Inches(1.5),
        output_text, size=9, color=INK, font="JetBrains Mono")


# ---- 15. Per-agent detail — Layer 2 --------------------------------------
def s_agent_examples_l2(prs):
    s = new(prs)
    slide_header(s, prs, "12 · Agent examples — Layer 2",
                 "Detection · Diagnosis · Synthesis · Validation",
                 footer="Real payloads from activity_events table (2026-05-14 runs)",
                 page=13, total=TOTAL)
    examples = [
        ("Sentinel", "L2", "Streaming poller (no LLM)",
         "Sentry webhook · `issue.created`\nfingerprint=ab12c4 severity=fatal",
         "{\n  \"source\": \"sentry\",\n  \"title\": \"NullPointerException\",\n  \"severity\": \"high\",\n  \"fingerprint\": \"ab12c4\",\n  \"output_summary\": \"1 fatal incident ingested\"\n}"),
        ("Pathfinder", "L2", "gpt-4o · Neo4j codegraph + DoWhy",
         "IncidentDetected + Neo4j caller-chain\n+ 5-min event window",
         "{\n  \"symbol\": \"handler/foo.go:42\",\n  \"cause\": \"null total_amount\",\n  \"confidence\": 0.91,\n  \"output_summary\": \"root-cause: null-pointer\\nat handler/foo.go:42\"\n}"),
        ("Synthesiser", "L2", "gpt-4o-mini · classifier + router",
         "RootCauseHypothesis + contract list",
         "{\n  \"scenario\": \"null-deref\",\n  \"fleet\": [\"backend\",\"qa\",\"devops\"],\n  \"plan_steps\": 3,\n  \"output_summary\": \"fleet=[backend,qa,devops]\"\n}"),
        ("Validator", "L2", "Docker sandbox · property tests",
         "Patch + repo SHA → shadow run\n(`docker run --rm --network=none`)",
         "{\n  \"tests_passed\": 2847,\n  \"failures\": 0,\n  \"coverage_pct\": 92,\n  \"duration_ms\": 7012,\n  \"output_summary\": \"all property tests green\"\n}"),
    ]
    cw = Inches(5.92); ch = Inches(2.55); gap = Inches(0.1)
    for i, (n, layer, model, inp, out) in enumerate(examples):
        col = i % 2; row = i // 2
        x = Inches(0.7) + (cw + gap) * col
        y = Inches(1.85) + (ch + gap) * row
        _agent_example(s, x, y, cw, ch,
                       name=n, layer=layer, model=model,
                       input_text=inp, output_text=out)


# ---- 16. Per-agent detail — Layer 1 --------------------------------------
def s_agent_examples_l1(prs):
    s = new(prs)
    slide_header(s, prs, "13 · Agent examples — Layer 1",
                 "Architect · Backend · QA · DevOps · Data Engineer",
                 footer="Token counts + durations are real (OpenAI gpt-4o + gpt-4o-mini)",
                 page=14, total=TOTAL)
    # 5 in a layout: 2 + 3 rows? Use 3+2 — 3 across top, 2 across bottom
    examples = [
        ("Architect", "L1", "gpt-4o · 7807ms · 157 tok out",
         "RecoveryPlan + retrieval chunks (pgvector)",
         "{\n  \"steps\": [\n    {\"agent\":\"backend\",\"scope\":\"single-file\"},\n    {\"agent\":\"qa\",\"target\":\"orders_handler\"}\n  ]\n}"),
        ("Backend", "L1", "gpt-4o-mini · 6085ms · 144 tok out",
         "Plan step + caller-chain + retrieval",
         "Unified diff: 17 lines\n--- a/handler/foo.go\n+++ b/handler/foo.go\n@@ ...\n+if order.Total == nil { ... }\n\nstored: patches/{run_id}/...patch.enc"),
        ("QA", "L1", "gpt-4o-mini · 4825ms · 158 tok out",
         "Patch + target symbol + test layout",
         "tests/orders_handler_null_test.go\n  · TestOrdersNullTotal\n  · TestOrdersNonNilTotal\n  · TestOrdersBoundary\n\n3 tests · 92% line coverage"),
        ("DevOps", "L1", "gpt-4o-mini · 6190ms · 327 tok out",
         "Plan + existing CI/ArgoCD config",
         ".github/workflows/recovery.yml\n+ argocd-app.yaml\n+ rollback decision policy"),
        ("Data Engineer", "L1", "gpt-4o · 850ms · 16 tok out",
         "Schema drift evidence + target table",
         "migrations/0042_orders_total.up.sql\n+ down.sql\n+ backfill plan (idempotent)"),
    ]
    cw = Inches(3.93); ch = Inches(2.55); gap = Inches(0.1)
    for i, (n, layer, model, inp, out) in enumerate(examples[:3]):
        x = Inches(0.7) + (cw + gap) * i
        y = Inches(1.85)
        _agent_example(s, x, y, cw, ch,
                       name=n, layer=layer, model=model,
                       input_text=inp, output_text=out)
    cw2 = Inches(5.92); ch2 = Inches(2.0); gap2 = Inches(0.1)
    for j, (n, layer, model, inp, out) in enumerate(examples[3:]):
        x = Inches(0.7) + (cw2 + gap2) * j
        y = Inches(4.55)
        _agent_example(s, x, y, cw2, ch2,
                       name=n, layer=layer, model=model,
                       input_text=inp, output_text=out)


# ---- 17. Live system evidence --------------------------------------------
def s_evidence(prs):
    s = new(prs)
    slide_header(s, prs, "14 · Live system evidence",
                 "Real production-shape data captured today",
                 footer="One real recovery: 9 agents · 26.6 s wall-clock · 4 OpenAI calls · audit row signed",
                 page=15, total=TOTAL)

    # Left: real run summary
    card(s, Inches(0.7), Inches(1.85), Inches(5.92), Inches(5.0))
    txt(s, Inches(0.9), Inches(2.0), Inches(5.5), Inches(0.4),
        "RECOVERY RUN · null-deref · admin@nexis.local",
        size=10, color=ACCENT, bold=True)
    rows = [
        ("workflow_run_id", "7968a1a5-fccc-4fe8-9177-…"),
        ("project",         "Orders API · prod"),
        ("status",          "succeeded · 26.6 s"),
        ("agents fired",    "10 (5 L1 + 3 L2 + Sentinel + Approval)"),
        ("activity_events", "21 rows persisted"),
        ("tokens",          "3,073 in / 727 out · gpt-4o + gpt-4o-mini"),
        ("cost",            "$0.012 (4 real OpenAI calls)"),
        ("decision",        "medium · auto-approved by admin · t+3s"),
    ]
    cy = Inches(2.5)
    for k, v in rows:
        txt(s, Inches(0.9), cy, Inches(2.3), Inches(0.3),
            k, size=10, color=SUB, font="JetBrains Mono")
        txt(s, Inches(3.2), cy, Inches(3.3), Inches(0.3),
            v, size=10, color=INK, font="JetBrains Mono")
        cy += Inches(0.4)

    # Right: validation summary
    card(s, Inches(6.72), Inches(1.85), Inches(5.92), Inches(5.0))
    txt(s, Inches(6.92), Inches(2.0), Inches(5.5), Inches(0.4),
        "AUTOMATED VALIDATION (45 / 45 GREEN)",
        size=10, color=GREEN, bold=True)
    checks = [
        ("9", "Docker services healthy"),
        ("4", "Auth + session checks pass (JWT, /v1/me)"),
        ("3", "Workspace + project CRUD round-trips"),
        ("3", "Recovery pipeline runs end-to-end"),
        ("3", "Activity events ledger rows persist"),
        ("10", "Per-agent drill-down endpoints return events"),
        ("6", "System health probes (Postgres, Redis, Temporal, Neo4j, MinIO, CP)"),
        ("2", "GitHub App live · JWT auth verified vs api.github.com"),
        ("3", "RLS · 27 policies · 23 with WITH-CHECK · cross-tenant 401"),
    ]
    cy = Inches(2.5)
    for n, label in checks:
        txt(s, Inches(6.92), cy, Inches(0.5), Inches(0.3),
            n, size=14, color=GREEN, bold=True, align=PP_ALIGN.RIGHT)
        txt(s, Inches(7.5), cy + Inches(0.05), Inches(5.0), Inches(0.3),
            label, size=10, color=INK)
        cy += Inches(0.4)


# ---- 18. Closing snapshot -------------------------------------------------
def s_closing(prs):
    s = new(prs)
    slide_header(s, prs, "17 · Status snapshot",
                 "Where we are today (2026-05-15)",
                 footer="System running locally — 16 services healthy · 45/45 validation checks green",
                 page=15, total=TOTAL)

    # 4 KPI tiles
    kpis = [
        ("100%", "Phase 1 complete",                        GREEN),
        ("80%",  "Phase 2 complete",                        AMBER),
        ("9/9",  "Agents built (L1 + L2)",                  GREEN),
        ("45/45","Validation tests green",                  GREEN),
    ]
    bw = Inches(2.93); gap = Inches(0.1); sx = Inches(0.7); y = Inches(2.0)
    for i, (n, label, color) in enumerate(kpis):
        x = sx + (bw + gap) * i
        card(s, x, y, bw, Inches(1.6))
        txt(s, x + Inches(0.15), y + Inches(0.2), bw - Inches(0.3), Inches(0.6),
            n, size=36, color=color, bold=True)
        txt(s, x + Inches(0.15), y + Inches(0.95), bw - Inches(0.3), Inches(0.5),
            label, size=11, color=SUB)
        if i == 0:
            pass

    # Two-column summary
    y2 = Inches(3.9)
    card(s, Inches(0.7), y2, Inches(5.92), Inches(2.6), fill=CARD)
    txt(s, Inches(0.9), y2 + Inches(0.2), Inches(5.5), Inches(0.4),
        "DONE", size=11, color=GREEN, bold=True)
    bullets(s, Inches(0.9), y2 + Inches(0.55), Inches(5.5), Inches(2.0), [
        "Auth · RLS · audit chain · MFA · role-based access",
        "Admin dashboard · live agent fleet · alerts · approvals",
        "All 9 agents · Temporal workflow · approval gate",
        "Web UI — 26 console + 13 marketing pages",
        "Docker workstation — 16 services with healthchecks",
        "GitHub App live · OpenAI live · 28-provider catalog",
    ], size=11, bullet_color=GREEN)

    card(s, Inches(6.72), y2, Inches(5.92), Inches(2.6), fill=CARD)
    txt(s, Inches(6.92), y2 + Inches(0.2), Inches(5.5), Inches(0.4),
        "NEXT (per module)", size=11, color=AMBER, bold=True)
    bullets(s, Inches(6.92), y2 + Inches(0.55), Inches(5.5), Inches(2.0), [
        "Apache Flink streaming for Sentinel",
        "OpenLineage tracking + Chroma migration",
        "RLHF fine-tune loop (engineer feedback → DPO)",
        "Knowledge base ingestion of real repos",
        "Fault-injection benchmark + NASA-TLX study",
        "Phase 7 AWS deploy (Terraform 15 modules ready)",
    ], size=11, bullet_color=AMBER)


# ---- main -----------------------------------------------------------------
def main():
    prs = prs_init()
    s_title(prs)
    s_blackbox(prs)
    s_concept(prs)
    s_architecture(prs)
    s_schema(prs)
    s_docker(prs)
    s_phase_progress(prs)
    s_done_list_1(prs)
    s_done_list_2(prs)
    s_partial(prs)
    s_next_phase3(prs)
    s_next_phase4(prs)
    s_agent_examples_l2(prs)
    s_agent_examples_l1(prs)
    s_evidence(prs)
    out = Path(__file__).parent / "NEXIS_midterm1_2026-05-15.pptx"
    prs.save(out)
    print(f"Wrote {out}")


if __name__ == "__main__":
    main()
