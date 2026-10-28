#!/bin/sh
# Builds _site/ from pages/*.md with pandoc, one HTML page per Markdown
# page, with a navigation bar in file-name order. A leading number and
# dash in a file name orders the bar and is left out of the page's URL
# (pages/02-install.md becomes _site/install.html).
# Links between pages point at the Markdown file (install.md); a link to
# a page that does not exist is reported as FILE:LINE: and stops the
# build, so M-g M-n jumps to it from *compilation*.
set -u
names=$(for f in pages/*.md; do basename "$f" .md | sed 's/^[0-9]*-//'; done)
broken=0
for f in pages/*.md; do
  for t in $(grep -o '](\([A-Za-z0-9_-]*\)\.md)' "$f" | sed 's/^](//; s/\.md)$//' | sort -u); do
    if ! printf '%s\n' "$names" | grep -qx "$t"; then
      grep -n "]($t\.md)" "$f" | sed "s|^\([0-9]*\):.*|$f:\1: broken link to $t.md|"
      broken=1
    fi
  done
done
if [ "$broken" -ne 0 ]; then
  echo "build failed: fix the broken links above"
  exit 1
fi
mkdir -p _site
nav=_site/.nav.html
printf '<nav>' > "$nav"
for f in pages/*.md; do
  name=$(basename "$f" .md | sed 's/^[0-9]*-//')
  title=$(sed -n 's/^# //p' "$f" | head -n 1)
  printf '<a href="%s.html">%s</a> ' "$name" "$title" >> "$nav"
done
printf '</nav>\n' >> "$nav"
for f in pages/*.md; do
  name=$(basename "$f" .md | sed 's/^[0-9]*-//')
  title=$(sed -n 's/^# //p' "$f" | head -n 1)
  sed 's/\.md)/.html)/g' "$f" |
    pandoc --from markdown --to html5 --standalone \
      --metadata pagetitle="$title — tally" \
      --include-before-body "$nav" --output "_site/$name.html" || exit 1
  echo "built _site/$name.html"
done
echo "site built: $(ls _site/*.html | wc -l) pages"
