#!/bin/bash
set -e
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/docs/report

CH="markdown/condensed/ch1_introduction.md markdown/condensed/ch2_literature.md markdown/condensed/ch3_analysis.md markdown/condensed/ch4_design.md markdown/condensed/ch5_implementation.md markdown/condensed/ch6_testing.md markdown/condensed/ch7_conclusion.md markdown/condensed/references.md"

echo "=== PASS 1: build without TOC to find real page numbers ==="
pandoc markdown/00_frontmatter_condensed.md $CH \
  -f markdown+raw_attribute+pipe_tables+grid_tables \
  --reference-doc=markdown/build/nexis-reference-compact.docx \
  --resource-path=markdown/condensed \
  --metadata title="" \
  -M pagetitle="Nexis: A Multi-Agent Autonomous Engineering Platform with Closed-Loop Fault Recovery (Condensed Edition)" \
  -o markdown/build/pass1.docx
python3 markdown/build/center_figures.py markdown/build/pass1.docx > /dev/null

rm -rf markdown/build/pass1_pdf
mkdir -p markdown/build/pass1_pdf
soffice --headless --convert-to pdf --outdir markdown/build/pass1_pdf markdown/build/pass1.docx > /dev/null 2>&1

python3 markdown/build/find_chapter_pages.py markdown/build/pass1_pdf/pass1.pdf > markdown/build/chapter_pages.json
echo "pass1 chapter pages:"
python3 -c "
import json
for h in json.load(open('markdown/build/chapter_pages.json')):
    if h['level']==1: print(' ', h['text'], '->', h['page'])
"

OFFSET=1
for ATTEMPT in 1 2 3; do
  echo ""
  echo "=== attempt $ATTEMPT: generating TOC with offset=$OFFSET ==="
  python3 markdown/build/gen_static_toc.py $OFFSET

  echo "=== building with TOC inserted ==="
  pandoc markdown/00_frontmatter_condensed.md markdown/00_toc_condensed.md $CH \
    -f markdown+raw_attribute+pipe_tables+grid_tables \
    --reference-doc=markdown/build/nexis-reference-compact.docx \
    --resource-path=markdown/condensed \
    --metadata title="" \
    -M pagetitle="Nexis: A Multi-Agent Autonomous Engineering Platform with Closed-Loop Fault Recovery (Condensed Edition)" \
    -o markdown/build/candidate.docx
  python3 markdown/build/center_figures.py markdown/build/candidate.docx > /dev/null

  rm -rf markdown/build/candidate_pdf
  mkdir -p markdown/build/candidate_pdf
  soffice --headless --convert-to pdf --outdir markdown/build/candidate_pdf markdown/build/candidate.docx > /dev/null 2>&1

  python3 markdown/build/find_chapter_pages.py markdown/build/candidate_pdf/candidate.pdf > markdown/build/chapter_pages_actual.json

  TRUE_OFFSET=$(python3 -c "
import json
guess = json.load(open('markdown/build/chapter_pages.json'))
actual = json.load(open('markdown/build/chapter_pages_actual.json'))
# first chapter-level heading with a known page in both
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

mv markdown/build/candidate.docx NEXIS_FYP_Report_Condensed.docx
python3 markdown/build/inject_updatefields.py NEXIS_FYP_Report_Condensed.docx

rm -rf markdown/build/final_pdf
mkdir -p markdown/build/final_pdf
soffice --headless --convert-to pdf --outdir markdown/build/final_pdf NEXIS_FYP_Report_Condensed.docx > /dev/null 2>&1
mv markdown/build/final_pdf/NEXIS_FYP_Report_Condensed.pdf NEXIS_FYP_Report_Condensed.pdf

echo ""
echo "=== FINAL PAGE COUNT ==="
python3 -c "
from pypdf import PdfReader
n = len(PdfReader('NEXIS_FYP_Report_Condensed.pdf').pages)
print('PAGES:', n)
print('STATUS:', 'OK <= 40' if n <= 40 else 'OVER BUDGET, need to trim')
"
rm -rf markdown/build/pass1_pdf markdown/build/pass1.docx markdown/build/candidate_pdf markdown/build/final_pdf
