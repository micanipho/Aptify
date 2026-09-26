# ADR-0007: Go backend, Next.js frontend, with Node as the fallback

**Status:** Accepted, conditional on the sandbox gate · 2026-09-26
**Supersedes:** PromptForge v0.2 §2 (TypeScript / Next.js end to end).

## Context

Aptify is a fresh codebase built to `docs/SPEC.md`, split into a backend (API + worker) and a
frontend. The backend's workload is I/O-bound: each run spends minutes waiting on model calls
and sandbox builds, and concurrency is a handful of runs. Raw performance does not decide the
language. What does:

1. a micro-VM sandbox that can be driven from it (ADR-0002)
2. structured model output against a schema (ADR-0004)
3. an exhaustive fold over a closed set of event types (ADR-0003)
4. a durable worker: leases, renewal, deadlines, recovery

PromptForge chose TypeScript because the generated apps share its toolchain. That does not
hold here: the backend never runs the generated toolchain (invariant #1), it only parses its
output and edits `package.json` as JSON.

## Decision

- **Backend: Go.** One binary, `cmd/aptify`, with `api`, `worker` and `eval` subcommands.
  Stdlib `net/http` for HTTP and SSE, `pgx` + `sqlc` for Postgres, official Anthropic, OpenAI
  and Google Go SDKs, S3-compatible object storage, `log/slog`.
- **Frontend: Next.js (App Router) + TypeScript**, a thin client. API and event types are
  generated from Go (`tygo`), with a CI check that fails on drift.
- **Database: Postgres.** Required by the event log, skip-locked claims and RLS.
- **Condition:** if no sandbox provider passes ADR-0002's requirements from Go without a
  TypeScript helper service, the backend is **Node (TypeScript)** instead, decided before
  any other backend code is written.

Rejected: **Rust** — its performance and memory model buy nothing on an I/O-bound service,
and it has no official model or sandbox SDKs; its enums would be the best fit for the event
fold, which does not outweigh that. **Next.js monolith** — ties the worker and 15-minute SSE
streams to serverless function limits.

## Consequences

- Go is strongest where PromptForge was weakest: `context.Context` deadlines, goroutine lease
  renewal and pgx make the durable worker straightforward.
- The sandbox adapter has no first-class SDK behind it and is the highest-risk code in the
  backend. It is proven first (build step 0).
- Go has no sum types. Events are a sealed interface checked by `gochecksumtype`, and decoding
  an unknown event tag is an error, never a skip.
- Schema generation and validation are separate code paths (no Zod); ADR-0004 adds a test to
  keep them agreeing.
- Two languages means generated contracts. SSE event payloads are exactly where OpenAPI-style
  descriptions are weakest, which is why types are generated from the Go structs directly.
- Getting to the first N=20 measurement is slower than it would be on Node. Accepted.
