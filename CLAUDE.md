# CLAUDE.md

> Read this first, every session. Applies to Claude Code, Codex, Cursor and any
> other agent working in this repository. `AGENTS.md` points here.

## Application Building Context

Read these in order before implementing or making any architectural decision:

1. `context/project-overview.md` — product definition, goals, features, scope
2. `context/architecture.md` — system structure, boundaries, storage model, invariants
3. `context/ui-context.md` — theme, colours, typography, component conventions
4. `context/code-standards.md` — implementation rules and conventions
5. `context/ai-workflow-rules.md` — development workflow, scoping rules, delivery
6. `context/progress-tracker.md` — current phase, completed work, open questions

Background reasoning (read when a decision needs justifying, not every session):
`docs/SYSTEM-DESIGN.md`, `docs/adr/`.

Update `context/progress-tracker.md` after each meaningful implementation change.

If implementation changes the architecture, scope, or standards documented in the
context files, update the relevant file **before** continuing.

## Working agreement

- Implement only what the current spec file in `context/specs/` describes.
- If a requirement is missing or ambiguous, stop and ask. Do not assume.
- Do not refactor, rename, or "improve" code outside the current unit's scope.
- Do not install packages that the current spec does not list under Dependencies.
- Before the first edit of a unit, create its branch: `feat/NN-unit-name`. Never commit
  unit work to `main`. Full git rules live in `context/ai-workflow-rules.md` under Git.

## The one thing that matters

The product metric is **refinement success rate at N=20** — twenty successive change
requests against a generated app, with the app still building at the end. First-shot
generation quality is not the metric. When a design choice trades first-shot polish
for refinement reliability, take the refinement side.

## Commands

Backend (Go, repo root). Frontend commands arrive with `web/`.

| Purpose        | Command                 |
| -------------- | ----------------------- |
| Build          | `go build ./...`        |
| Vet            | `go vet ./...`          |
| Lint           | `golangci-lint run`     |
| Format check   | `gofmt -l .`            |
| Test           | `go test ./...`         |

## Hard rules

- **Never** write code that executes generated output inside this process. All
  generated code runs in the sandbox (`internal/infra/sandbox/`). No exceptions.
- **Never** put a secret in `internal/domain/` or pass one into a sandbox call.
- **Never** mutate a run's status outside `internal/domain/run/transition.go`.
- **Never** import from `internal/infra/` or `internal/http/` inside `internal/domain/` or
  `internal/app/`.
- **Never** skip an unknown event type when decoding the log; it is an error.
