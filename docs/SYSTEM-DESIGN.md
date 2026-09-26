# Aptify — System Design: decisions taken

**Version:** 0.3
**Date:** 2026-09-26
**Supersedes:** PromptForge System Design v0.2 (2026-09-05), which chose a TypeScript /
Next.js monolith and Vercel Sandbox.
**Companions:** `docs/SPEC.md` for behaviour and contracts, `docs/adr/` for individual
decisions, `README.md` for the stack and build order.

Aptify is a fresh codebase built to the PromptForge design. This document records what was
decided, what changed on the way from PromptForge, and what is still open. Nothing here
describes code that exists yet; "is" means "is decided", not "is built".

---

## 1. The open questions, answered

| # | Question | Decision | Why |
|---|---|---|---|
| 1 | Commercial or portfolio? | **Commercial-shaped, sequenced so Iteration 1 stands alone as a portfolio piece.** | Sandboxing and durable orchestration are managed primitives or small amounts of code now. Building them properly costs days; building them cheaply and rewriting later costs more. |
| 2 | Is the refinement thesis accepted? | **Accepted.** Refinement success rate at N=20 is the product metric. Breadth of stack coverage is explicitly not. | Nobody has solved this. Everybody has solved first-shot generation. |
| 3 | Whose hosting accounts? | **The user's.** Generated apps deploy to the user's Vercel and push to the user's GitHub. | Keeps the compute bill, the abuse surface and the legal liability for generated code with the person who prompted for it. Costs a clumsier onboarding; worth it. |
| 4 | Acceptable refinement rate at N=20? | **≥ 80%, provisional.** Encoded as the eval harness's threshold (`aptify eval`); it exits non-zero below it. | A number that can be wrong beats no number. Revisit after the first real measurement, not before. |
| 5 | Self-hosted or managed sandbox? | **Managed, micro-VM class. Provider not yet chosen** — decided by the sandbox gate. Revisit self-hosting only if sandbox spend exceeds LLM spend. | See ADR-0002 and ADR-0007. |
| 6 | Which repo is the line of development? | **This one.** Greenfield; no code carried over from PromptForge. | Starting clean removes the question of what to reconcile instead of answering it. |
| 7 | Monolith or split? | **Split:** Go backend (API + worker), Next.js frontend. | The durable worker and long SSE streams need a long-lived process, not serverless functions. See ADR-0007. |

## 2. Stack decision

**Go backend, Next.js + TypeScript frontend, Postgres.** See ADR-0007 for the full trade-off
against Node and Rust.

PromptForge v0.2 argued for TypeScript end to end because "the thing the product emits, type-
checks, lints and runs in the sandbox are all the same toolchain." That argument does not
survive invariant #1: the backend never runs the generated toolchain. It parses the text
output of `tsc`, ESLint and npm, and edits `package.json` as JSON — neither needs the same
language. What TypeScript does buy is first-class sandbox SDKs, Zod-based structured output
and shared types with the frontend. Aptify gives those up knowingly, in exchange for Go's
strength on the part PromptForge left weakest: the durable worker.

The accepted cost is a slower path to the first N=20 measurement than Node would give. The
condition on the decision: **if no micro-VM sandbox can be driven from Go without a
TypeScript helper service, the backend is Node instead.**

## 3. Carried over from PromptForge

Each of these is a requirement on Aptify, with its reason.

### 3.1 The sandbox lands in Iteration 1

PromptForge's v0.1 plan validated with an in-process `tsc --noEmit` "temporarily". v0.2
rejected that, and Aptify keeps the rejection: **generated code never runs in our process,
and never will.** A "temporary" exception to that rule is how it becomes permanent. The only
change is that the provider is no longer assumed — it is chosen by the sandbox gate.

See ADR-0002.

### 3.2 The state machine is the deliverable; the driver is not

An append-only event log, a total transition function
(`internal/domain/run/transition.go`) and a policy that decides the next action from state
alone (`internal/domain/run/policy.go`). Neither touches I/O, so the entire control flow — the
repair loop, every budget guard, cancellation, resumption after a crash — must be testable in
about a second with no model, no network and no sandbox.

The driver stays thin and replaceable. See ADR-0003.

### 3.3 A refinement re-plans; it does not skip planning

The cheap implementation skips the plan step on a refinement and goes straight to a diff.
PromptForge built that, and its eval harness caught the consequence: by change request twenty
the specification describes a project that no longer exists. A refinement re-plans against
the previous specification — one extra model call — and the specification stays a true
description of the project for all twenty changes.

## 4. Departures from PromptForge

### 4.1 The worker is a separate process from the start

PromptForge's Iteration 1 driver was a detached task in the request process, so a redeploy
mid-run left a run resumable but unclaimed until a poller existed. Aptify does not build that
intermediate stage. Once Postgres lands, the API only creates runs, and `aptify worker` is the
only thing that drives them, claiming with `SELECT … FOR UPDATE SKIP LOCKED` and a renewed
lease. In memory mode (no shared store) API and worker share one process.

### 4.2 The sandbox provider is an open decision, not a given

PromptForge chose Vercel Sandbox partly because its SDK was in the product's own toolchain.
From Go, no candidate is equally easy: E2B has only community Go SDKs, Daytona has an official
Go SDK but defaults to containers, and Vercel Sandbox is REST-only from Go. The requirements
in ADR-0002 are unchanged; the provider is chosen by proving one against them.

### 4.3 Model access is per-provider, behind the same gateway

PromptForge routed through Vercel AI Gateway with the AI SDK. Aptify keeps the single
gateway port (ADR-0004) and the `provider/model` routing by prefix (SPEC §7), implemented with
the official Anthropic, OpenAI and Google Go SDKs. An aggregating gateway stays reachable
through its OpenAI-compatible API.

## 5. What is deliberately absent at first

- **Authentication.** A single constant user and workspace until Iteration 5. Every route
  carries an owner id from the start, so this is a substitution, not a refactor.
- **Publish and deploy.** The `publish` and `deploy` steps exist in the state machine and
  return an explicit "not implemented until Iteration 3" error — a declared gap, not a null
  path.
- **Workspaces, RLS, quotas, billing.** Iteration 5. `workspace_id` is on every owned row from
  the first migration (ADR-0006).
- **Provisioned services for local work.** Memory mode is the default, so the backend runs
  with `go run ./cmd/aptify` and no database, model or sandbox.

## 6. Cost control is designed into the domain

A runaway repair loop is the most likely way to lose real money. It is enforced in the domain,
not in a dashboard:

- Every model call returns priced usage; the engine records a `cost.recorded` event.
- `nextAction` checks the ceiling **before dispatching work**, not after.
- Unknown models are priced at the most expensive known rate. An unpriced model must never
  become an unbounded one.
- Repair attempts, cost and wall clock are all hard budgets, each with a test proving the run
  terminates when it is exhausted.
- Money is integer cents throughout; no floating-point in the ledger.

## 7. Still open

- **Sandbox provider** — the first decision to make (README, "The sandbox gate").
- **Measurement budget.** PromptForge stalled on a free-tier limit of 20 requests/model/day
  against a sweep needing ≥90. Decide the paid tier or provider before the eval harness
  exists, not after.
- **Retention thresholds** — the shape is in the schema (`artifact_blobs.ref_count`); the
  numbers are not chosen.
- **Prompt caching.** Refinement repeats context heavily and this is where the cost model
  improves most, but it should be measured before it is built.
- **Whether the plan step needs a cheaper model than the code step.** Assumed yes;
  unverified.
- **Ownership checks** before Iteration 5: none exist while there is one constant user. This
  stays listed here until authorisation lands, rather than being quietly assumed handled.
