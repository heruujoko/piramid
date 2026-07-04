package records

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/home"
)

func newTestStore(t *testing.T) (*Store, home.Paths) {
	t.Helper()
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	if err := home.Init(paths); err != nil {
		t.Fatal(err)
	}
	return New(paths), paths
}

func TestWritesGoalPlanTaskAndVerificationToExpectedPaths(t *testing.T) {
	st, paths := newTestStore(t)
	goal := domain.Goal{
		ID:          "GOAL-1",
		Text:        "maintain a pull request",
		ProjectPath: "/tmp/project",
		Status:      domain.GoalDraft,
		CreatedAt:   time.Date(2026, 6, 25, 1, 2, 3, 0, time.UTC),
	}
	plan := domain.Plan{Version: 1, GoalID: goal.ID}
	task := domain.Task{ID: "TASK-1", Goal: "do work", ProjectPath: "/tmp/project"}

	goalRecord, err := st.WriteGoal(goal)
	if err != nil {
		t.Fatal(err)
	}
	if goalRecord.Path != filepath.Join(paths.Goals, "GOAL-1", "goal.yaml") {
		t.Fatalf("goal path = %q", goalRecord.Path)
	}
	planRecord, err := st.WritePlan(goal.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	if planRecord.Path != filepath.Join(paths.Goals, "GOAL-1", "generated-plan.yaml") {
		t.Fatalf("plan path = %q", planRecord.Path)
	}
	taskRecord, err := st.WriteTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if taskRecord.Path != filepath.Join(paths.Tasks, "TASK-1", "task.yaml") {
		t.Fatalf("task path = %q", taskRecord.Path)
	}

	attempt, err := st.CreateAttempt(task.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Root != filepath.Join(paths.Attempts, "TASK-1", "0001") {
		t.Fatalf("attempt root = %q", attempt.Root)
	}
	if attempt.GateContext != filepath.Join(attempt.Root, "gate.context.md") {
		t.Fatalf("gate context path = %q", attempt.GateContext)
	}
	if info, err := os.Stat(attempt.Root); err != nil || !info.IsDir() {
		t.Fatalf("attempt root dir not created: %v", err)
	}
	report, err := st.WriteVerification(attempt, domain.Verification{
		Status:  domain.VerificationPass,
		Reasons: []string{"done"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Path != filepath.Join(attempt.Root, "verification.yaml") {
		t.Fatalf("verification path = %q", report.Path)
	}
}

func TestImmutableWriteRejectsDifferentContent(t *testing.T) {
	st, _ := newTestStore(t)
	task := domain.Task{ID: "TASK-1", Goal: "first", ProjectPath: "/tmp/project"}
	if _, err := st.WriteTask(task); err != nil {
		t.Fatal(err)
	}
	task.Goal = "changed"

	if _, err := st.WriteTask(task); err == nil {
		t.Fatal("WriteTask() replaced immutable content")
	}
}

func TestFileRecordHashAndSizeMatchPersistedContent(t *testing.T) {
	st, _ := newTestStore(t)
	record, err := st.WriteTask(domain.Task{
		ID:          "TASK-1",
		Goal:        "do work",
		ProjectPath: "/tmp/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(record.Path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(content)) != record.Size {
		t.Fatalf("size = %d, want %d", record.Size, len(content))
	}
	if Hash(content) != record.SHA256 {
		t.Fatalf("hash = %q, want %q", record.SHA256, Hash(content))
	}
}

func TestIdentifiersCannotEscapeHome(t *testing.T) {
	st, _ := newTestStore(t)
	for _, id := range []string{"../escape", "nested/task", "", "."} {
		t.Run(id, func(t *testing.T) {
			_, err := st.WriteTask(domain.Task{ID: id, Goal: "work", ProjectPath: "/tmp/project"})
			if err == nil {
				t.Fatalf("WriteTask(%q) succeeded", id)
			}
		})
	}
}

func TestAtomicWriteLeavesNoTemporarySibling(t *testing.T) {
	st, _ := newTestStore(t)
	record, err := st.WriteTask(domain.Task{
		ID:          "TASK-1",
		Goal:        "do work",
		ProjectPath: "/tmp/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(record.Path + ".tmp-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
}
