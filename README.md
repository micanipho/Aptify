# Aptify

Aptify is a new implementation of the **PromptForge** design, written from scratch with a
**Go backend** and a **Next.js frontend**. It turns a natural-language prompt into a working
web app that has been shown to build. It then keeps that app building through many change
requests in a row.

> **Status: design only.** This repo holds no code yet. The design comes from the documents
> in [`docs/`](docs/). No code is carried over from the original PromptForge codebase; the
> paths the docs mention follow the Go layout below.

---

## The product in one paragraph

A run goes **prompt → specification → generated files → sandbox validation → repair loop →
downloadable zip**. Only one number matters: the **refinement success rate at N=20**. That is
twenty change requests in a row against one generated app, which must still build at the end.
The provisional target is ≥ 80%. First-shot quality is not the metric. When a design choice
trades first-shot polish against refinement reliability, refinement wins.

## Documents

| Document | What it covers |
| --- | --- |
| [`docs/SPEC.md`](docs/SPEC.md) | **The spec to build from.** Behaviour and contracts, independent of any stack: domain model, run state machine, engine, template merge, model gateway, sandbox gate, storage ports, data model, HTTP/SSE API, invariants, build plan and lessons inherited from PromptForge. |
| [`docs/SYSTEM-DESIGN.md`](docs/SYSTEM-DESIGN.md) | Decisions taken (v0.3), what changed from PromptForge, and what is still open |
| [`docs/adr/0001`](docs/adr/0001-narrow-the-scope.md) | One stack for generated apps (Next.js / TS / Postgres / GitHub / Vercel), competing on refinement |
| [`docs/adr/0002`](docs/adr/0002-execution-sandbox.md) | Validation runs in a micro-VM sandbox from day one, with no credentials and an egress allowlist. The provider is chosen by the sandbox gate |
| [`docs/adr/0003`](docs/adr/0003-durable-orchestration.md) | A pure event-sourced state machine with a driver that can be swapped out |
| [`docs/adr/0004`](docs/adr/0004-llm-gateway.md) | Every model call goes through one gateway: pricing, cost ceiling, prompt versions, structured output |
| [`docs/adr/0005`](docs/adr/0005-progress-streaming.md) | Progress over SSE, replayed from the event log (subscribe first, then replay) |
| [`docs/adr/0006`](docs/adr/0006-tenancy.md) | Explicit workspaces, `workspace_id` on every owned row from the first migration, Postgres RLS |
| [`docs/adr/0007`](docs/adr/0007-go-backend.md) | Go backend + Next.js frontend, with Node as the fallback if the sandbox gate fails |

## Architecture

```
 Frontend (prompt form, run console, download)
        │  HTTP JSON + Server-Sent Events
 Backend control plane
   API ─► Run engine ─► pure domain (event log → fold → policy)
            ├─► Model gateway ─► LLM providers
            ├─► Run / Project store ─► Postgres
            ├─► Artifact store ─► object storage (content-addressed)
            └─► Event bus (live fanout only, never authoritative)
        │  files in, report out, no credentials
 Execution plane: micro-VM sandbox (the ONLY place generated code runs)
```

Layering: **domain** (pure) ← **application** (engine + ports) ← **infrastructure** (one
adapter per port) ← **delivery** (HTTP, UI). Every port has an in-memory adapter, so the whole
system runs with no database, no model and no sandbox.

### Invariants (non-negotiable)

1. Generated code never runs outside the sandbox.
2. No credential ever reaches the sandbox.
3. Run status changes only by folding an event, and only in one place.
4. Domain and application layers never depend on infrastructure.
5. Every step is idempotent on `(runId, step, attempt)`.
6. Every budget (repairs, cost, wall clock) is checked **before** work is dispatched.
7. Unsupported targets fail at the request boundary.
8. Config is read and validated once, at startup.
9. The event log beats any projection. Events are never updated or deleted.
10. A failing refinement never replaces a working version.

---

## Stack

| Layer | Choice |
| --- | --- |
| Backend (API + worker) | **Go**: one binary with `api` and `worker` subcommands |
| Frontend | **Next.js** (App Router) + TypeScript: a thin client with a prompt form, an SSE console and a download link |
| Database | **Postgres**: the event log, `FOR UPDATE SKIP LOCKED` leases and RLS (ADR-0006) all rely on Postgres features |
| Object storage | S3-compatible (S3 / R2 / MinIO), content-addressed at `content/<sha256>` |
| Execution plane | Micro-VM sandbox. **The provider is not chosen yet**: see [the sandbox gate](#the-sandbox-gate-decide-this-first) |
| Generated apps | Next.js / TypeScript / Postgres (ADR-0001). This is a product decision, separate from Aptify's own stack |

### Why Go, and what it costs

**The case against, stated first.** The language does not decide the product metric. The
N=20 refinement rate depends on the prompts, the protected template merge, the failure parser
and the repair loop, and none of those depend on the language. The first real risk is
**measurement**: the docs record a free-tier limit of 20 requests per model per day, against a
sweep that needs 90 or more. Node/TypeScript would get to the first N=20 measurement fastest,
because every dependency has an official TS SDK. Go was chosen with that cost accepted.

PromptForge argued that the product and the generated app should share a toolchain. That
argument does not hold (see `SYSTEM-DESIGN.md` §2). Invariant #1 means the backend never runs the
generated toolchain. It only parses the text output of `tsc`, ESLint and npm, and edits
`package.json` as JSON, which Go does as well as anything.

**What Go is best at here:** the part the docs flag as the known weak point, a durable worker.
`context.Context` handles per-step timeouts and deadlines. Goroutines handle lease renewal
alongside long model and sandbox calls. pgx handles row locks and `SKIP LOCKED` claims. It all
ships as a single static binary running as a long-lived container.

#### Go vs Node vs Rust

| | **Go (chosen)** | Node (TS) | Rust |
| --- | --- | --- | --- |
| Micro-VM sandbox SDK | **Gap.** E2B: community SDKs only. Daytona: official Go SDK, but containers by default, so needs Kata micro-VMs. Vercel Sandbox: JS/Python only, REST from Go | Official from Vercel, E2B and Daytona | None official |
| Model SDKs | Official Anthropic, OpenAI and Google Go SDKs | Best available (AI SDK) | Community only |
| Structured output | Struct → JSON Schema, plus validation written by hand | Zod: schema and validation in one | serde + schemars |
| Event union + exhaustive fold | Sealed interface + `gochecksumtype` linter | Tagged unions, compile-time only | Enums + `match`: the best fit |
| Durable worker, leases, timeouts | **Best** | Workable, needs care on one thread | Strong, but async Rust is slow to write |
| Types shared with the frontend | Generated (`tygo`) + drift check | One shared package | Generated (`ts-rs`) |
| Solo development speed | Fast | Fastest | Slowest |

**Rust is ruled out.** Its speed and GC-free memory safety buy nothing when the backend spends
its time waiting on network calls. Its best feature for this project, enums for the event log,
does not make up for having no official model or sandbox SDKs. **Node is the fallback.** If
the sandbox gate below fails in Go, switch to Node before writing anything else.

### The sandbox gate: decide this first

ADR-0002 is the one requirement that must not be improvised, and it is Go's one real gap.
Before building anything else, spend a day proving **one** provider from Go:

- [ ] Create a **micro-VM** (Firecracker, Kata or similar), not a plain container
- [ ] Deny all outgoing traffic except the npm registry (and font hosts)
- [ ] Pass an empty environment, with no credentials
- [ ] Run `npm install` and `next build` on a known-good fixture, and read back exit codes, stdout and stderr
- [ ] Always destroy the sandbox (`defer`), with a hard timeout as a backstop
- [ ] Record elapsed sandbox time, for pricing

| Candidate | From Go | Risk |
| --- | --- | --- |
| E2B | Community SDK, or a thin client over its REST + envd APIs | Keeping an unofficial client maintained |
| Daytona | Official Go SDK | Isolation: containers by default, so Kata must be available and enforced |
| Vercel Sandbox | REST API | No Go SDK; manual auth and command streaming |

**If no candidate passes without a TypeScript helper service, use Node for the backend.** A
TS helper would bring back the two-toolchain problem the design rejects.

### Libraries

| Concern | Choice |
| --- | --- |
| HTTP routing | stdlib `net/http` (Go 1.22+ method + path patterns) |
| SSE | `net/http` + `http.Flusher`, 15 s heartbeat, `X-Accel-Buffering: no` |
| Postgres | `pgx/v5` + `sqlc` for typed queries; `goose` for migrations |
| Event log + claims | Hand-written, as specified in SPEC §9 (row lock on the run, gapless `seq`, `SKIP LOCKED` lease) |
| Models | `anthropic-sdk-go`, `openai-go` (also covers OpenAI-compatible gateways), `google.golang.org/genai` |
| JSON Schema for structured output | `invopop/jsonschema`. Constraints are **also** written into field descriptions (SPEC §7), and caps on nested arrays are checked after parsing, not sent in the schema |
| Object storage | `aws-sdk-go-v2` S3 client |
| Zip, hashing, logging | stdlib `archive/zip`, `crypto/sha256`, `log/slog` |
| Lint | `golangci-lint` with `exhaustive` and `gochecksumtype` enabled |
| Go → TS types | `tygo` into the frontend, with a CI check that fails on drift |

### Layout

```
cmd/aptify/               main: `aptify api`, `aptify worker`, `aptify eval`
internal/domain/run/      events, transition (fold), policy: pure, no I/O
internal/domain/...       ids, specification, template merge, path safety, errors
internal/app/             engine + port interfaces
internal/infra/memory/    in-memory adapter for every port
internal/infra/pg/        run, project and artifact stores (pgx + sqlc)
internal/infra/blob/      S3 content store
internal/infra/llm/       model gateway, pricing table, versioned prompts
internal/infra/sandbox/   sandbox adapter + failure parser
internal/http/            JSON API + SSE
internal/porttest/        one contract suite, run against every adapter
db/migrations/  db/queries/
web/                      Next.js frontend
```

### Go-specific rules

- **Events are a sealed interface** (`type Event interface{ isRunEvent() }`), with one struct
  per event type. `gochecksumtype` fails the build when a switch misses an event type.
  Decoding from the log switches on the `type` tag and **returns an error for unknown tags**.
  It never silently skips one.
- **IDs are distinct types** (`type RunID string`, `type ProjectID string`, …), so the compiler
  stops you passing one kind of ID where another is expected.
- **Money is integer cents**, never `float64`.
- **`context.Context` is for deadlines, not cancellation.** User cancellation stays cooperative
  (SPEC §12): it is recorded as an event and honoured between steps. It does not cancel
  the context of an in-flight model call or sandbox command.
- **Config** is read into one struct in `main`, validated at startup, and passed down.
  Nothing else calls `os.Getenv` (invariant #8).

### Deployment

The backend runs as a **long-lived container**, not as serverless functions. The worker's
poller (ADR-0003) and SSE streams lasting up to 15 minutes (ADR-0005) both need a process
that stays up. The frontend can deploy anywhere. Only the backend makes sandbox and model
calls.

---

## Build order

0. **The sandbox gate** (above). Choose Go or Node based on the result.
1. Domain: events, fold, policy, with the full control-flow test suite (no I/O)
2. Ports with in-memory adapters, plus the shared contract test suite
3. Model gateway with pricing, and a failed call still records its cost
4. Sandbox adapter and the gate order `install → build → typecheck → lint`, plus the failure parser
5. Base template and protected merge
6. HTTP API, SSE stream, single-page console, zip download
7. Postgres + object-storage adapters, then the `SKIP LOCKED` worker
8. **Run the N=20 measurement.** Everything after this (publish/deploy, history UI, auth + RLS,
   quotas) waits for that number.
