#!/bin/sh
# Describe each picture, slowly, as a long-running job would.
for f in *.png; do
    sleep 1
    echo "$f: $(wc -c < "$f") bytes"
done
echo "report done"
