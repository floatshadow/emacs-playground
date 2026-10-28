#set page(paper: "a5", margin: 1.6cm)
#set text(font: "Libertinus Serif", size: 10.5pt)
#set heading(numbering: "1.")

= Lantern: a build report

Lantern turns a folder of Markdown notes into a website. This report
measures how long a build takes as the folder grows.

== Method

We built three folders of notes, five times each, and kept the median.

== Results

Builds grow roughly linearly with the number of notes.

== Discussion

The cost is dominated by Markdown parsing; writing files is cheap.
