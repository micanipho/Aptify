# ADR-0004: One gateway for every model call

**Status:** Accepted · 2026-09-05 · Revised 2026-09-26 (Go implementation)

## Context

Token accounting, per-run cost ceilings, prompt versioning, caching and provider failover are
each only enforceable if there is exactly one place a model can be called. Scattered provider
SDK calls make all five impossible at once. Without token accounting the product cannot be
priced, abuse cannot be detected, and nobody notices that a repair loop spent $40 on a single
pathological prompt.

## Decision

Every model call goes through the `LLMGateway` port (`internal/app`). The adapter
(`internal/infra/llm`) routes by model-id prefix (SPEC §7): `anthropic/…` and `google/…` go
direct through the official Go SDKs, `gateway/…` through an aggregating gateway's
OpenAI-compatible API. Changing or failing over a provider is configuration, not code. No
other package imports a provider SDK.

The gateway owns:

- **Structured output** — plans and file sets are requested with a JSON Schema generated from
  Go structs and validated after parsing; unparseable or invalid output is retried, not
  patched up. Constraints are also stated in field descriptions, and caps on nested arrays
  are checked after parsing rather than sent (SPEC §7)
- **Pricing** — a table checked against published rates, in integer cents. Unknown models
  bill at the most expensive known rate, because an unpriced model must not become an
  unbounded one
- **Prompt versioning** — prompts live in `internal/infra/llm/prompts` with a version string
  recorded on every run; a snapshot test fails when a prompt changes without a version bump,
  so an output-quality regression traces to the edit that caused it
- **Usage** — every call returns priced usage, which the engine turns into a `cost.recorded`
  event on the run log. A call that fails after spending returns an error carrying its
  estimated usage

## Consequences

- The per-run cost ceiling is enforceable in the domain, checked before work is dispatched
  rather than noticed afterwards.
- Cost per run, per project and per model is queryable from day one, which is what makes
  pricing and abuse detection possible later.
- Go has no Zod equivalent: schema generation and validation are two pieces of code that can
  disagree. A test sends every schema through the validator with known-good and known-bad
  fixtures.
- Caching by `hash(prompt + model + params)` becomes a gateway-internal change when the
  measurement justifies it. Refinement repeats context heavily, so this is the most likely
  place the cost model improves.
- A circuit breaker belongs here too: when a provider is down the run should fail cleanly
  rather than burn its repair budget on retries. Not in the first iteration.
