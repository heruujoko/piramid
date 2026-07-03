# M2 — SQLite Runtime State (Fires & Gates) Implementation Plan

**Date:** 2026-07-03
**Milestone:** M2 Runtime state
**Design source:** `docs/specs/2026-07-02-loop-dashboard-design.md`
**Task breakdown:** `docs/specs/2026-07-02-loop-dashboard-task-breakdown.md` (T-020, T-021, T-022)
**Target codebase:** `/Users/herujokoutomo/Code/piramid`
**Baseline:** `go test ./...` → 163 passed in 23 packages (green before M2)

## Objective

Persist runtime loop state — **fires** and **gates** — in SQLite while preserving
all existing goal/task/attempt persistence. M2 delivers durable storage plus the
store-interface surface that M3 (loop scheduler), M4 (mid-run gate), and M5 (gate
decisions) will consume. No scheduler, runner, or API behavior changes in M2.

Primary proof: `internal/store/sqlite` tests for create/list fires and
create/get/list-open/resolve gates, plus event-row assertions.

## Constraints (locked from approved design)

- SQLite is runtime state only; definitions stay file-backed.
- Forward-only embedded migrations under `migrations/` (`//go:embed *.sql`),
  applied by `internal/store/sqlite/migrate.go` in numeric-prefix order, each
  wrapped in a transaction and recorded in `schema_migrations`.
- All timestamps stored as RFC3339Nano UTC strings via `formatTime` / `parseTime`.
- Every state transition emits an `events` row via `appendEvent(tx, ...)`.
- `Store` interface in `internal/store/store.go` is the single boundary; the
  sqlite package implements it and asserts `var _ storepkg.Store = (*Store)(nil)`.
- Domain types already exist in `internal/domain/loop.go` (`Fire`, `FireStatus`,
  `Gate`, `GateStatus`, `GateDecision`, `GateContext`, `GateThread`). Do NOT
  redefine them.
- `ErrInvalidState` already exists in `internal/store/store.go`; reuse it for
  illegal transitions (e.g. resolving a non-open gate).

## Existing patterns to mirror

- Migration files: `migrations/001_initial.sql`, `migrations/002_attempt_verifier_logs.sql`.
- Store methods per entity in dedicated files: `goals.go`, `tasks.go`,
  `attempts.go`, `operations.go`, `events.go`.
- Transaction shape: `BeginTx` → `defer tx.Rollback()` → mutate → `appendEvent` →
  `tx.Commit()`.
- Event emission: `appendEvent(ctx, tx, entityType, entityID, eventType, payload, now)`.
- Row-affected guard returning `sql.ErrNoRows` when an update touches zero rows
  (see `UpdateGoalStatus`).
- Time helpers `formatTime` / `parseTime` in `events.go`.
- Test harness: `openTestStore(t)` in `store_test.go`, table-driven where useful.

---

## Work breakdown

The three tasks are best landed as **two PRs**:

- **PR-A** = T-020 migration only (schema lands first so store code can compile
  against real tables).
- **PR-B** = T-021 + T-022 store methods + tests (they share the migration and
  the same test file; splitting them adds churn without isolation benefit).

Fires and gates methods are independent of each other, so within PR-B they can be
implemented in either order or in parallel by two coders (see Parallelization).

---

### T-020 — Fires/gates migration  (PR-A)

**File:** `migrations/003_fires_and_gates.sql` (new)

**Step 1 — `fires` table.** Columns per design Phase 2:

```sql
CREATE TABLE IF NOT EXISTS fires (
    id TEXT PRIMARY KEY,
    loop_id TEXT NOT NULL,
    goal_id TEXT REFERENCES goals(id),
    status TEXT NOT NULL,
    scheduled_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

- `loop_id` is a plain TEXT (loops are file-backed, not a DB table — no FK).
- `goal_id` is nullable FK; a fire is created before its goal in some flows, so
  keep it nullable and set on link.

**Step 2 — `gates` table.** Embed thread/front-matter payload as JSON for Phase 1
(the task breakdown defaults to embedded JSON; no separate `gate_threads` table):

```sql
CREATE TABLE IF NOT EXISTS gates (
    id TEXT PRIMARY KEY,
    fire_id TEXT NOT NULL REFERENCES fires(id),
    goal_id TEXT REFERENCES goals(id),
    task_id TEXT REFERENCES tasks(id),
    attempt_id INTEGER REFERENCES attempts(id),
    status TEXT NOT NULL,
    context_path TEXT NOT NULL,
    context_json TEXT NOT NULL DEFAULT '{}',
    context_body TEXT NOT NULL DEFAULT '',
    decision TEXT,
    decision_note TEXT NOT NULL DEFAULT '',
    opened_at TEXT NOT NULL,
    resolved_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

- `context_json` holds the marshaled `domain.GateContext` front-matter.
- `context_body` holds the preserved Markdown body.
- `decision` is nullable until the gate is resolved.

**Step 3 — indexes** (mirror the `idx_*` naming in `001_initial.sql`):

```sql
CREATE INDEX IF NOT EXISTS idx_fires_loop_scheduled ON fires(loop_id, scheduled_at);
CREATE INDEX IF NOT EXISTS idx_fires_status ON fires(status);
CREATE INDEX IF NOT EXISTS idx_gates_status ON gates(status);
CREATE INDEX IF NOT EXISTS idx_gates_fire ON gates(fire_id);
CREATE INDEX IF NOT EXISTS idx_gates_goal ON gates(goal_id);
```

**Validation / acceptance:**

- Fresh DB migrates: covered implicitly by every test using `openTestStore` (which
  runs `migrate` on open). Add an explicit test that `SELECT` on `fires`/`gates`
  succeeds after open.
- Existing DB migrates: the numeric-prefix + `schema_migrations` guard makes 003
  apply exactly once on an already-migrated DB. Add a test that opens a store,
  closes, reopens against the same file, and confirms no error and no duplicate
  application.
- Idempotent: `IF NOT EXISTS` on tables/indexes + version guard.

---

### T-021 — Store interface for fires  (PR-B)

**Files:** `internal/store/store.go` (interface + input structs),
`internal/store/sqlite/fires.go` (new), `internal/store/sqlite/store_test.go` (tests).

**Step 1 — interface additions** in `store.go`:

```go
CreateFire(ctx context.Context, fire domain.Fire) (domain.Fire, error)
UpdateFireStatus(ctx context.Context, id string, status domain.FireStatus, now time.Time) error
ListFires(ctx context.Context, loopID string, limit int) ([]domain.Fire, error)
GetLatestFireByLoop(ctx context.Context, loopID string) (domain.Fire, error)
```

- `ListFires` with empty `loopID` lists across all loops (ordered
  `scheduled_at DESC, id DESC`), honoring `limit` (0 → sensible default, e.g. 100).
- `GetLatestFireByLoop` returns `sql.ErrNoRows` when the loop has never fired
  (M3 scheduler uses this for due-detection).

**Step 2 — `fires.go` implementation.** Mirror `goals.go`:

- `CreateFire`: default `CreatedAt`/`UpdatedAt`/`Status` (→ `FIRE_SCHEDULED`) when
  zero; insert; `appendEvent(tx, "fire", fire.ID, "FIRE_CREATED", payload, now)`;
  commit; return stored fire.
- `UpdateFireStatus`: update `status` + `updated_at` (and `started_at`/`finished_at`
  when the target status implies them — set `started_at` on `FIRE_RUNNING` if
  currently null, `finished_at` on terminal statuses `FIRE_DONE`/`FIRE_REJECTED`/
  `FIRE_DEFERRED`/`FIRE_FAILED`); guard `RowsAffected == 1` else `sql.ErrNoRows`;
  emit `FIRE_STATUS_CHANGED` with `{status}`. Emit the design's named events
  (`fire.started`, `fire.done`, etc.) as `event_type` where the transition maps
  cleanly; otherwise the generic `FIRE_STATUS_CHANGED` is acceptable for M2 — keep
  event_type values consistent and documented in a small map at top of file.
- `ListFires` / `GetLatestFireByLoop`: `SELECT` + `scanFire(rows)` helper using
  `parseTime` and nullable handling (`sql.NullString` for `goal_id`,
  `started_at`, `finished_at`).

**Acceptance:**

- Test create → list returns the fire.
- Test status update transitions and emits an event row (`entity_type = "fire"`).
- Test update on unknown fire id returns `sql.ErrNoRows`.
- Test `GetLatestFireByLoop` orders by `scheduled_at DESC` and returns latest.

---

### T-022 — Store interface for gates  (PR-B)

**Files:** `internal/store/store.go`, `internal/store/sqlite/gates.go` (new),
`internal/store/sqlite/store_test.go`.

**Step 1 — input struct + interface** in `store.go`:

```go
type ResolveGateInput struct {
    ID       string
    Decision domain.GateDecision
    Note     string
    Now      time.Time
}

CreateGate(ctx context.Context, gate domain.Gate) (domain.Gate, error)
GetGate(ctx context.Context, id string) (domain.Gate, error)
ListOpenGates(ctx context.Context) ([]domain.Gate, error)
ResolveGate(ctx context.Context, in ResolveGateInput) error
```

**Step 2 — `gates.go` implementation:**

- `CreateGate`: marshal `gate.Context` (front-matter fields) → `context_json`;
  store `gate.Context.Body` → `context_body`; default status `GATE_OPEN`,
  `OpenedAt`/`CreatedAt`/`UpdatedAt`; insert with nullable `goal_id`/`task_id`/
  `attempt_id`; `appendEvent(tx, "gate", gate.ID, "gate.opened", payload, now)`;
  return stored gate.
- `GetGate`: select by id; unmarshal `context_json` back into `domain.GateContext`,
  reattach `context_body` to `.Body`; return `sql.ErrNoRows` if missing.
- `ListOpenGates`: `WHERE status = 'GATE_OPEN' ORDER BY opened_at ASC`.
- `ResolveGate`: within a tx, read current status; if not `GATE_OPEN` return
  `storepkg.ErrInvalidState`; map decision → resolved status
  (`approve`/`route` → `GATE_RESOLVED`, `defer` → `GATE_DEFERRED`,
  `reject` → `GATE_REJECTED`); update `status`, `decision`, `decision_note`,
  `resolved_at`, `updated_at`; `appendEvent(tx, "gate", id, "gate.resolved",
  {decision, note}, now)`; commit.

**Acceptance:**

- Test create → get round-trips context front-matter + body exactly.
- Test `ListOpenGates` returns only open gates.
- Test resolve on an open gate closes it, persists decision + note, emits
  `gate.resolved` event.
- Test resolve on an already-resolved gate returns `ErrInvalidState`.
- Test get/resolve on unknown id returns `sql.ErrNoRows`.

---

## Sequencing & dependencies

```
T-020 (migration)  ──►  T-021 (fires methods)  ──►  tests
                   └─►  T-022 (gates methods)  ──►  tests
```

- T-020 must land (or at least exist in the branch) before T-021/T-022 compile.
- T-021 and T-022 are mutually independent.
- No dependency on M1 definitions loader beyond the already-merged domain types.

## Parallelization

- Single coder: do T-020, then T-021, then T-022 sequentially — low coordination
  cost, one test file.
- Two coders (optional): after T-020 lands, one takes fires (`fires.go`), one
  takes gates (`gates.go`). Shared edits are limited to `store.go` (interface) and
  `store_test.go`; coordinate those two files to avoid merge conflicts, or have
  one coder own both shared files' additions.

Recommendation: **single coder, sequential.** The surface is small and the shared
files (`store.go`, `store_test.go`) make parallel work more conflict-prone than
it's worth.

## Risks & mitigations

- **Nullable columns mishandled in scans** → use `sql.NullString`/`sql.NullInt64`
  and convert explicitly; add round-trip tests that exercise null `goal_id`/
  `attempt_id`.
- **Event naming drift** (`FIRE_STATUS_CHANGED` vs `fire.started`) → the design
  lists dotted event names; the existing code uses SCREAMING_SNAKE. Pick dotted
  names for the new fire/gate events to match the design's SSE contract that M6
  will consume, and document the chosen names at the top of `fires.go`/`gates.go`.
  This is the one naming decision to confirm — flag if the existing SSE consumers
  assume SCREAMING_SNAKE.
- **Interface assertion breaks compile** → adding methods to the `Store` interface
  forces every implementer to satisfy it. Only `sqlite.Store` implements it, and
  any test doubles must gain the new methods. Grep for other `storepkg.Store`
  implementers before landing the interface change.

## Validation checklist (definition of done for M2)

- [ ] `migrations/003_fires_and_gates.sql` creates `fires`, `gates`, and all five
      indexes; fresh + reopened DB both migrate cleanly and idempotently.
- [ ] `internal/store/store.go` exposes the four fire methods and four gate
      methods (plus `ResolveGateInput`).
- [ ] `internal/store/sqlite/fires.go` and `gates.go` implement them; interface
      assertion still compiles.
- [ ] New tests in `store_test.go` cover: create/list/status-update/latest for
      fires; create/get/list-open/resolve + invalid-state + not-found for gates;
      event rows asserted for each transition.
- [ ] `go test ./...` stays green (≥163 prior tests still pass, new tests added).
- [ ] Update `docs/specs/2026-07-02-loop-dashboard-task-breakdown.md`: check off
      T-020, T-021, T-022 and flip M2 to complete.
