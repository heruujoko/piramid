package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
	"github.com/heruujoko/piramid/migrations"
)

func TestMigration003UpgradesExistingDatabaseAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_initial.sql", "002_attempt_verifier_logs.sql"} {
		content, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(content)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (1, '2026-07-03T00:00:00Z'), (2, '2026-07-03T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after migration: %v", err)
	}
	defer reopened.Close()

	var applied int
	if err := reopened.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 3").Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("version 3 applied count = %d, want 1", applied)
	}
}

func TestMigration004AddsTaskResumePrompt(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	var columnName string
	err := st.db.QueryRowContext(ctx, `
		SELECT name FROM pragma_table_info('tasks') WHERE name = 'resume_prompt'
	`).Scan(&columnName)
	if err != nil {
		t.Fatalf("tasks.resume_prompt missing: %v", err)
	}
	if columnName != "resume_prompt" {
		t.Fatalf("column = %q, want resume_prompt", columnName)
	}
}

func testFire(id, loopID string, scheduledAt time.Time) domain.Fire {
	return domain.Fire{
		ID:          id,
		LoopID:      loopID,
		Status:      domain.FireScheduled,
		ScheduledAt: scheduledAt,
	}
}

func TestCreateFireThenListReturnsFire(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	sched := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)

	created, err := st.CreateFire(ctx, testFire("FIRE-1", "loop-a", sched))
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != domain.FireScheduled {
		t.Fatalf("status = %s, want FIRE_SCHEDULED", created.Status)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("CreatedAt/UpdatedAt should be set")
	}

	fires, err := st.ListFires(ctx, "loop-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 1 || fires[0].ID != "FIRE-1" {
		t.Fatalf("fires = %#v", fires)
	}
	if !fires[0].ScheduledAt.Equal(sched) {
		t.Fatalf("scheduled_at = %v, want %v", fires[0].ScheduledAt, sched)
	}

	var eventCount int
	if err := st.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events WHERE entity_type = ? AND entity_id = ? AND event_type = ?",
		"fire", "FIRE-1", "FIRE_CREATED",
	).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("FIRE_CREATED events = %d, want 1", eventCount)
	}
}

func TestUpdateFireStatusTransitionsAndEmitsEvent(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	sched := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	if _, err := st.CreateFire(ctx, testFire("FIRE-1", "loop-a", sched)); err != nil {
		t.Fatal(err)
	}

	runAt := sched.Add(time.Minute)
	if err := st.UpdateFireStatus(ctx, "FIRE-1", domain.FireRunning, runAt); err != nil {
		t.Fatal(err)
	}
	fires, err := st.ListFires(ctx, "loop-a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if fires[0].Status != domain.FireRunning {
		t.Fatalf("status = %s, want FIRE_RUNNING", fires[0].Status)
	}
	if !fires[0].StartedAt.Equal(runAt) {
		t.Fatalf("started_at = %v, want %v", fires[0].StartedAt, runAt)
	}

	doneAt := sched.Add(2 * time.Minute)
	if err := st.UpdateFireStatus(ctx, "FIRE-1", domain.FireDone, doneAt); err != nil {
		t.Fatal(err)
	}
	fires, _ = st.ListFires(ctx, "loop-a", 10)
	if !fires[0].FinishedAt.Equal(doneAt) {
		t.Fatalf("finished_at = %v, want %v", fires[0].FinishedAt, doneAt)
	}
	if !fires[0].StartedAt.Equal(runAt) {
		t.Fatalf("started_at overwritten = %v, want %v", fires[0].StartedAt, runAt)
	}

	var eventCount int
	if err := st.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events WHERE entity_type = ? AND entity_id = ? AND event_type = ?",
		"fire", "FIRE-1", "FIRE_STATUS_CHANGED",
	).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 {
		t.Fatalf("FIRE_STATUS_CHANGED events = %d, want 2", eventCount)
	}
}

func TestUpdateFireStatusUnknownIDReturnsNotFound(t *testing.T) {
	st := openTestStore(t)
	err := st.UpdateFireStatus(context.Background(), "missing", domain.FireRunning, time.Now().UTC())
	if err != sql.ErrNoRows {
		t.Fatalf("error = %v, want sql.ErrNoRows", err)
	}
}

func TestGetLatestFireByLoopReturnsMostRecentlyScheduled(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	if _, err := st.CreateFire(ctx, testFire("FIRE-1", "loop-a", base)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateFire(ctx, testFire("FIRE-2", "loop-a", base.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateFire(ctx, testFire("FIRE-3", "loop-b", base.Add(2*time.Hour))); err != nil {
		t.Fatal(err)
	}

	latest, err := st.GetLatestFireByLoop(ctx, "loop-a")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != "FIRE-2" {
		t.Fatalf("latest = %s, want FIRE-2", latest.ID)
	}
}

func TestGetLatestFireByLoopOrdersVariableWidthRFC3339NanoTimestamps(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)
	if _, err := st.CreateFire(ctx, testFire("FIRE-OLDER", "loop-a", base)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateFire(ctx, testFire("FIRE-LATEST", "loop-a", base.Add(time.Nanosecond))); err != nil {
		t.Fatal(err)
	}

	latest, err := st.GetLatestFireByLoop(ctx, "loop-a")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != "FIRE-LATEST" {
		t.Fatalf("latest = %s, want FIRE-LATEST", latest.ID)
	}

	fires, err := st.ListFires(ctx, "loop-a", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 2 || fires[0].ID != "FIRE-LATEST" || fires[1].ID != "FIRE-OLDER" {
		t.Fatalf("fires = %#v, want latest before older", fires)
	}
}

func TestGetLatestFireByLoopUnknownReturnsNotFound(t *testing.T) {
	st := openTestStore(t)
	_, err := st.GetLatestFireByLoop(context.Background(), "nope")
	if err != sql.ErrNoRows {
		t.Fatalf("error = %v, want sql.ErrNoRows", err)
	}
}

func seedGate(t *testing.T, st *Store, id string) domain.Gate {
	t.Helper()
	ctx := context.Background()
	if _, err := st.CreateFire(ctx, testFire("FIRE-1", "loop-a", time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC))); err != nil {
		// fire may already exist from a prior call; ignore duplicate
		_ = err
	}
	gate := domain.Gate{
		ID:          id,
		FireID:      "FIRE-1",
		Status:      domain.GateOpen,
		ContextPath: "/state/gates/" + id + "/context.md",
		Context: domain.GateContext{
			Gate:            "plan_review",
			Phase:           "planning",
			LoopID:          "loop-a",
			FireID:          "FIRE-1",
			Summary:         "Review the generated plan",
			DecisionOptions: []domain.GateDecision{domain.GateDecisionApprove, domain.GateDecisionReject},
			Body:            "## Plan\n\nDetails here.",
		},
	}
	created, err := st.CreateGate(ctx, gate)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func TestCreateGateThenGetRoundTripsContext(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	seedGate(t, st, "GATE-1")

	got, err := st.GetGate(ctx, "GATE-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.GateOpen {
		t.Fatalf("status = %s, want GATE_OPEN", got.Status)
	}
	if got.Context.Summary != "Review the generated plan" {
		t.Fatalf("summary = %q", got.Context.Summary)
	}
	if got.Context.Body != "## Plan\n\nDetails here." {
		t.Fatalf("body = %q", got.Context.Body)
	}
	if len(got.Context.DecisionOptions) != 2 {
		t.Fatalf("decision options = %#v", got.Context.DecisionOptions)
	}

	var eventCount int
	if err := st.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events WHERE entity_type = ? AND entity_id = ? AND event_type = ?",
		"gate", "GATE-1", "GATE_OPENED",
	).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("GATE_OPENED events = %d, want 1", eventCount)
	}
}

func TestListOpenGatesReturnsOnlyOpen(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	seedGate(t, st, "GATE-1")
	seedGate(t, st, "GATE-2")
	if err := st.ResolveGate(ctx, storepkg.ResolveGateInput{
		ID: "GATE-2", Decision: domain.GateDecisionApprove, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	open, err := st.ListOpenGates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].ID != "GATE-1" {
		t.Fatalf("open gates = %#v", open)
	}
}

func TestResolveGateClosesAndPersistsDecision(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	seedGate(t, st, "GATE-1")
	resolveAt := time.Date(2026, 7, 3, 10, 0, 0, 0, time.UTC)
	if err := st.ResolveGate(ctx, storepkg.ResolveGateInput{
		ID: "GATE-1", Decision: domain.GateDecisionReject, Note: "not now", Now: resolveAt,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.GetGate(ctx, "GATE-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.GateRejected {
		t.Fatalf("status = %s, want GATE_REJECTED", got.Status)
	}
	if got.Decision != domain.GateDecisionReject {
		t.Fatalf("decision = %s", got.Decision)
	}
	if got.DecisionNote != "not now" {
		t.Fatalf("note = %q", got.DecisionNote)
	}
	if !got.ResolvedAt.Equal(resolveAt) {
		t.Fatalf("resolved_at = %v, want %v", got.ResolvedAt, resolveAt)
	}

	var eventCount int
	if err := st.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events WHERE entity_type = ? AND entity_id = ? AND event_type = ?",
		"gate", "GATE-1", "GATE_RESOLVED",
	).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("GATE_RESOLVED events = %d, want 1", eventCount)
	}
}

func TestResolveGateOnResolvedReturnsInvalidState(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	seedGate(t, st, "GATE-1")
	if err := st.ResolveGate(ctx, storepkg.ResolveGateInput{
		ID: "GATE-1", Decision: domain.GateDecisionApprove, Now: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	err := st.ResolveGate(ctx, storepkg.ResolveGateInput{
		ID: "GATE-1", Decision: domain.GateDecisionApprove, Now: time.Now().UTC(),
	})
	if err != storepkg.ErrInvalidState {
		t.Fatalf("error = %v, want ErrInvalidState", err)
	}
}

func TestGetGateUnknownReturnsNotFound(t *testing.T) {
	st := openTestStore(t)
	_, err := st.GetGate(context.Background(), "missing")
	if err != sql.ErrNoRows {
		t.Fatalf("error = %v, want sql.ErrNoRows", err)
	}
}

func TestResolveGateUnknownReturnsNotFound(t *testing.T) {
	st := openTestStore(t)
	err := st.ResolveGate(context.Background(), storepkg.ResolveGateInput{
		ID: "missing", Decision: domain.GateDecisionApprove, Now: time.Now().UTC(),
	})
	if err != sql.ErrNoRows {
		t.Fatalf("error = %v, want sql.ErrNoRows", err)
	}
}

func TestMigration003CreatesFiresAndGatesTables(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	for _, table := range []string{"fires", "gates"} {
		var name string
		err := st.db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name = ?", table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}

	for _, index := range []string{
		"idx_fires_loop_scheduled",
		"idx_fires_status",
		"idx_gates_status",
		"idx_gates_fire",
		"idx_gates_goal",
	} {
		var name string
		err := st.db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='index' AND name = ?", index,
		).Scan(&name)
		if err != nil {
			t.Fatalf("index %q missing: %v", index, err)
		}
	}
}
