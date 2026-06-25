package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/api"
	"github.com/heruujoko/piramid/internal/bootstrap"
	"github.com/heruujoko/piramid/internal/config"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/home"
	"github.com/heruujoko/piramid/internal/intake"
)

func buildFakePi(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "fake-pi")
	command := exec.Command("go", "build", "-o", output, "./test/e2e/fixtures/fake-pi")
	command.Dir = root
	if content, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build fake Pi: %v\n%s", err, content)
	}
	return output
}

func configureE2E(t *testing.T, paths home.Paths, fakePi string) {
	t.Helper()
	if err := home.Init(paths); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Workers.Count = 1
	for _, runtime := range []*config.RuntimeConfig{
		&cfg.Runtime.Planner, &cfg.Runtime.Executor, &cfg.Runtime.Verifier,
	} {
		runtime.Adapter = "command"
		runtime.Command = fakePi
		runtime.Args = []string{"-p", "{{prompt}}"}
		runtime.Timeout = config.Duration(10 * time.Second)
	}
	cfg.Retry.InitialDelay = config.Duration(20 * time.Millisecond)
	cfg.Retry.MaxDelay = config.Duration(50 * time.Millisecond)
	content, err := config.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Config, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func startE2E(t *testing.T, paths home.Paths) (*bootstrap.Running, *api.Client) {
	t.Helper()
	port := 0
	running, err := bootstrap.Start(context.Background(), bootstrap.Options{
		Paths: paths, Host: "127.0.0.1", Port: &port,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = running.Close(ctx)
	})
	return running, api.NewClient("http://"+running.ListenAddress(), 5*time.Second)
}

func waitForTask(
	t *testing.T,
	client *api.Client,
	taskID string,
	status domain.TaskStatus,
) domain.TaskView {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task, err := client.GetTask(context.Background(), taskID)
		if err == nil && task.Status == status {
			return task
		}
		time.Sleep(25 * time.Millisecond)
	}
	task, err := client.GetTask(context.Background(), taskID)
	t.Fatalf("task did not reach %s: task=%#v err=%v", status, task, err)
	return domain.TaskView{}
}

func TestLifecyclePlansExecutesRetriesAndVerifies(t *testing.T) {
	fakePi := buildFakePi(t)
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	configureE2E(t, paths, fakePi)
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	_, client := startE2E(t, paths)

	draft, err := client.DraftGoal(context.Background(), intake.DraftRequest{
		GoalText:    "Create a verified deterministic result",
		ProjectPath: project,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Plan.Tasks) != 1 || draft.Plan.Tasks[0].ID != "E2E-TASK" {
		t.Fatalf("draft = %#v", draft.Plan)
	}
	if err := client.ConfirmGoal(context.Background(), draft.Goal.ID); err != nil {
		t.Fatal(err)
	}
	task := waitForTask(t, client, "E2E-TASK", domain.TaskCompleted)
	if len(task.Attempts) != 2 {
		t.Fatalf("attempts = %#v", task.Attempts)
	}
	if task.Attempts[0].Verification == nil ||
		task.Attempts[0].Verification.Status != domain.VerificationFail {
		t.Fatalf("first verification = %#v", task.Attempts[0].Verification)
	}
	if task.Attempts[1].Verification == nil ||
		task.Attempts[1].Verification.Status != domain.VerificationPass {
		t.Fatalf("second verification = %#v", task.Attempts[1].Verification)
	}
	content, err := os.ReadFile(filepath.Join(project, "result.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "verified\n" {
		t.Fatalf("result = %q", content)
	}
	for _, path := range []string{
		filepath.Join(paths.Goals, draft.Goal.ID, "planner-prompt.md"),
		filepath.Join(paths.Tasks, "E2E-TASK", "task.yaml"),
		filepath.Join(paths.Attempts, "E2E-TASK", "0001", "executor-prompt.md"),
		filepath.Join(paths.Attempts, "E2E-TASK", "0001", "verification.yaml"),
		filepath.Join(paths.Attempts, "E2E-TASK", "0002", "executor-prompt.md"),
		filepath.Join(paths.Attempts, "E2E-TASK", "0002", "verification.yaml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing evidence %s: %v", path, err)
		}
	}
}

func TestDirectStructuredPlanEnqueueCompletes(t *testing.T) {
	fakePi := buildFakePi(t)
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	configureE2E(t, paths, fakePi)
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	_, client := startE2E(t, paths)
	plan := domain.Plan{
		Version: 1,
		GoalID:  "DIRECT-GOAL",
		Tasks: []domain.Task{{
			ID: "E2E-TASK", Title: "Direct task",
			Goal:        "Create result.txt with verified content",
			ProjectPath: project,
			Outputs:     []domain.Output{{Type: "file", Path: "result.txt"}},
			DOD:         []string{"result.txt contains verified"},
			MaxAttempts: 3, Timeout: 30 * time.Second, TimeoutText: "30s",
		}},
	}
	if err := client.Enqueue(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	task := waitForTask(t, client, "E2E-TASK", domain.TaskCompleted)
	if len(task.Attempts) != 2 {
		t.Fatalf("attempts = %#v", task.Attempts)
	}
}
