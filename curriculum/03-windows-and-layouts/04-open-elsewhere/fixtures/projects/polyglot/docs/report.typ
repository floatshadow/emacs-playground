#set page(paper: "a5", margin: 1.6cm)
#set text(font: "Libertinus Serif", 10.5pt)

= Polyglot: a tiny service, measured

We greet in Rust, score in Python and
prove termination in OCaml.

== Latency

#table(
  columns: 3,
  [lang], [p50], [p99],
  [Rust], [0.4 ms], [1.1 ms],
  [Python], [3.2 ms], [9.8 ms],
)

$ E = sum_(i=1)^n x_i / n $
