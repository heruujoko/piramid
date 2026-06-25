package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return st
}

func testGoalPlan() (domain.Goal, domain.Plan) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	goal := domain.Goal{
		ID:          "GOAL-1",
		Text:        "Maintain a pull request",
		ProjectPath: "/tmp/project",
		Status:      domain.GoalConfirmed,
		CreatedAt:   now,
	}
	plan := domain.Plan{
		Version: 1,
		GoalID:  goal.ID,
		Tasks: []domain.Task{
			{
				ID:          "TASK-1",
				Title:       "Inspect",
				Goal:        "Inspect the pull request",
				ProjectPath: "/tmp/project",
				DOD:         []string{"inspection complete"},
				MaxAttempts: 3,
				TimeoutText: "1h",
			},
			{
				ID:          "TASK-2",
				Title:       "Fix",
				Goal:        "Fix the pull request",
				ProjectPath: "/tmp/project",
				DOD:         []string{"checks pass"},
				MaxAttempts: 3,
				TimeoutText: "2h",
				DependsOn:   []string{"TASK-1"},
			},
		},
	}
	return goal, plan
}

func TestOpenEnablesSQLiteSafetyPragmas(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	var foreignKeys int
	if err := st.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	var journalMode string
	if err := st.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(journalMode) != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var busyTimeout int
	if err := st.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatal(err)
	}
	if busyTimeout < 1000 {
		t.Fatalf("busy_timeout = %d, want at least 1000", busyTimeout)
	}
}

func TestAdmitPlanPersistsGoalTasksAndDependenciesAtomically(t *testing.T) {
	st := openTestStore(t)
	goal, plan := testGoalPlan()
	paths := storepkg.PersistedPaths{
		GoalPath: "/state/goals/GOAL-1/goal.yaml",
		PlanPath: "/state/goals/GOAL-1/generated-plan.yaml",
		TaskPaths: map[string]string{
			"TASK-1": "/state/tasks/TASK-1/task.yaml",
			"TASK-2": "/state/tasks/TASK-2/task.yaml",
		},
		TaskHashes: map[string]string{
			"TASK-1": "hash-1",
			"TASK-2": "hash-2",
		},
	}

	if err := st.AdmitPlan(context.Background(), goal, plan, paths); err != nil {
		t.Fatal(err)
	}

	task, err := st.GetTask(context.Background(), "TASK-2")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskPending {
		t.Fatalf("status = %s, want PENDING", task.Status)
	}
	if len(task.Dependencies) != 1 || task.Dependencies[0] != "TASK-1" {
		t.Fatalf("dependencies = %#v", task.Dependencies)
	}
	if task.Timeout != 2*time.Hour {
		t.Fatalf("timeout = %s, want 2h", task.Timeout)
	}
}

func TestAdmitPlanAllowsChildBeforeParent(t *testing.T) {
	st := openTestStore(t)
	goal, plan := testGoalPlan()
	plan.Tasks[0].ParentTaskID = "TASK-2"

	if err := st.AdmitPlan(context.Background(), goal, plan, storepkg.PersistedPaths{}); err != nil {
		t.Fatalf("AdmitPlan() error = %v", err)
	}
}

func TestAdmitPlanRollsBackOnDuplicateTask(t *testing.T) {
	st := openTestStore(t)
	goal, plan := testGoalPlan()
	plan.Tasks[1].ID = "TASK-1"

	err := st.AdmitPlan(context.Background(), goal, plan, storepkg.PersistedPaths{})
	if err == nil {
		t.Fatal("AdmitPlan() error = nil")
	}

	var count int
	if err := st.db.QueryRow("SELECT COUNT(*) FROM goals").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("goal rows = %d, want 0", count)
	}
}

func TestDependencyForeignKeyRejectsUnknownTask(t *testing.T) {
	st := openTestStore(t)

	_, err := st.db.Exec(
		"INSERT INTO task_dependencies(task_id, depends_on_task_id) VALUES (?, ?)",
		"TASK-1", "TASK-404",
	)
	if err == nil {
		t.Fatal("foreign-key violating insert succeeded")
	}
}

func TestStartAttemptIsUniqueAndRecordsTransitionEvent(t *testing.T) {
	st := openTestStore(t)
	goal, plan := testGoalPlan()
	if err := st.AdmitPlan(context.Background(), goal, plan, storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}

	input := storepkg.StartAttemptInput{
		TaskID:     "TASK-1",
		WorkerID:   "worker-1",
		Runtime:    "pi-cli",
		PromptPath: "/attempt/prompt.md",
		PromptHash: "prompt-hash",
		StdoutPath: "/attempt/stdout.log",
		StderrPath: "/attempt/stderr.log",
		StartedAt:  time.Now().UTC(),
	}
	attempt, err := st.StartAttempt(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.AttemptNumber != 1 {
		t.Fatalf("attempt number = %d, want 1", attempt.AttemptNumber)
	}

	_, err = st.db.Exec(`
		INSERT INTO attempts(
			task_id, attempt_number, worker_id, status, runtime, model,
			prompt_path, prompt_hash, stdout_path, stderr_path, started_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "TASK-1", 1, "worker-2", "RUNNING", "pi-cli", "", "", "", "", "", time.Now().UTC())
	if err == nil {
		t.Fatal("duplicate attempt number insert succeeded")
	}

	var status string
	if err := st.db.QueryRow("SELECT status FROM tasks WHERE id = ?", "TASK-1").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != string(domain.TaskRunning) {
		t.Fatalf("task status = %s, want RUNNING", status)
	}

	var eventCount int
	if err := st.db.QueryRow(
		"SELECT COUNT(*) FROM events WHERE entity_id = ? AND event_type = ?",
		"TASK-1", "TASK_STARTED",
	).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("start events = %d, want 1", eventCount)
	}
}

func TestFailedVerificationBecomesRunnableAtRetryTime(t *testing.T) {
	st := openTestStore(t)
	goal, plan := testGoalPlan()
	plan.Tasks = plan.Tasks[:1]
	if err := st.AdmitPlan(context.Background(), goal, plan, storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	first, err := st.StartAttempt(context.Background(), storepkg.StartAttemptInput{
		TaskID:    "TASK-1",
		WorkerID:  "worker-1",
		Runtime:   "pi-cli",
		StartedAt: startedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MoveToVerification(context.Background(), storepkg.FinishExecutionInput{
		TaskID:     "TASK-1",
		AttemptID:  first.ID,
		FinishedAt: startedAt.Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	retryAt := startedAt.Add(5 * time.Minute)
	if err := st.FinishVerification(context.Background(), storepkg.FinishVerificationInput{
		TaskID:    "TASK-1",
		AttemptID: first.ID,
		Verification: domain.Verification{
			Status:      domain.VerificationFail,
			Reasons:     []string{"checks failed"},
			RetryPrompt: "fix the checks",
		},
		NextRunAt:  retryAt,
		FinishedAt: startedAt.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}

	before, err := st.ListRunnable(context.Background(), retryAt.Add(-time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 0 {
		t.Fatalf("runnable before retry = %d, want 0", len(before))
	}

	ready, err := st.ListRunnable(context.Background(), retryAt, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready) != 1 || ready[0].ID != "TASK-1" {
		t.Fatalf("runnable at retry = %#v", ready)
	}

	second, err := st.StartAttempt(context.Background(), storepkg.StartAttemptInput{
		TaskID:    "TASK-1",
		WorkerID:  "worker-2",
		Runtime:   "pi-cli",
		StartedAt: retryAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.AttemptNumber != 2 {
		t.Fatalf("attempt number = %d, want 2", second.AttemptNumber)
	}
}

func TestReconcileBlockedMarksDescendantsOfTerminalFailure(t *testing.T) {
	st := openTestStore(t)
	goal, plan := testGoalPlan()
	if err := st.AdmitPlan(context.Background(), goal, plan, storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(
		"UPDATE tasks SET status = ? WHERE id = ?",
		domain.TaskFailed, "TASK-1",
	); err != nil {
		t.Fatal(err)
	}

	count, err := st.ReconcileBlocked(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("blocked count = %d, want 1", count)
	}
	task, err := st.GetTask(context.Background(), "TASK-2")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskBlocked {
		t.Fatalf("status = %s, want BLOCKED", task.Status)
	}
}

func TestListRunnableUsesDeterministicEnqueueOrder(t *testing.T) {
	st := openTestStore(t)
	goal, plan := testGoalPlan()
	plan.Tasks[1].DependsOn = nil
	if err := st.AdmitPlan(context.Background(), goal, plan, storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}
	runnable, err := st.ListRunnable(context.Background(), time.Now().Add(time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runnable) != 2 || runnable[0].ID != "TASK-1" || runnable[1].ID != "TASK-2" {
		t.Fatalf("runnable = %#v", runnable)
	}
}

func TestReopenPreservesRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	goal, plan := testGoalPlan()
	if err := st.AdmitPlan(context.Background(), goal, plan, storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	task, err := reopened.GetTask(context.Background(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "TASK-1" {
		t.Fatalf("task ID = %q", task.ID)
	}
}

func TestGetTaskReturnsNotFound(t *testing.T) {
	st := openTestStore(t)
	_, err := st.GetTask(context.Background(), "missing")
	if err == nil || err != sql.ErrNoRows {
		t.Fatalf("GetTask() error = %v, want sql.ErrNoRows", err)
	}
}
