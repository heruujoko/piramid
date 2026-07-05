# Gate Contract

pi-ramid uses a durable human-gate contract to pause a running task, surface a decision in the UI/API, and resume from a fresh seeded attempt.

## Trigger

An executor raises a gate by:

1. reading `PIRAMID_GATE_CONTEXT` from its environment
2. writing a `gate.context.md` file to that exact path
3. exiting with status code `42`

`42` is reserved for gate handoff. Any other non-zero exit is treated as an execution or verification failure.

## Runtime flow

```text
Executor writes gate.context.md + exits 42
  -> runner parses gate context
  -> gate row is created in SQLite
  -> attempt is parked as GATED
  -> linked fire is moved to FIRE_GATED
  -> worker lease is released
  -> human resolves gate via API/UI
  -> pi-ramid seeds a fresh attempt with restore context
```

## File format

`gate.context.md` is markdown with YAML front-matter.

```yaml
---
gate: review
phase: pr-summary
loop_id: LOOP-PR-MAINTAINER
fire_id: FIRE-LOOP-PR-MAINTAINER-20260704120000
goal_id: GOAL-LOOP-PR-MAINTAINER-20260704120000
summary: "Human review needed: 3 unresolved PR threads"
decision_options:
  - approve
  - route
  - defer
  - reject
---
## Thread Ledger

1. **[comment]** auth/handlers.go:42 — missing error wrap
2. **[ci]** 2 failing tests remain after fix
```

## Required front-matter fields

- `gate` — gate type label
- `phase` — stage where the gate fired
- `summary` — human-readable reason for the pause
- `decision_options` — allowed decisions; must contain one or more of:
  - `approve`
  - `route`
  - `defer`
  - `reject`

## Optional front-matter fields

- `loop_id`
- `fire_id`
- `goal_id`
- `task_id`

In production, fire/goal linkage is derived from store state and treated as authoritative. Values in the gate file are audit payload, not trusted linkage.

## Decision semantics

### `approve`
Resume the gated task immediately with a restore prompt.

### `route`
Resume the gated task immediately with a restore prompt plus human note.

### `defer`
Mark the task blocked and the fire deferred.

### `reject`
Cancel the task and mark the fire rejected.

## API surface

- `GET /v1/gates` — list open gates
- `GET /v1/gates/{id}` — fetch gate detail, thread ledger, body, and allowed decisions
- `POST /v1/gates/{id}/decision` — resolve a gate

Request body:

```json
{
  "decision": "approve",
  "note": "Looks good, continue"
}
```

## UI surface

Phase 1 exposes gates in the React board as:

- a pending-gates badge in the header
- a Human Gate column in the board
- a gate modal with summary, metadata, thread ledger, decisions, and note field

## Notes

- Gate rows are fire-backed runtime state.
- The E2E gate story creates a real fire before gating so it matches the schema.
- A missing or invalid `gate.context.md` becomes an operational failure, not a gate.
