# Loop Dashboard — Implementation Plan

**Date:** 2026-07-02
**Status:** Draft implementation plan
**Design source:** `docs/specs/2026-07-02-loop-dashboard-design.md`
**Target codebase:** `/Users/herujokoutomo/Code/piramid`

## Objective

Rework pi-ramid into a loop-first orchestrator while preserving its working execution engine. Phase 1 ships the read-only lifecycle board and durable human-gate spine:

`Pattern → Loop → Fire → Goal → Tasks`, with mid-run gates raised by `exit 42` + `PIRAMID_GATE_CONTEXT=.../gate.context.md`, rendered in a React board, and resumed by a human decision.

## Constraints from approved design

- One Go binary, one process, one VPS.
- Keep pi-ramid name.
- Keep engine internals where possible: worker pool, task dispatch, retry mechanics, log paths, SQLite, SSE event stream, recovery.
- Rewrite domain/API/scheduler top layer around loops.
- Files are source of truth for definitions:
  - `patterns/*.yaml`
  - `loops/*.yaml`
- SQLite is runtime state only:
  - fires, goals, tasks, attempts, gates, events.
- Phase 1 excludes auth, in-app authoring, timeline, live PTY/xterm, and performance work.
- Cron is 5-field UTC.
- Frontend is React + Vite, embedded/served by Go.

## Current pi-ramid surfaces to reuse

Relevant existing files/modules:

- `internal/domain/goal.go`, `task.go`, `attempt.go`, `status.go` — domain types to extend/refactor.
- `internal/store/store.go` — store interface to extend with fires/gates.
- `internal/store/sqlite/*` — migrations and persistence implementation.
- `internal/engine/scheduler.go` — task runnable scheduler to keep as execution scheduler.
- `internal/engine/runner.go` — detect exit 42, record gate, skip verifier, free worker.
- `internal/runtime/*` — process execution; must pass `PIRAMID_GATE_CONTEXT` env var.
- `internal/api/server.go`, `events.go` — add loop/gate endpoints, extend SSE event types.
- `internal/app/service.go` — application service boundary for API calls.
- `internal/bootstrap/bootstrap.go`, `internal/config/config.go` — config definition root and wire new services.
- `internal/records/records.go` — attempt record paths; extend/coordinate gate artifact paths.

## Proposed package additions

Add focused packages rather than cramming loop-specific behavior into existing engine code:

- `internal/definitions/`
  - Loads, validates, and watches file-backed loop/pattern definitions.
  - Owns `DefinitionRoot`, parser, validator, debounce/reload.
- `internal/cron/`
  - 5-field UTC parser + next-fire calculator.
  - Can start by porting the prototype parser, then harden with tests.
- `internal/gate/`
  - Parses `gate.context.md` front-matter + Markdown body.
  - Validates decision options and exposes API DTOs.
- `web/`
  - React + Vite app.
- `internal/web/` or `internal/api/static.go`
  - Go `embed.FS` handler for built frontend.

## Phase 0 — Preparation and safety net

**Goal:** create a safe branch and establish current baseline.

1. In `/Users/herujokoutomo/Code/piramid`, create a feature branch, e.g. `loop-first-domain`.
2. Run current tests:
   - `go test ./...`
3. Record current API/CLI behavior that should not regress unexpectedly:
   - task enqueue/list/retry/cancel still works unless intentionally moved.
   - existing SSE `/v1/events` still works.
4. Add a short design reference doc in pi-ramid, e.g. `docs/loop-dashboard-design.md`, linking back to the approved spec in `loop-engineering`.

**Validation:** current tests pass before changes or failures are documented as pre-existing.

## Phase 1 — Domain model and file definitions

**Goal:** introduce loop/pattern/fire/gate types without changing execution behavior yet.

1. Add/extend domain types in `internal/domain/`:
   - `Pattern`
   - `Loop`
   - `LoopAutonomy` (`L1`, `L2`, `L3`)
   - `Fire`
   - `FireStatus`
   - `Gate`
   - `GateStatus`
   - `GateDecision`
   - `GateThread`
   - `GateArtifact`
2. Keep existing `Goal`, `Task`, `Attempt` types, but add fields needed for joins:
   - `FireID` on Goal and/or Task records where appropriate.
   - `GateID` / `GateContextPath` on attempts if needed for checkpoint provenance.
3. Add `internal/definitions/`:
   - `LoadRoot(root string) (Definitions, error)`
   - `ListPatterns()`, `ListLoops()`
   - validators for pattern/loop schema.
   - duplicate id detection.
   - loop `pattern` must resolve to a known pattern id.
4. Add sample definitions under test fixtures, not production defaults:
   - `test/definitions/patterns/post-merge-cleanup.yaml`
   - `test/definitions/loops/post-merge-cleanup.yaml`

**Validation tests:**

- Pattern YAML parses and validates required fields.
- Loop YAML parses, validates cron/autonomy/trigger/token cap, and fails on unknown pattern.
- Duplicate pattern/loop ids fail.
- Missing definition root returns a useful error.

## Phase 2 — Runtime persistence: fires and gates

**Goal:** persist runtime loop state while preserving existing task/attempt persistence.

1. Add SQLite migrations in `internal/store/sqlite/migrations` or current migration mechanism:
   - `fires`
     - `id`, `loop_id`, `goal_id`, `status`, `scheduled_at`, `started_at`, `finished_at`, `created_at`, `updated_at`, `last_error`.
   - `gates`
     - `id`, `fire_id`, `goal_id`, `task_id`, `attempt_id`, `status`, `gate`, `phase`, `summary`, `context_path`, `context_body`, `decision`, `decision_note`, `opened_at`, `resolved_at`, `created_at`, `updated_at`.
   - optional `gate_threads` only if querying threads individually is needed. Phase 1 can keep threads embedded as JSON payload from front-matter.
2. Extend `internal/store/store.go` with methods:
   - `CreateFire(ctx, Fire) (Fire, error)`
   - `UpdateFireStatus(ctx, id, status, now) error`
   - `ListFires(ctx, loopID string, limit int) ([]Fire, error)`
   - `CreateGate(ctx, Gate) (Gate, error)`
   - `GetGate(ctx, id string) (Gate, error)`
   - `ListOpenGates(ctx) ([]Gate, error)`
   - `ResolveGate(ctx, id string, decision GateDecision, note string, now time.Time) error`
3. Implement methods in `internal/store/sqlite/`.
4. Emit store events for state transitions:
   - `fire.created`
   - `fire.started`
   - `fire.gated`
   - `fire.done`
   - `gate.opened`
   - `gate.resolved`

**Validation tests:**

- Migration creates tables from empty DB.
- Create/list fires works.
- Create/list/get/resolve gates works.
- Resolving a non-open gate returns `ErrInvalidState`.
- Event rows are emitted with expected `entity_type`, `entity_id`, `event_type`, payload JSON.

## Phase 3 — Cron loop scheduler top layer

**Goal:** add a loop scheduler that creates fires from file-backed loops, while leaving the existing task scheduler intact.

1. Add `internal/cron/`:
   - Parse 5-field UTC expressions.
   - Compute `NextAfter(expr, now)`.
   - Reject invalid fields, too many/few fields, bad ranges/steps.
2. Add a loop scheduler, separate from `internal/engine/Scheduler`:
   - e.g. `internal/looprunner/scheduler.go` or `internal/engine/loop_scheduler.go`.
   - Reads definitions snapshot from `definitions.Service`.
   - On each poll, finds loops whose cron should fire.
   - Creates a `Fire` row.
   - Creates/drafts a `Goal` linked to the fire.
   - For Phase 1, either:
     - auto-confirms L1/L2 test loops to reach running state, or
     - creates a pre-execution gate if the loop requires confirmation.
3. Wire into `internal/daemon/daemon.go` / `internal/bootstrap/bootstrap.go` so it runs alongside existing task scheduler.
4. Store last-fired state in SQLite, not in YAML.

**Validation tests:**

- Cron parser examples: `*/15 * * * *`, `0 */6 * * *`, `30 9 * * *`.
- Scheduler creates exactly one fire per due window.
- Scheduler does not fire inactive loops.
- Scheduler handles definition reload without duplicate fires.

## Phase 4 — Gate artifact parser and exit-42 runner handling

**Goal:** implement durable mid-run gates.

1. Add `internal/gate/context.go`:
   - parse front-matter bounded by `---`.
   - decode YAML into `domain.GateContext`.
   - preserve Markdown body.
   - validate required fields: `gate`, `phase`, `loop_id`, `fire_id`, `summary`, `decision_options`.
2. Extend attempt record path creation in `internal/records/records.go`:
   - define per-attempt gate context path, e.g. `.piramid/records/tasks/<task>/attempt-<n>/gate.context.md`.
3. In `internal/engine/runner.go`:
   - pass `PIRAMID_GATE_CONTEXT=<path>` to executor runtime environment.
   - after executor returns, check `executorResult.ExitCode`.
   - if exit code == 42:
     - parse gate context file.
     - create gate row.
     - update fire status to `gated`.
     - emit `gate.opened` / `fire.gated`.
     - do **not** move to verification.
     - return nil so dispatch worker is released.
   - exit 0 continues existing verification path.
   - other non-zero exits remain operational/execution failures as appropriate.
4. Add a store method or app-level transaction for `RecordGateFromAttempt` so runner logic does not know too much about DB joins.
5. Define behavior if exit 42 occurs but context file is missing/invalid:
   - treat as operational failure with class `gate_context_invalid`.
   - do not create an open gate.

**Validation tests:**

- Runner exit 42 with valid artifact creates an open gate and skips verifier.
- Worker is released after gated attempt.
- Missing `gate.context.md` on exit 42 records operational failure.
- Exit 0 still follows existing verifier path.
- Non-zero non-42 remains failure.

## Phase 5 — Gate decision and checkpoint restore

**Goal:** let a human resolve a gate and resume the paused fire via a fresh seeded attempt.

1. Add application service method in `internal/app/service.go`:
   - `ResolveGate(ctx, gateID string, decision GateDecision, note string) error`
2. Store decision and mark gate resolved.
3. Define checkpoint/restore seed format. Proposed seed passed into the fresh attempt prompt:
   - original task YAML.
   - path to `gate.context.md`.
   - decision value.
   - human note.
   - instruction: resume from the paused phase; do not repeat completed ledger entries unless needed.
4. Create or retry the underlying task with `RetryPrompt` containing the restore seed.
5. Update Fire state:
   - `approve` / `route`: back to `running`.
   - `defer`: terminal or waiting state (`deferred`) until manually resumed; Phase 1 can mark `deferred`.
   - `reject`: terminal `rejected`.

**Validation tests:**

- `route` decision records note and enqueues/retries fresh attempt with restore prompt.
- `approve` resumes without note requirement.
- `reject` marks gate and fire terminal rejected.
- `defer` marks deferred and does not enqueue work.
- Invalid decision not in `decision_options` fails.

## Phase 6 — Phase-1 HTTP API

**Goal:** expose the read-only board and gate decision endpoints.

1. Extend app interface used by `internal/api/server.go`:
   - `ListLoops(ctx) ([]LoopView, error)`
   - `ListLoopFires(ctx, loopID string) ([]FireView, error)`
   - `ListOpenGates(ctx) ([]GateSummary, error)`
   - `GetGate(ctx, gateID string) (GateDetail, error)`
   - `ResolveGate(ctx, gateID string, input GateDecisionInput) error`
2. Implement endpoints:
   - `GET /v1/loops`
   - `GET /v1/loops/{id}/fires`
   - `GET /v1/gates`
   - `GET /v1/gates/{id}`
   - `POST /v1/gates/{id}/decision`
3. Extend existing SSE event stream in `internal/api/events.go` rather than adding a second stream.
4. Define stable JSON DTOs for frontend; do not expose DB internals directly.

**Validation tests:**

- API returns loops parsed from definition root with latest fire status.
- Gate detail includes front-matter fields + rendered/raw Markdown body.
- Gate decision endpoint changes status and emits event.
- SSE stream includes new event types and remains resumable.

## Phase 7 — Config and definition root wiring

**Goal:** make definition root configurable and safe.

1. Extend `internal/config/config.go`:
   - `DefinitionRoot string` or nested `Loops.DefinitionRoot`.
   - Default could be `$PIRAMID_HOME/definitions`.
2. Extend CLI start flags in `internal/cli/start.go`:
   - `--definitions <path>`.
3. Bootstrap creates default directory structure if missing:
   - `patterns/`
   - `loops/`
4. Add fs-watch or polling reload:
   - debounce writes.
   - validate full snapshot before swapping active definitions.
   - if invalid, keep previous good snapshot and emit event/log.
5. If definition root is a git repo, Phase 1 only detects it for display; commits are deferred with authoring UI.

**Validation tests:**

- Config file and CLI flag both set root.
- Missing root can be created or errors clearly.
- Invalid YAML after reload does not poison active definitions.

## Phase 8 — React board frontend

**Goal:** ship the Phase-1 operator console.

1. Create `web/` with Vite + React + TypeScript.
2. Build API client:
   - `listLoops`
   - `listLoopFires`
   - `listOpenGates`
   - `getGate`
   - `resolveGate`
   - SSE `EventSource` subscription.
3. Build board UI:
   - columns: Scheduled, Running, Human Gate, Done.
   - card shows loop id, pattern, cron, latest fire state, next fire, risk/autonomy if available.
4. Build header:
   - pi-ramid connection indicator.
   - pending gates badge.
5. Build gate modal:
   - summary, phase, loop/fire metadata.
   - thread ledger from parsed `gate.context.md`.
   - decision buttons: approve, route, defer, reject.
   - note box, required for route/reject if API enforces it.
6. Embed frontend:
   - `go:embed web/dist` or copy build output to `internal/web/dist` depending on repo convention.
   - Serve `/` SPA fallback from Go.
7. Keep styling pragmatic; visual polish later.

**Validation tests:**

- Unit tests for API client DTO parsing where practical.
- Manual browser flow against seeded fixture data.
- Optional Playwright smoke: board loads, gate opens, decision posts.

## Phase 9 — End-to-end demo fixture

**Goal:** prove the focused story without needing a real PR at first.

1. Add a fake executor/runtime test path that exits 42 and writes a valid `gate.context.md`.
2. Seed definitions:
   - `patterns/post-merge-cleanup.yaml`
   - `loops/post-merge-cleanup.yaml`
3. Run daemon with test definitions.
4. Force or wait for cron fire.
5. Observe:
   - fire created.
   - task starts.
   - exit 42 creates gate.
   - React board shows Human Gate.
   - decision route resumes a fresh attempt.
   - final status reaches running/done depending on fixture.

**Validation:** one command or documented script demonstrates the full gate spine.

## Phase 10 — Documentation and operator guidance

**Goal:** make the new model understandable.

1. Update pi-ramid `README.md`:
   - loop-first model.
   - definition root setup.
   - exit 42 gate contract.
   - `gate.context.md` schema.
2. Add `docs/gates.md`:
   - how skills raise gates.
   - examples for pr-maintainer.
3. Add `docs/definitions.md`:
   - pattern and loop YAML examples.
   - cron semantics.
4. Add `docs/api.md` or extend existing API docs.

**Validation:** docs include a minimal runnable example.

## Suggested execution order for coder agents

1. **Domain + definitions** (Phase 1) — isolated and testable.
2. **SQLite fires/gates** (Phase 2) — schema + store tests.
3. **Cron scheduler** (Phase 3) — can be tested with fake clock.
4. **Exit-42 gates** (Phase 4) — runner behavior, key risk.
5. **Gate decisions / restore** (Phase 5) — second key risk.
6. **HTTP API** (Phase 6) — frontend contract.
7. **React board** (Phase 8) — can mock API until backend is ready.
8. **E2E fixture and docs** (Phases 9–10).

Backend phases 1–3 and frontend scaffolding can happen in parallel after DTOs are drafted, but avoid implementing the real frontend gate flow before Phase 6 DTOs stabilize.

## Risks and mitigations

- **Exit 42 ambiguity / missing artifact**
  - Mitigation: treat missing/invalid artifact as operational failure; test it.
- **Definition reload race**
  - Mitigation: debounce and swap only a fully valid snapshot.
- **Gate restore repeats already-completed work**
  - Mitigation: restore prompt includes ledger and explicit resume instruction; later add structured phase checkpoints.
- **Store interface gets too broad**
  - Mitigation: use application-level services for composite transitions; keep store methods primitive and transactional.
- **Frontend blocks on backend completeness**
  - Mitigation: define DTO fixtures early; build UI against mocked API responses.
- **Auth is out of scope**
  - Mitigation: document trusted-network assumption prominently; bind to localhost by default unless configured.

## Definition of done for Phase 1

- `go test ./...` passes in pi-ramid.
- A configured definition root with one pattern and one loop is loaded and validated.
- Cron creates a Fire and Goal.
- A test/fake running task can write `gate.context.md` and exit 42.
- The attempt is recorded as gated, worker is freed, Fire state becomes `gated`, and a Gate row exists.
- `GET /v1/loops`, `GET /v1/gates`, `GET /v1/gates/{id}` return correct data.
- React board loads from the Go-served static bundle.
- Pending gate badge and gate modal render the gate artifact.
- Posting a decision records it and starts/resumes a fresh seeded attempt.
- The implementation docs explain the file root, cron semantics, and gate contract.
