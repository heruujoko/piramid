# Loop Dashboard — Design Spec

**Date:** 2026-07-02
**Status:** Approved (design), pending implementation plan
**Supersedes:** the throwaway prototype at `loop-dashboard/prototype/dashboard.html`

## Purpose

A personal operator console for maintaining loop-engineering automations. Each
**loop** is a YAML definition (like a GitHub Actions workflow) that fires on a
**cron** schedule, produces a goal, and runs work through an orchestrator. When a
loop hits a `human_gates` condition it **pauses** and hands full context to a
human, who makes a decision that resumes it.

The backend is a rewrite of **pi-ramid**'s domain + API to put loops at the top,
reusing pi-ramid's existing execution engine. The frontend is a React SPA served
by the same Go binary.

The Phase-1 focused story is **post-merge-cleanup + pr-maintainer**: after a PR
merges, a loop scans follow-up debt, enumerates PR review threads, and gates
mid-run on `unresolved-feedback` for a human decision.

## Decisions (locked)

1. **Backend strategy** — rewrite pi-ramid's `domain/`, `api/`, `intake/`, and
   the scheduler's top layer to make **loops** the top concept. **Keep** the
   engine internals: worker pool + project leasing, retry mechanics, attempt log
   streaming, SSE events with `Last-Event-ID` resume, SQLite store, recovery.
   A from-scratch rewrite was rejected: the runtime model is correct and generic;
   the gap is purely the domain layer *above* it.
2. **Domain hierarchy** — `Pattern → Loop → Fire → Goal → Tasks`. Gates live on
   the Goal.
3. **Fire lifecycle** — supports **mid-run gates** (a gate may fire before
   execution *or* be raised by a running task).
4. **Gate mechanism** — **checkpoint + restore**, not live-hold. A gating skill
   writes a `gate.context.md` artifact and exits; the engine frees the worker and
   records a durable gate; a human decision starts a fresh, seeded attempt.
5. **Gate signal** — process exit code **42 = gated**, exit 0 = done. The artifact
   path is provided to the skill via the `PIRAMID_GATE_CONTEXT` env var.
6. **Gate artifact** — a single **`gate.context.md`** (YAML front-matter +
   Markdown body). One source of truth, rendered by the dashboard *and* consumed
   by the engine on restore.
7. **Persistence** — **files are the source of truth** (`patterns/*.yaml`,
   `loops/*.yaml`) under a **configurable definition root** (a plain directory or
   a git repo, set via UI or config file). SQLite holds **runtime state only**
   (fires, goals, tasks, attempts, gates, events). The engine fs-watches the root
   and validates before applying.
8. **Frontend** — **React + Vite**, built to a static bundle served from the Go
   binary via `embed.FS`. Performance / Lighthouse is an explicit non-goal.
9. **Phase 1 scope** — **gate spine, read-only, board only** (see §7).
10. **Name** — stays `pi-ramid`.
11. **Cron** — 5-field, UTC, GitHub-Actions style (`*/n`, ranges, steps).

## System shape

```
pi-ramid (Go, one binary)
├── loop layer (NEW)      patterns registry, loops, cron scheduler, gate lifecycle
├── engine (KEPT)         worker pool + leasing, retries, log streaming, SSE, recovery
├── store: SQLite         runtime only (fires, goals, tasks, attempts, gates, events)
├── def root: <dir|git>   patterns/*.yaml, loops/*.yaml  ← source of truth (fs-watched)
└── /v1 API + / (static)  serves React build via embed.FS
```

One process, one VPS. Reachable only over a trusted network (Tailscale/tailnet or
localhost). No auth layer — see §7 non-goals.

## Domain model

`Pattern → Loop → Fire → Goal → Tasks`, gates on the Goal.

- **Pattern** — reusable template validated against the loop-engineering
  `registry.schema.json`: `id`, `name`, `file`, `goal`, `cadence`, `risk`,
  `tools`, `skills`, `state`, `phases`, `human_gates`, `week_one_mode`,
  `token_cost`, optional `cost` block. Authored as `patterns/<id>.yaml`.
- **Loop** — an instance binding a pattern to a **cron**, a **project path**, an
  **autonomy** level (L1 report / L2 assisted / L3 unattended), a **trigger**
  (piramid | pi), a **goal** text, `human_gates`, and a `token.daily_cap`.
  Authored as `loops/<id>.yaml`. `pattern` must resolve to a registry id.
- **Fire** — one cron activation of a loop. Owns the lifecycle state machine and
  a `runs[]`-style attempt/decision trail. Runtime row in SQLite, keyed by
  `loop_id` (string FK to the loop file's `id`).
- **Goal** — the confirmed intent produced by a fire; pi-ramid's existing goal
  (`DRAFT → CONFIRMED → REJECTED`) enriched with gate context.
- **Task / Attempt** — unchanged execution units the engine already runs.

## Fire lifecycle (mid-run gates)

```
fired → drafting → gated → confirmed → running → gated → done
                                                    ↘ rejected / deferred / failed
```

- `fired` — cron matched; a Fire row is created.
- `drafting` — the loop's `goal` text + `human_gates[]` become a Goal in DRAFT.
- `gated` (pre-execution) — the Fire pauses for goal confirm/reject.
- `confirmed` — goal confirmed; tasks may be enqueued.
- `running` — tasks/attempts execute on the engine.
- `gated` (mid-run) — a running task raised a gate (see §Gate mechanism).
- `done` — all work complete; `rejected` / `deferred` / `failed` are terminal
  alternatives.

The novel capability is **`running → gated`**: a task pauses itself mid-run,
which pre-execution-only gate models cannot express.

## Gate mechanism (load-bearing contract)

1. A running `pi` skill hits a `human_gates` condition. It **writes
   `gate.context.md`** to the path in the `PIRAMID_GATE_CONTEXT` env var, then
   **exits 42**.
2. The engine reads **exit 42** as "paused, not done": it records a **Gate** row
   from the artifact, emits a `gate.opened` SSE event, **terminates the attempt
   and frees the worker** (checkpoint, not live-hold). The gate is durable and
   survives a daemon restart.
3. The dashboard renders the gate modal directly from `gate.context.md` (trigger,
   stalled phase, thread ledger, decision options).
4. On human decision (`approve | route | defer | reject` + optional note), the
   engine **starts a fresh attempt** seeded with the checkpoint + the decision.
   The note becomes the agent's in-thread reply for `route`/`reject`.

### `gate.context.md` schema

YAML front-matter (machine contract) + Markdown body (human-readable ledger):

```yaml
---
gate: unresolved-feedback          # which human_gates entry fired
phase: route-item                  # the phase that stalled
loop_id: LOOP-05
fire_id: <fire id>
summary: "PR #182 has one actionable thread beyond minimal-fix scope"
decision_options: [approve, route, defer, reject]
suggested_decision: route          # optional
threads:
  - id: r182c1
    location: internal/api.go:124
    author: cobusgreyling
    author_type: human             # human | bot
    reviewed_commit: a1f3c2e
    classification: needs-human    # actionable | informational | already-applied | outdated | needs-human
    reason: "Actionable, but touches implementation and test files."
ledger: "r182c1 -> open . r182c2 -> routed . r182r1 -> informational"
artifacts:
  - kind: diff
    path: .piramid/fires/<fire>/proposed.patch
---

## internal/api.go:124 — @cobusgreyling (human)

> This error path swallows the cause — please wrap with %w so callers can
> unwrap. Also, the test in api_test.go should assert the wrapped error.

**Classification:** actionable — beyond minimal-fix scope (2 files).

...
```

The front-matter is the API/restore contract; the body is what a human reads.
Skills that gate (pr-maintainer, etc.) MUST produce this file. This is the same
artifact the dashboard renders, so the contract is load-bearing, not overhead.

## API surface (Phase 1)

Under the existing `/v1` prefix:

- `GET  /v1/loops` — list loops (parsed from files) + latest fire status each.
- `GET  /v1/loops/{id}/fires` — fire history for a loop.
- `GET  /v1/gates` — open gates (drives the pending-gates badge / queue).
- `GET  /v1/gates/{id}` — full gate; serves parsed `gate.context.md`.
- `POST /v1/gates/{id}/decision` — body `{ decision, note }`; records the
  decision and resumes the fire (fresh seeded attempt).
- `GET  /v1/events` — **existing SSE**, extended with `fire.*` and `gate.*`
  event types (`fire.started`, `fire.gated`, `gate.opened`, `gate.resolved`,
  `fire.done`, …), resumable via `Last-Event-ID`.

Existing task/goal/worker/log endpoints remain for internal use and later phases.

## Frontend (Phase 1)

React + Vite, one surface: the **lifecycle board**
(Scheduled → Running → Human Gate → Done), a **pending-gates badge** in the
header, and the **gate modal**.

- Live state is driven by the SSE stream via `EventSource`; `fire.*`/`gate.*`
  events update the board and badge reactively.
- The gate modal renders trigger, stalled phase, the pr-maintainer thread ledger,
  the four decision buttons (each showing the downstream action), and a note box.
- No authoring UI, no timeline, no live PTY console in Phase 1. Loops are authored
  by editing YAML files in the definition root, which the FE reads and lists.
- Built to a static bundle embedded in the Go binary (`embed.FS`).

## Non-goals / deferred (conscious Phase-2+)

- **Auth** — assume a trusted network; add before any public exposure.
- **In-app authoring** — the loop/pattern editor with live schema validation.
- **Timeline surface** — the cadence/next-fire rail.
- **Live PTY / xterm console** — streaming a running agent's terminal over
  websocket. Deliberately sidestepped by the checkpoint gate model.
- **Multi-gate queue UI** — richer than the badge + single modal.
- **Performance / Lighthouse** — explicit non-goal.

## Open items for planning

- Exact fs-watch debounce + validate-before-apply behavior for the definition
  root (editor swap-writes, partial writes).
- Concrete SSE event payload shapes for `fire.*` / `gate.*`.
- SQLite schema for `fires`, `gates`, and the gate↔goal↔fire joins.
- The checkpoint/restore seeding format handed to the fresh attempt.
- Cron parser reuse (the prototype has a working 5-field UTC parser).
