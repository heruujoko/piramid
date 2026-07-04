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

func TestRunnerPassesGateContextPathToExecutor(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.executor.onRun = func(runtimepkg.Invocation) error {
		return os.WriteFile(filepath.Join(fixture.project, "result.txt"), []byte("result"), 0o600)
	}
	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err != nil {
		t.Fatal(err)
	}
	if len(fixture.executor.invocations) != 1 {
		t.Fatalf("executor invocations = %d", len(fixture.executor.invocations))
	}
	var gateEnv string
	for _, entry := range fixture.executor.invocations[0].Environment {
		if strings.HasPrefix(entry, "PIRAMID_GATE_CONTEXT=") {
			gateEnv = strings.TrimPrefix(entry, "PIRAMID_GATE_CONTEXT=")
		}
	}
	if gateEnv == "" {
		t.Fatal("PIRAMID_GATE_CONTEXT not passed to executor environment")
	}
	if filepath.Base(gateEnv) != "gate.context.md" {
		t.Fatalf("gate context path = %q, want basename gate.context.md", gateEnv)
	}
	if !strings.Contains(gateEnv, filepath.Join("TASK-1", "0001")) {
		t.Fatalf("gate context path = %q, want per-attempt path", gateEnv)
	}
	// With an empty configured executor env, the runner must seed from the
	// process environment so credentials/HOME/PATH survive alongside the gate var.
	env := fixture.executor.invocations[0].Environment
	if len(env) <= 1 {
		t.Fatalf("executor env = %v, want inherited process env plus gate var", env)
	}
	inherited := false
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") || strings.HasPrefix(entry, "HOME=") {
			inherited = true
		}
	}
	if !inherited {
		t.Fatal("executor env dropped inherited process environment (no PATH/HOME)")
	}
	// Verifier invocation must not carry the gate context env var.
	for _, entry := range fixture.verifier.invocations[0].Environment {
		if strings.HasPrefix(entry, "PIRAMID_GATE_CONTEXT=") {
			t.Fatal("verifier environment should not include PIRAMID_GATE_CONTEXT")
		}
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

// gateContextMD builds a valid gate.context.md body referencing the given fire.
func gateContextMD(fireID, loopID, goalID string) string {
	return "---\n" +
		"gate: human-review\n" +
		"phase: execution\n" +
		"loop_id: " + loopID + "\n" +
		"fire_id: " + fireID + "\n" +
		"goal_id: " + goalID + "\n" +
		"summary: executor needs a decision\n" +
		"decision_options: [approve, route, defer, reject]\n" +
		"---\n" +
		"## Gate\n\nPlease decide.\n"
}

// gateEnvPath extracts the PIRAMID_GATE_CONTEXT value from an invocation env.
func gateEnvPath(env []string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "PIRAMID_GATE_CONTEXT="); ok {
			return v
		}
	}
	return ""
}

func TestRunnerExit42CreatesOpenGateAndSkipsVerifier(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	// Create a real fire the gate will link to and park as gated.
	fire, err := fixture.store.CreateFire(context.Background(), domain.Fire{
		ID: "FIRE-1", LoopID: "LOOP-1", GoalID: "GOAL-1",
		Status: domain.FireRunning, ScheduledAt: fixture.now, CreatedAt: fixture.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.executor.result.ExitCode = GateExitCode
	fixture.executor.onRun = func(inv runtimepkg.Invocation) error {
		gatePath := gateEnvPath(inv.Environment)
		if gatePath == "" {
			return errors.New("PIRAMID_GATE_CONTEXT not in executor env")
		}
		return os.WriteFile(gatePath, []byte(gateContextMD(fire.ID, "LOOP-1", "GOAL-1")), 0o600)
	}

	if err := fixture.runner.Run(context.Background(), fixture.dispatch); err != nil {
		t.Fatalf("Run error = %v", err)
	}
	// Verifier must not be invoked for a gated attempt.
	if len(fixture.verifier.invocations) != 0 {
		t.Fatalf("verifier invoked %d times, want 0", len(fixture.verifier.invocations))
	}
	// Exactly one open gate linked to the fire/goal/task/attempt.
	openGates, err := fixture.store.ListOpenGates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(openGates) != 1 {
		t.Fatalf("open gates = %d, want 1", len(openGates))
	}
	g := openGates[0]
	if g.FireID != fire.ID || g.GoalID != "GOAL-1" || g.TaskID != "TASK-1" {
		t.Fatalf("gate linkage = fire=%q goal=%q task=%q", g.FireID, g.GoalID, g.TaskID)
	}
	if g.AttemptID != "1" {
		t.Fatalf("gate attempt id = %q, want 1", g.AttemptID)
	}
	if g.Status != domain.GateOpen {
		t.Fatalf("gate status = %s, want GATE_OPEN", g.Status)
	}
	if filepath.Base(g.ContextPath) != "gate.context.md" {
		t.Fatalf("gate context path = %q", g.ContextPath)
	}
	if g.Context.FireID != fire.ID || g.Context.Summary == "" {
		t.Fatalf("gate context not stored: %+v", g.Context)
	}
	// Fire must be parked as gated.
	fires, err := fixture.store.ListFires(context.Background(), "LOOP-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(fires) != 1 || fires[0].Status != domain.FireGated {
		t.Fatalf("fire status = %v, want FIRE_GATED", fires)
	}
}

// TestRunnerDefaultGateIDGeneratorIsUniqueWithinSecond verifies that the default
// gate ID generator never collides when two attempts open a gate in the same
// wall-clock second, which previously caused CreateGate to fail on the
// gates.id primary key.
func TestRunnerDefaultGateIDGeneratorIsUniqueWithinSecond(t *testing.T) {
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	seen := make(map[string]struct{}, 256)
	const iterations = 256
	for i := 0; i < iterations; i++ {
		id := defaultGateIDGenerator(now)
		if !strings.HasPrefix(id, "GATE-20260704120000-") {
			t.Fatalf("id %q missing sortable timestamp prefix", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("defaultGateIDGenerator produced a duplicate id %q at iteration %d "+
				"(same-second collision that breaks gates.id primary key)", id, i)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != iterations {
		t.Fatalf("unique ids = %d, want %d", len(seen), iterations)
	}
}

func TestRunnerExit42MissingGateContextRecordsFailure(t *testing.T) {
	fixture := newRunnerFixture(t, 3)
	fixture.executor.result.ExitCode = GateExitCode
	// Executor exits 42 but writes no gate.context.md.
	fixture.executor.onRun = func(runtimepkg.Invocation) error { return nil }

	err := fixture.runner.Run(context.Background(), fixture.dispatch)
	if err == nil {
		t.Fatal("Run error = nil, want gate_context_invalid failure")
	}
	if len(fixture.verifier.invocations) != 0 {
		t.Fatalf("verifier invoked %d times, want 0", len(fixture.verifier.invocations))
	}
	openGates, err := fixture.store.ListOpenGates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(openGates) != 0 {
		t.Fatalf("open gates = %d, want 0 (no gate for invalid context)", len(openGates))
	}
	// The attempt must be recorded as a failed operational failure.
	task, err := fixture.store.GetTask(context.Background(), "TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status == domain.TaskCompleted {
		t.Fatalf("task status = COMPLETED, want non-completed")
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
