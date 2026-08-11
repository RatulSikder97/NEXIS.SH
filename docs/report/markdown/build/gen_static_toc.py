import json, sys, re

offset = int(sys.argv[1]) if len(sys.argv) > 1 else 1
data = json.load(open('markdown/build/chapter_pages.json'))

lines = []
lines.append('```{=openxml}')
lines.append('<w:p><w:pPr><w:pStyle w:val="TOCHeading"/></w:pPr><w:r><w:t>Table of Contents</w:t></w:r></w:p>')

def esc(s):
    return (s.replace('&', '&amp;').replace('<', '&lt;').replace('>', '&gt;')
             .replace('"', '&quot;'))


def toc_row(text, page, bold, indent_twips):
    page_str = str(page) if page is not None else '?'
    b_open = '<w:b/>' if bold else ''
    sz = '22' if bold else '20'
    return (
        f'<w:p><w:pPr><w:ind w:left="{indent_twips}"/>'
        f'<w:tabs><w:tab w:val="right" w:leader="dot" w:pos="9600"/></w:tabs>'
        f'<w:spacing w:after="{"70" if bold else "40"}"/></w:pPr>'
        f'<w:r><w:rPr>{b_open}<w:sz w:val="{sz}"/></w:rPr><w:t xml:space="preserve">{esc(text)}</w:t></w:r>'
        f'<w:r><w:rPr>{b_open}<w:sz w:val="{sz}"/></w:rPr><w:tab/><w:t>{page_str}</w:t></w:r></w:p>'
    )


for h in data:
    pg = h['page'] + offset if h['page'] is not None else None
    if h['level'] == 1:
        lines.append(toc_row(h['text'], pg, bold=True, indent_twips=0))
    else:
        lines.append(toc_row(h['text'], pg, bold=False, indent_twips=260))

lines.append('<w:p><w:r><w:br w:type="page"/></w:r></w:p>')
lines.append('```')
lines.append('')

open('markdown/00_toc_condensed.md', 'w').write('\n'.join(lines))
print(f'Wrote TOC with offset={offset}, {len(data)} entries')
