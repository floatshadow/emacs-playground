#set page(paper: "a6", margin: 1.2cm)
#set text(size: 10pt)

= Polyglot latency

#table(
  columns: 3,
  [lang], [p50], [p99],
  [Rust], [0.4 ms], [1.1 ms],
  [Python], [3.2 ms], [9.8 ms],
  [OCaml], [0.9 ms], [2.7 ms],
)
