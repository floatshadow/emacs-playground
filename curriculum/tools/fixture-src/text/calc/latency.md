# Request latency

One request, stage by stage:

| stage   | time    |
|---------|---------|
| dns     | 1200 us |
| connect | 35 ms   |
| tls     | 0.09 s  |
| server  | 210 ms  |

Total:

What is left of a half-second budget:

0.5 s - 336.2 ms
