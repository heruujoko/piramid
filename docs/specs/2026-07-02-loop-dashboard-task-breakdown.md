# Loop Dashboard — Task Breakdown & Tracking Checklist

**Date:** 2026-07-02
**Status:** Draft execution tracker
**Design:** `docs/specs/2026-07-02-loop-dashboard-design.md`
**Plan:** `docs/specs/2026-07-02-loop-dashboard-implementation-plan.md`
**Implementation target:** `/Users/herujokoutomo/Code/piramid`

## How to use this tracker

- Treat each `T-*` item as a small PR-sized task unless marked **large**.
- Update checkboxes as work lands.
- A task is complete only when its **Acceptance checklist** is complete.
- Do not start frontend real API integration before API DTOs in `T-060` are stable.
- Preserve the approved scope: Phase 1 is read-only board + gate modal + gate decision. No auth, authoring UI, timeline, or live PTY.

## Milestone map

| Milestone | Goal | Primary proof |
|---|---|---|
| M0 Baseline | Safe branch, current tests known | `go test ./...` baseline recorded |
| M1 Definitions | Patterns/loops load from files | fixture root validates and rejects bad files |
| M2 Runtime state | Fires/gates persist in SQLite | store tests for create/list/resolve |
| M3 Loop scheduler | Cron creates Fire + Goal | fake clock test produces one due fire |
| M4 Mid-run gate | exit 42 creates durable Gate | runner test skips verifier and frees worker |
| M5 Resume | decision resumes fresh seeded attempt | route/approve/reject/defer tests pass |
| M6 API | Phase-1 `/v1` endpoints stable | API tests + SSE event tests pass |
| M7 UI | React board renders gate spine | board loads, gate modal resolves decision |
| M8 E2E | One command demonstrates story | fake post-merge loop gates and resumes |
| M9 Docs | Operator can run it | README/docs include runnable example |

## Overall progress

- [x] M0 Baseline
- [x] M1 Definitions
- [x] M2 Runtime state
- [x] M3 Loop scheduler
- [x] M4 Mid-run gate
- [x] M5 Resume (T-050 gate resolution, T-051 restore prompt, T-052 fire resume/terminate)
- [x] M6 API
- [ ] M7 UI
- [ ] M8 E2E
- [ ] M9 Docs

---

# M0 — Baseline and repo preparation

## T-001 — Create implementation branch

**Type:** repo hygiene  
**Depends on:** none  
**Files:** pi-ramid git branch only

### Steps

- [x] In `/Users/herujokoutomo/Code/piramid`, create branch `loop-first-domain`.
- [x] Confirm working tree is clean or stash unrelated work.
- [x] Link the design/plan docs in a local note or PR description.

### Acceptance checklist

- [x] `git branch --show-current` returns `loop-first-domain`.
- [x] `git status --short` has no unrelated modifications.

## T-002 — Record baseline test status

**Type:** validation  
**Depends on:** T-001  
**Files:** optional `docs/baseline-loop-first.md`

### Steps

- [x] Run `go test ./...` in pi-ramid.
- [x] Record pass/fail output.
- [x] If failing, identify whether failures are pre-existing.

### Acceptance checklist

- [x] Baseline command output is recorded.
- [x] Any pre-existing failures are documented before loop-first changes begin.

## T-003 — Add design reference inside pi-ramid

**Type:** docs  
**Depends on:** T-001  
**Files:** `docs/loop-dashboard-design.md`

### Steps

- [x] Add a short document linking to the approved loop-engineering design and plan.
- [x] Summarize locked decisions: one binary, files-as-truth, exit 42, React board.

### Acceptance checklist

- [x] A new developer in pi-ramid can find the approved design from the pi-ramid repo.

---

# M1 — Domain model and file-backed definitions

## T-010 — Add loop-first domain types

**Type:** backend/domain  
**Depends on:** M0  
**Files:** `internal/domain/*.go`

### Steps

- [x] Add `Pattern` type.
- [x] Add `Loop` type.
- [x] Add `LoopAutonomy` enum: `L1`, `L2`, `L3`.
- [x] Add `LoopTrigger` enum: `piramid`, `pi`.
- [x] Add `Fire` type.
- [x] Add `FireStatus` enum: `FIRE_SCHEDULED`, `FIRE_DRAFTING`, `FIRE_GATED`, `FIRE_CONFIRMED`, `FIRE_RUNNING`, `FIRE_DONE`, `FIRE_REJECTED`, `FIRE_DEFERRED`, `FIRE_FAILED`.
- [x] Add `Gate` type.
- [x] Add `GateStatus` enum: `GATE_OPEN`, `GATE_RESOLVED`, `GATE_REJECTED`, `GATE_DEFERRED`.
- [x] Add `GateDecision` enum: `approve`, `route`, `defer`, `reject`.
- [x] Add `GateContext`, `GateThread`, `GateArtifact` types.
- [x] Add `FireID` linkage to `Goal` and/or task records if needed.

### Acceptance checklist

- [x] Types compile.
- [x] Status/decision constants are string-backed and JSON/YAML friendly.
- [x] No existing task/attempt tests are broken by type changes.

## T-011 — Implement definition loader package skeleton

**Type:** backend/definitions  
**Depends on:** T-010  
**Files:** `internal/definitions/*.go`

### Steps

- [x] Create `internal/definitions` package.
- [x] Define `Snapshot` with `Patterns []domain.Pattern`, `Loops []domain.Loop`, `LoadedAt time.Time`.
- [x] Define `LoadRoot(root string) (Snapshot, error)`.
- [x] Implement directory discovery for `patterns/*.yaml` and `loops/*.yaml`.
- [x] Return clear errors for missing root, missing subdirs, invalid file extension, unreadable files.

### Acceptance checklist

- [x] Empty but valid root loads as empty snapshot.
- [x] Missing root returns a typed/useful error.
- [x] Loader is deterministic: files sorted by path before parse.

## T-012 — Implement pattern YAML validation

**Type:** backend/definitions  
**Depends on:** T-011  
**Files:** `internal/definitions/patterns.go`, tests

### Steps

- [x] Parse pattern YAML into `domain.Pattern`.
- [x] Validate required fields: `id`, `name`, `file`, `goal`, `cadence`, `risk`, `tools`, `skills`, `state`, `phases`, `human_gates`.
- [x] Validate `id` regex: `^[a-z][a-z0-9-]*$`.
- [x] Validate `file` matches `<id>.md` or current spec's `*.md` rule.
- [x] Validate `risk` enum: `low`, `medium`, `high`.
- [x] Validate arrays are non-empty where required.
- [x] Validate duplicate pattern ids fail the whole snapshot.

### Acceptance checklist

- [x] Valid fixture pattern passes.
- [x] Missing required field fails with field name.
- [x] Duplicate pattern id fails.
- [x] Bad enum/regex fails.

## T-013 — Implement loop YAML validation

**Type:** backend/definitions  
**Depends on:** T-011, T-012, T-030  
**Files:** `internal/definitions/loops.go`, tests

### Steps

- [x] Parse loop YAML into `domain.Loop`.
- [x] Validate required fields: `id`, `pattern`, `active`, `cron`, `autonomy`, `trigger`, `goal`, `project_path`, `human_gates`, `token.daily_cap`.
- [x] Validate `pattern` resolves to a loaded pattern id.
- [x] Validate cron using `internal/cron` parser.
- [x] Validate autonomy enum.
- [x] Validate trigger enum.
- [x] Validate duplicate loop ids fail the whole snapshot.

### Acceptance checklist

- [x] Valid fixture loop passes.
- [x] Unknown pattern fails.
- [x] Invalid cron fails.
- [x] Duplicate loop id fails.

## T-014 — Add definition fixtures

**Type:** test fixtures  
**Depends on:** T-012, T-013  
**Files:** `test/definitions/patterns/*.yaml`, `test/definitions/loops/*.yaml`

### Steps

- [x] Add `patterns/post-merge-cleanup.yaml`.
- [x] Add `loops/post-merge-cleanup.yaml`.
- [x] Add invalid fixtures for bad cron, unknown pattern, duplicate ids. (invalid cases covered by table-driven loader tests)

### Acceptance checklist

- [x] Loader tests use fixtures rather than inline strings only.
- [x] Fixture names mirror expected real-world layout.

---

# M2 — SQLite runtime state for fires and gates

## T-020 — Add fires/gates migrations

**Type:** backend/store  
**Depends on:** T-010  
**Files:** `internal/store/sqlite/migrate.go`, migration files or embedded SQL

### Steps

- [x] Add `fires` table. (`migrations/003_fires_and_gates.sql`)
- [x] Add `gates` table.
- [x] Add indexes:
  - [x] `fires(loop_id, scheduled_at)`
  - [x] `fires(status)`
  - [x] `gates(status)`
  - [x] `gates(fire_id)`
  - [x] `gates(goal_id)` if goal linkage is stored.
- [x] Decide whether `gate_threads` is needed now; default to embedded JSON in `gates.context_json` for Phase 1. (Embedded JSON chosen; `gates.context_json` + `gates.context_body`.)

### Acceptance checklist

- [x] Fresh DB migrates successfully.
- [x] Existing DB migrates successfully in tests. (existing `TestReopenPreservesRecords`)
- [x] Migration is idempotent under current migration framework. (`IF NOT EXISTS` + `schema_migrations` version guard)

## T-021 — Extend store interface for fires

**Type:** backend/store  
**Depends on:** T-020  
**Files:** `internal/store/store.go`, `internal/store/sqlite/fires.go`

### Steps

- [x] Add `CreateFire`.
- [x] Add `UpdateFireStatus`.
- [x] Add `ListFires`.
- [x] Add `GetLatestFireByLoop` or include latest status in a query helper.
- [x] Emit events for fire transitions. (`FIRE_CREATED`, `FIRE_STATUS_CHANGED`)

### Acceptance checklist

- [x] Store tests cover create/list/status update.
- [x] Invalid state transition returns `ErrInvalidState` if transition rules are enforced at store level. (Fire status transitions are not gated at store level in M2; unknown-id updates return `sql.ErrNoRows`. Gate resolution enforces `ErrInvalidState`.)
- [x] Event rows are emitted with expected payload.

## T-022 — Extend store interface for gates

**Type:** backend/store  
**Depends on:** T-020  
**Files:** `internal/store/store.go`, `internal/store/sqlite/gates.go`

### Steps

- [x] Add `CreateGate`.
- [x] Add `GetGate`.
- [x] Add `ListOpenGates`.
- [x] Add `ResolveGate`.
- [x] Store context path, parsed front-matter JSON, Markdown body, decision, note.
- [x] Emit `gate.opened` and `gate.resolved` events. (Named `GATE_OPENED` / `GATE_RESOLVED` to match existing SCREAMING_SNAKE event convention; SSE passes `event_type` as an opaque string.)

### Acceptance checklist

- [x] Store tests cover create/get/list open/resolve.
- [x] Resolving closed gate fails. (`ErrInvalidState`.)
- [x] Decision note persists.
- [x] Event rows are emitted.

---

# M3 — Cron parser and loop scheduler

## T-030 — Implement 5-field UTC cron parser

**Type:** backend/cron  
**Depends on:** none  
**Files:** `internal/cron/*.go`

### Steps

- [x] Create `internal/cron` package.
- [x] Parse minute/hour/day-of-month/month/day-of-week.
- [x] Support `*`, `*/n`, `a-b`, `a-b/n`, comma lists if easy.
- [x] Compute `NextAfter(expr string, after time.Time) (time.Time, error)` in UTC.
- [x] Reject invalid field counts and ranges.

### Acceptance checklist

- [x] `*/15 * * * *` computes next quarter-hour.
- [x] `0 */6 * * *` computes next 6-hour boundary.
- [x] `30 9 * * *` computes next 09:30 UTC.
- [x] Invalid examples fail with useful errors.

## T-031 — Implement loop scheduler service

**Type:** backend/scheduler  
**Depends on:** T-013, T-021, T-030  
**Files:** new `internal/looprunner/scheduler.go` or equivalent

### Steps

- [x] Define scheduler config: definitions source, store, clock, poll interval. (`internal/looprunner.Scheduler`)
- [x] On each tick, list active loops.
- [x] Determine due loops using cron + last fire state.
- [x] Create Fire row for each due loop.
- [x] Create linked Goal in DRAFT.
- [x] Decide Phase-1 pre-execution behavior:
  - [ ] either auto-confirm configured loops for demo, or
  - [ ] create a pre-execution gate requiring explicit confirmation.
  - Decision: create a linked DRAFT goal and leave existing human confirmation flow in control; do not auto-confirm or create a gate in T-031.
- [x] Emit `fire.created` / `fire.started` as appropriate. (`CreateFire` emits existing `FIRE_CREATED`; scheduler does not mark fire started.)

### Acceptance checklist

- [x] Fake clock test creates one fire when due.
- [x] Re-running same tick does not duplicate fire.
- [x] Inactive loop does not fire.
- [x] Invalid definitions snapshot prevents scheduling but keeps daemon alive.

## T-032 — Wire loop scheduler into daemon/bootstrap

**Type:** backend/bootstrap  
**Depends on:** T-031, T-071  
**Files:** `internal/bootstrap/bootstrap.go`, `internal/daemon/daemon.go`

### Steps

- [x] Instantiate definitions service.
- [x] Instantiate loop scheduler alongside existing task scheduler.
- [x] Start/stop loop scheduler with daemon context.
- [x] Log definition load failures and scheduler errors.

### Acceptance checklist

- [x] `piramid start` starts both schedulers.
- [x] Canceling context stops both cleanly.
- [x] Existing task scheduler behavior still works.

---

# M4 — Mid-run gate handling

## T-040 — Parse `gate.context.md`

**Type:** backend/gate  
**Depends on:** T-010  
**Files:** `internal/gate/context.go`, tests

### Steps

- [x] Split front-matter bounded by `---`.
- [x] Decode YAML front-matter into `domain.GateContext`.
- [x] Preserve Markdown body as string.
- [x] Validate required fields.
- [x] Validate `decision_options` contains only valid decisions.

### Acceptance checklist

- [x] Valid context parses.
- [x] Missing front-matter fails.
- [x] Missing required field fails.
- [x] Invalid decision option fails.
- [x] Markdown body is preserved exactly. (PR #6, merged)

## T-041 — Allocate gate context path per attempt

**Type:** backend/records  
**Depends on:** T-040  
**Files:** `internal/records/records.go`, `internal/engine/runner.go`

### Steps

- [x] Extend attempt paths with `GateContext` path.
- [x] Ensure parent directory exists. (`CreateAttempt` calls `MkdirAll(root)`)
- [x] Pass `PIRAMID_GATE_CONTEXT=<path>` into executor environment.
- [x] Include path in process/attempt records if useful for inspection. (exposed via `AttemptPaths.GateContext`)

### Acceptance checklist

- [x] Runner test can observe env var passed to fake runtime.
- [x] Path is unique per task attempt. (`<attempts>/<taskID>/<NNNN>/gate.context.md`)
- [x] Directory exists before executor starts.

## T-042 — Implement exit-42 handling in runner

**Type:** backend/engine  
**Depends on:** T-022, T-040, T-041  
**Files:** `internal/engine/runner.go`, tests

### Steps

- [x] Define constant `GateExitCode = 42`.
- [x] After executor returns, branch on exit code before verifier.
- [x] If exit 42:
  - [x] parse `gate.context.md`.
  - [x] create gate row linked to fire/goal/task/attempt.
  - [x] update fire status to `gated`.
  - [x] emit gate/fire events via store. (`CreateGate` emits `GATE_OPENED`; `UpdateFireStatus` emits `FIRE_STATUS_CHANGED`)
  - [x] return nil without verifier.
- [x] If exit 42 but context missing/invalid, record operational failure `gate_context_invalid`.

### Acceptance checklist

- [x] Exit 42 with valid artifact creates open gate.
- [x] Verifier is not invoked for gated attempts.
- [x] Dispatch worker is released. (`defer dispatch.Done()`)
- [x] Missing artifact records failure.
- [x] Exit 0 path remains unchanged.

---

# M5 — Gate decisions and checkpoint restore

## T-050 — Add gate decision service method

**Type:** backend/app  
**Depends on:** T-022  
**Files:** `internal/app/service.go`

### Steps

- [x] Add `ResolveGate(ctx, gateID, decision, note)`.
- [x] Validate decision is allowed by gate context.
- [x] Store decision and note.
- [x] Emit `gate.resolved`.

### Acceptance checklist

- [x] Invalid gate id returns 404-equivalent app error.
- [x] Invalid decision returns validation error.
- [x] Valid decision closes gate.

## T-051 — Define restore prompt/seed format

**Type:** backend/prompt  
**Depends on:** T-050  
**Files:** `internal/prompt/*` or app service helper, tests

### Steps

- [x] Build restore seed containing:
  - [x] original task YAML or task id.
  - [x] gate context path.
  - [x] gate summary.
  - [x] decision.
  - [x] human note.
  - [x] resume instruction.
- [x] Ensure `route` and `reject` include note text.
- [x] Keep seed deterministic and testable.

### Acceptance checklist

- [x] Snapshot/unit test verifies seed content.
- [x] Seed instructs agent not to repeat completed ledger work.

## T-052 — Resume or terminate fire based on decision

**Type:** backend/app/store  
**Depends on:** T-050, T-051  
**Files:** `internal/app/service.go`, store methods as needed

### Steps

- [x] `approve`: mark fire running and enqueue/retry fresh attempt.
- [x] `route`: mark fire running and enqueue/retry fresh attempt with note.
- [x] `defer`: mark fire deferred; do not enqueue.
- [x] `reject`: mark fire rejected; do not enqueue.
- [x] Emit `fire.running`, `fire.deferred`, or `fire.rejected` events.

### Acceptance checklist

- [x] Approve resumes.
- [x] Route resumes with note in retry prompt.
- [x] Defer does not resume.
- [x] Reject does not resume.
- [x] Events are emitted.

---

# M6 — HTTP API and SSE contract

## T-060 — Define Phase-1 DTOs

**Type:** backend/api contract  
**Depends on:** T-010, T-021, T-022  
**Files:** `internal/api/dto.go` or `internal/app/views.go`

### Steps

- [x] Define `LoopView`.
- [x] Define `FireView`.
- [x] Define `GateSummary`.
- [x] Define `GateDetail`.
- [x] Define `GateDecisionInput`.
- [x] Define event payloads for `fire.*` and `gate.*`.

### Acceptance checklist

- [x] DTOs include only stable frontend fields.
- [x] No raw DB/internal structs are exposed directly.
- [x] Example JSON payloads can be added to docs/tests.

## T-061 — Implement loop endpoints

**Type:** backend/api  
**Depends on:** T-060  
**Files:** `internal/api/server.go`, `internal/app/service.go`

### Steps

- [x] `GET /v1/loops`.
- [x] `GET /v1/loops/{id}/fires`.
- [x] App service combines definitions snapshot + runtime latest fire.
- [x] Return useful 404 if loop id unknown.

### Acceptance checklist

- [x] API tests cover success and unknown loop.
- [x] Response includes latest fire status where available.

## T-062 — Implement gate endpoints

**Type:** backend/api  
**Depends on:** T-052, T-060  
**Files:** `internal/api/server.go`, `internal/app/service.go`

### Steps

- [x] `GET /v1/gates`.
- [x] `GET /v1/gates/{id}`.
- [x] `POST /v1/gates/{id}/decision`.
- [x] Return validation errors as structured JSON.

### Acceptance checklist

- [x] API tests cover list/detail/decision.
- [x] Detail includes parsed front-matter + Markdown body.
- [x] Decision endpoint resolves and resumes/terminates as expected.

## T-063 — Extend SSE events

**Type:** backend/events  
**Depends on:** T-021, T-022, T-060  
**Files:** `internal/api/events.go`, store event emitters

### Steps

- [x] Preserve existing `/v1/events` behavior.
- [x] Add event payloads for fire/gate transitions.
- [x] Confirm `Last-Event-ID` resume still works.

### Acceptance checklist

- [x] Existing event tests still pass.
- [x] New event types are returned after relevant store actions.
- [x] Resume after event id returns only newer events.

---

# M7 — Config and definition root

## T-070 — Add definition root config

**Type:** backend/config  
**Depends on:** T-011  
**Files:** `internal/config/config.go`, tests

### Steps

- [x] Add `definition_root` config field or nested `loops.definition_root`.
- [x] Default to `$PIRAMID_HOME/definitions`.
- [x] Validate path is absolute after resolution.

### Acceptance checklist

- [x] Config parser accepts field.
- [x] Default path is deterministic.

## T-071 — Add CLI start flag

**Type:** backend/cli  
**Depends on:** T-070  
**Files:** `internal/cli/start.go`, tests

### Steps

- [x] Add `--definitions <path>`.
- [x] CLI flag overrides config file.
- [x] Bootstrap receives resolved root.

### Acceptance checklist

- [x] CLI test covers flag.
- [x] Start command passes root to bootstrap.

## T-072 — Implement safe reload/watch

**Type:** backend/definitions  
**Depends on:** T-013, T-070  
**Files:** `internal/definitions/watch.go`

### Steps

- [x] Choose implementation: fsnotify or polling. Start with polling if dependency minimization matters.
- [x] Debounce file changes.
- [x] Load full snapshot.
- [x] Validate full snapshot.
- [x] Swap active snapshot only if valid.
- [x] Keep previous good snapshot on invalid update.
- [x] Emit/log validation errors.

### Acceptance checklist

- [x] Valid update swaps snapshot.
- [x] Invalid update does not replace previous snapshot.
- [x] Partial write simulation does not poison active definitions.

---

# M8 — React Phase-1 board

## T-080 — Scaffold Vite React app

**Type:** frontend  
**Depends on:** T-060 can be mocked initially  
**Files:** `web/*`

### Steps

- [ ] Create Vite React TypeScript app under `web/`.
- [ ] Add scripts: `dev`, `build`, `test` if using tests.
- [ ] Add basic CSS theme inspired by prototype.
- [ ] Add API base URL config.

### Acceptance checklist

- [ ] `npm install` / chosen package manager install works.
- [ ] `npm run build` produces static output.
- [ ] App shows placeholder board.

## T-081 — Implement frontend API client and event store

**Type:** frontend  
**Depends on:** T-060  
**Files:** `web/src/api/*`, `web/src/state/*`

### Steps

- [ ] Implement REST client functions.
- [ ] Implement `EventSource` subscription.
- [ ] Merge SSE updates into local state.
- [ ] Add loading/error states.

### Acceptance checklist

- [ ] Client works against mocked JSON fixtures.
- [ ] SSE handler updates gate count and fire status.

## T-082 — Build lifecycle board

**Type:** frontend  
**Depends on:** T-081  
**Files:** `web/src/components/Board*.tsx`

### Steps

- [ ] Columns: Scheduled, Running, Human Gate, Done.
- [ ] Cards show loop id, pattern, cron, latest fire status, next fire if exposed.
- [ ] Human Gate cards are clickable.
- [ ] Empty states for no loops/no fires.

### Acceptance checklist

- [ ] Board renders loops from API.
- [ ] Gate cards are visually distinct.
- [ ] Board updates after SSE event.

## T-083 — Build header pending-gates badge

**Type:** frontend  
**Depends on:** T-081  
**Files:** `web/src/components/Header*.tsx`

### Steps

- [ ] Render connection indicator.
- [ ] Render pending gate count.
- [ ] Clicking badge opens first/highest-priority gate.

### Acceptance checklist

- [ ] Badge count matches `GET /v1/gates`.
- [ ] Badge updates on `gate.opened` and `gate.resolved` events.

## T-084 — Build gate modal

**Type:** frontend  
**Depends on:** T-062, T-081  
**Files:** `web/src/components/GateModal*.tsx`

### Steps

- [ ] Fetch gate detail.
- [ ] Render summary, phase, loop/fire metadata.
- [ ] Render thread ledger from parsed front-matter + Markdown body.
- [ ] Render decision buttons.
- [ ] Render note box.
- [ ] POST decision.
- [ ] Show optimistic or post-success close behavior.

### Acceptance checklist

- [ ] Modal renders fixture gate.
- [ ] Route/reject include note.
- [ ] Successful decision closes modal and updates badge.
- [ ] API validation errors display inline.

## T-085 — Embed static frontend in Go binary

**Type:** backend/frontend integration  
**Depends on:** T-080  
**Files:** `internal/api/static.go` or similar, `Makefile`

### Steps

- [ ] Add `go:embed` for built assets.
- [ ] Serve `/` and SPA fallback.
- [ ] Ensure `/v1/*` routes still route to API.
- [ ] Add make target to build frontend then Go binary.

### Acceptance checklist

- [ ] `piramid start` serves UI.
- [ ] Browser refresh on nested route works if routing is used.
- [ ] API routes are not shadowed by static handler.

---

# M9 — E2E focused story fixture

## T-090 — Fake runtime that gates

**Type:** test/e2e  
**Depends on:** T-042  
**Files:** `internal/engine/runner_test.go` or `test/e2e/*`

### Steps

- [ ] Add fake executor that writes valid `gate.context.md` to `PIRAMID_GATE_CONTEXT`.
- [ ] Fake executor exits 42.
- [ ] Fake verifier should not be called.

### Acceptance checklist

- [ ] Test proves gate row is created.
- [ ] Test proves verifier not called.

## T-091 — Demo definition root

**Type:** test/e2e fixtures  
**Depends on:** T-014  
**Files:** `test/definitions/demo/*`

### Steps

- [ ] Add demo post-merge-cleanup pattern.
- [ ] Add demo post-merge-cleanup loop with near-term cron or manual trigger path.
- [ ] Include realistic `human_gates: [unresolved-feedback]`.

### Acceptance checklist

- [ ] Demo root loads.
- [ ] Demo loop can be forced to fire in test.

## T-092 — End-to-end script/test

**Type:** e2e  
**Depends on:** T-032, T-052, T-062, T-090, T-091

### Steps

- [ ] Start app with demo root and fake runtime.
- [ ] Force fire or advance fake clock.
- [ ] Observe gate through API.
- [ ] POST route decision.
- [ ] Observe resumed attempt or terminal state depending on fake runtime.

### Acceptance checklist

- [ ] One command demonstrates cron/fire/gate/decision/resume.
- [ ] Failure output points to the broken stage.

---

# M10 — Documentation

## T-100 — Update README with loop-first overview

**Type:** docs  
**Depends on:** M6  
**Files:** `README.md`

### Steps

- [ ] Explain `Pattern → Loop → Fire → Goal → Tasks`.
- [ ] Explain definition root.
- [ ] Explain Phase-1 UI.
- [ ] Document trusted-network/no-auth assumption.

### Acceptance checklist

- [ ] New user understands what changed from task-first pi-ramid.

## T-101 — Document gate contract

**Type:** docs  
**Depends on:** T-040, T-042  
**Files:** `docs/gates.md`

### Steps

- [ ] Explain `PIRAMID_GATE_CONTEXT`.
- [ ] Explain exit 42.
- [ ] Show full `gate.context.md` example.
- [ ] Explain decisions and restore behavior.

### Acceptance checklist

- [ ] Skill author can implement a gating skill from docs alone.

## T-102 — Document definitions

**Type:** docs  
**Depends on:** T-013, T-030  
**Files:** `docs/definitions.md`

### Steps

- [ ] Show pattern YAML example.
- [ ] Show loop YAML example.
- [ ] Explain cron semantics.
- [ ] Explain plain dir vs git repo definition root.

### Acceptance checklist

- [ ] Operator can create a valid loop by hand.

## T-103 — Document API surface

**Type:** docs  
**Depends on:** T-062  
**Files:** `docs/api.md` or existing API docs

### Steps

- [ ] Document loop endpoints.
- [ ] Document gate endpoints.
- [ ] Document SSE events.

### Acceptance checklist

- [ ] Frontend/API consumers can use docs without reading Go code.

---

# Parallelization guide

## Can run in parallel after M0

- T-010 domain types and T-030 cron parser.
- T-011 definition loader skeleton and T-020 migration design, once domain field names are agreed.
- T-080 React scaffold can begin with mocked DTOs while backend DTOs stabilize.

## Should remain sequential

- T-042 exit-42 handling depends on T-022 gates + T-040 parser + T-041 path env.
- T-052 restore depends on T-050/T-051 and must not be guessed early.
- T-084 real gate modal should wait for T-060/T-062 DTOs.

## Suggested PR slices

1. PR 1 — Domain + cron + definition loader (`T-010` to `T-014`, `T-030`).
2. PR 2 — SQLite fires/gates (`T-020` to `T-022`).
3. PR 3 — Loop scheduler wiring (`T-031`, `T-032`, `T-070`, `T-071`, partial `T-072`).
4. PR 4 — Gate artifact + exit 42 (`T-040` to `T-042`).
5. PR 5 — Gate decisions + restore (`T-050` to `T-052`).
6. PR 6 — HTTP API + SSE DTOs (`T-060` to `T-063`). → **PR #9 merged**
7. PR 7 — React board + embedded static (`T-080` to `T-085`).
8. PR 8 — E2E fixture + docs (`T-090` to `T-103`).

---

# Phase-1 final acceptance checklist

- [x] `go test ./...` passes.
- [ ] Frontend build passes.
- [x] One configured definition root loads pattern + loop YAML.
- [ ] Invalid definition changes do not poison active snapshot.
- [x] Cron creates a Fire and linked Goal.
- [x] A fake/real task can write `gate.context.md` and exit 42.
- [x] Exit 42 creates an open Gate, marks Fire gated, emits SSE event, and frees worker.
- [x] `GET /v1/loops` returns loops + latest fire status.
- [x] `GET /v1/gates` returns open gates.
- [x] `GET /v1/gates/{id}` returns parsed front-matter + Markdown body.
- [x] `POST /v1/gates/{id}/decision` records the decision and resumes or terminates correctly.
- [ ] React board renders Scheduled/Running/Human Gate/Done.
- [ ] Pending-gates badge updates from SSE.
- [ ] Gate modal renders the thread ledger and can submit a decision.
- [ ] README/docs explain loop definitions, cron, and gate contract.
- [ ] Auth remains explicitly documented as out of scope/trusted-network only.
