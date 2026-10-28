# paper-lab

The brief: a short paper on how Lantern's build time grows with the
number of notes, written in Typst, with an appendix in LaTeX and both
PDFs released under a git tag.

- `paper.typ`: the paper (you write it). Typst writes `paper.pdf`.
- `appendix.tex`: the appendix, every measurement. latexmk writes
  `appendix.pdf`.
- `refs.bib`: the references both of them cite.
- `figures/`: three drafts from the designer. One of them is the bar
  chart of build times; the file names do not say which.

1. A skeleton of `paper.typ`, with its PDF following every save.
2. The bar chart as Figure 1, picked from the thumbnails.
3. Two citations and the bibliography.
4. A Model section with an equation in the appendix.
5. A reviewer writes: "page 2 of the appendix misspells
   measurements". Find it in the PDF and fix it.
6. Commit everything and tag the release `v1.0`.
