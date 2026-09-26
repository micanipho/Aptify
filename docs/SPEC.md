# Aptify — Specification

The behaviour, contracts and invariants Aptify is built to. It is stack-neutral on purpose:
Aptify's own stack (Go backend, Next.js frontend — ADR-0007) lives in the ADRs and the
README, not here.

**Origin.** This began as a snapshot of PromptForge as built on 2026-09-21 (branch
`feat/02b-real-generation`). Aptify is a fresh codebase: no code is carried over, only this
design and the lessons in §18. Where this document says "must", it is a requirement on Aptify,
not a description of something that already exists.

Types are written in a neutral notation: `string`, `int`, `bool`, `timestamp` (epoch ms),
`T[]`, `T?` (optional), `A | B` (union), `map<K,V>`.

---

## 1. What the product is

A user writes a natural-language prompt. The system turns it into a **working, buildable web
application**, proven to build in an isolated sandbox, and hands it back as a download. Then
it keeps that application working across many successive **change requests** (refinements).

**The one metric:** refinement success rate at N=20 — twenty successive change requests
against a generated app, with the app still building at the end. First-shot quality is not the
metric. When a design choice trades first-shot polish for refinement reliability, take
refinement.

A run is: **prompt → specification → generated files → sandbox validation → (repair loop) →
downloadable archive.**

---

## 2. System split

```
 ┌───────────── Frontend ─────────────┐
 │ prompt form, run console, download │
 └──────────────┬─────────────────────┘
                │ HTTP JSON + Server-Sent Events
 ┌──────────────▼─────────────────────────────────────────────┐
 │ Backend — control plane                                    │
 │  API ─► Run engine (loop) ─► pure domain (state machine)   │
 │           │                                                │
 │           ├─► Model gateway ──► LLM provider(s)            │
 │           ├─► Run store (event log)       ┐                │
 │           ├─► Project store               ├─► relational DB│
 │           ├─► Artifact store ─► object storage (by hash)   │
 │           └─► Event bus (live fanout only)                 │
 └───────────┬────────────────────────────────────────────────┘
             │ files in, report out — no credentials
 ┌───────────▼───────────────┐
 │ Execution plane (sandbox) │  the ONLY place generated code runs
 └───────────────────────────┘
```

Layering rule, whatever the language: **domain** (pure, no I/O) ← **application** (engine +
port interfaces) ← **infrastructure** (one adapter per port) ← **delivery** (HTTP, UI).
Dependencies point inwards only. Every port has an in-memory implementation so the whole
system runs end to end with no database, no model and no sandbox.

---

## 3. Domain model

### Identifiers

All ids are opaque strings, typed distinctly so one cannot be passed as another:
`ProjectId, PromptRevisionId, SpecificationId, RunId, ArtifactSetId, UserId, WorkspaceId,
ContentHash`.

Generated ids are **sortable and URL-safe**: `<prefix>_<time base36, 9 chars><16 random hex>`
(prefixes: `proj`, `pr`, `spec`, `run`, `as`). The time prefix keeps database indexes
append-friendly. `ContentHash` is the hex SHA-256 of file content.

### Entities

```
Project            { id, ownerId: UserId, workspaceId: WorkspaceId, name, target: TargetStack, createdAt }
PromptRevision     { id, projectId, revision: int (1-based, unique per project), text, createdAt }   append-only
StoredSpecification{ id, projectId, promptRevisionId, spec: Specification, promptVersion: string, createdAt }
ArtifactSet        { id, projectId, runId, version: int, parentId?: ArtifactSetId, files: ArtifactFile[], createdAt }
ArtifactFile       { path, hash: ContentHash, bytes: int, mode: "text" | "binary" }
GeneratedFile      { path, content }        -- the in-flight form of a file
```

- A **refinement's change request becomes the next PromptRevision**, so there is one
  append-only history, not two.
- `ArtifactSet.version` is 1 for the first generation and +1 for every new set (each generate
  and each repair writes a new set). `(projectId, version)` is unique.
- Files are **content-addressed**: identical content across versions is stored once.

### Target stack (fail-fast enums)

Declared values are wider than implemented ones, on purpose. Any unimplemented value is
rejected **at the request boundary** with `unsupported_target` — never allowed to reach a run.

| Dimension | Declared | Implemented |
| --- | --- | --- |
| framework | nextjs, react-vite, vue, angular, blazor | nextjs |
| language | typescript, javascript, csharp | typescript |
| database | postgres, mongodb, sqlite | postgres |
| gitProvider | github, gitlab, bitbucket, azure-devops | github |
| deployTarget | vercel, netlify, railway, render, azure, none | vercel, none |

Default: `nextjs / typescript / postgres / github / none`. (This is the stack of the
*generated* apps — a product decision, independent of Aptify's own stack.)

### Path safety

Generated paths are untrusted. Reject any path that: starts with `/`, contains `\`, contains
a `..` segment, or contains a NUL byte. Check on write **and again** at archive time
(zip-slip).

### Errors

Every domain error has a stable `code`; the API maps code → HTTP status.

`unsupported_target, illegal_transition, budget_exhausted, validation_failed,
invalid_specification, run_cancelled, not_found, concurrent_run_write`.

A separate **model-call-failed** error carries the *usage already spent* by a failed model
call (see §7), so the engine can record the cost before failing the run.

---

## 4. The run state machine (the core asset)

A run is an **append-only event log**. Its state is never stored as truth — it is always
rebuilt by folding the log. Status changes happen in exactly one function (the fold).

### Statuses

`queued, planning, generating, validating, repairing, packaging, publishing, deploying,
succeeded, failed, cancelling, cancelled`. Terminal: `succeeded, failed, cancelled`.

### Steps

`plan, generate, validate, repair, package, publish, deploy`. Each step maps to the status it
puts the run in (plan→planning, …). `publish` and `deploy` are declared but not implemented.

Pipeline for both run kinds (`generation`, `refinement`): **plan → generate → validate →
package**. Refinement re-plans too (against the previous spec) so the spec never goes stale.

### Events

```
run.queued          { kind, specificationId? }
step.started        { step, attempt }
step.succeeded      { step, attempt, artifactSetId?, specificationId?, templateVersion? }
step.failed         { step, attempt, message, retryable }
validation.passed   {}
validation.failed   { failures: ValidationFailure[] }
cost.recorded       { cents, inputTokens, outputTokens, model, cacheReadTokens?, cacheWriteTokens?, sandboxMs?, failed? }
run.cancelRequested {}
run.cancelled       {}
run.failed          { reason }
run.succeeded       { artifactSetId }
log                 { level: debug|info|warn, message }     -- no state change

StoredRunEvent      { runId, seq: int (1-based, gapless per run), at: timestamp, event }
ValidationFailure   { gate: install|typecheck|lint|test|build, file?, line?, message }
```

Adding a field to an event is safe; changing an existing field's meaning is not — events are
persisted and replayed.

### Legal status edges

| From | May enter |
| --- | --- |
| queued | planning, generating, cancelling, failed |
| planning | generating, cancelling, failed |
| generating | validating, cancelling, failed |
| validating | repairing, packaging, publishing, cancelling, failed, succeeded |
| repairing | generating, validating, cancelling, failed |
| packaging | publishing, succeeded, cancelling, failed |
| publishing | deploying, succeeded, cancelling, failed |
| deploying | succeeded, cancelling, failed |
| cancelling | cancelled, failed |
| terminal | nothing |

Same-status is always allowed. Events that move status: `step.started` (→ step's status),
`run.cancelRequested` (→ cancelling), `run.cancelled`, `run.failed`, `run.succeeded`.
An illegal edge **throws** — never silently ignored. A terminal run accepts only `log`.

### State (the projection)

```
RunState {
  id, kind, status,
  specificationId?, artifactSetId?,
  attempts: map<Step, int>,        -- latest attempt number per step
  completed: Step[],
  repairAttempts: int,
  validationFailures: ValidationFailure[],
  budget: { maxRepairAttempts, maxCostCents, maxWallClockMs },
  cost: { cents, inputTokens, outputTokens, calls },
  cancelRequested: bool,
  failureReason?, startedAt, endedAt?,
  version: int                      -- +1 per non-log event
}
```

Default budget: **3 repairs, 200 cents, 15 minutes wall clock**.

### Fold rules (per event, beyond the status change)

- `run.queued`: set kind, specificationId.
- `step.started`: record attempt. If `repair`: `repairAttempts = attempt`, and remove
  `validate` and `package` from `completed`. If `validate`: clear `validationFailures`.
- `step.succeeded`: add step to `completed`; if `repair`, clear `validationFailures` (the
  repair consumed them — otherwise a second repair is scheduled for the same errors); copy
  `artifactSetId` / `specificationId` if present.
- `step.failed`: no state change.
- `validation.passed`: clear failures. `validation.failed`: set failures, remove `validate`
  from `completed`.
- `cost.recorded`: add cents and tokens, `calls += 1`.
- `run.cancelRequested`: `cancelRequested = true`. `run.failed`: set `failureReason`.
  `run.succeeded`: set `artifactSetId`. Terminal statuses set `endedAt`.
- `log`: does not advance `version`.

### Policy — "what next?" (pure function of state and now)

Evaluated in this order:

1. Terminal → **done**.
2. `cancelRequested` → **finish: cancelled**.
3. `cost.cents >= maxCostCents` → **finish: failed** "Cost ceiling reached (x/y cents)."
4. `now - startedAt >= maxWallClockMs` → **finish: failed** "Wall-clock budget exceeded."
5. Validation failures present → if `repairAttempts >= maxRepairAttempts` → **finish: failed**
   "Validation still failing after N repair attempts."; else **step: repair, attempt
   repairAttempts+1**.
6. Status is `repairing` (repair just succeeded) → **step: validate, attempt+1**.
7. First pipeline step not in `completed` → **step**. If the run is already sitting in that
   step's status (interrupted mid-flight), **resume the same attempt number**; otherwise
   attempt+1.
8. Nothing pending → **finish: succeeded** (in practice success is emitted by `package`).

Budgets are checked **before** dispatch, never after. The failure-reason strings are matched by
attribution (§11) — change them in both places.

---

## 5. The engine

The engine holds **no branching logic**. Loop: rebuild state from log → ask the policy → do
exactly that one thing → append resulting events → publish them to the bus → repeat. Drive to
completion with a loop fuse (40 iterations; a fuse, not a budget).

`advance(runId)`:
- `done` → return. `finish` → append `run.cancelled` or `run.failed{reason}`.
- `step` → append `step.started`, run the step, append its events.
- If the step throws: append (in this order) the failed call's `cost.recorded{failed:true}` if
  it was a model call that spent money, then `step.failed{retryable:false}`, then
  `run.failed{"<step> failed: <message>"}`. **Order matters**: after `run.failed` the run is
  terminal and would reject the cost event.

Steps:

| Step | Does | Emits |
| --- | --- | --- |
| plan | Latest prompt revision → model `plan` (refinement passes the latest spec as `basedOn`) → save specification | `cost.recorded`, `step.succeeded{specificationId}` |
| generate | Model `generate` with spec + template summary (+ existing files and change request on refinement) → **template merge** over (existing files or template files) → persist new artifact set | `cost.recorded`, optional `log{warn}` naming discarded template files, `step.succeeded{artifactSetId, templateVersion}` |
| validate | Materialise current set → sandbox with gates `install, build, typecheck, lint`, timeout `min(maxWallClock, 10 min)` | always `cost.recorded{model:"sandbox", sandboxMs}`; then `validation.failed{failures}` **or** `validation.passed` + `step.succeeded` |
| repair | Model `repair` with spec, current files, failures, attempt, template summary → template merge over current files → persist new set (parent = current) | `cost.recorded`, optional warn, `step.succeeded{artifactSetId, templateVersion}` |
| package | — | `step.succeeded{artifactSetId}`, `run.succeeded{artifactSetId}` |

Persisting files: check each path is safe, put content (by hash), build an `ArtifactSet` with
`version = parent.version + 1` and `parentId`, save it.

A refinement returns only changed files; merging over the full existing set means an omitted
file is kept, never silently deleted.

**Idempotency:** every step must be safe to re-run for the same `(runId, step, attempt)`. That
is what makes resumption after a crash correct.

---

## 6. The base template and its merge rule

Every generated project starts from a **fixed, versioned base template** (starting at
`template@1.0.0`): the toolchain config, the dependency manifest and its lockfile, the global
stylesheet. It must be derived from a fixture project proven to build in the real sandbox, and
a test must fail if template and fixture drift apart.

`applyTemplate(template, base, overlay) → { files, rejected[], addedDependencies[] }`:

- Paths the template does not own: later wins (overlay over base).
- Paths the template owns: always the template's content. An overlay that differs is listed in
  `rejected` and discarded.
- **Exception — the dependency manifest:** the overlay may *add* packages the template does not
  pin (either dependency section). It may never change or remove a pinned one, nor change any
  other manifest field; attempting to is a rejection. Additions already in `base` are carried
  forward. Sections are rendered sorted.
- `base` is the template for a first generation and the current project for refinement/repair,
  so every merge also returns a project to the current template.
- Output order: template files first (template order), then the rest in arrival order.

The model is told the template's version, owned paths and pinned dependency versions. In the
file context sent to the model, **template-owned files are omitted except the manifest** (the
lockfile alone is ~240 KB).

---

## 7. Model gateway

Every model call goes through **one** gateway. It exists so token accounting, the cost
ceiling, prompt versioning and caching are enforceable in one place.

```
plan(runId, prompt, basedOn?: Specification)                         → Result<Specification>
generate(runId, specification, template, existingFiles?, changeRequest?) → Result<GeneratedFile[]>
repair(runId, specification, files, failures, attempt, template)     → Result<GeneratedFile[]>

Result<T> { value: T, usage: Usage, promptVersion: string }
Usage     { model, inputTokens (total, incl. cache reads), outputTokens, cacheReadTokens, cacheWriteTokens, cents }
```

- **Structured output** for all three calls, validated against a schema. Output ceilings:
  plan 8k tokens, generate/repair 64k. Up to 2 internal retries.
- **Model routing** by id prefix: `anthropic/…` and `google/…` go direct to that provider,
  `gateway/…` through an aggregating gateway. Separate plan model and code model (code model
  serves both generate and repair). Refuse to start if the chosen route's key is missing.
- **Pricing:** per-model table of $/M tokens for input, output, cache read, cache write.
  `cents = ceil(((input − cacheRead)·in + cacheRead·cr + cacheWrite·cw + output·out) / 1e6 · 100)`,
  uncached input clamped at 0. An unknown model is priced at a deliberately high fallback
  ($5 in / $25 out) so it trips the ceiling early rather than running free.
- **A failed call is never free.** If a call throws after spending, estimate usage as
  `ceil((systemPrompt + userPrompt chars) / 4)` input tokens, zero output, and raise
  model-call-failed carrying it.
- **Prompts are versioned artifacts** (`plan@x.y.z`, `generate@x.y.z`, `repair@x.y.z`, each
  starting at `1.0.0`). Every run records the version. Any edit — even whitespace — bumps the version; a snapshot test
  enforces it.

### Specification schema (plan output)

```
Specification {
  name: string (1..80)
  summary: string (≤600)
  entities: Entity[] (≤15)
  pages: Page[] (1..25)
  auth: { enabled: bool, provider: "none"|"credentials"|"oauth-github" = "credentials", roles: string[] = [] }
  dependencies: string[] (≤15) = []
  assumptions: string[] (≤20) = []
}
Entity { name: PascalCase /^[A-Z][a-zA-Z0-9]*$/, description: string (≤300), fields: Field[] (1..40) }
Field  { name: camelCase /^[a-z][a-zA-Z0-9]*$/,
         type: string|text|integer|decimal|boolean|timestamp|uuid|json|enum,
         optional: bool = false, unique: bool = false, enumValues?: string[], references?: string }
Page   { route: /^\/[a-z0-9\-/[\]]*$/ (dynamic segments as [id]), title (≤120), purpose (≤300),
         entity?: string, kind: list|detail|form|dashboard|auth|static, authenticated: bool = true }
```

**Portable lessons about sending this schema to a model:**
- Providers strip JSON Schema keywords differently (one drops `pattern` and `maxLength`). Every
  constraint must **also be stated in the field's description**, or the model is never told the
  rule and the first enforcement is rejecting a paid response.
- One provider rejects the whole request (400, before billing) when **nested arrays carry
  `maxItems`** — it behaves like a schema-complexity budget. Keep the caps on `entities` and
  `fields` as post-parse validation, not in the transmitted schema; state them in descriptions
  and in the prompt.

### File-set schema (generate and repair output)

`{ files: { path: string (≥1), content: string }[] (1..200) }`

### Prompt contents (intent, not wording)

- **plan:** target stack is fixed; model only what was asked; `id` and `createdAt` are implicit
  on every entity; naming rules; prose length limits; entity/field caps; few pages that do real
  work; keep dependencies empty unless required; record every assumption (the field the user
  reads to catch a misunderstanding).
- **generate:** strict typing, no unused imports/vars (lint is an error); server-rendered by
  default, client-side only where needed; use the project's import alias; do not emit template
  files; emit the root layout; never import an unlisted package — to add one, emit a manifest
  containing only that dependency; safe relative paths. User prompt = spec + template summary
  (+ existing files + "return ONLY the files this change touches").
- **repair:** return only changed files; fix only what the failures name; template files are
  fixed; a missing dependency is fixed by adding it to the manifest, not by rewriting code; fix a
  shared root cause once. User prompt = attempt number + spec + template summary + current files
  + failures as `[gate] file:line — message`.

---

## 8. Execution plane (sandbox) and the validation gate

```
validate(files: GeneratedFile[], { timeoutMs, gates: Gate[] }) → ValidationReport
ValidationReport { passed, failures: ValidationFailure[], commands: { command, exitCode, stdout, stderr, durationMs }[], sandboxMs, cents }
```

Hard requirements for any implementation:
- Kernel/hardware-level isolation (micro-VM class). Plain containers on a shared host are not
  enough against a hostile install script.
- **No credentials** ever passed in; empty environment.
- **Egress allowlist**: package registry and font hosts only. Everything else denied. No route
  to your own infrastructure.
- Fresh environment per validation, destroyed after. No reuse.
- Hard wall-clock cap; remaining time shrinks per gate.
- The adapter prices its own time (`cents`), recorded on pass and fail.

**Gate order: install → build → typecheck → lint**, stop at first failure. Build runs before
typecheck because the generated framework creates types during build; a correct project fails
typecheck before its first build. Lint is run with zero warnings allowed.

### Failure parsing (feeds the repair prompt)

Structured failures repair far more reliably, and cheaper, than raw logs.
- Compiler-format lines `file(line,col): error CODE: msg` → `{file, line, "CODE: msg"}`
  (used for build and typecheck).
- Linter "stylish" format: a file-path line, then `line:col error msg rule`.
- Installer `npm error …` lines, deduplicated.
- **Never return an empty list for a non-zero exit** — fall back to the last 20 non-empty lines.
- Truncate **by file**, not by count: at most 10 files × 3 failures each, files sorted by path,
  project-level (no file) failures first. Deterministic truncation keeps repeat runs
  comparable.

---

## 9. Storage contracts (ports)

### Run store — the event log is the source of truth

```
create(id, projectId, kind, specificationId?, budget)
append(runId, events[], expectedSeq?) → StoredRunEvent[]
events(runId, afterSeq?) → StoredRunEvent[]        -- exact tail, for stream replay
projectId(runId) → ProjectId
state(runId) → RunState?                          -- ALWAYS the fold of events(); never a stored status
claimNext(leaseMs) → RunId?                       -- for a future worker poller
releaseLease(runId)
```

- `append` is atomic: events inserted **and** the listing projection (status, cost, artifact
  pointer, endedAt) updated in one transaction, or neither. The fold validates the events —
  an illegal event mid-batch rolls back the whole batch.
- `seq` is **gapless and monotonic per run**, allocated inside the transaction. Do not use a
  shared sequence (gaps) — `seq` is the stream resume cursor. Lock the run's row to serialise
  appends, including the very first.
- `expectedSeq` mismatch → `concurrent_run_write`. This detects two workers on one run.
- No update and no delete on events, ever.
- `claimNext`: oldest unfinished run whose lease is null or expired, skipping rows locked by
  others; set `leaseUntil = now + leaseMs`. Two racing callers must never get the same run.

### Project store

```
createProject, getProject, listProjects(ownerId)
addPromptRevision        -- duplicate (projectId, revision) is a conflict error, never an overwrite
latestPromptRevision(projectId)
saveSpecification, getSpecification
latestSpecification(projectId)   -- order by createdAt desc, id desc (deterministic tie-break)
```

### Artifact store — content-addressed

```
putContent(content) → { hash, bytes }    -- SHA-256; skip upload if hash exists; idempotent
getContent(hash) → content               -- missing hash is an ERROR, never empty string
saveSet(set)                             -- manifest + increment refCount once per distinct hash, one transaction
getSet(id), latestForProject(projectId)
materialise(set) → GeneratedFile[]       -- bounded parallel fetch (8 at a time), byte-exact incl. empty & unicode
```

Content is stored **private** (not addressable by public URL). Reference counts exist for
retention (delete unreachable content later).

### Event bus — live fanout only

`publish(runId, events)`, `subscribe(runId, handler) → unsubscribe`. Never authoritative: a
subscriber that misses something recovers by replaying the log from its last `seq`. For more
than one backend instance, tail the event log rather than adding a second transport that can
disagree with it.

### Contract tests

One shared test suite per port, run against **every** implementation (in-memory and real).
Cases that must be in it: atomic append; gapless seq under concurrency; stale `expectedSeq`
rejects; state after reopen equals state before; exact tail replay; claim race yields one
winner; expired lease reclaimable, live one not; identical content uploads once; materialise
round-trips byte-for-byte.

---

## 10. Relational data model

| Table | Columns | Notes |
| --- | --- | --- |
| workspaces | id, name, createdAt | tenant |
| users | id, email, createdAt | |
| memberships | workspaceId, userId, role (owner/member), createdAt | PK (workspaceId, userId) |
| projects | id, **workspaceId**, ownerId, name, target (json), createdAt | index (workspaceId, createdAt) |
| prompt_revisions | id, projectId, revision, text, createdAt | unique (projectId, revision) |
| specifications | id, projectId, promptRevisionId, spec (json), promptVersion, createdAt | |
| runs | id, projectId, kind, budget (json), status, costCents, artifactSetId, leaseUntil, createdAt, endedAt | status/cost are a **projection**; index (endedAt, leaseUntil) for claiming |
| run_events | runId, seq, at, event (json) | PK (runId, seq); append-only |
| llm_invocations | id, runId, workspaceId, step, model, promptVersion, inputTokens, outputTokens, cents, cached, at | per-call ledger (not yet written) |
| artifact_sets | id, projectId, runId, version, parentId, files (json manifest), createdAt | unique (projectId, version) |
| artifact_blobs | hash, bytes, refCount, createdAt | content itself lives in object storage at `content/<hash>` |

`workspaceId` exists on owned rows from the first migration, even while single-user.

---

## 11. Attribution (measurement)

A pure fold over a run's log, like the state fold, producing:

```
RunAttribution {
  outcome: succeeded|failed|cancelled|incomplete
  failureClass: none | plan_call | generate_call | repair_call | step_error |
                repairs_exhausted | cost_ceiling | wall_clock | cancelled | unclassified | incomplete
  failureReason?, passedFirstValidation: bool, firstFailedGate?, failedGates[],
  validations, repairAttempts,
  byStage: map<Step, Spend>, total: Spend, wallClockMs
}
Spend { calls, failedCalls, cents, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, sandboxMs, models[] }
```

Rules: a `cost.recorded` is attributed to the most recent `step.started`. A `step.failed`
preceded (within the same step) by a `cost.recorded{failed:true}` is a `<step>_call` failure;
otherwise `step_error`. Policy failures are classified by matching the policy's reason
prefixes. Spend is split by stage because per-stage model choice is decided on it.

An evaluation harness runs a fixed set of prompts × repeats, checkpoints results per case,
resumes safely (refusing a changed configuration), halts at a spend ceiling, and reports
first-shot rate, cost per validated run, first failed gate, failure classes and spend by stage.

---

## 12. HTTP API

All errors: `{ error: { code, message?, issues?, ...details } }`.

| Method & path | Body | Success | Errors |
| --- | --- | --- | --- |
| `POST /api/projects` | `{ name (1..80), prompt (10..8000), target?: partial TargetStack }` | 201 `{ id, name }` — creates project + revision 1 | 400 invalid_request, 422 unsupported_target |
| `GET /api/projects` | — | 200 `{ projects: Project[] }` (current user) | |
| `POST /api/projects/:id/runs` | `{ kind: generation\|refinement = generation, changeRequest? (3..4000) }` | 202 `{ id, kind, basedOnVersion: int\|null }` — appends change request as a new revision, creates the run, starts driving it in the background | 400 (refinement without changeRequest), 404 |
| `GET /api/runs/:id` | — | 200 `{ id, kind, status, completed, repairAttempts, validationFailures, cost, failureReason, artifactSetId, version }` | 404 |
| `POST /api/runs/:id/cancel` | — | 200 `{ status: "cancelling", cancelled: true }`; on a terminal run 200 `{ status, cancelled: false }` (no-op, not an error) | 404 |
| `GET /api/runs/:id/download` | — | 200 zip archive, `promptforge-<projectId>-v<version>.zip`; paths re-checked | 404, 409 not_ready (only `succeeded` runs are downloadable) |
| `GET /api/runs/:id/events` | header `Last-Event-ID` | Server-Sent Events stream | 404 |

Run budget's cost ceiling is configurable per deployment (default 200 cents).

Cancellation is **cooperative**: it records intent; the engine honours it between steps. An
in-flight model call or sandbox command is not interrupted.

### Progress stream protocol

- Each event frame: `id: <seq>`, `event: <event.type>`, `data: <StoredRunEvent as JSON>`.
  Events are **named** — a client listening only for the default message type receives nothing.
- On connect: **subscribe to live events first, then replay** the log after `Last-Event-ID`;
  drop anything with `seq <= lastSent` (makes the overlap safe).
- If the run is already terminal after replay, close immediately.
- Close the stream after `run.succeeded`, `run.failed` or `run.cancelled`.
- Heartbeat comment frame every 15 s during long steps. Disable proxy buffering.

---

## 13. Frontend

The first UI is one page (single-user):

- **Prompt form** → `POST /api/projects` then `POST /api/projects/:id/runs`.
- **Run console:** opens the event stream; subscribes to **every named event type**;
  derives status from `step.started` (step → status) and the terminal events; shows a running
  log and a running cost total from `cost.recorded`.
- **Cancel** button while running → `POST /cancel`.
- **Download** link when status is `succeeded`.
- Status pill for idle / each status.

Client rules learned the hard way:
1. Listen for each named event type explicitly (see stream protocol).
2. **Close the connection yourself on a terminal event.** The server ends the stream; an
   auto-reconnecting client treats that as a drop and replays the whole run forever.

Later: specification review/edit before generation, refinement history with
per-version diff and restore, sign-in, project list.

UI tokens: colours come from design tokens only — no raw colour values in components.

---

## 14. Auth and tenancy (designed, lands in Iteration 5)

- Until then: a constant user (`user_local`) and workspace (`ws_local`); no ownership check.
- Planned: GitHub sign-in, **server-side sessions** (revocable), workspace provisioned on first
  sign-in, 401 before validation on every route.
- **Enforcement in the database, not in application filters:** row-level security on every
  owned table, forced (applies to the table owner too), via a non-bypassing app role, with the
  workspace id set as a **transaction-scoped** session variable — so owned data is only touched
  inside an explicit transaction.
- Do not deploy between "authentication done" and "authorisation enforced".

---

## 15. Invariants (non-negotiable)

1. Generated code never executes outside the sandbox — not in the API process, not in a script.
2. No credential ever reaches the sandbox. Publishing happens in the control plane afterwards.
3. Run status changes only by folding an event, in one place.
4. Domain and application layers never depend on infrastructure or delivery.
5. Every step is idempotent on `(runId, step, attempt)` — no second repo, deploy or charge.
6. Every budget is checked before dispatch.
7. Unsupported targets fail at the request boundary.
8. Environment/config is read in exactly one place, validated at startup; refuse to start
   when a selected route's credential is missing.
9. The event log wins over any projection; no update/delete of events.
10. A failing refinement never replaces a working version.

---

## 16. Configuration

| Setting | Meaning |
| --- | --- |
| storage mode | `memory` (default) or `cloud` (relational DB + object storage) |
| execution mode | `memory` (scripted doubles) or `cloud` (real model + sandbox); independent of storage |
| plan model / code model | prefixed model ids (defaults: a mid-tier model for plan, a top-tier for code) |
| provider keys | one per route actually used |
| sandbox cents per second | **required** for cloud execution, no default — a guessed rate makes a plausible, wrong ledger |
| max run cents | per-run cost ceiling (default 200) |
| database URL, object-storage token | for cloud storage |

---

## 17. Build plan

Nothing is built yet. Order matters: every step lands with its tests, and nothing that spends
money is built before the pure core that decides when to spend it.

| # | Step | Done when |
| --- | --- | --- |
| 0 | Sandbox gate | One provider proven from the backend language against §8's hard requirements (ADR-0002, ADR-0007) |
| 1 | Domain | Events, fold, policy; the full control-flow suite passes with no I/O, including resumption at the same attempt |
| 2 | Ports + in-memory adapters | Every port has an in-memory adapter; the §9 contract suites pass against it |
| 3 | Model gateway | Structured output, pricing, versioned prompts; a failed call records its cost |
| 4 | Sandbox adapter + failure parser | Good fixture passes, deliberately broken fixture yields structured failures |
| 5 | Base template + protected merge | Template derived from the fixture; drift test in place |
| 6 | Delivery | HTTP API, SSE stream with replay, single-page console, zip download |
| 7 | Durable storage + worker | Postgres and object-storage adapters pass the same contract suites; a separate worker claims runs with skip-locked leases, renews them, and recovers expired ones |
| 8 | **N=20 measurement** | Attribution + resumable eval harness; first measured refinement rate |

**Driver from day one.** PromptForge drove runs with a detached background task in the request
process, so a restart left a run resumable but unclaimed. Aptify should not repeat that once
Postgres exists: the API only creates runs, and the worker (step 7) is the only thing that
drives them. In memory mode, API and worker run in one process because there is no shared
store to claim from.

**Measurement risk.** PromptForge's measurement was blocked by a free-tier limit of 20
requests/model/day against a sweep needing ≥90. Budget for a paid tier, or a provider with
sufficient limits, before step 8 — not after.

After the measurement, in order of need:

| Next | What |
| --- | --- |
| Step timeouts & retries | Per-step timeout, retryable vs permanent classification, bounded attempts |
| Cost ledger | Persist per-call invocations; per-run and per-project cost views |
| Multi-instance fanout | Stream by tailing the event log |
| Publish & deploy | Git hosting app (repo, push) in the control plane; secret scanning as a gate; deploy + webhook status |
| Retention | Delete content unreachable from any succeeded run or current version |
| Diff-based refinement | Send only relevant files; store diffs |
| History UI, repair tuning | Browse/restore versions; improve parser and repair prompt against measured failures |
| Auth, workspaces + RLS | See §14 |
| Quotas, tracing | Per-workspace limits; one trace id across API → worker → model → sandbox |

---

## 18. Inherited lessons (things that broke in PromptForge, and why)

- A completed step was re-dispatched forever when completion was *inferred* from attempt
  counters — track completion explicitly.
- A completed repair that kept its failures scheduled a second repair for the same errors —
  consume them on repair success.
- Record a failed model call's cost **before** the terminal event.
- Unpinned dependency ranges in the template made a known-good project fail on an upstream
  minor release — pin with a lockfile.
- A claim-next implementation that read the run in one step and took the lease in a later
  one handed one run to two callers — take the lease atomically with the read.
- An aggregate with a row lock is rejected by the database, and a run with no events has no row
  to lock — lock the parent run row instead.
- Models re-authoring toolchain files wasted output and introduced breakage — the protected
  template merge fixed it.
- Sending the lockfile as context cost ~60–80k input tokens per call — omit fixed files.
- Output ceilings make truncation deterministic and attributable; without them a truncated
  object silently re-runs the entire generation at full price.
- Measure before tuning: a prompt change is accepted only against a ≥15-point measured move.
