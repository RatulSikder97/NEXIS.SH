import docx
from docx import Document
from docx.shared import Pt, Inches, RGBColor
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.oxml.ns import qn
from docx.oxml import OxmlElement
import sys, os
sys.path.insert(0, os.path.dirname(__file__))
from docx_design_helpers import (
    style_bottom_border, style_left_accent_bar, set_pPr_border,
    rStyle_shade, style_pPr_shade, style_box_border, style_indent,
    force_doc_default_font,
)

# Monochrome document chrome — only images/diagrams carry color.
INK = RGBColor(0x1a, 0x1a, 0x1a)
INK_HEX = "1A1A1A"
GRAY_LINE = "999999"
GRAY_FILL = "F2F2F2"

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

force_doc_default_font(doc, name=FONT)

# Normal / body text — 12pt Times New Roman, 1.5 line spacing, 8pt after-paragraph spacing
normal = doc.styles['Normal']
set_font(normal, size=12, color=INK)
pf = normal.paragraph_format
pf.line_spacing = 1.5
pf.space_after = Pt(8)
pf.space_before = Pt(0)

# Heading 1 = Chapter title — large bold, page-break-before
h1 = doc.styles['Heading1']
set_font(h1, size=20, bold=True, color=INK)
h1.paragraph_format.space_before = Pt(0)
h1.paragraph_format.space_after = Pt(20)
h1.paragraph_format.page_break_before = True
h1.paragraph_format.keep_with_next = True
style_bottom_border(h1, sz=14, color=INK_HEX, space=8)

# Heading 2 = Section — 16pt bold
h2 = doc.styles['Heading2']
set_font(h2, size=16, bold=True, color=INK)
h2.paragraph_format.space_before = Pt(20)
h2.paragraph_format.space_after = Pt(10)
h2.paragraph_format.page_break_before = False
h2.paragraph_format.keep_with_next = True

# Heading 3 = Subsection — 13pt bold
h3 = doc.styles['Heading3']
set_font(h3, size=13, bold=True, color=INK)
h3.paragraph_format.space_before = Pt(14)
h3.paragraph_format.space_after = Pt(8)
h3.paragraph_format.keep_with_next = True

# Heading 4 — 12pt bold (for any deeper nesting)
h4 = doc.styles['Heading4']
set_font(h4, size=12, bold=True, color=INK)
h4.paragraph_format.space_before = Pt(12)
h4.paragraph_format.space_after = Pt(6)

# Title style (unused directly but keep consistent)
title = doc.styles['Title']
set_font(title, size=26, bold=True, color=INK)
title.paragraph_format.alignment = WD_ALIGN_PARAGRAPH.CENTER

# TOC heading — match chapter heading weight
toch = doc.styles['TOCHeading']
set_font(toch, size=20, bold=True, color=INK)
toch.paragraph_format.page_break_before = True
toch.paragraph_format.space_after = Pt(20)

# Caption style — bold label, small font, matches \captionsetup{labelfont=bf,font=small}
cap = doc.styles['Caption']
set_font(cap, size=10, color=INK)
cap.paragraph_format.space_before = Pt(4)
cap.paragraph_format.space_after = Pt(14)

# Inline code + fenced-code-block runs both use the "Verbatim Char" character
# style (pandoc emits w:rStyle="VerbatimChar" on every code run) — this is the
# style that actually governs rendering, not the "Source Code" paragraph style.
vchar = doc.styles['Verbatim Char']
set_font(vchar, name='Consolas', size=9.5, color=INK)
rStyle_shade(vchar, fill=GRAY_FILL)

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
style_pPr_shade(sc, fill=GRAY_FILL)
style_box_border(sc, sz=4, color=GRAY_LINE, space=8)
style_indent(sc, left_twips=160, right_twips=160)

# Table styles
if 'Table' in [s.name for s in doc.styles]:
    tbl = doc.styles['Table']
    set_font(tbl, size=10.5, color=INK)

# Hyperlinks: black + underline, not blue — monochrome document.
if 'Hyperlink' in [s.name for s in doc.styles]:
    hl = doc.styles['Hyperlink']
    hl.font.underline = True
    hl.font.color.rgb = INK

# ---- Page setup: 1-inch margins, Letter (matches LaTeX geometry margin=1in) ----
for section in doc.sections:
    section.top_margin = Inches(1)
    section.bottom_margin = Inches(1)
    section.left_margin = Inches(1)
    section.right_margin = Inches(1)
    section.different_first_page_header_footer = True

# No decorative running header — plain academic report, header left blank.
# ---- Footer: plain centered page number, suppressed on the title page ----
section = doc.sections[0]

for footer_obj, is_first in ((section.first_page_footer, True), (section.footer, False)):
    footer_obj.is_linked_to_previous = False
    if is_first:
        continue
    p = footer_obj.paragraphs[0] if footer_obj.paragraphs else footer_obj.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
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

    run = p.add_run()
    run.font.name = FONT
    run.font.size = Pt(10)
    run.font.color.rgb = INK
    add_field(p, "PAGE")

doc.save(OUT)
print("wrote", OUT)
