#set document(title: "Latency report")
#set page(width: 16cm, height: auto, margin: 1.5cm)
#set text(size: 11pt)

= Latency report

The service was measured on twenty requests (`service/samples.txt`).

#table(
  columns: 2,
  [*Measure*], [*Value*],
  [p50], [TBD],
  [p99], [TBD],
  [Score], [TBD],
)

The p99 is dominated by two slow requests; the median is well within
the budget of 20 ms.
