# Loop Dashboard — Design Spec

> **Source:** `docs/specs/2026-07-02-loop-dashboard-design.md`
> This file is the canonical spec. All implementation must match this document.

---

## Objective

Rework pi-ramid into a loop-first orchestrator while preserving its working execution engine. Phase 1 ships:

- **Read-only lifecycle board** — Pattern → Loop → Fire → Goal → Tasks
- **Durable human-gate spine** — mid-run gates raised by `exit 42` + `PIRAMID_GATE_CONTEXT=.../gate.context.md`, rendered in a React board, and resumed by a human decision

---

## Constraints

- One Go binary, one process, one VPS
- Keep pi-ramid name
- Keep engine internals where possible: worker pool, task dispatch, retry mechanics, log paths, SQLite, SSE event stream, recovery
- Rewrite domain/API/scheduler top layer around loops
- Files are source of truth for definitions: `patterns/*.yaml`, `loops/*.yaml`
- SQLite is runtime state only: fires, goals, tasks, attempts, gates, events
- Phase 1 excludes: auth, in-app authoring, timeline, live PTY/xterm, performance work
- Cron is 5-field UTC
- Frontend: React + Vite, embedded/served by Go

---

## Architecture

### Entity hierarchy

```
Pattern ← Loop ← Fire ← Goal ← Task ← Attempt
                                    ↓
                                  Gate ← gate.context.md
```

### Key domain types

| Type | Description |
|------|-------------|
| `Pattern` | Reusable skill/workflow template (e.g. `pr-review`, `code-review`) |
| `Loop` | Cron-scheduled fire trigger; references a pattern |
| `Fire` | Runtime instance of a loop firing; links to a Goal |
| `Goal` | Seeding prompt + metadata passed to the task executor |
| `Task` | Atomic work unit scheduled on the worker pool |
| `Attempt` | A single execution attempt of a task |
| `Gate` | Human-approval checkpoint raised by executor `exit 42` |
| `GateDecision` | `approve`, `route`, `defer`, `reject` |
| `GateStatus` | `open`, `resolved` |
| `LoopAutonomy` | `L1` (auto-fire), `L2` (confirm), `L3` (gate-first) |

### Gate contract

1. Executor receives `PIRAMID_GATE_CONTEXT=<path>` env var
2. When human input is needed: write `gate.context.md` and `exit 42`
3. Runner detects exit 42, parses the artifact, creates an open `Gate` row, parks the `Fire`
4. Human reviews in React board and posts a decision
5. A fresh seeded attempt resumes from the checkpoint

### gate.context.md front-matter schema

```yaml
---
gate: review
phase: pr-summary
fire_id: FIRE-...
loop_id: LOOP-...
summary: "Human review needed: 3 open threads on authentication"
decision_options:
  - approve
  - route
  - defer
  - reject
---
## Thread Ledger

1. **[author-note]: Found 3 threading issues in auth module**
   - auth/handlers.go:42 — missing error wrap
   - auth/middleware.go:18 — context leak
2. **[CI]: 2 failing tests**
3. **[comment]: PR thread "error handling" unresolved**
```

---

## Frontend — Phase 1 scope

### Board columns

| Column | Description |
|--------|-------------|
| **Scheduled** | Fires with future `scheduled_at` |
| **Running** | Fires with `status = running` |
| **Human Gate** | Fires with open gates; badge count in header |
| **Done** | Fires with terminal status (`done`, `rejected`, `deferred`) |

### Gate modal

- Summary, phase, loop/fire metadata
- Thread ledger from parsed `gate.context.md`
- Decision buttons: **Approve**, **Route**, **Defer**, **Reject**
- Note field (required for route/reject)

### Header

- pi-ramid connection indicator (SSE live)
- Pending gates badge

---

## Proposed package additions

| Package | Responsibility |
|---------|---------------|
| `internal/definitions/` | File-backed loop/pattern loader, validator, watcher |
| `internal/cron/` | 5-field UTC parser + next-fire calculator |
| `internal/gate/` | `gate.context.md` parser, DTOs |
| `web/` | React + Vite frontend |
| `internal/web/` | Go `embed.FS` static handler |

---

## Reuse targets

- `internal/domain/{goal,task,attempt,status}.go` — extend with fire/gate fields
- `internal/store/store.go` — add fire/gate methods
- `internal/store/sqlite/*` — add migrations
- `internal/engine/scheduler.go` — keep as execution scheduler
- `internal/engine/runner.go` — detect exit 42, record gate, free worker
- `internal/runtime/*` — pass `PIRAMID_GATE_CONTEXT`
- `internal/api/{server,events}.go` — extend SSE, add loop/gate endpoints
- `internal/app/service.go` — application service boundary
- `internal/bootstrap/bootstrap.go`, `internal/config/config.go` — wire new services
- `internal/records/records.go` — extend with gate artifact paths
