# modelvault — Measured Metrics

All numbers here are produced by tests in this repo (deterministic seeds, so
they reproduce exactly). Each section names the command that generates it.

## Phase 2.3 — Fixed vs Content-Defined Chunking

**Question:** after a small edit near the *start* of a file, how many chunks
must a second backup re-store?

**Setup:** a 1 MiB file; 200 bytes inserted at offset 1000; then re-chunked.
- `fixed-4096` — fixed-size chunks of 4096 bytes
- `cdc-avg4096` — content-defined, min 1024 / avg ~4096 / max 16384 bytes

**Reproduce:**

**Result:**

| strategy      | chunks (v2) | chunks re-stored | reuse  |
|---------------|------------:|-----------------:|-------:|
| fixed-4096    |         257 |              257 |   0.0% |
| cdc-avg4096   |         209 |                1 |  99.5% |

**Takeaway:** fixed-size chunking re-stores the *entire* file after a single
insertion near the front, because every chunk boundary shifts. Content-defined
chunking re-stores only the chunk that actually changed — a 257× reduction in
data written for this edit. This is why modelvault uses CDC for incremental
backups.