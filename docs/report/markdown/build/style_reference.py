import docx
from docx import Document
from docx.shared import Pt, Inches, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml.ns import qn
from docx.oxml import OxmlElement
import sys, os
sys.path.insert(0, os.path.dirname(__file__))
from docx_design_helpers import style_bottom_border, style_left_accent_bar

BLACK = RGBColor(0x1f, 0x29, 0x37)  # matches the diagram ink color, not pure #000
ACCENT = "2563EB"
MUTED = RGBColor(0x6b, 0x72, 0x80)

SRC = "markdown/build/base-reference.docx"
OUT = "markdown/build/nexis-reference.docx"

doc = Document(SRC)

FONT = "Times New Roman"

def set_font(style, name=FONT, size=None, bold=None, color=None):
    style.font.name = name
    rpr = style.element.get_or_add_rPr()
    rFonts = rpr.find(qn('w:rFonts'))
    if rFonts is None:
        rFonts = OxmlElement('w:rFonts')
        rpr.append(rFonts)
    rFonts.set(qn('w:eastAsia'), name)
    rFonts.set(qn('w:ascii'), name)
    rFonts.set(qn('w:hAnsi'), name)
    rFonts.set(qn('w:cs'), name)
    if size is not None:
        style.font.size = Pt(size)
    if bold is not None:
        style.font.bold = bold
    if color is not None:
        style.font.color.rgb = color

# Normal / body text — 12pt Times New Roman, 1.5 line spacing, 8pt after-paragraph spacing
normal = doc.styles['Normal']
set_font(normal, size=12)
pf = normal.paragraph_format
pf.line_spacing = 1.5
pf.space_after = Pt(8)
pf.space_before = Pt(0)

# Heading 1 = Chapter title — large bold, page-break-before
h1 = doc.styles['Heading1']
set_font(h1, size=20, bold=True, color=BLACK)
h1.paragraph_format.space_before = Pt(0)
h1.paragraph_format.space_after = Pt(20)
h1.paragraph_format.page_break_before = True
h1.paragraph_format.keep_with_next = True
style_bottom_border(h1, sz=18, color=ACCENT, space=8)

# Heading 2 = Section — 16pt bold, accent-blue left bar
h2 = doc.styles['Heading2']
set_font(h2, size=16, bold=True, color=BLACK)
h2.paragraph_format.space_before = Pt(20)
h2.paragraph_format.space_after = Pt(10)
h2.paragraph_format.page_break_before = False
h2.paragraph_format.keep_with_next = True
style_left_accent_bar(h2, sz=24, color=ACCENT, space=8, indent_twips=170)

# Heading 3 = Subsection — 13pt bold
h3 = doc.styles['Heading3']
set_font(h3, size=13, bold=True, color=BLACK)
h3.paragraph_format.space_before = Pt(14)
h3.paragraph_format.space_after = Pt(8)
h3.paragraph_format.keep_with_next = True

# Heading 4 — 12pt bold italic (for any deeper nesting)
h4 = doc.styles['Heading4']
set_font(h4, size=12, bold=True, color=BLACK)
h4.paragraph_format.space_before = Pt(12)
h4.paragraph_format.space_after = Pt(6)

# Title style (unused directly but keep consistent)
title = doc.styles['Title']
set_font(title, size=26, bold=True, color=BLACK)
title.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.CENTER

# TOC heading — match chapter heading weight
toch = doc.styles['TOCHeading']
set_font(toch, size=20, bold=True, color=BLACK)
toch.paragraph_format.page_break_before = True
toch.paragraph_format.space_after = Pt(20)

# Caption style — bold label, small font, matches \captionsetup{labelfont=bf,font=small}
cap = doc.styles['Caption']
set_font(cap, size=10)
cap.paragraph_format.space_before = Pt(4)
cap.paragraph_format.space_after = Pt(14)

# Inline code + fenced-code-block runs both use the "Verbatim Char" character
# style (pandoc emits w:rStyle="VerbatimChar" on every code run) — this is the
# style that actually governs rendering, not the "Source Code" paragraph style.
from docx_design_helpers import rStyle_shade, style_pPr_shade, style_box_border, style_indent
vchar = doc.styles['Verbatim Char']
set_font(vchar, name='Consolas', size=9.5, color=BLACK)
rStyle_shade(vchar, fill='EEF1F6')

# "Source Code" (the fenced-code-block paragraph style) does NOT exist in the
# base pandoc reference.docx at all — pandoc silently synthesizes a bare
# default for it at assembly time if the reference doc doesn't define one,
# which means any customization here is a no-op unless the style is created
# up front so pandoc finds and reuses THIS definition instead.
from docx.enum.style import WD_STYLE_TYPE
try:
    sc = doc.styles['Source Code']
except KeyError:
    sc = doc.styles.add_style('Source Code', WD_STYLE_TYPE.PARAGRAPH)
    sc.base_style = doc.styles['Normal']
sc.paragraph_format.line_spacing = 1.0
sc.paragraph_format.space_before = Pt(8)
sc.paragraph_format.space_after = Pt(12)
style_pPr_shade(sc, fill='EEF1F6')
style_box_border(sc, sz=4, color='C7CEDB', space=8)
style_indent(sc, left_twips=160, right_twips=160)

# Table styles
if 'Table' in [s.name for s in doc.styles]:
    tbl = doc.styles['Table']
    set_font(tbl, size=10.5)

# Hyperlink color -> black (matches LaTeX linkcolor=black; urlcolor stays blue by Word default)
if 'Hyperlink' in [s.name for s in doc.styles]:
    hl = doc.styles['Hyperlink']
    hl.font.underline = False

# ---- Page setup: 1-inch margins, Letter (matches LaTeX geometry margin=1in) ----
for section in doc.sections:
    section.top_margin = Inches(1)
    section.bottom_margin = Inches(1)
    section.left_margin = Inches(1)
    section.right_margin = Inches(1)

# ---- Running header: small-caps muted title, thin bottom rule ----
section = doc.sections[0]
header = section.header
header.is_linked_to_previous = False
hp = header.paragraphs[0] if header.paragraphs else header.add_paragraph()
for run_el in list(hp.runs):
    run_el._element.getparent().remove(run_el._element)
hp.alignment = WD_ALIGN_PARAGRAPH.RIGHT
hrun = hp.add_run("NEXIS  —  MULTI-AGENT AUTONOMOUS ENGINEERING PLATFORM")
hrun.font.name = FONT
hrun.font.size = Pt(8.5)
hrun.font.color.rgb = MUTED
hp_el = hp._p
hpPr = hp_el.get_or_add_pPr()
from docx_design_helpers import set_pPr_border
set_pPr_border(hpPr, 'bottom', sz=6, color='D1D5DB', space=6)

# ---- Footer: "Page X of Y" right-aligned, matches \fancyfoot[R]{Page \thepage\ of \pageref{LastPage}} ----
section = doc.sections[0]
footer = section.footer
footer.is_linked_to_previous = False
p = footer.paragraphs[0] if footer.paragraphs else footer.add_paragraph()
p.alignment = WD_ALIGN_PARAGRAPH.RIGHT
for run_el in list(p.runs):
    run_el._element.getparent().remove(run_el._element)

def add_field(paragraph, instr_text):
    run = paragraph.add_run()
    fld_begin = OxmlElement('w:fldChar')
    fld_begin.set(qn('w:fldCharType'), 'begin')
    instr = OxmlElement('w:instrText')
    instr.set(qn('xml:space'), 'preserve')
    instr.text = instr_text
    fld_sep = OxmlElement('w:fldChar')
    fld_sep.set(qn('w:fldCharType'), 'separate')
    fld_end = OxmlElement('w:fldChar')
    fld_end.set(qn('w:fldCharType'), 'end')
    r_el = run._element
    r_el.append(fld_begin)
    r2 = paragraph.add_run()._element
    r2.append(instr)
    r3 = paragraph.add_run()._element
    r3.append(fld_sep)
    r4 = paragraph.add_run()._element
    r4.append(fld_end)

run = p.add_run("Page ")
run.font.name = FONT
run.font.size = Pt(10)
add_field(p, "PAGE")
run = p.add_run(" of ")
run.font.name = FONT
run.font.size = Pt(10)
add_field(p, "NUMPAGES")

doc.save(OUT)
print("wrote", OUT)
