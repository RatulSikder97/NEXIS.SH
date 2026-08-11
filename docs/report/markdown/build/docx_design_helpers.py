from docx.oxml.ns import qn
from docx.oxml import OxmlElement

PPR_ORDER = ['pStyle', 'keepNext', 'keepLines', 'pageBreakBefore', 'framePr',
    'widowControl', 'numPr', 'suppressLineNumbers', 'pBdr', 'shd', 'tabs',
    'suppressAutoHyphens', 'kinsoku', 'wordWrap', 'overflowPunct',
    'topLinePunct', 'autoSpaceDE', 'autoSpaceDN', 'bidi', 'adjustRightInd',
    'snapToGrid', 'spacing', 'ind', 'contextualSpacing', 'mirrorIndents',
    'suppressOverlap', 'jc', 'textDirection', 'textAlignment',
    'textboxTightWrap', 'outlineLvl', 'divId', 'cnfStyle', 'rPr', 'sectPr',
    'pPrChange']


def insert_in_pPr_order(pPr, new_el, tag_localname):
    idx_new = PPR_ORDER.index(tag_localname)
    insert_before = None
    for child in pPr:
        local = child.tag.split('}')[-1]
        if local in PPR_ORDER and PPR_ORDER.index(local) > idx_new:
            insert_before = child
            break
    if insert_before is not None:
        insert_before.addprevious(new_el)
    else:
        pPr.append(new_el)


def set_pPr_border(pPr, side, sz=None, color=None, space=None, val='single'):
    pBdr = pPr.find(qn('w:pBdr'))
    if pBdr is None:
        pBdr = OxmlElement('w:pBdr')
        insert_in_pPr_order(pPr, pBdr, 'pBdr')
    side_el = pBdr.find(qn(f'w:{side}'))
    if side_el is None:
        side_el = OxmlElement(f'w:{side}')
        pBdr.append(side_el)
    side_el.set(qn('w:val'), val)
    if sz is not None:
        side_el.set(qn('w:sz'), str(sz))
    if space is not None:
        side_el.set(qn('w:space'), str(space))
    if color is not None:
        side_el.set(qn('w:color'), color)


def style_bottom_border(style, sz=18, color='2563EB', space=6):
    pPr = style.element.get_or_add_pPr()
    set_pPr_border(pPr, 'bottom', sz=sz, color=color, space=space)


def style_left_accent_bar(style, sz=24, color='2563EB', space=6, indent_twips=170):
    pPr = style.element.get_or_add_pPr()
    set_pPr_border(pPr, 'left', sz=sz, color=color, space=space)
    ind = pPr.find(qn('w:ind'))
    if ind is None:
        ind = OxmlElement('w:ind')
        insert_in_pPr_order(pPr, ind, 'ind')
    ind.set(qn('w:left'), str(indent_twips))


def shade_paragraph(paragraph, fill='F4F5F7'):
    pPr = paragraph._p.get_or_add_pPr()
    shd = pPr.find(qn('w:shd'))
    if shd is None:
        shd = OxmlElement('w:shd')
        insert_in_pPr_order(pPr, shd, 'shd')
    shd.set(qn('w:val'), 'clear')
    shd.set(qn('w:color'), 'auto')
    shd.set(qn('w:fill'), fill)


def style_pPr_shade(style, fill='F7F9FC'):
    pPr = style.element.get_or_add_pPr()
    shd = pPr.find(qn('w:shd'))
    if shd is None:
        shd = OxmlElement('w:shd')
        insert_in_pPr_order(pPr, shd, 'shd')
    shd.set(qn('w:val'), 'clear')
    shd.set(qn('w:color'), 'auto')
    shd.set(qn('w:fill'), fill)


def style_box_border(style, sz=4, color='D1D5DB', space=8):
    pPr = style.element.get_or_add_pPr()
    for side in ('top', 'left', 'bottom', 'right'):
        set_pPr_border(pPr, side, sz=sz, color=color, space=space)


def style_indent(style, left_twips=140, right_twips=140):
    pPr = style.element.get_or_add_pPr()
    ind = pPr.find(qn('w:ind'))
    if ind is None:
        ind = OxmlElement('w:ind')
        insert_in_pPr_order(pPr, ind, 'ind')
    ind.set(qn('w:left'), str(left_twips))
    ind.set(qn('w:right'), str(right_twips))


def rStyle_shade(style, fill='F0F2F5'):
    """Character-style run background (works on inline runs, unlike paragraph shd)."""
    rPr = style.element.get_or_add_rPr()
    shd = rPr.find(qn('w:shd'))
    if shd is None:
        shd = OxmlElement('w:shd')
        rPr.append(shd)
    shd.set(qn('w:val'), 'clear')
    shd.set(qn('w:color'), 'auto')
    shd.set(qn('w:fill'), fill)
