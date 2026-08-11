import sys, os
from docx import Document
from docx.enum.text import WD_ALIGN_PARAGRAPH
from docx.shared import Pt, RGBColor
sys.path.insert(0, os.path.dirname(__file__))
from docx_design_helpers import shade_paragraph

path = sys.argv[1]
doc = Document(path)

centered = 0
for p in doc.paragraphs:
    xml = p._p.xml
    is_image = '<w:drawing' in xml
    text = p.text.strip()
    is_caption = text.startswith('Figure ') and ':' in text[:40]
    if is_image or is_caption:
        p.alignment = WD_ALIGN_PARAGRAPH.CENTER
        centered += 1

MUTED_INK = RGBColor(0x1f, 0x29, 0x37)
tables_styled = 0
for table in doc.tables:
    if not table.rows:
        continue
    header_row = table.rows[0]
    for cell in header_row.cells:
        for para in cell.paragraphs:
            for run in para.runs:
                run.font.bold = True
                run.font.color.rgb = MUTED_INK
            shade_paragraph(para, fill='E8EDFB')
    tables_styled += 1

doc.save(path)
print(f"{path}: centered {centered} paragraphs, styled {tables_styled} table headers")
