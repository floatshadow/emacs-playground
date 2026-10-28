#!/bin/sh
# Sort this week's sample data into out/summary.txt, one value per line.
set -e
mkdir -p out
sort -n sample.txt > out/summary.txt
echo "Sorted sample written to out/summary.txt"
