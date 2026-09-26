# ADR-0001: Narrow to one stack and compete on refinement

**Status:** Accepted · 2026-09-05 · Carried into Aptify 2026-09-26

This is about the stack of the *generated* apps. Aptify's own stack is ADR-0007.

## Context

The declared surface was five frameworks, three languages, three databases, six deploy
targets and four Git providers — 1,080 combinations, of which one had a code path. The
competitors at that breadth (Lovable, v0, Bolt, Replit Agent, Firebase Studio) are all
further along.

## Decision

Implement exactly one value per dimension: Next.js, TypeScript, Postgres, GitHub,
Vercel. Keep the enums, because they document where the product is going, but make
every unimplemented value fail with `unsupported_target` at the request boundary.

Compete on **refinement success rate at N=20** — twenty successive change requests
against a generated app with the app still building at the end.

## Consequences

- A prospect who wants Vue is lost. Accepted.
- The validation gate, the failure parser and the repair prompts can all be tuned for
  one toolchain, which is the only way the repair loop gets good.
- The target check runs before a run is created, never inside a step, so an
  unsupported request costs nothing and fails legibly.
- Breadth returns only if the N=20 number holds. Not before.
