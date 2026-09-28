# Unit 01 — Run domain

**Branch:** `feat/01-run-domain`
**Build step:** 1 (README "Build order"; SPEC §17)
**Source of truth:** `docs/SPEC.md` §3 (domain model) and §4 (run state machine)

## Goal

The pure core that decides everything about a run: identifiers, target-stack check, path
safety, domain errors, run events, the fold that rebuilds run state from the event log, and
the policy that decides the next action. No I/O, no clock, no randomness — callers pass time
and random bytes in.

## Dependencies

None beyond the Go standard library. `golangci-lint` config is added; the binary is not
installed by this unit.

## Files

| Path | Contents |
| --- | --- |
| `internal/domain/ids.go` | Distinct id types; `GenerateID(prefix, nowMs, random)` → `<prefix>_<time base36, 9 chars><16 hex>` |
| `internal/domain/errors.go` | `Code` constants (SPEC §3 Errors), `*Error`, `CodeOf(err)` |
| `internal/domain/target.go` | Target-stack enums, `WithDefaults`, `CheckSupported` → `unsupported_target` |
| `internal/domain/pathsafe.go` | `CheckPath`: rejects empty, leading `/`, `\`, `..` segment, NUL |
| `internal/domain/run/status.go` | Statuses, steps, kinds, step → status, legal edges |
| `internal/domain/run/events.go` | Sealed `Event` interface, one struct per event, flat JSON with a `type` tag |
| `internal/domain/run/state.go` | `State`, `Budget` (default 3 repairs / 200 cents / 15 min), `Cost` |
| `internal/domain/run/transition.go` | `Apply` and `Replay`: the only place status changes |
| `internal/domain/run/policy.go` | `NextAction(state, nowMs)`: SPEC §4 policy rules in order |

## Acceptance

`go vet ./...` and `go test ./...` pass, with tests proving:

- every event type round-trips through JSON; an unknown `type` is an error, never skipped
- an illegal status edge returns `illegal_transition`; a terminal run accepts only `log`
- `log` does not advance `version`; `Apply` never mutates its input state
- the first event must be `run.queued` (seq 1) and `seq` must be gapless
- a successful repair consumes its validation failures (no second repair for the same errors)
- an interrupted step — including an interrupted repair — resumes at the same attempt
- each budget (repairs, cost, wall clock) finishes the run with the SPEC reason string
- cancellation takes precedence over every budget
- driving the policy on a happy path yields plan → generate → validate → package

## Decisions made in this unit

- **Event JSON** is flat: `{"type":"step.started","step":"plan","attempt":1}`. Fields are
  camelCase, optional ones omitted when empty. This is a persisted contract from now on.
- **Interrupted repair resumes.** SPEC §4 rule 5 as first written would start a *new* repair
  attempt after a crash mid-repair, contradicting ADR-0003's "resume at the same attempt".
  The policy checks for an interrupted repair (status `repairing` with failures still
  present) before the repair budget. SPEC §4 is updated to match.
- **Unsafe paths** are an `*UnsafePathError`, not a domain error code: the engine turns step
  errors into `step.failed` + `run.failed`, so no API code is needed.
- **Timestamps** are epoch milliseconds (`int64`); money is integer cents (`int64`).
