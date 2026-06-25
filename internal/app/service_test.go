package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
