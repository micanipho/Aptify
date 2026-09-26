# ADR-0006: Model workspaces explicitly; carry the column from day one

**Status:** Accepted · 2026-09-05 · Carried into Aptify 2026-09-26

## Context

The previous design used an identity framework's tenant construct as a workspace. A
tenant there is heavyweight — its own connection string, editions, features, settings —
while a workspace is a folder with members. Conflating them meant creating a workspace
provisioned a tenant, and a user could not belong to two workspaces without
impersonation gymnastics.

It also shipped an aggregate that referenced tenants without implementing the interface
that installs the automatic query filter. The result was not a weak filter; it was no
filter at all.

## Decision

Model `Workspace` and `Membership` as ordinary tables. No framework tenancy.

`workspace_id` is on every owned row in the **first** migration, even though Iteration 1
is single-user and nothing filters on it yet.

Enforcement, when it arrives in Iteration 5, is Postgres row-level security with the
workspace id carried in a session variable — not application-level `WHERE` clauses.

## Consequences

- Retrofitting a tenant column onto a populated database is the migration that produces
  a data leak. Adding a filter to a column that already exists is routine. The column
  costs nothing now and removes the dangerous migration later.
- RLS is the only approach where a forgotten predicate is not a breach. With
  application-level filtering, every query is a place where one can be forgotten, and
  the failure is silent.
- Until Iteration 5, the current user and workspace are constants, and the absence of an
  ownership check is listed under "Still open" in `docs/SYSTEM-DESIGN.md`, rather than
  quietly assumed handled.
- With pgx, the workspace id is set per transaction with `set_config('app.workspace_id', $1,
  true)`, so a pooled connection never carries one request's workspace into the next.
