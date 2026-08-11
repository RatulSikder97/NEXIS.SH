import re, sys, json
from pypdf import PdfReader

pdf_path = sys.argv[1]
reader = PdfReader(pdf_path)
pages_text = [(p.extract_text() or '') for p in reader.pages]

CHAPTER_FILES = [
    'ch1_introduction.md', 'ch2_literature.md', 'ch3_analysis.md',
    'ch4_design.md', 'ch5_implementation.md', 'ch6_testing.md',
    'ch7_conclusion.md', 'references.md',
]

headings = []
for fname in CHAPTER_FILES:
    path = f'markdown/condensed/{fname}'
    in_fence = False
    for line in open(path, encoding='utf-8'):
        line = line.rstrip('\n')
        if line.startswith('```'):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        m1 = re.match(r'^# (.+)', line)
        m2 = re.match(r'^## (.+)', line)
        if m1:
            headings.append({'level': 1, 'text': m1.group(1).strip()})
        elif m2:
            headings.append({'level': 2, 'text': m2.group(1).strip()})


def normalize(s):
    s = s.replace('—', '-').replace('–', '-')
    s = re.sub(r'\s+', ' ', s)
    return s.strip()


def find_page(text, start_page):
    needle_full = normalize(text)
    needle_short = normalize(text[:22])
    for i in range(start_page, len(pages_text)):
        hay = normalize(pages_text[i])
        if needle_full in hay or needle_short in hay:
            return i + 1
    return None


results = []
cursor = 0
for h in headings:
    pg = find_page(h['text'], cursor)
    if pg is not None:
        cursor = pg - 1
    results.append({'level': h['level'], 'text': h['text'], 'page': pg})

print(json.dumps(results, indent=2))
