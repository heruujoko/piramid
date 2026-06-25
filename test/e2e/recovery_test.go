package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/api"
	"github.com/heruujoko/piramid/internal/bootstrap"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/home"
	storepkg "github.com/heruujoko/piramid/internal/store"
	sqlitestore "github.com/heruujoko/piramid/internal/store/sqlite"
)

func TestRestartPreservesInterruptedAttemptAndCompletesNewAttempts(t *testing.T) {
	fakePi := buildFakePi(t)
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	configureE2E(t, paths, fakePi)
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}

	st, err := sqlitestore.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{
		ID: "E2E-TASK", Title: "Recovered task",
		Goal:        "Create result.txt with verified content",
		ProjectPath: project,
		Outputs:     []domain.Output{{Type: "file", Path: "result.txt"}},
		DOD:         []string{"result.txt contains verified"},
		MaxAttempts: 3, Timeout: 30 * time.Second, TimeoutText: "30s",
	}
	goal := domain.Goal{
		ID: "GOAL-RECOVERY", Text: "recover work", ProjectPath: project,
		Status: domain.GoalConfirmed, CreatedAt: time.Now().UTC(),
	}
	if err := st.AdmitPlan(context.Background(), goal,
		domain.Plan{Version: 1, GoalID: goal.ID, Tasks: []domain.Task{task}},
		storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}
	first, err := st.StartAttempt(context.Background(), storepkg.StartAttemptInput{
		TaskID: task.ID, WorkerID: "pi-worker-01", Runtime: "command", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	port := 0
	running, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Paths: paths, Host: "127.0.0.1", Port: &port,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close(context.Background())
	client := api.NewClient("http://"+running.ListenAddress(), 5*time.Second)
	completed := waitForTask(t, client, task.ID, domain.TaskCompleted)
	if len(completed.Attempts) != 3 {
		t.Fatalf("attempts = %#v", completed.Attempts)
	}
	if completed.Attempts[0].ID != first.ID ||
		completed.Attempts[0].Status != domain.AttemptInterrupted {
		t.Fatalf("first attempt = %#v", completed.Attempts[0])
	}
	if completed.Attempts[2].Verification == nil ||
		completed.Attempts[2].Verification.Status != domain.VerificationPass {
		t.Fatalf("final attempt = %#v", completed.Attempts[2])
	}
}
