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


## Phase 3.5 — Serial vs Concurrent Backup Throughput

**Question:** how much does the fan-out/fan-in pipeline speed up a backup?

**Setup:** 8 MiB of unique data, 4096-byte fixed chunks, against an in-memory
backend — deliberately isolating hashing (CPU work) from disk I/O so the number
reflects the concurrency we actually added, not disk variance.

**Machine:** Intel Core i7-13700H, 20 logical CPUs, Windows.

**Reproduce:**
go test -run=^$ -bench=. -benchmem ./internal/engine


**Result:**

| path                     | time / op | throughput | speedup |
|--------------------------|----------:|-----------:|--------:|
| serial                   |  14.36 ms |  584 MB/s  |  1.00×  |
| concurrent (20 workers)  |   9.38 ms |  894 MB/s  |  1.53×  |

**Takeaway:** concurrent hashing gives a real 1.53× speedup. It isn't ~N× (N =
cores) because of Amdahl's law: the ordered collector and snapshot assembly are
serial, the store stage contends on a single lock in this in-memory backend, and
per-chunk channel/allocation overhead is significant for small 4 KiB chunks. The
parallel win is expected to grow once the backend is network-bound (S3 / Azure),
where each store is slow and independent.