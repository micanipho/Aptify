# Progress tracker

## Current phase

Build step 1 — run domain (`context/specs/01-run-domain.md`).

## Completed

| Unit | Branch | What |
| --- | --- | --- |
| — | `main` | Design docs, README, ADR-0001…0007 |
| 01 | `feat/01-run-domain` | Go module; ids, errors, target check, path safety; run events, fold, policy with tests |

## Next

- **Step 0 — sandbox gate** (ADR-0002, ADR-0007). Needs a provider account and API key.
  Its result can still switch the backend to Node.
- Step 2 — ports + in-memory adapters + contract suites.

## Open questions

- **Sandbox provider** — E2B, Daytona (Kata only) or Vercel Sandbox. Blocks step 4.
- **Measurement budget** — paid model tier or provider limits for the N=20 sweep (SPEC §17).
- **Missing context files.** `CLAUDE.md` lists `context/project-overview.md`,
  `architecture.md`, `ui-context.md`, `code-standards.md` and `ai-workflow-rules.md`; none
  exist in this repo yet. Until they do, `docs/SPEC.md`, `docs/SYSTEM-DESIGN.md` and the
  README stand in for them.
- **Ownership checks** — none while there is one constant user (ADR-0006).
- **`golangci-lint`** is configured but not installed; `exhaustive` and `gochecksumtype`
  are not yet enforced.
