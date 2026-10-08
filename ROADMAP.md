# modelvault — Roadmap

Content-addressed, deduplicated backup & restore for ML model artifacts, in Go.

Progress is tracked per phase. A box is checked when that step lands on `main`;
the full per-step history is in the Git commit log (`Phase X.Y` messages).

## Phase 1 — Foundation & Local MVP
- [x] 1.1 Project scaffold: module, entrypoint, first GitHub push
- [x] 1.2 Core interfaces: Chunker, Backend, Snapshot
- [x] 1.3 Fixed-size chunker + unit tests
- [x] 1.4 Local-disk backend with atomic writes + tests
- [x] 1.5 Backup engine + `backup` CLI (content-addressed dedup)
- [x] 1.6 Restore with integrity verification + `list`/`restore` CLI

## Phase 2 — Content-Defined Chunking
- [ ] 2.1 CDC chunker (rolling hash / FastCDC-style)
- [ ] 2.2 Selectable chunker
- [ ] 2.3 Benchmark: fixed vs CDC dedup ratio

## Phase 3 — Concurrency
- [ ] 3.1 Concurrent hashing (fan-out / fan-in)
- [ ] 3.2 Bounded worker pool for stores (backpressure)
- [ ] 3.3 context.Context cancellation + errgroup
- [ ] 3.4 Parallel restore
- [ ] 3.5 Benchmark: serial vs pipelined

## Phase 4 — Cloud Backends
- [ ] 4.1 S3 backend (AWS SDK for Go v2)
- [ ] 4.2 Azure Blob backend
- [ ] 4.3 Backend selection via config/flags
- [ ] 4.4 Integration tests (MinIO / Azurite)

## Phase 5 — Service & Observability
- [ ] 5.1 REST API (and/or gRPC)
- [ ] 5.2 Prometheus metrics
- [ ] 5.3 Multi-stage Dockerfile
- [ ] 5.4 Kubernetes manifests
- [ ] 5.5 `verify` command (re-hash stored chunks)

## Phase 6 — Polish & Showcase
- [ ] 6.1 README + architecture diagram
- [ ] 6.2 Documented benchmarks (dedup ratio, throughput)
- [ ] 6.3 CI (GitHub Actions: build, test, vet, lint)
- [ ] 6.4 Demo script / sample run
- [ ] 6.5 Tag v1.0.0 release