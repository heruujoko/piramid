# Gate Resume Flow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete M5 gate decisions so `approve`/`route` resume a gated task with a compact restore prompt, while `defer`/`reject` park or terminate the linked fire/task.

**Architecture:** Add first-class `TaskGated`, add one-shot task-level `resume_prompt` storage, and introduce `internal/restore` to build concise resume prompts. `app.Service.ResolveGate` becomes the orchestrator that validates gate decisions, builds restore context, changes task/fire state, then resolves the gate.

**Tech Stack:** Go, SQLite migrations, existing `internal/store` interface, existing HTTP API handlers, existing runner and store tests.

## Global Constraints

- No React board work in this PR.
- No full checkpoint subsystem.
- No continuation-task model.
- No full ledger/log embedding.
- No LLM-generated summaries.
- No auth changes.
- Restore prompt includes only concise thread summaries and last 20 stdout/stderr lines.
- `note` is optional; use deterministic fallback note text when empty.
- If resume/fire update fails, leave gate open.

---

## File Structure

- `internal/domain/status.go` — add `TaskGated` and transition rules.
- `migrations/004_task_resume_prompt.sql` — add `tasks.resume_prompt`.
- `internal/store/store.go` — add `ResumeGatedTaskInput`, `ResumeGatedTask`, and `SetTaskStatus`.
- `internal/store/sqlite/tasks.go` — read `resume_prompt`, prefer it over verification retry prompt in task views/runnable records.
- `internal/store/sqlite/attempts.go` — clear `resume_prompt` when `StartAttempt` consumes it.
- `internal/store/sqlite/operations.go` — implement `ResumeGatedTask` and `SetTaskStatus`.
- `internal/store/sqlite/fires_gates_test.go` / `store_test.go` — add migration and store behavior tests.
- `internal/engine/runner.go` — exit 42 should mark task `GATED` as part of gate handling.
- `internal/engine/runner_test.go` — assert exit 42 marks task `GATED`.
- `internal/restore/prompt.go` — build compact restore prompt and tail logs.
- `internal/restore/prompt_test.go` — test fallback notes, thread summaries, and log tailing.
- `internal/app/service.go` — orchestrate decision branches.
- `internal/app/service_test.go` — integration-ish app service tests using sqlite store.
- `docs/specs/2026-07-02-loop-dashboard-task-breakdown.md` — mark T-051/T-052 done after implementation.

---

### Task 1: Add first-class gated task status

**Files:**
- Modify: `internal/domain/status.go`
- Test: existing compile-time users via `go test ./internal/domain ./internal/engine ./internal/store/sqlite`

**Interfaces:**
- Produces: `domain.TaskGated TaskStatus = "GATED"`
- Produces: transition support `TaskRunning -> TaskGated`, `TaskGated -> TaskPending`, `TaskGated -> TaskBlocked`, `TaskGated -> TaskCancelled`

- [ ] **Step 1: Add the status constant and transitions**

In `internal/domain/status.go`, update the constants and `allowedTaskTransitions`:

```go
const (
	TaskPending   TaskStatus = "PENDING"
	TaskRunning   TaskStatus = "RUNNING"
	TaskVerifying TaskStatus = "VERIFYING"
	TaskRetryWait TaskStatus = "RETRY_WAIT"
	TaskCompleted TaskStatus = "COMPLETED"
	TaskFailed    TaskStatus = "FAILED"
	TaskBlocked   TaskStatus = "BLOCKED"
	TaskCancelled TaskStatus = "CANCELLED"
	TaskGated     TaskStatus = "GATED"
)
```

Add transitions:

```go
TaskRunning: {
	TaskVerifying: {},
	TaskFailed:    {},
	TaskCancelled: {},
	TaskGated:     {},
},
TaskGated: {
	TaskPending:   {},
	TaskBlocked:   {},
	TaskCancelled: {},
},
```

- [ ] **Step 2: Run tests**

Run:

```bash
go test ./internal/domain ./internal/engine ./internal/store/sqlite
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/domain/status.go
git commit -m "feat(domain): add gated task status"
```

---

### Task 2: Add one-shot task resume prompt storage

**Files:**
- Create: `migrations/004_task_resume_prompt.sql`
- Modify: `internal/store/sqlite/tasks.go`
- Modify: `internal/store/sqlite/attempts.go`
- Test: `internal/store/sqlite/fires_gates_test.go`

**Interfaces:**
- Produces DB column: `tasks.resume_prompt TEXT NOT NULL DEFAULT ''`
- `GetTask` and `ListRunnable` must set `TaskRecord.RetryPrompt` to `resume_prompt` when non-empty, otherwise latest verification retry prompt.
- `StartAttempt` must clear `resume_prompt` after selecting the task for execution.

- [ ] **Step 1: Write migration test first**

In `internal/store/sqlite/fires_gates_test.go`, add:

```go
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
```

- [ ] **Step 2: Run migration test to verify it fails**

Run:

```bash
go test ./internal/store/sqlite -run TestMigration004AddsTaskResumePrompt -count=1
```

Expected: FAIL because the column does not exist.

- [ ] **Step 3: Add migration**

Create `migrations/004_task_resume_prompt.sql`:

```sql
ALTER TABLE tasks ADD COLUMN resume_prompt TEXT NOT NULL DEFAULT '';
```

- [ ] **Step 4: Update task reads to prefer `resume_prompt`**

In `internal/store/sqlite/tasks.go`, update the SELECT expressions in `GetTask` and `ListRunnable`.

Replace the current retry prompt expression shape:

```sql
COALESCE((
    SELECT v.retry_prompt
    FROM verifications v
    JOIN attempts a ON a.id = v.attempt_id
    WHERE a.task_id = tasks.id
    ORDER BY a.attempt_number DESC
    LIMIT 1
), '')
```

with:

```sql
COALESCE(NULLIF(tasks.resume_prompt, ''), (
    SELECT v.retry_prompt
    FROM verifications v
    JOIN attempts a ON a.id = v.attempt_id
    WHERE a.task_id = tasks.id
    ORDER BY a.attempt_number DESC
    LIMIT 1
), '')
```

For `ListRunnable`, use alias `t`:

```sql
COALESCE(NULLIF(t.resume_prompt, ''), (
    SELECT v.retry_prompt
    FROM verifications v
    JOIN attempts a ON a.id = v.attempt_id
    WHERE a.task_id = t.id
    ORDER BY a.attempt_number DESC
    LIMIT 1
), '')
```

- [ ] **Step 5: Clear one-shot prompt in `StartAttempt`**

In `internal/store/sqlite/attempts.go`, update the task update query in `StartAttempt` from:

```sql
UPDATE tasks SET status = ?, attempt_count = ?, next_run_at = NULL, updated_at = ?
WHERE id = ?
```

to:

```sql
UPDATE tasks
SET status = ?, attempt_count = ?, next_run_at = NULL, resume_prompt = '', updated_at = ?
WHERE id = ?
```

Keep the Go args aligned:

```go
`, domain.TaskRunning, attemptNumber, formatTime(startedAt), input.TaskID)
```

- [ ] **Step 6: Run tests**

Run:

```bash
go test ./internal/store/sqlite -run 'TestMigration004AddsTaskResumePrompt|TestAdmitPlan|TestRunner' -count=1
```

Expected: PASS. If `TestRunner` pattern does not match in this package, it is okay as long as sqlite tests pass.

- [ ] **Step 7: Commit**

```bash
git add migrations/004_task_resume_prompt.sql internal/store/sqlite/tasks.go internal/store/sqlite/attempts.go internal/store/sqlite/fires_gates_test.go
git commit -m "feat(store): add one-shot task resume prompt"
```

---

### Task 3: Add store operations for gated tasks

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/sqlite/operations.go`
- Test: `internal/store/sqlite/store_test.go`

**Interfaces:**
- Produces:

```go
type ResumeGatedTaskInput struct {
	TaskID        string
	RestorePrompt string
	Override      bool
	Now           time.Time
}
```

- Produces store methods:

```go
ResumeGatedTask(context.Context, ResumeGatedTaskInput) error
SetTaskStatus(context.Context, string, domain.TaskStatus, time.Time) error
```

- [ ] **Step 1: Write failing store tests**

In `internal/store/sqlite/store_test.go`, add tests using existing helpers. Use the same plan/goal seeding pattern already used in this file.

Add helper if needed:

```go
func admitSingleTaskForGateTest(t *testing.T, st *Store, status domain.TaskStatus) string {
	t.Helper()
	ctx := context.Background()
	goal := domain.Goal{ID: "GOAL-GATE", Text: "gate goal", ProjectPath: t.TempDir(), Status: domain.GoalConfirmed, CreatedAt: time.Now().UTC()}
	plan := domain.Plan{GoalID: goal.ID, Tasks: []domain.Task{{
		ID: "TASK-GATE", Title: "gate task", Goal: "do gated work", ProjectPath: goal.ProjectPath,
		DOD: []string{"done"}, Model: "test", MaxAttempts: 1, Timeout: time.Minute, TimeoutText: "1m",
	}}}
	if err := st.AdmitPlan(ctx, goal, plan, storepkg.PersistedPaths{}); err != nil {
		t.Fatal(err)
	}
	_, err := st.db.ExecContext(ctx, `UPDATE tasks SET status = ?, attempt_count = 1, max_attempts = 1 WHERE id = ?`, status, "TASK-GATE")
	if err != nil {
		t.Fatal(err)
	}
	return "TASK-GATE"
}
```

Add resume test:

```go
func TestResumeGatedTaskSetsPendingAndStoresResumePrompt(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	taskID := admitSingleTaskForGateTest(t, st, domain.TaskGated)
	now := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)

	err := st.ResumeGatedTask(ctx, storepkg.ResumeGatedTaskInput{
		TaskID: taskID, RestorePrompt: "resume from gate", Override: true, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	view, err := st.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != domain.TaskPending {
		t.Fatalf("status = %s, want PENDING", view.Status)
	}
	if view.RetryPrompt != "resume from gate" {
		t.Fatalf("retry prompt = %q, want restore prompt", view.RetryPrompt)
	}
	if view.MaxAttempts != 2 {
		t.Fatalf("max attempts = %d, want 2 override bump", view.MaxAttempts)
	}
}
```

Add invalid state test:

```go
func TestResumeGatedTaskRejectsNonGatedTask(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	taskID := admitSingleTaskForGateTest(t, st, domain.TaskFailed)

	err := st.ResumeGatedTask(ctx, storepkg.ResumeGatedTaskInput{
		TaskID: taskID, RestorePrompt: "resume", Override: true, Now: time.Now().UTC(),
	})
	if err == nil || !errors.Is(err, storepkg.ErrInvalidState) {
		t.Fatalf("error = %v, want ErrInvalidState", err)
	}
}
```

Add terminal mapping test:

```go
func TestSetTaskStatusFromGated(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	taskID := admitSingleTaskForGateTest(t, st, domain.TaskGated)

	if err := st.SetTaskStatus(ctx, taskID, domain.TaskBlocked, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	view, err := st.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != domain.TaskBlocked {
		t.Fatalf("status = %s, want BLOCKED", view.Status)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/store/sqlite -run 'TestResumeGatedTask|TestSetTaskStatusFromGated' -count=1
```

Expected: FAIL because methods do not exist.

- [ ] **Step 3: Add store interface types and methods**

In `internal/store/store.go`, add to `Store`:

```go
ResumeGatedTask(context.Context, ResumeGatedTaskInput) error
SetTaskStatus(context.Context, string, domain.TaskStatus, time.Time) error
```

Add type:

```go
type ResumeGatedTaskInput struct {
	TaskID        string
	RestorePrompt string
	Override      bool
	Now           time.Time
}
```

- [ ] **Step 4: Implement `ResumeGatedTask`**

In `internal/store/sqlite/operations.go`, add:

```go
func (s *Store) ResumeGatedTask(ctx context.Context, in storepkg.ResumeGatedTaskInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	var attempts, maxAttempts int
	if err := tx.QueryRowContext(ctx, `
		SELECT status, attempt_count, max_attempts FROM tasks WHERE id = ?
	`, in.TaskID).Scan(&status, &attempts, &maxAttempts); err != nil {
		return err
	}
	if status != string(domain.TaskGated) {
		return fmt.Errorf("%w: task %s is %s", storepkg.ErrInvalidState, in.TaskID, status)
	}
	if attempts >= maxAttempts && !in.Override {
		return fmt.Errorf("%w: task %s exhausted attempts", storepkg.ErrInvalidState, in.TaskID)
	}
	if attempts >= maxAttempts {
		maxAttempts = attempts + 1
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, max_attempts = ?, resume_prompt = ?, next_run_at = ?, updated_at = ?
		WHERE id = ?
	`, domain.TaskPending, maxAttempts, in.RestorePrompt, formatTime(now), formatTime(now), in.TaskID); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", in.TaskID, "GATE_RESUME",
		map[string]any{"override": in.Override}, now); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 5: Implement `SetTaskStatus`**

In `internal/store/sqlite/operations.go`, add:

```go
func (s *Store) SetTaskStatus(ctx context.Context, taskID string, status domain.TaskStatus, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var current string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = ?", taskID).Scan(&current); err != nil {
		return err
	}
	if !domain.CanTransition(domain.TaskStatus(current), status) {
		return fmt.Errorf("%w: task %s cannot transition from %s to %s", storepkg.ErrInvalidState, taskID, current, status)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, resume_prompt = '', next_run_at = NULL, updated_at = ? WHERE id = ?
	`, status, formatTime(now), taskID); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", taskID, "TASK_STATUS_CHANGED",
		map[string]any{"status": status}, now); err != nil {
		return err
	}
	return tx.Commit()
}
```

- [ ] **Step 6: Run store tests**

Run:

```bash
go test ./internal/store/sqlite -run 'TestResumeGatedTask|TestSetTaskStatusFromGated|TestMigration004AddsTaskResumePrompt' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/sqlite/operations.go internal/store/sqlite/store_test.go
git commit -m "feat(store): resume and terminalize gated tasks"
```

---

### Task 4: Mark exit-42 attempts as gated

**Files:**
- Modify: `internal/engine/runner.go`
- Modify: `internal/engine/runner_test.go`
- Maybe modify: `internal/store/store.go`, `internal/store/sqlite/operations.go` if Task 3 `SetTaskStatus` is not suitable for running attempts

**Interfaces:**
- Consumes: `domain.TaskGated`
- Consumes: `Store.SetTaskStatus(ctx, taskID, domain.TaskGated, now)` or a focused store method if `SetTaskStatus` transition validation suffices.

- [ ] **Step 1: Extend existing runner gate test**

In `internal/engine/runner_test.go`, inside `TestRunnerExit42CreatesOpenGateAndSkipsVerifier`, after fire status assertion add:

```go
task, err := fixture.store.GetTask(context.Background(), "TASK-1")
if err != nil {
	t.Fatal(err)
}
if task.Status != domain.TaskGated {
	t.Fatalf("task status = %s, want GATED", task.Status)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/engine -run TestRunnerExit42CreatesOpenGateAndSkipsVerifier -count=1
```

Expected: FAIL because task remains `RUNNING` or other non-gated status.

- [ ] **Step 3: Update `handleGate` to mark task gated**

In `internal/engine/runner.go`, inside `handleGate`, after `CreateGate` succeeds and before fire status update, add:

```go
if err := r.store.SetTaskStatus(ctx, task.ID, domain.TaskGated, now); err != nil {
	return r.operationalFailure(ctx, task, attempt, "task_gated", err)
}
```

Also add `SetTaskStatus(context.Context, string, domain.TaskStatus, time.Time) error` to the runner store interface near the top of `runner.go`.

- [ ] **Step 4: Run runner test**

Run:

```bash
go test ./internal/engine -run TestRunnerExit42CreatesOpenGateAndSkipsVerifier -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/runner.go internal/engine/runner_test.go
git commit -m "feat(engine): mark exit-42 tasks gated"
```

---

### Task 5: Add restore prompt package

**Files:**
- Create: `internal/restore/prompt.go`
- Create: `internal/restore/prompt_test.go`

**Interfaces:**
- Produces:

```go
type LogTail struct {
	StdoutPath string
	StderrPath string
	StdoutTail string
	StderrTail string
}

type BuildInput struct {
	Gate     domain.Gate
	Decision domain.GateDecision
	Note     string
	LogTail  LogTail
}

func FallbackNote(decision domain.GateDecision) string
func BuildPrompt(input BuildInput) string
func TailFile(path string, lines int) string
```

- [ ] **Step 1: Write failing tests**

Create `internal/restore/prompt_test.go`:

```go
package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heruujoko/piramid/internal/domain"
)

func TestFallbackNote(t *testing.T) {
	cases := map[domain.GateDecision]string{
		domain.GateDecisionApprove: "Approved without note.",
		domain.GateDecisionRoute:   "Routed without note; resume from gate context.",
		domain.GateDecisionDefer:   "Deferred without note.",
		domain.GateDecisionReject:  "Rejected without note.",
	}
	for decision, want := range cases {
		if got := FallbackNote(decision); got != want {
			t.Fatalf("FallbackNote(%s) = %q, want %q", decision, got, want)
		}
	}
}

func TestBuildPromptUsesConciseThreadSummaries(t *testing.T) {
	gate := domain.Gate{
		ID: "GATE-1", FireID: "FIRE-1", TaskID: "TASK-1", AttemptID: "7",
		ContextPath: "/records/TASK-1/0001/gate.context.md",
		Context: domain.GateContext{
			Phase: "execution", Summary: "Needs review", Body: "FULL BODY SHOULD NOT APPEAR",
			Threads: []domain.GateThread{{ID: "T1", Title: "Fix API", Location: "api.go:10", Summary: "Use 400 for invalid input"}},
		},
	}
	prompt := BuildPrompt(BuildInput{
		Gate: gate, Decision: domain.GateDecisionRoute, Note: "Use option B",
		LogTail: LogTail{StdoutPath: "/tmp/stdout", StderrPath: "/tmp/stderr", StdoutTail: "out20", StderrTail: "err20"},
	})
	for _, want := range []string{"Decision: route", "Note: Use option B", "T1", "Fix API", "api.go:10", "Use 400", "out20", "err20", gate.ContextPath} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "FULL BODY SHOULD NOT APPEAR") {
		t.Fatalf("prompt included full gate body:\n%s", prompt)
	}
}

func TestTailFileReturnsLastNLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	var b strings.Builder
	for i := 1; i <= 25; i++ {
		b.WriteString("line ")
		b.WriteString(string(rune('A' + i - 1)))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	tail := TailFile(path, 20)
	if strings.Contains(tail, "line A") || !strings.Contains(tail, "line F") || !strings.Contains(tail, "line Y") {
		t.Fatalf("tail = %q, want last 20 lines F..Y", tail)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/restore -count=1
```

Expected: FAIL because package does not exist / functions missing.

- [ ] **Step 3: Implement prompt builder**

Create `internal/restore/prompt.go`:

```go
package restore

import (
	"fmt"
	"os"
	"strings"

	"github.com/heruujoko/piramid/internal/domain"
)

type LogTail struct {
	StdoutPath string
	StderrPath string
	StdoutTail string
	StderrTail string
}

type BuildInput struct {
	Gate     domain.Gate
	Decision domain.GateDecision
	Note     string
	LogTail  LogTail
}

func FallbackNote(decision domain.GateDecision) string {
	switch decision {
	case domain.GateDecisionApprove:
		return "Approved without note."
	case domain.GateDecisionRoute:
		return "Routed without note; resume from gate context."
	case domain.GateDecisionDefer:
		return "Deferred without note."
	case domain.GateDecisionReject:
		return "Rejected without note."
	default:
		return "Decision recorded without note."
	}
}

func BuildPrompt(input BuildInput) string {
	note := strings.TrimSpace(input.Note)
	if note == "" {
		note = FallbackNote(input.Decision)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Resume this task from a human gate.\n\n")
	fmt.Fprintf(&b, "Decision: %s\n", input.Decision)
	fmt.Fprintf(&b, "Note: %s\n\n", note)
	fmt.Fprintf(&b, "Gate:\n")
	fmt.Fprintf(&b, "- id: %s\n", input.Gate.ID)
	fmt.Fprintf(&b, "- phase: %s\n", input.Gate.Context.Phase)
	fmt.Fprintf(&b, "- summary: %s\n", input.Gate.Context.Summary)
	fmt.Fprintf(&b, "- context path: %s\n\n", input.Gate.ContextPath)
	fmt.Fprintf(&b, "Threads:\n")
	if len(input.Gate.Context.Threads) == 0 {
		fmt.Fprintf(&b, "- none\n")
	}
	for _, thread := range input.Gate.Context.Threads {
		fmt.Fprintf(&b, "- id: %s\n  title: %s\n  location: %s\n  summary: %s\n",
			thread.ID, thread.Title, thread.Location, thread.Summary)
	}
	fmt.Fprintf(&b, "\nPrevious attempt:\n")
	fmt.Fprintf(&b, "- attempt id: %s\n", input.Gate.AttemptID)
	fmt.Fprintf(&b, "- stdout path: %s\n", input.LogTail.StdoutPath)
	fmt.Fprintf(&b, "- stderr path: %s\n", input.LogTail.StderrPath)
	fmt.Fprintf(&b, "- last 20 stdout lines:\n%s\n", emptyTail(input.LogTail.StdoutTail))
	fmt.Fprintf(&b, "- last 20 stderr lines:\n%s\n\n", emptyTail(input.LogTail.StderrTail))
	fmt.Fprintf(&b, "Instruction:\n")
	fmt.Fprintf(&b, "Resume from the paused phase. Do not repeat completed ledger work unless needed.\n")
	fmt.Fprintf(&b, "Use the full gate context file if more detail is required.\n")
	return b.String()
}

func TailFile(path string, lines int) string {
	if strings.TrimSpace(path) == "" || lines <= 0 {
		return "(log unavailable)"
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "(log unavailable: " + err.Error() + ")"
	}
	parts := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	if len(parts) == 1 && parts[0] == "" {
		return "(empty log)"
	}
	return strings.Join(parts, "\n")
}

func emptyTail(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(empty log)"
	}
	return value
}
```

- [ ] **Step 4: Run restore tests**

Run:

```bash
go test ./internal/restore -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/restore/prompt.go internal/restore/prompt_test.go
git commit -m "feat(restore): build gate resume prompts"
```

---

### Task 6: Orchestrate gate decisions in app service

**Files:**
- Modify: `internal/app/service.go`
- Test: `internal/app/service_test.go`

**Interfaces:**
- Consumes: `restore.BuildPrompt`, `restore.TailFile`
- Consumes: `Store.ResumeGatedTask`, `Store.SetTaskStatus`, `Store.UpdateFireStatus`, `Store.ResolveGate`
- Produces: `Service.ResolveGate` complete decision behavior.

- [ ] **Step 1: Add an app-level fake store for gate decisions**

In `internal/app/service_test.go`, add this fake store. It embeds `storepkg.Store` so only methods used by `ResolveGate` need concrete implementations.

```go
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
```

Add a fixture helper:

```go
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
				Threads: []domain.GateThread{{ID: "T1", Title: "Review", Location: "file.go:1", Summary: "Fix thing"}},
			},
		},
		logPaths: storepkg.AttemptLogPaths{Stdout: stdoutPath, Stderr: stderrPath},
	}
	service := NewService(nil, st, nil, nil)
	service.Now = func() time.Time { return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC) }
	return service, st
}
```

- [ ] **Step 2: Add failing app tests**

Add approve test:

```go
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
```

Add route/defer/reject/invalid tests:

```go
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
```

- [ ] **Step 3: Run app tests to verify they fail**

Run:

```bash
go test ./internal/app -run 'TestResolveGate' -count=1
```

Expected: FAIL because orchestration is not implemented.

- [ ] **Step 4: Implement allowed-decision validation helper**

In `internal/app/service.go`, add near `ResolveGate`:

```go
func decisionAllowed(gate domain.Gate, decision domain.GateDecision) bool {
	for _, option := range gate.Context.DecisionOptions {
		if option == decision {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Implement log tail helper**

In `internal/app/service.go`, import `strconv` and `github.com/heruujoko/piramid/internal/restore`, then add:

```go
func (s *Service) gateLogTail(ctx context.Context, gate domain.Gate) restore.LogTail {
	attemptID, err := strconv.ParseInt(gate.AttemptID, 10, 64)
	if err != nil || attemptID == 0 {
		return restore.LogTail{StdoutTail: "(log unavailable)", StderrTail: "(log unavailable)"}
	}
	paths, err := s.Store.GetAttemptLogPaths(ctx, attemptID)
	if err != nil {
		return restore.LogTail{StdoutTail: "(log unavailable: " + err.Error() + ")", StderrTail: "(log unavailable: " + err.Error() + ")"}
	}
	return restore.LogTail{
		StdoutPath: paths.Stdout,
		StderrPath: paths.Stderr,
		StdoutTail: restore.TailFile(paths.Stdout, 20),
		StderrTail: restore.TailFile(paths.Stderr, 20),
	}
}
```

- [ ] **Step 6: Replace `ResolveGate` implementation**

In `internal/app/service.go`, replace the body of `ResolveGate` with:

```go
func (s *Service) ResolveGate(ctx context.Context, gateID string, input domain.GateDecisionInput) error {
	if input.Decision == "" {
		return fmt.Errorf("%w: decision is required", ErrInvalid)
	}
	decision := domain.GateDecision(input.Decision)
	switch decision {
	case domain.GateDecisionApprove,
		domain.GateDecisionRoute,
		domain.GateDecisionDefer,
		domain.GateDecisionReject:
	default:
		return fmt.Errorf("%w: invalid decision: %s", ErrInvalid, input.Decision)
	}

	gate, err := s.Store.GetGate(ctx, gateID)
	if err != nil {
		return mapStoreError(err)
	}
	if gate.Status != domain.GateOpen {
		return fmt.Errorf("%w: gate %s is %s", ErrConflict, gateID, gate.Status)
	}
	if !decisionAllowed(gate, decision) {
		return fmt.Errorf("%w: decision %s is not allowed for gate %s", ErrInvalid, decision, gateID)
	}

	note := input.Note
	if note == "" {
		note = restore.FallbackNote(decision)
	}
	now := s.now()

	switch decision {
	case domain.GateDecisionApprove, domain.GateDecisionRoute:
		prompt := restore.BuildPrompt(restore.BuildInput{
			Gate: gate, Decision: decision, Note: note, LogTail: s.gateLogTail(ctx, gate),
		})
		if gate.TaskID != "" {
			if err := s.Store.ResumeGatedTask(ctx, storepkg.ResumeGatedTaskInput{
				TaskID: gate.TaskID, RestorePrompt: prompt, Override: true, Now: now,
			}); err != nil {
				return mapStoreError(err)
			}
		}
		if gate.FireID != "" {
			if err := s.Store.UpdateFireStatus(ctx, gate.FireID, domain.FireRunning, now); err != nil {
				return mapStoreError(err)
			}
		}
	case domain.GateDecisionDefer:
		if gate.TaskID != "" {
			if err := s.Store.SetTaskStatus(ctx, gate.TaskID, domain.TaskBlocked, now); err != nil {
				return mapStoreError(err)
			}
		}
		if gate.FireID != "" {
			if err := s.Store.UpdateFireStatus(ctx, gate.FireID, domain.FireDeferred, now); err != nil {
				return mapStoreError(err)
			}
		}
	case domain.GateDecisionReject:
		if gate.TaskID != "" {
			if err := s.Store.SetTaskStatus(ctx, gate.TaskID, domain.TaskCancelled, now); err != nil {
				return mapStoreError(err)
			}
		}
		if gate.FireID != "" {
			if err := s.Store.UpdateFireStatus(ctx, gate.FireID, domain.FireRejected, now); err != nil {
				return mapStoreError(err)
			}
		}
	}

	return mapStoreError(s.Store.ResolveGate(ctx, storepkg.ResolveGateInput{
		ID: gateID, Decision: decision, Note: note, Now: now,
	}))
}
```

- [ ] **Step 7: Run app tests**

Run:

```bash
go test ./internal/app -run 'TestResolveGate' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/app/service.go internal/app/service_test.go
git commit -m "feat(app): resolve gates into task and fire transitions"
```

---

### Task 7: Update tracker and run full verification

**Files:**
- Modify: `docs/specs/2026-07-02-loop-dashboard-task-breakdown.md`

**Interfaces:**
- Consumes: all prior tasks.
- Produces: updated tracker showing M5 complete.

- [ ] **Step 1: Update tracker checkboxes**

In `docs/specs/2026-07-02-loop-dashboard-task-breakdown.md`:

- Change overall progress M5 from partial to checked.
- Mark T-051 steps and acceptance checklist checked.
- Mark T-052 steps and acceptance checklist checked.
- In final acceptance checklist, keep UI/E2E/docs unchecked; only backend gate resume items should be checked.

- [ ] **Step 2: Run full tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Commit docs update**

```bash
git add docs/specs/2026-07-02-loop-dashboard-task-breakdown.md
git commit -m "docs: mark gate resume backend complete"
```

- [ ] **Step 4: Final status check**

Run:

```bash
git status --short
git log --oneline -8
```

Expected: clean working tree; recent commits include this plan's feature commits.

---

## Self-Review

Spec coverage:

- `TaskGated` lifecycle: Task 1 and Task 4.
- One-shot `resume_prompt`: Task 2 and Task 3.
- Restore prompt package: Task 5.
- App service orchestration: Task 6.
- Defer/reject mapping: Task 3 and Task 6.
- Last 20 stdout/stderr lines: Task 5 and Task 6.
- Tracker update: Task 7.

No placeholders remain. Function/type names are consistent across tasks: `ResumeGatedTaskInput`, `ResumeGatedTask`, `SetTaskStatus`, `restore.BuildPrompt`, `restore.TailFile`, `TaskGated`.
