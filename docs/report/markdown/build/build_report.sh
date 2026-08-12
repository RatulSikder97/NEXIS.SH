#!/bin/bash
# Usage: build_report.sh <frontmatter.md> <chapters_dir> <reference.docx> <pagetitle> <output_basename>
set -e
cd /Users/ratulsikder/PROJECTS/NEXIS_ECO/web_app/docs/report

FRONTMATTER="$1"
CHDIR="$2"
REFDOC="$3"
PAGETITLE="$4"
OUTBASE="$5"

CH="$CHDIR/ch1_introduction.md $CHDIR/ch2_literature.md $CHDIR/ch3_analysis.md $CHDIR/ch4_design.md $CHDIR/ch5_implementation.md $CHDIR/ch6_testing.md $CHDIR/ch7_conclusion.md $CHDIR/references.md"

echo "=== [$OUTBASE] PASS 1: build without TOC to find real page numbers ==="
pandoc "$FRONTMATTER" $CH \
  -f markdown+raw_attribute+pipe_tables+grid_tables \
  --reference-doc="$REFDOC" \
  --resource-path="$CHDIR" \
  --metadata title="" \
  -M pagetitle="$PAGETITLE" \
  -o markdown/build/pass1.docx
python3 markdown/build/center_figures.py markdown/build/pass1.docx > /dev/null

rm -rf markdown/build/pass1_pdf
mkdir -p markdown/build/pass1_pdf
soffice --headless --convert-to pdf --outdir markdown/build/pass1_pdf markdown/build/pass1.docx > /dev/null 2>&1

python3 markdown/build/find_chapter_pages.py markdown/build/pass1_pdf/pass1.pdf "$CHDIR" > markdown/build/chapter_pages.json
echo "pass1 chapter pages:"
python3 -c "
import json
for h in json.load(open('markdown/build/chapter_pages.json')):
    if h['level']==1: print(' ', h['text'], '->', h['page'])
"

OFFSET=1
for ATTEMPT in 1 2 3; do
  echo ""
  echo "=== [$OUTBASE] attempt $ATTEMPT: generating TOC with offset=$OFFSET ==="
  python3 markdown/build/gen_static_toc.py $OFFSET

  pandoc "$FRONTMATTER" markdown/00_toc_generated.md $CH \
    -f markdown+raw_attribute+pipe_tables+grid_tables \
    --reference-doc="$REFDOC" \
    --resource-path="$CHDIR" \
    --metadata title="" \
    -M pagetitle="$PAGETITLE" \
    -o markdown/build/candidate.docx
  python3 markdown/build/center_figures.py markdown/build/candidate.docx > /dev/null

  rm -rf markdown/build/candidate_pdf
  mkdir -p markdown/build/candidate_pdf
  soffice --headless --convert-to pdf --outdir markdown/build/candidate_pdf markdown/build/candidate.docx > /dev/null 2>&1

  python3 markdown/build/find_chapter_pages.py markdown/build/candidate_pdf/candidate.pdf "$CHDIR" > markdown/build/chapter_pages_actual.json

  TRUE_OFFSET=$(python3 -c "
import json
guess = json.load(open('markdown/build/chapter_pages.json'))
actual = json.load(open('markdown/build/chapter_pages_actual.json'))
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

mv markdown/build/candidate.docx "${OUTBASE}.docx"
python3 markdown/build/inject_updatefields.py "${OUTBASE}.docx"

rm -rf markdown/build/final_pdf
mkdir -p markdown/build/final_pdf
soffice --headless --convert-to pdf --outdir markdown/build/final_pdf "${OUTBASE}.docx" > /dev/null 2>&1
mv "markdown/build/final_pdf/$(basename ${OUTBASE}).pdf" "${OUTBASE}.pdf"

echo ""
echo "=== [$OUTBASE] FINAL PAGE COUNT ==="
python3 -c "
from pypdf import PdfReader
n = len(PdfReader('${OUTBASE}.pdf').pages)
print('PAGES:', n)
"
rm -rf markdown/build/pass1_pdf markdown/build/pass1.docx markdown/build/candidate_pdf markdown/build/final_pdf
