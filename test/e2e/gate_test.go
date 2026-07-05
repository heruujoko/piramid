package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/home"
	"github.com/heruujoko/piramid/internal/intake"
	sqlitestore "github.com/heruujoko/piramid/internal/store/sqlite"
)

func buildFakeGatingPi(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "fake-gating-pi")
	command := exec.Command("go", "build", "-o", output, "./test/e2e/fixtures/fake-gating-pi")
	command.Dir = root
	if content, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fake gating Pi: %v\n%s", err, content)
	}
	return output
}

func TestGateLifecycle(t *testing.T) {
	fakePi := buildFakeGatingPi(t)
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	configureE2E(t, paths, fakePi)
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	_, client := startE2E(t, paths)

	// 1. Draft and confirm a goal
	draft, err := client.DraftGoal(context.Background(), intake.DraftRequest{
		GoalText:    "Create a verified result that gates for human review",
		ProjectPath: project,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Plan.Tasks) != 1 || draft.Plan.Tasks[0].ID != "GATE-TASK" {
		t.Fatalf("draft = %#v", draft.Plan)
	}
	store, err := sqlitestore.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.CreateFire(context.Background(), domain.Fire{
		ID:          "FIRE-E2E-1",
		LoopID:      "LOOP-E2E-1",
		GoalID:      draft.Goal.ID,
		Status:      domain.FireRunning,
		ScheduledAt: time.Now().UTC(),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := client.ConfirmGoal(context.Background(), draft.Goal.ID); err != nil {
		t.Fatal(err)
	}

	// 2. Wait for the task to become GATED.
	task := waitForTask(t, client, "GATE-TASK", domain.TaskGated)
	if len(task.Attempts) == 0 {
		t.Fatal("expected at least one attempt")
	}

	// 3. Verify an open gate exists
	gates, err := client.ListOpenGates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(gates) == 0 {
		t.Fatal("expected at least one open gate")
	}
	gate := gates[0]
	if gate.Summary == "" {
		t.Fatalf("gate missing summary: %#v", gate)
	}

	// 4. Get gate detail
	detail, err := client.GetGate(context.Background(), gate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.DecisionOptions) == 0 {
		t.Fatal("gate has no decision options")
	}

	// 5. Approve the gate
	if err := client.ResolveGate(context.Background(), gate.ID, domain.GateDecisionInput{
		Decision: "approve",
		Note:     "Looks good, proceed",
	}); err != nil {
		t.Fatal(err)
	}

	// 6. Wait for the task to complete after gate resolution
	task = waitForTask(t, client, "GATE-TASK", domain.TaskCompleted)
	if len(task.Attempts) < 2 {
		t.Fatalf("expected at least 2 attempts after gate resume, got %d", len(task.Attempts))
	}

	// 7. Verify the gate is now resolved
	gates, err = client.ListOpenGates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(gates) != 0 {
		t.Fatalf("expected 0 open gates after resolution, got %d", len(gates))
	}

	// 8. Verify the output file was created
	content, err := os.ReadFile(filepath.Join(project, "result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "verified\n" {
		t.Fatalf("result = %q, expected verified", content)
	}
}
