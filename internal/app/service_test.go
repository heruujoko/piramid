package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/home"
	"github.com/heruujoko/piramid/internal/intake"
	"github.com/heruujoko/piramid/internal/records"
	runtimepkg "github.com/heruujoko/piramid/internal/runtime"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

type leasedDraftRepository struct {
	intake.Repository
}

func (leasedDraftRepository) SaveGoalDraft(
	context.Context,
	domain.Goal,
	storepkg.PersistedPaths,
) error {
	return nil
}

func (leasedDraftRepository) AcquireReadLease(context.Context, string, string) error {
	return storepkg.ErrProjectLeased
}

type unusedPlanner struct{}

func (unusedPlanner) Run(
	context.Context,
	runtimepkg.Invocation,
) (runtimepkg.Result, error) {
	return runtimepkg.Result{}, errors.New("planner should not run")
}

func TestDraftGoalMapsProjectLeaseToConflict(t *testing.T) {
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	if err := home.Init(paths); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	intakeService := intake.NewService(intake.ServiceConfig{
		Repository: leasedDraftRepository{},
		Records:    records.New(paths),
		Planner:    unusedPlanner{},
		Runtime: intake.RuntimeConfig{
			Command: "pi",
			Args:    []string{"-p", "{{prompt}}"},
			Timeout: time.Minute,
		},
		IDGenerator: func() string { return "GOAL-1" },
	})
	service := NewService(intakeService, nil, nil, nil)

	_, err := service.DraftGoal(context.Background(), intake.DraftRequest{
		GoalText:    "Maintain this PR",
		ProjectPath: project,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("DraftGoal() error = %v, want ErrConflict", err)
	}
}

type gateDecisionStore struct {
	storepkg.Store
	gate          domain.Gate
	logPaths      storepkg.AttemptLogPaths
	resumed       *storepkg.ResumeGatedTaskInput
	setTaskStatus *domain.TaskStatus
	fireStatus    *domain.FireStatus
	resolved      *storepkg.ResolveGateInput
}

func (s *gateDecisionStore) GetGate(context.Context, string) (domain.Gate, error) {
	return s.gate, nil
}

func (s *gateDecisionStore) GetAttemptLogPaths(context.Context, int64) (storepkg.AttemptLogPaths, error) {
	return s.logPaths, nil
}

func (s *gateDecisionStore) ResumeGatedTask(_ context.Context, input storepkg.ResumeGatedTaskInput) error {
	copy := input
	s.resumed = &copy
	return nil
}

func (s *gateDecisionStore) SetTaskStatus(_ context.Context, _ string, status domain.TaskStatus, _ time.Time) error {
	copy := status
	s.setTaskStatus = &copy
	return nil
}

func (s *gateDecisionStore) UpdateFireStatus(_ context.Context, _ string, status domain.FireStatus, _ time.Time) error {
	copy := status
	s.fireStatus = &copy
	return nil
}

func (s *gateDecisionStore) ResolveGate(_ context.Context, input storepkg.ResolveGateInput) error {
	copy := input
	s.resolved = &copy
	return nil
}

func newGateDecisionService(t *testing.T, decisionOptions []domain.GateDecision) (*Service, *gateDecisionStore) {
	t.Helper()
	stdoutPath := filepath.Join(t.TempDir(), "stdout.log")
	stderrPath := filepath.Join(t.TempDir(), "stderr.log")
	if err := os.WriteFile(stdoutPath, []byte(strings.Repeat("old out\n", 25)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stderrPath, []byte(strings.Repeat("old err\n", 25)), 0o600); err != nil {
		t.Fatal(err)
	}
	st := &gateDecisionStore{
		gate: domain.Gate{
			ID: "GATE-1", FireID: "FIRE-1", GoalID: "GOAL-1", TaskID: "TASK-1", AttemptID: "7",
			Status: domain.GateOpen, ContextPath: filepath.Join(t.TempDir(), "gate.context.md"),
			Context: domain.GateContext{
				Gate: "human-review", Phase: "execution", LoopID: "LOOP-1", FireID: "FIRE-1",
				GoalID: "GOAL-1", TaskID: "TASK-1", Summary: "Needs direction",
				DecisionOptions: decisionOptions,
				Threads:         []domain.GateThread{{ID: "T1", Title: "Review", Location: "file.go:1", Summary: "Fix thing"}},
			},
		},
		logPaths: storepkg.AttemptLogPaths{Stdout: stdoutPath, Stderr: stderrPath},
	}
	service := NewService(nil, st, nil, nil)
	service.Now = func() time.Time { return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC) }
	return service, st
}

func TestResolveGateApproveResumesTaskAndFire(t *testing.T) {
	service, st := newGateDecisionService(t, []domain.GateDecision{
		domain.GateDecisionApprove, domain.GateDecisionRoute, domain.GateDecisionDefer, domain.GateDecisionReject,
	})

	err := service.ResolveGate(context.Background(), "GATE-1", domain.GateDecisionInput{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	if st.resumed == nil || st.resumed.TaskID != "TASK-1" {
		t.Fatalf("resume input = %#v, want TASK-1", st.resumed)
	}
	if !strings.Contains(st.resumed.RestorePrompt, "Decision: approve") || !strings.Contains(st.resumed.RestorePrompt, "Approved without note.") {
		t.Fatalf("restore prompt missing approve fallback:\n%s", st.resumed.RestorePrompt)
	}
	if st.fireStatus == nil || *st.fireStatus != domain.FireRunning {
		t.Fatalf("fire status = %#v, want FIRE_RUNNING", st.fireStatus)
	}
	if st.resolved == nil || st.resolved.Decision != domain.GateDecisionApprove {
		t.Fatalf("resolved = %#v, want approve", st.resolved)
	}
}

func TestResolveGateRouteIncludesNoteAndThreadSummary(t *testing.T) {
	service, st := newGateDecisionService(t, []domain.GateDecision{domain.GateDecisionRoute})
	err := service.ResolveGate(context.Background(), "GATE-1", domain.GateDecisionInput{Decision: "route", Note: "Use option B"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Decision: route", "Use option B", "T1", "Fix thing", "old out", "old err"} {
		if !strings.Contains(st.resumed.RestorePrompt, want) {
			t.Fatalf("restore prompt missing %q:\n%s", want, st.resumed.RestorePrompt)
		}
	}
}

func TestResolveGateDeferBlocksTaskAndDefersFire(t *testing.T) {
	service, st := newGateDecisionService(t, []domain.GateDecision{domain.GateDecisionDefer})
	if err := service.ResolveGate(context.Background(), "GATE-1", domain.GateDecisionInput{Decision: "defer"}); err != nil {
		t.Fatal(err)
	}
	if st.setTaskStatus == nil || *st.setTaskStatus != domain.TaskBlocked {
		t.Fatalf("task status = %#v, want BLOCKED", st.setTaskStatus)
	}
	if st.fireStatus == nil || *st.fireStatus != domain.FireDeferred {
		t.Fatalf("fire status = %#v, want FIRE_DEFERRED", st.fireStatus)
	}
	if st.resumed != nil {
		t.Fatalf("resume called for defer: %#v", st.resumed)
	}
	if st.resolved == nil || st.resolved.Decision != domain.GateDecisionDefer {
		t.Fatalf("resolved = %#v, want defer", st.resolved)
	}
}

func TestResolveGateRejectCancelsTaskAndRejectsFire(t *testing.T) {
	service, st := newGateDecisionService(t, []domain.GateDecision{domain.GateDecisionReject})
	if err := service.ResolveGate(context.Background(), "GATE-1", domain.GateDecisionInput{Decision: "reject"}); err != nil {
		t.Fatal(err)
	}
	if st.setTaskStatus == nil || *st.setTaskStatus != domain.TaskCancelled {
		t.Fatalf("task status = %#v, want CANCELLED", st.setTaskStatus)
	}
	if st.fireStatus == nil || *st.fireStatus != domain.FireRejected {
		t.Fatalf("fire status = %#v, want FIRE_REJECTED", st.fireStatus)
	}
	if st.resolved == nil || st.resolved.Decision != domain.GateDecisionReject {
		t.Fatalf("resolved = %#v, want reject", st.resolved)
	}
}

func TestResolveGateRejectsDecisionNotAllowedByGate(t *testing.T) {
	service, st := newGateDecisionService(t, []domain.GateDecision{domain.GateDecisionApprove})
	err := service.ResolveGate(context.Background(), "GATE-1", domain.GateDecisionInput{Decision: "reject"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	if st.resolved != nil || st.resumed != nil || st.fireStatus != nil || st.setTaskStatus != nil {
		t.Fatalf("state changed despite invalid decision: %#v", st)
	}
}
