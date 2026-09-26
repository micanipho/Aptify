# ADR-0002: Validate in a micro-VM sandbox, from Iteration 1

**Status:** Accepted · 2026-09-05 · Revised 2026-09-26 (provider no longer fixed; see ADR-0007)
**Supersedes:** the v0.1 proposal's "E2B or Daytona in Iteration 2, `tsc` in-process until
then", and PromptForge's choice of Vercel Sandbox.

## Context

We run `npm install` and a build over LLM-generated code produced from an untrusted prompt.
Assume the prompt is adversarial: a user can cause arbitrary code to execute on our
infrastructure. That is not a bug in the design, it *is* the design, and the only question is
where it executes.

The v0.1 plan accepted an in-process `tsc --noEmit` for Iteration 1 as a temporary,
knowingly-unsafe compromise. PromptForge rejected that and used Vercel Sandbox, whose SDK
shared its toolchain. Aptify's backend is Go (ADR-0007), where no provider has that advantage,
so the provider is re-opened; the requirements are not.

## Decision

Validate in a **managed micro-VM sandbox** from the first iteration. No generated code ever
executes in the API or worker process.

Requirements, which any provider must meet before it is chosen:

| Requirement | What must be proven |
| --- | --- |
| Kernel/hardware isolation | Micro-VM class (Firecracker, Kata or equivalent). A plain container is a failure. |
| No route to our infrastructure | Default-deny networking; no reach to our VPC or a metadata endpoint |
| Egress allowlist | The npm registry and font hosts, nothing else |
| CPU / wall-clock caps | Configurable resources and a hard timeout |
| Destroyed after every run | Explicit stop in a `defer`; the provider timeout as the backstop |
| No secrets, ever | Empty environment, asserted by the port's contract test |
| Priced | Elapsed sandbox time returned, so the adapter can price it |

Candidates from Go, all unproven: E2B (community SDKs, or a thin client over its APIs),
Daytona (official Go SDK; must run on Kata, not its default containers), Vercel Sandbox
(REST API). The first one to pass every row above is chosen. If none passes without a
TypeScript helper service, the backend language changes instead (ADR-0007).

## Consequences

- The "temporary" unsafe window never exists. A rule with one exception is not a rule.
- Publishing stays in the control plane, which holds the tokens. The sandbox produces a
  validation report and never sees a credential.
- The sandbox adapter is the least-supported code in the backend; it gets the contract suite
  first and runs against a good and a deliberately broken fixture in CI.
- Sandbox time becomes a cost line. `npm install` dominates it; caching `node_modules` by
  lockfile hash is the first optimisation when it starts to matter.
- Revisit self-hosting Firecracker only if sandbox spend exceeds LLM spend.
- Plain Docker on a shared host remains unacceptable: insufficient isolation against a hostile
  `postinstall` script. That applies to a managed provider's container mode too.
