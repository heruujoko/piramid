# Gate Resume Flow Design

**Date:** 2026-07-04
**Status:** Approved
**Scope:** Next PR after Phase 6; complete M5 gate decisions and checkpoint restore (`T-051`, `T-052`).

## Objective

Complete the backend gate lifecycle so a fire paused by `exit 42` can be resumed or terminated by `POST /v1/gates/{id}/decision`.

The existing system can already open a gate:

```text
Task RUNNING
  → executor exits 42
  → gate.context.md is parsed
  → Gate OPEN is persisted
  → Fire becomes FIRE_GATED
```

This design adds the missing decision path:

```text
Gate decision
  → approve/route resumes the gated task with a restore prompt
  → defer/reject terminates or parks the fire without retrying
```

## Current context

Relevant modules:

| Module | Responsibility |
|---|---|
| `internal/engine/runner.go` | Runs task attempts. On exit 42, creates an open gate and parks the fire. |
| `internal/gate` | Parses `gate.context.md`. |
| `internal/store` | Persists tasks, attempts, fires, gates, and events. |
| `internal/app/service.go` | Application boundary used by HTTP API. |
| `internal/api/gates.go` | HTTP adapter for gate list/detail/decision endpoints. |
| `internal/domain/loop.go` | Owns `Fire`, `Gate`, and loop-first domain types. |

Existing gap: `Service.ResolveGate` currently validates the decision enum and calls `Store.ResolveGate`, which closes the gate but does not resume/terminate the linked task/fire.

## Architecture

Add a focused `internal/restore` package for restore prompt construction.

```text
POST /v1/gates/{id}/decision
        ↓
api.resolveGate
        ↓
app.Service.ResolveGate
        ↓
load Gate from Store
validate decision is in Gate.Context.DecisionOptions
        ↓
branch by decision
```

Decision branches:

```text
approve/route
  → get previous attempt log paths
  → tail last 20 stdout/stderr lines
  → restore.BuildPrompt(...)
  → Store.ResumeGatedTask(...)
  → Store.UpdateFireStatus(..., FIRE_RUNNING)
  → Store.ResolveGate(...)

defer
  → Store.SetTaskStatus(..., BLOCKED)
  → Store.UpdateFireStatus(..., FIRE_DEFERRED)
  → Store.ResolveGate(...)

reject
  → Store.SetTaskStatus(..., CANCELLED)
  → Store.UpdateFireStatus(..., FIRE_REJECTED)
  → Store.ResolveGate(...)
```

Ordering rule: state changes that make progress happen before resolving the gate. If resume/fire update fails, the gate remains open for retry.

## Domain lifecycle

Add first-class `TaskGated`.

Task lifecycle:

```text
PENDING → RUNNING → GATED
GATED  → PENDING    approve / route
GATED  → BLOCKED    defer
GATED  → CANCELLED  reject
```

Fire lifecycle:

```text
FIRE_GATED → FIRE_RUNNING   approve / route
FIRE_GATED → FIRE_DEFERRED  defer
FIRE_GATED → FIRE_REJECTED  reject
```

Gate lifecycle:

```text
GATE_OPEN → GATE_RESOLVED  approve / route
GATE_OPEN → GATE_DEFERRED  defer
GATE_OPEN → GATE_REJECTED  reject
```

Why `TaskGated`: a human gate is not a failed execution. The task should say it is waiting at a gate, while the fire says the loop is gated.

## Restore prompt

New package: `internal/restore`.

It builds a compact retry prompt. It does not summarize with an LLM and does not inline the full gate ledger.

Prompt contents:

```text
Resume this task from a human gate.

Decision: <approve|route>
Note: <human note or deterministic fallback note>

Gate:
- id: <gate id>
- phase: <phase>
- summary: <summary>
- context path: <gate.context.md path>

Threads:
- id: <thread id>
  title: <title>
  location: <location>
  summary: <summary>

Previous attempt:
- attempt id: <attempt id>
- stdout path: <path>
- stderr path: <path>
- last 20 stdout lines:
  <tail>
- last 20 stderr lines:
  <tail>

Instruction:
Resume from the paused phase. Do not repeat completed ledger work unless needed.
Use the full gate context file if more detail is required.
```

Rules:

- Use `GateThread.Summary`, not the full ledger body.
- Include `gate.context.md` path, not the full Markdown body.
- Include last 20 lines from prior stdout/stderr.
- Include previous attempt id and log paths.
- `note` is optional for all decisions.
- If `note` is empty, use deterministic fallback text:
  - `approve`: `Approved without note.`
  - `route`: `Routed without note; resume from gate context.`
  - `defer`: `Deferred without note.`
  - `reject`: `Rejected without note.`

Only `approve` and `route` build a restore prompt because only those resume execution.

## Store changes

Add the minimum store surface needed by the app service.

Likely additions:

```go
type ResumeGatedTaskInput struct {
    TaskID        string
    RestorePrompt string
    Override      bool
    Now           time.Time
}

ResumeGatedTask(context.Context, ResumeGatedTaskInput) error
SetTaskStatus(context.Context, taskID string, status domain.TaskStatus, now time.Time) error
```

`ResumeGatedTask`:

- requires current task status `GATED`
- sets task status to `PENDING`
- stores the restore prompt for the next runnable attempt in a new nullable task-level field, e.g. `tasks.resume_prompt`
- updates `ListRunnable` / `GetTask` to prefer `tasks.resume_prompt` over the latest verification retry prompt
- clears `tasks.resume_prompt` when `StartAttempt` consumes it, so future retries do not reuse stale gate instructions
- extends max attempts if the previous attempt exhausted the configured cap
- sets `next_run_at` to now
- emits a task event such as `GATE_RESUME`

Why a task-level `resume_prompt`: existing retry prompts come from verification rows, but gated attempts skip verification. Writing a fake verification would blur gate/resume semantics. A task-level one-shot prompt keeps the gate path explicit.

`SetTaskStatus` for defer/reject:

- only supports explicit app-owned transitions needed by this flow
- `GATED → BLOCKED` for defer
- `GATED → CANCELLED` for reject
- clears any pending `resume_prompt`
- emits task status event

## App service behavior

`Service.ResolveGate` becomes the orchestrator.

Common validation:

1. Load gate via `Store.GetGate`.
2. Ensure gate is open.
3. Parse `input.Decision`.
4. Ensure the decision exists in `gate.Context.DecisionOptions`.
5. Derive fallback note when `input.Note` is empty.

`approve` / `route`:

1. Read previous attempt id from `gate.AttemptID`.
2. Get log paths with `Store.GetAttemptLogPaths`.
3. Tail last 20 lines from stdout and stderr.
4. Build restore prompt via `restore.BuildPrompt`.
5. Call `Store.ResumeGatedTask`.
6. If `gate.FireID` is non-empty, call `Store.UpdateFireStatus(..., FIRE_RUNNING)`.
7. Call `Store.ResolveGate`.

`defer`:

1. Set linked task to `BLOCKED` when `gate.TaskID` is non-empty.
2. If `gate.FireID` is non-empty, mark fire `FIRE_DEFERRED`.
3. Resolve gate with decision `defer`.

`reject`:

1. Set linked task to `CANCELLED` when `gate.TaskID` is non-empty.
2. If `gate.FireID` is non-empty, mark fire `FIRE_REJECTED`.
3. Resolve gate with decision `reject`.

If a gate has no linked task or fire, the app applies what it can and still resolves the gate only after the relevant available state changes succeed.

## Error handling

- Unknown gate → existing not-found mapping.
- Non-open gate → conflict / invalid state.
- Decision not in `decision_options` → invalid request; gate remains open.
- Missing prior attempt logs → build prompt with paths and an explicit unavailable tail note; do not fail resume just because logs are missing.
- Failure in `ResumeGatedTask`, task status update, or fire status update → return error and leave gate open.
- Failure in final `ResolveGate` after task/fire updates → surface error; the next call should reconcile idempotently where safe.

## Tests

Add focused tests for:

- exit-42 marks task `GATED`.
- `approve` resumes:
  - decision is allowed
  - restore prompt is built
  - task becomes `PENDING`
  - fire becomes `FIRE_RUNNING`
  - gate becomes `GATE_RESOLVED`
- `route` resumes and restore prompt includes:
  - note or fallback note
  - thread summaries
  - last 20 stdout/stderr lines
  - log paths
- `defer`:
  - task becomes `BLOCKED`
  - fire becomes `FIRE_DEFERRED`
  - gate becomes `GATE_DEFERRED`
  - no restore prompt is stored
- `reject`:
  - task becomes `CANCELLED`
  - fire becomes `FIRE_REJECTED`
  - gate becomes `GATE_REJECTED`
  - no restore prompt is stored
- invalid decision not listed in `gate.Context.DecisionOptions` fails and leaves gate open.
- resume failure leaves gate open.

Run `go test ./...`.

## Out of scope

- React board.
- Full checkpoint subsystem.
- Continuation-task model.
- Full ledger/log embedding.
- LLM-generated summaries.
- Auth.
