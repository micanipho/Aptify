# ADR-0003: A pure state machine with a replaceable driver

**Status:** Accepted · 2026-09-05 · Revised 2026-09-26 (Go paths; worker process from the start)

## Context

A run is minutes long, calls several services that rate-limit and fail, must survive a
redeploy of our own backend, and must be cancellable. An earlier implementation used a status
string plus a JSON blob of completed steps, advanced by service methods — a workflow engine,
hand-rolled, without durability, retries, timeouts or idempotency.

Starting on a workflow engine (Temporal or similar) is rejected: learning one while the domain
is still undefined is how that earlier attempt died.

## Decision

Split the problem in two, and make only the first half load-bearing.

**The state machine** (`internal/domain/run/`) is pure — no I/O, no clock, no context:

- `events.go` — the append-only log; the only source of truth. Events are a sealed interface
  with one struct per type; `gochecksumtype` fails the build on a non-exhaustive switch
- `transition.go` — a total fold; the only place `status` changes; returns an error on an
  illegal edge rather than ignoring it
- `policy.go` — `NextAction(state, now)`, which decides the next step from state alone

**The driver** (`internal/app`, the engine) does I/O and holds no branching logic of its own:
ask the policy, perform the action, append the resulting events, ask again.

Non-negotiables, each with a test:

- every activity is idempotent on `(runId, step, attempt)`
- an interrupted step resumes at the *same* attempt, never a new one — incrementing would
  create a second GitHub repository in Iteration 3
- every budget (repair attempts, cost, wall clock) is checked before dispatch
- cancellation is cooperative, checked between steps; it is an event, not a cancelled
  `context.Context`
- a terminal run accepts no further events except log lines, so a late webhook cannot revive it
- a fresh engine over the same log finishes a half-done run without re-executing completed
  steps

## Consequences

- The entire control flow is testable in about a second with no model, no network and no
  sandbox. A control-flow bug that could only be reproduced by spending money would never get
  fixed.
- Driver stages: in-process loop in memory mode → `aptify worker` claiming runs from Postgres
  with `SELECT … FOR UPDATE SKIP LOCKED` and a renewed lease → a workflow engine, only if retry
  and timeout handling ever justifies it. Each stage changes the driver and no domain tests.
- PromptForge's intermediate stage — a detached task in the request process, which a redeploy
  left unclaimed — is skipped. Once Postgres exists, the API only creates runs.
- `context.Context` carries deadlines (per-step and wall-clock), never user cancellation, so an
  in-flight model call or sandbox command is not interrupted mid-spend (SPEC §12).
