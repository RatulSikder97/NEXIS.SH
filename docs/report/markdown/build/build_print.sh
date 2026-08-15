#!/bin/bash
# Builds the print edition: docs/report/print/NEXIS_FYP_Report_P.docx (+ .pdf)
#
# Source: markdown/00_frontmatter_print.md + markdown/print/ch*.md
# Budget: the body (Chapter 1 -> References) must fit 50 pages. Cover,
#         declaration/signature page, abstract and table of contents are
#         front matter and do not count against that budget.
set -e
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/docs/report

OUT_DIR=print
OUT_DOCX=$OUT_DIR/NEXIS_FYP_Report_P.docx
OUT_PDF=$OUT_DIR/NEXIS_FYP_Report_P.pdf
mkdir -p $OUT_DIR

CH="markdown/print/ch1_introduction.md markdown/print/ch2_literature.md markdown/print/ch3_analysis.md markdown/print/ch4_design.md markdown/print/ch5_implementation.md markdown/print/ch6_testing.md markdown/print/ch7_conclusion.md markdown/print/references.md"

PANDOC_ARGS=(
  -f markdown+raw_attribute+pipe_tables+grid_tables
  --reference-doc=markdown/build/nexis-reference-compact.docx
  --resource-path=markdown/print
  --metadata title=""
  -M pagetitle="Nexis: A Multi-Agent Autonomous Engineering Platform with Closed-Loop Fault Recovery"
)

echo "=== PASS 1: build without TOC to find real page numbers ==="
pandoc markdown/00_frontmatter_print.md $CH "${PANDOC_ARGS[@]}" -o markdown/build/print_pass1.docx
python3 markdown/build/center_figures.py markdown/build/print_pass1.docx > /dev/null

rm -rf markdown/build/print_pass1_pdf
mkdir -p markdown/build/print_pass1_pdf
soffice --headless --convert-to pdf --outdir markdown/build/print_pass1_pdf markdown/build/print_pass1.docx > /dev/null 2>&1

python3 markdown/build/find_chapter_pages.py markdown/build/print_pass1_pdf/print_pass1.pdf markdown/print > markdown/build/chapter_pages_print.json
echo "pass1 chapter pages:"
python3 -c "
import json
for h in json.load(open('markdown/build/chapter_pages_print.json')):
    if h['level']==1: print(' ', h['text'], '->', h['page'])
"

OFFSET=1
for ATTEMPT in 1 2 3; do
  echo ""
  echo "=== attempt $ATTEMPT: generating TOC with offset=$OFFSET ==="
  python3 markdown/build/gen_toc_print.py $OFFSET

  echo "=== building with TOC inserted ==="
  pandoc markdown/00_frontmatter_print.md markdown/00_toc_print.md $CH "${PANDOC_ARGS[@]}" -o markdown/build/print_candidate.docx
  python3 markdown/build/center_figures.py markdown/build/print_candidate.docx > /dev/null

  rm -rf markdown/build/print_candidate_pdf
  mkdir -p markdown/build/print_candidate_pdf
  soffice --headless --convert-to pdf --outdir markdown/build/print_candidate_pdf markdown/build/print_candidate.docx > /dev/null 2>&1

  python3 markdown/build/find_chapter_pages.py markdown/build/print_candidate_pdf/print_candidate.pdf markdown/print > markdown/build/chapter_pages_print_actual.json

  TRUE_OFFSET=$(python3 -c "
import json
guess = json.load(open('markdown/build/chapter_pages_print.json'))
actual = json.load(open('markdown/build/chapter_pages_print_actual.json'))
for g, a in zip(guess, actual):
    if g['level']==1 and g['page'] is not None and a['page'] is not None:
        print(a['page'] - g['page'])
        break
else:
    print($OFFSET)
")

  echo "guessed offset=$OFFSET, true offset=$TRUE_OFFSET"
  if [ "$TRUE_OFFSET" == "$OFFSET" ]; then
    echo "CONVERGED"
    break
  fi
  OFFSET=$TRUE_OFFSET
done

mv markdown/build/print_candidate.docx $OUT_DOCX
python3 markdown/build/inject_updatefields.py $OUT_DOCX

rm -rf markdown/build/print_final_pdf
mkdir -p markdown/build/print_final_pdf
soffice --headless --convert-to pdf --outdir markdown/build/print_final_pdf $OUT_DOCX > /dev/null 2>&1
mv markdown/build/print_final_pdf/NEXIS_FYP_Report_P.pdf $OUT_PDF

echo ""
echo "=== PAGE BUDGET ==="
python3 -c "
import json
from pypdf import PdfReader
r = PdfReader('$OUT_PDF')
total = len(r.pages)
actual = json.load(open('markdown/build/chapter_pages_print_actual.json'))
first_body = next((h['page'] for h in actual if h['level']==1 and h['page']), None)
body = total - (first_body - 1) if first_body else total
print('TOTAL PAGES  :', total)
print('FRONT MATTER :', (first_body or 1) - 1, '(cover, declaration, abstract, contents)')
print('BODY PAGES   :', body, '(Chapter 1 -> References)')
print('STATUS       :', 'OK <= 50' if body <= 50 else 'OVER BUDGET, trim')
"
rm -rf markdown/build/print_pass1_pdf markdown/build/print_pass1.docx markdown/build/print_candidate_pdf markdown/build/print_final_pdf
