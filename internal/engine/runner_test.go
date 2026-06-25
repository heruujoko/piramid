package engine

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
	"github.com/heruujoko/piramid/internal/records"
	runtimepkg "github.com/heruujoko/piramid/internal/runtime"
	storepkg "github.com/heruujoko/piramid/internal/store"
	sqlitestore "github.com/heruujoko/piramid/internal/store/sqlite"
)

type runnerRuntime struct {
	output      string
	result      runtimepkg.Result
	err         error
	invocations []runtimepkg.Invocation
	onRun       func(runtimepkg.Invocation) error
}

func (r *runnerRuntime) Run(_ context.Context, invocation runtimepkg.Invocation) (runtimepkg.Result, error) {
	r.invocations = append(r.invocations, invocation)
	if r.onRun != nil {
		if err := r.onRun(invocation); err != nil {
			return runtimepkg.Result{}, err
		}
	}
	if invocation.StdoutPath != "" {
		if err := os.WriteFile(invocation.StdoutPath, []byte(r.output), 0o600); err != nil {
			return runtimepkg.Result{}, err
		}
	}
	if invocation.StderrPath != "" {
		if err := os.WriteFile(invocation.StderrPath, nil, 0o600); err != nil {
			return runtimepkg.Result{}, err
		}
	}
	return r.result, r.err
}

type runnerFixture struct {
	store    *sqlitestore.Store
	records  *records.Store
	project  string
	dispatch Dispatch
	executor *runnerRuntime
	verifier *runnerRuntime
	runner   *Runner
	now      time.Time
}

func newRunnerFixture(t *testing.T, maxAttempts int) *runnerFixture {
	t.Helper()
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	if err := home.Init(paths); err != nil {
		t.Fatal(err)
	}
	st, err := sqlitestore.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	project, err = filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	task := domain.Task{
		ID:          "TASK-1",
		Title:       "Create result",
		Goal:        "Create result.txt",
		ProjectPath: project,
		Outputs:     []domain.Output{{Type: "file", Path: "result.txt"}},
		DOD:         []string{"result.txt exists"},
		MaxAttempts: maxAttempts,
		Timeout:     time.Minute,
		TimeoutText: "1m",
	}
	goal := domain.Goal{
		ID: "GOAL-1", Text: "create result", ProjectPath: project,
		Status: domain.GoalConfirmed, CreatedAt: time.Now().UTC(),
	}
	plan := domain.Plan{Version: 1, GoalID: goal.ID, Tasks: []domain.Task{task}}
	if err := st.AdmitPlan(context.Background(), goal, plan, storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 6, 25, 2, 0, 0, 0, time.UTC)
	attempt, err := st.StartAttempt(context.Background(), storepkg.StartAttemptInput{
		TaskID: "TASK-1", WorkerID: "pi-worker-01", Runtime: "pi-cli", StartedAt: start,
	})
	if err != nil {
		t.Fatal(err)
	}
	executor := &runnerRuntime{result: runtimepkg.Result{
		ProcessID: 10, ExitCode: 0, StartedAt: start, FinishedAt: start.Add(time.Second),
	}}
	verifier := &runnerRuntime{
		output: "status: PASS\nreasons:\n  - done\n",
		result: runtimepkg.Result{
			ProcessID: 11, ExitCode: 0, StartedAt: start.Add(time.Second),
			FinishedAt: start.Add(2 * time.Second),
		},
	}
	recordStore := records.New(paths)
	runner := NewRunner(RunnerConfig{
		Store:    st,
		Records:  recordStore,
		Executor: executor,
		Verifier: verifier,
		ExecutorRuntime: RoleRuntime{
			Command: "pi", Args: []string{"-p", "{{prompt}}"}, Timeout: time.Minute,
		},
		VerifierRuntime: RoleRuntime{
			Command: "pi", Args: []string{"-p", "{{prompt}}"}, Timeout: time.Minute,
		},
		Now:        func() time.Time { return start.Add(3 * time.Second) },
		RetryDelay: func(int) time.Duration { return 5 * time.Minute },
	})
	return &runnerFixture{
		store: st, records: recordStore, project: project,
		dispatch: Dispatch{Task: domain.TaskRecord{Task: task, Status: domain.TaskRunning},
			Attempt: attempt, WorkerID: "pi-worker-01"},
		executor: executor, verifier: verifier, runner: runner, now: start,
	}
}

func TestRunnerCompletesOnlyAfterSeparateVerifierPasses(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.executor.onRun = func(runtimepkg.Invocation) error {
		return os.WriteFile(filepath.Join(fixture.project, "result.txt"), []byte("result"), 0o600)
	}

	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err != nil {
		t.Fatal(err)
	}
	task, err := fixture.store.GetTask(context.Background(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskCompleted {
		t.Fatalf("status = %s, want COMPLETED", task.Status)
	}
	if len(fixture.executor.invocations) != 1 || len(fixture.verifier.invocations) != 1 {
		t.Fatalf("executor=%d verifier=%d", len(fixture.executor.invocations), len(fixture.verifier.invocations))
	}
	if !strings.Contains(strings.Join(fixture.verifier.invocations[0].Args, "\n"), "result.txt") {
		t.Fatal("verifier prompt omitted artifact evidence")
	}
}

func TestRunnerVerifiesNonZeroExecutorExit(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.executor.result.ExitCode = 2
	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err != nil {
		t.Fatal(err)
	}
	if len(fixture.verifier.invocations) != 1 {
		t.Fatal("non-zero executor result did not enter verification")
	}
}

func TestRunnerUsesTaskTimeoutForExecutor(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.dispatch.Task.Timeout = 30 * time.Second
	fixture.dispatch.Task.TimeoutText = "30s"

	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err != nil {
		t.Fatal(err)
	}
	if len(fixture.executor.invocations) != 1 {
		t.Fatalf("executor invocations = %d, want 1", len(fixture.executor.invocations))
	}
	if got := fixture.executor.invocations[0].Timeout; got != 30*time.Second {
		t.Fatalf("executor timeout = %s, want 30s", got)
	}
}

func TestRunnerPersistsExactRetryPrompt(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.verifier.output = `status: FAIL
reasons:
  - missing result
retry_prompt: Create result.txt and verify its contents.
`
	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err != nil {
		t.Fatal(err)
	}
	task, err := fixture.store.GetTask(context.Background(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskRetryWait {
		t.Fatalf("status = %s, want RETRY_WAIT", task.Status)
	}
	runnable, err := fixture.store.ListRunnable(context.Background(), fixture.now.Add(10*time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runnable) != 1 ||
		runnable[0].RetryPrompt != "Create result.txt and verify its contents." {
		t.Fatalf("runnable retry = %#v", runnable)
	}
}

func TestRunnerFailsTerminallyAtAttemptLimit(t *testing.T) {
	fixture := newRunnerFixture(t, 1)
	fixture.verifier.output = `status: FAIL
reasons: [still wrong]
retry_prompt: Try again.
`
	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err != nil {
		t.Fatal(err)
	}
	task, err := fixture.store.GetTask(context.Background(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskFailed {
		t.Fatalf("status = %s, want FAILED", task.Status)
	}
}

func TestRunnerRecordsOperationalFailureWithoutInventingRetryPrompt(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.executor.err = errors.New("cannot launch")

	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err == nil {
		t.Fatal("Run() error = nil")
	}
	task, err := fixture.store.GetTask(context.Background(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskRetryWait || task.RetryPrompt != "" {
		t.Fatalf("task = %#v", task.TaskRecord)
	}
}

func TestRunnerTreatsExecutorTimeoutAsOperationalFailure(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.executor.result.TimedOut = true
	fixture.executor.result.Interrupted = true
	fixture.executor.result.ExitCode = -1

	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err == nil {
		t.Fatal("Run() error = nil")
	}
	if len(fixture.verifier.invocations) != 0 {
		t.Fatalf("verifier invocations = %d, want 0", len(fixture.verifier.invocations))
	}
	task, err := fixture.store.GetTask(context.Background(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskRetryWait || task.RetryPrompt != "" {
		t.Fatalf("task = %#v", task.TaskRecord)
	}
}

func TestRunnerTreatsMalformedVerifierOutputAsSystemFailure(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.verifier.output = "status: MAYBE\n"

	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err == nil {
		t.Fatal("Run() error = nil")
	}
	task, err := fixture.store.GetTask(context.Background(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != domain.TaskRetryWait {
		t.Fatalf("status = %s, want RETRY_WAIT", task.Status)
	}
}

func TestRunnerRejectsEscapingArtifactPath(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.dispatch.Task.Outputs = []domain.Output{{Type: "file", Path: "../escape.txt"}}
	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err != nil {
		t.Fatal(err)
	}
	promptText := strings.Join(fixture.verifier.invocations[0].Args, "\n")
	if !strings.Contains(promptText, "escapes project") {
		t.Fatalf("verifier prompt = %q", promptText)
	}
}

func TestParseVerificationIsStrict(t *testing.T) {
	tests := []string{
		"status: MAYBE\nreasons: [x]\n",
		"status: FAIL\nreasons: [x]\n",
		"status: PASS\nreasons: []\n",
		"status: PASS\nreasons: [x]\nunknown: true\n",
		"status: PASS\nreasons: [x]\n---\nstatus: PASS\n",
	}
	for _, input := range tests {
		if _, err := ParseVerification(strings.NewReader(input), true); err == nil {
			t.Fatalf("ParseVerification(%q) error = nil", input)
		}
	}
}
