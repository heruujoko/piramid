# Pi-Ramid v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Pi-Ramid v1 as a machine-wide Go service that turns goals into task graphs, schedules Pi executions across projects, verifies results through separate Pi invocations, retries failures, survives restarts, and exposes CLI and TUI clients.

**Architecture:** A headless application service owns intake and orchestration. Domain packages contain immutable contracts and lifecycle rules; adapters provide SQLite, filesystem, Pi process, HTTP, CLI, and TUI integrations. SQLite stores authoritative metadata while prompts, logs, reports, and artifacts remain human-readable files under `~/.piramid`.

**Tech Stack:** Go, Cobra, Bubble Tea, `modernc.org/sqlite`, `gopkg.in/yaml.v3`, standard `net/http`, Server-Sent Events, Go `testing`.

---

## Delivery sequence

```text
Foundation
  ├─ Task 1: Go module and executable shell
  ├─ Task 2: Domain contracts and validation
  ├─ Task 3: Home, initialization, and configuration
  └─ Task 4: SQLite schema and repositories

Execution core
  ├─ Task 5: Filesystem records and prompt rendering
  ├─ Task 6: Runtime adapters and process supervision
  ├─ Task 7: Goal intake and planner
  ├─ Task 8: Scheduler and workspace leases
  ├─ Task 9: Executor, verifier, and retry lifecycle
  └─ Task 10: Crash recovery

Operator surfaces
  ├─ Task 11: HTTP API, client, and event streaming
  ├─ Task 12: Foreground and daemon CLI
  ├─ Task 13: Goal and task-management CLI
  ├─ Task 14: Read-only doctor
  └─ Task 15: Operational TUI

Release
  └─ Task 16: End-to-end tests, documentation, and builds
```

Tasks are ordered. Tasks 5 and 6 may run in parallel after Task 4. Tasks 13
and 14 may run in parallel after Task 12. All other tasks should preserve the
sequence above.

## Package and file map

```text
cmd/piramid/main.go                 executable entrypoint
internal/app/service.go             application use cases
internal/config/config.go           YAML configuration and defaults
internal/domain/goal.go             goal records and planner output
internal/domain/task.go             immutable task contract
internal/domain/attempt.go          attempts and verification contracts
internal/domain/status.go           lifecycle states and transition rules
internal/domain/validate.go         graph and contract validation
internal/home/home.go               ~/.piramid path resolution and init
internal/store/store.go             persistence interfaces and transactions
internal/store/sqlite/              embedded SQLite implementation
internal/records/records.go         atomic human-readable file records
internal/prompt/render.go           deterministic prompt composition
internal/runtime/runtime.go         runtime adapter interfaces
internal/runtime/command.go         shell-free subprocess execution
internal/intake/service.go          natural-language goal planning
internal/engine/scheduler.go        readiness and dispatch
internal/engine/runner.go           attempt lifecycle
internal/engine/recovery.go         restart reconciliation
internal/api/server.go              HTTP/JSON and SSE server
internal/api/client.go              shared CLI/TUI client
internal/cli/                       Cobra commands
internal/doctor/doctor.go           read-only dependency checks
internal/tui/                       Bubble Tea state and views
internal/testutil/                  fixtures and deterministic fake runtimes
migrations/                         embedded forward-only SQL migrations
docs/                               operator and task-contract documentation
```

## Task 1: Bootstrap the Go module and executable

**Depends on:** None

**Files:**

- Create: `go.mod`
- Create: `cmd/piramid/main.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/root_test.go`
- Create: `.gitignore`
- Create: `Makefile`

- [ ] **Step 1: Write the root command test**

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandShowsProductName(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Pi-Ramid") {
		t.Fatalf("help output did not identify Pi-Ramid: %q", out.String())
	}
}
```

- [ ] **Step 2: Initialize dependencies and verify the test fails**

Run:

```bash
go mod init github.com/heruujoko/piramid
go get github.com/spf13/cobra
go test ./internal/cli
```

Expected: compilation fails because `NewRootCommand` does not exist.

- [ ] **Step 3: Implement the root command and executable**

`internal/cli/root.go`:

```go
package cli

import "github.com/spf13/cobra"

func NewRootCommand() *cobra.Command {
	return &cobra.Command{
		Use:           "piramid",
		Short:         "Pi-Ramid AI work orchestrator",
		Long:          "Pi-Ramid schedules, executes, verifies, and records AI work delegated to Pi.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
}
```

`cmd/piramid/main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/heruujoko/piramid/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Add standard build commands**

`Makefile` must expose:

```make
.PHONY: test build vet

test:
	go test ./...

build:
	go build -o bin/piramid ./cmd/piramid

vet:
	go vet ./...
```

`.gitignore` must include:

```text
/bin/
/.tmp/
*.db
*.db-shm
*.db-wal
```

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./...
go vet ./...
go build -o bin/piramid ./cmd/piramid
```

Expected: all commands exit successfully and `bin/piramid --help` identifies
Pi-Ramid.

Commit:

```bash
git add go.mod go.sum Makefile .gitignore cmd/piramid internal/cli
git commit -m "chore: bootstrap piramid command"
```

## Task 2: Define immutable domain contracts and lifecycle rules

**Depends on:** Task 1

**Files:**

- Create: `internal/domain/goal.go`
- Create: `internal/domain/task.go`
- Create: `internal/domain/attempt.go`
- Create: `internal/domain/status.go`
- Create: `internal/domain/validate.go`
- Create: `internal/domain/validate_test.go`

- [ ] **Step 1: Write table-driven validation tests**

Cover these exact cases:

- valid single-task graph;
- empty task ID;
- relative project path;
- empty DOD;
- unknown dependency;
- dependency cycle;
- duplicate task ID;
- non-positive timeout;
- non-positive maximum attempts.

Use this canonical valid fixture:

```go
func validPlan() domain.Plan {
	return domain.Plan{
		Version: 1,
		GoalID:  "GOAL-1",
		Tasks: []domain.Task{{
			ID:          "TASK-1",
			Title:       "Maintain PR",
			Goal:        "Leave the pull request ready for review.",
			ProjectPath: "/tmp/project",
			DOD:         []string{"required checks pass"},
			MaxAttempts: 3,
			Timeout:     time.Hour,
		}},
	}
}
```

- [ ] **Step 2: Run the tests and confirm missing domain types fail compilation**

Run:

```bash
go test ./internal/domain
```

Expected: compilation fails because `Plan`, `Task`, and `ValidatePlan` are
undefined.

- [ ] **Step 3: Implement the contracts**

Define:

```go
type Goal struct {
	ID          string
	Text        string
	ProjectPath string
	Status      GoalStatus
	CreatedAt   time.Time
}

type Plan struct {
	Version int    `yaml:"version" json:"version"`
	GoalID  string `yaml:"goal_id" json:"goal_id"`
	Tasks   []Task `yaml:"tasks" json:"tasks"`
}

type Task struct {
	ID           string        `yaml:"id" json:"id"`
	ParentTaskID string        `yaml:"parent_task_id,omitempty" json:"parent_task_id,omitempty"`
	Title        string        `yaml:"title" json:"title"`
	Goal         string        `yaml:"goal" json:"goal"`
	ProjectPath  string        `yaml:"project_path" json:"project_path"`
	Inputs       []Input       `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs      []Output      `yaml:"expected_outputs,omitempty" json:"expected_outputs,omitempty"`
	DOD          []string      `yaml:"dod" json:"dod"`
	Model        string        `yaml:"model,omitempty" json:"model,omitempty"`
	MaxAttempts  int           `yaml:"max_attempts" json:"max_attempts"`
	Timeout      time.Duration `yaml:"-" json:"-"`
	TimeoutText  string        `yaml:"timeout" json:"timeout"`
	DependsOn    []string      `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
}

type Verification struct {
	Status      VerificationStatus `yaml:"status" json:"status"`
	Reasons     []string           `yaml:"reasons" json:"reasons"`
	RetryPrompt string             `yaml:"retry_prompt,omitempty" json:"retry_prompt,omitempty"`
}
```

Define explicit task states:

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
)
```

Also define the persisted read models used by later packages:

```go
type TaskRecord struct {
	Task
	Status       TaskStatus
	AttemptCount int
	NextRunAt    time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Attempt struct {
	ID            int64
	TaskID        string
	AttemptNumber int
	WorkerID      string
	Status        AttemptStatus
	StartedAt     time.Time
}

type TaskView struct {
	TaskRecord
	Dependencies []string
	Attempts     []Attempt
}
```

Implement `CanTransition(from, to TaskStatus) bool` from the state diagram in
the design specification. Reject every transition not explicitly listed.

- [ ] **Step 4: Implement graph validation**

`ValidatePlan` must:

1. parse `TimeoutText` with `time.ParseDuration`;
2. require absolute, cleaned project paths;
3. require unique IDs and non-empty goals/DOD;
4. require positive maximum attempts and timeout;
5. verify every dependency exists;
6. detect cycles with a three-color depth-first traversal;
7. return errors containing the task ID and violated field.

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./internal/domain
go test ./...
```

Expected: all validation and transition tests pass.

Commit:

```bash
git add internal/domain
git commit -m "feat: define task and lifecycle contracts"
```

## Task 3: Implement machine-wide home, initialization, and configuration

**Depends on:** Task 2

**Files:**

- Create: `internal/home/home.go`
- Create: `internal/home/init.go`
- Create: `internal/home/home_test.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `internal/cli/init.go`

- [ ] **Step 1: Write home-resolution and initialization tests**

Tests must assert:

- `PIRAMID_HOME` overrides the OS home;
- default resolves to `<user-home>/.piramid`;
- initialization creates every directory from the design;
- `orchestrator.md`, `planner.md`, `executor.md`, and `verifier.md` exist and
  are empty;
- repeated initialization is idempotent;
- an existing config is not overwritten.

- [ ] **Step 2: Write config default and validation tests**

Expected defaults:

```go
Config{
	Version: 1,
	Server: ServerConfig{Host: "127.0.0.1", Port: 7433},
	Workers: WorkersConfig{Count: 3},
}
```

Validation must reject an empty host, a port outside `1..65535`, a worker count
below one, unknown adapter names, empty commands, and runtime arguments with
unknown placeholders.

- [ ] **Step 3: Run tests and confirm failure**

Run:

```bash
go test ./internal/home ./internal/config
```

Expected: compilation fails because home and config functions are undefined.

- [ ] **Step 4: Implement home paths and idempotent initialization**

Expose:

```go
type Paths struct {
	Root      string
	Config    string
	Database  string
	Prompts   string
	Goals     string
	Tasks     string
	Attempts  string
	Artifacts string
	Runtime   string
}

func Resolve() (Paths, error)
func Init(paths Paths) error
```

Create directories with `0700`, regular files with `0600`, and config with
safe defaults. Do not replace existing files.

- [ ] **Step 5: Implement typed YAML configuration**

Use `yaml.Decoder.KnownFields(true)` so misspelled fields fail. Define separate
planner, executor, and verifier runtime configurations. Validate the
allow-listed placeholders during load.

- [ ] **Step 6: Add `piramid init`**

The command calls `home.Resolve`, `home.Init`, and prints the resolved root.
It performs no network or Pi invocation.

- [ ] **Step 7: Verify and commit**

Run:

```bash
go test ./internal/home ./internal/config ./internal/cli
go test ./...
```

Expected: initialization tests pass without writing to the real home directory.

Commit:

```bash
git add internal/home internal/config internal/cli/init.go
git commit -m "feat: initialize machine-wide piramid home"
```

## Task 4: Add embedded SQLite schema, transactions, and repositories

**Depends on:** Task 3

**Files:**

- Create: `migrations/001_initial.sql`
- Create: `internal/store/store.go`
- Create: `internal/store/sqlite/database.go`
- Create: `internal/store/sqlite/migrate.go`
- Create: `internal/store/sqlite/goals.go`
- Create: `internal/store/sqlite/tasks.go`
- Create: `internal/store/sqlite/attempts.go`
- Create: `internal/store/sqlite/events.go`
- Create: `internal/store/sqlite/store_test.go`

- [ ] **Step 1: Add the embedded SQLite dependency**

Run:

```bash
go get modernc.org/sqlite
```

- [ ] **Step 2: Write repository integration tests**

Use a temporary database and assert:

- opening enables `foreign_keys`, WAL mode, and busy timeout;
- a goal and multi-task graph commit atomically;
- duplicate task IDs roll back the entire admission;
- dependency foreign keys reject unknown tasks;
- attempts are unique by `(task_id, attempt_number)`;
- task transition and event insertion occur in one transaction;
- reopening preserves all records.

- [ ] **Step 3: Run tests and confirm failure**

Run:

```bash
go test ./internal/store/sqlite
```

Expected: compilation fails because `Open` and repository methods are missing.

- [ ] **Step 4: Create the initial schema**

`001_initial.sql` must create:

```sql
goals(id, text, project_path, status, plan_path, created_at, updated_at)
tasks(id, goal_id, parent_task_id, title, goal_text, project_path,
      task_path, task_hash, status, model, max_attempts, timeout_seconds,
      attempt_count, next_run_at, created_at, updated_at)
task_dependencies(task_id, depends_on_task_id)
attempts(id, task_id, attempt_number, worker_id, status, runtime,
         model, prompt_path, prompt_hash, stdout_path, stderr_path,
         process_id, exit_code, started_at, finished_at, failure_class)
artifacts(id, attempt_id, relative_path, absolute_path, sha256, size_bytes)
verifications(attempt_id, status, report_path, reason_summary, retry_prompt)
workspace_leases(id, project_path, mode, holder_type, holder_id, acquired_at)
events(id, entity_type, entity_id, event_type, payload_json, created_at)
schema_migrations(version, applied_at)
```

Add foreign keys and indexes for status/next-run queries, task dependencies in
both directions, attempt lookup, lease project/mode lookup, and event ordering.
Lease transactions enforce these rules:

- any number of `READ` planner leases may coexist;
- a `WRITE` executor/verifier lease requires no existing lease for the project;
- a `READ` lease cannot start while a `WRITE` lease exists;
- executor-to-verifier transition retains the same `WRITE` lease.

- [ ] **Step 5: Implement transaction-focused interfaces**

Expose narrow methods rather than generic SQL access:

```go
type Store interface {
	AdmitPlan(context.Context, domain.Goal, domain.Plan, PersistedPaths) error
	ListRunnable(context.Context, time.Time, int) ([]domain.TaskRecord, error)
	AcquireReadLease(context.Context, projectPath, holderID string) error
	ReleaseReadLease(context.Context, projectPath, holderID string) error
	StartAttempt(context.Context, StartAttemptInput) (domain.Attempt, error)
	MoveToVerification(context.Context, FinishExecutionInput) error
	FinishVerification(context.Context, FinishVerificationInput) error
	RecordOperationalFailure(context.Context, OperationalFailureInput) error
	RecoverActive(context.Context, time.Time) ([]domain.InterruptedAttempt, error)
	GetTask(context.Context, string) (domain.TaskView, error)
	ListTasks(context.Context, TaskFilter) ([]domain.TaskView, error)
}
```

Each lifecycle method must update state, append an event, and manage leases in
one database transaction.

- [ ] **Step 6: Verify and commit**

Run:

```bash
go test ./internal/store/sqlite -race
go test ./...
```

Expected: all repository tests pass and no transaction leaves partial rows.

Commit:

```bash
git add go.mod go.sum migrations internal/store
git commit -m "feat: persist orchestration state in sqlite"
```

## Task 5: Add human-readable records and deterministic prompt rendering

**Depends on:** Task 4

**Files:**

- Create: `internal/records/records.go`
- Create: `internal/records/records_test.go`
- Create: `internal/prompt/render.go`
- Create: `internal/prompt/render_test.go`

- [ ] **Step 1: Write atomic-record tests**

Assert that:

- goal, plan, task, attempt, and verification paths match the design layout;
- YAML writes use a sibling temporary file and atomic rename;
- an existing immutable task file cannot be overwritten with different bytes;
- SHA-256 and byte size match persisted content;
- paths cannot escape the Pi-Ramid home.

- [ ] **Step 2: Write prompt rendering tests**

Use fixed prompt files and assert exact order:

```text
orchestrator policy

role policy

--- TASK PACKAGE ---
<canonical YAML>

--- RETRY FEEDBACK ---
<verifier text>
```

The retry section must be absent on attempt one. Empty policy files must not
produce leading blank sections. Repeated rendering must produce identical
bytes and hashes.

- [ ] **Step 3: Run tests and confirm failure**

Run:

```bash
go test ./internal/records ./internal/prompt
```

Expected: compilation fails because record and renderer APIs are missing.

- [ ] **Step 4: Implement atomic records**

Expose role-specific methods:

```go
WriteGoal(goal domain.Goal) (FileRecord, error)
WritePlan(goalID string, plan domain.Plan) (FileRecord, error)
WriteTask(task domain.Task) (FileRecord, error)
CreateAttempt(taskID string, number int) (AttemptPaths, error)
WriteVerification(paths AttemptPaths, v domain.Verification) (FileRecord, error)
```

Canonical YAML uses two-space indentation and stable struct field order.

- [ ] **Step 5: Implement deterministic prompt rendering**

Expose:

```go
type Role string

const (
	RolePlanner  Role = "planner"
	RoleExecutor Role = "executor"
	RoleVerifier Role = "verifier"
)

type RenderInput struct {
	Role          Role
	Orchestrator  []byte
	RolePolicy    []byte
	Body          []byte
	RetryFeedback string
}

func Render(input RenderInput) (content []byte, sha256Hex string)
```

- [ ] **Step 6: Verify and commit**

Run:

```bash
go test ./internal/records ./internal/prompt
go test ./...
```

Expected: exact-byte prompt tests and immutable-record tests pass.

Commit:

```bash
git add internal/records internal/prompt
git commit -m "feat: persist records and render prompts"
```

## Task 6: Implement safe runtime adapters and process supervision

**Depends on:** Task 4

**Files:**

- Create: `internal/runtime/runtime.go`
- Create: `internal/runtime/template.go`
- Create: `internal/runtime/command.go`
- Create: `internal/runtime/command_test.go`
- Create: `internal/testutil/helperprocess.go`

- [ ] **Step 1: Write template expansion tests**

Assert:

- every documented placeholder expands;
- unknown placeholders fail;
- arguments remain separate when values contain spaces or shell characters;
- prompt content may be supplied directly or by prompt file;
- no shell is involved.

- [ ] **Step 2: Write subprocess tests**

The deterministic helper process must:

- print separate stdout and stderr lines;
- print its working directory;
- print selected safe environment values;
- exit with a requested code;
- wait until cancelled.

Tests must assert streaming to separate files, exit code capture, timeout,
context cancellation, process-group termination, and secret-value redaction
from recorded metadata.

- [ ] **Step 3: Run tests and confirm failure**

Run:

```bash
go test ./internal/runtime
```

Expected: compilation fails because adapter APIs are missing.

- [ ] **Step 4: Define the runtime contract**

```go
type Invocation struct {
	Command     string
	Args        []string
	WorkingDir string
	Environment []string
	Timeout     time.Duration
	StdoutPath  string
	StderrPath  string
}

type Result struct {
	ProcessID  int
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	TimedOut   bool
	Interrupted bool
}

type Adapter interface {
	Run(context.Context, Invocation) (Result, error)
}
```

- [ ] **Step 5: Implement `pi-cli` and generic command adapters**

Both adapters use `exec.CommandContext` directly. Set `cmd.Dir` to the
canonical task project path. Create stdout/stderr files before launch and sync
them before returning. On cancellation, terminate the process group, wait a
bounded grace period, then force termination.

- [ ] **Step 6: Verify and commit**

Run:

```bash
go test ./internal/runtime -race
go test ./...
```

Expected: all process tests pass without invoking a real Pi installation.

Commit:

```bash
git add internal/runtime internal/testutil
git commit -m "feat: supervise configurable runtime processes"
```

## Task 7: Build natural-language goal intake through a planner Pi invocation

**Depends on:** Tasks 5 and 6

**Files:**

- Create: `internal/intake/service.go`
- Create: `internal/intake/parser.go`
- Create: `internal/intake/service_test.go`
- Modify: `internal/app/service.go`

- [ ] **Step 1: Write intake service tests**

Cover:

- canonical project path is used as planner working directory;
- planner prompt contains the natural-language goal and exact output schema;
- planner acquires a shared read lease and releases it after plan persistence;
- planner waits when an executor/verifier owns the project's write lease;
- valid one-task and multi-task YAML plans are accepted;
- invalid YAML, cycles, relative paths, and planner non-zero exits are rejected;
- no task is admitted before confirmation;
- confirmed plans persist goal, planner logs, prompt, plan, and task files;
- admission is atomic if persistence fails.

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/intake
```

Expected: compilation fails because `intake.Service` is undefined.

- [ ] **Step 3: Implement the planner instruction contract**

The generated planner body must require:

- YAML only;
- schema version 1;
- one or more tasks;
- absolute project paths;
- terminal DOD statements;
- explicit dependencies;
- maximum attempts and timeout;
- no secrets;
- no prose outside the YAML document.

- [ ] **Step 4: Implement the two-phase intake API**

```go
type DraftRequest struct {
	GoalText   string
	ProjectPath string
}

type Draft struct {
	Goal domain.Goal
	Plan domain.Plan
	Paths records.GoalPaths
}

func (s *Service) Draft(ctx context.Context, req DraftRequest) (Draft, error)
func (s *Service) Confirm(ctx context.Context, goalID string) error
func (s *Service) Reject(ctx context.Context, goalID string) error
```

`Draft` invokes the planner and persists draft evidence. `Confirm` validates
again and admits the complete graph transactionally. `Reject` preserves the
draft but marks it rejected. `Draft` holds a shared project read lease from
before process launch until planner output and logs are durably recorded.

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./internal/intake -race
go test ./...
```

Expected: all planner and admission tests pass with a fake runtime.

Commit:

```bash
git add internal/intake internal/app
git commit -m "feat: plan natural-language goals through pi"
```

## Task 8: Implement deterministic scheduling and workspace leases

**Depends on:** Task 7

**Files:**

- Create: `internal/engine/scheduler.go`
- Create: `internal/engine/scheduler_test.go`
- Create: `internal/engine/workers.go`

- [ ] **Step 1: Write scheduler tests with a fake clock**

Assert:

- dependencies must complete before dispatch;
- failed or cancelled dependencies block descendants;
- `next_run_at` controls retry readiness;
- enqueue time and task ID provide deterministic tie-breaking;
- two tasks for one canonical project never run concurrently;
- different projects can use separate workers concurrently;
- planner capacity cannot consume all executor slots;
- cancellation before dispatch prevents process launch.

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/engine -run Scheduler
```

Expected: compilation fails because the scheduler is missing.

- [ ] **Step 3: Implement worker and scheduler types**

```go
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type Scheduler struct {
	store       store.Store
	clock       Clock
	workerCount int
	dispatch    chan<- Dispatch
}

type Dispatch struct {
	Task    domain.TaskRecord
	Attempt domain.Attempt
}
```

The scheduler asks the store for eligible tasks, acquires the lease and starts
the attempt transactionally, then emits `Dispatch`. It never mutates task state
in memory without a committed store transition.

- [ ] **Step 4: Implement graceful start and stop**

`Run(ctx)` stops selecting new work when context is cancelled and waits for
active dispatch loops to report completion. It must not abandon a committed
running attempt silently.

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./internal/engine -run Scheduler -race
go test ./...
```

Expected: deterministic scheduling tests pass under the race detector.

Commit:

```bash
git add internal/engine/scheduler.go internal/engine/scheduler_test.go internal/engine/workers.go
git commit -m "feat: schedule dependency-aware project work"
```

## Task 9: Implement executor, verifier, retry, and completion lifecycle

**Depends on:** Task 8

**Files:**

- Create: `internal/engine/runner.go`
- Create: `internal/engine/verifier.go`
- Create: `internal/engine/runner_test.go`
- Modify: `internal/store/store.go`
- Modify: `internal/store/sqlite/attempts.go`

- [ ] **Step 1: Write lifecycle tests**

Cover:

- executor exit zero always enters verification;
- executor exit non-zero with useful evidence enters verification;
- launch failure repeats unchanged invocation without invented retry feedback;
- timeout records operational failure;
- verifier `PASS` completes task and releases lease;
- verifier `FAIL` persists exact retry prompt and schedules retry;
- missing retry prompt on `FAIL` is a verifier-system failure;
- malformed status is a verifier-system failure;
- declared output files are discovered, hashed, sized, and linked to the attempt;
- missing or escaping output paths are included in verifier evidence and never
  copied as trusted artifacts;
- attempt limit produces terminal failure;
- previous attempts and files remain unchanged.

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/engine -run Runner
```

Expected: compilation fails because runner and verifier parsers are missing.

- [ ] **Step 3: Implement verifier parsing**

Use strict YAML decoding with known fields. Accept only uppercase `PASS` and
`FAIL`. Require at least one reason. Require non-empty `retry_prompt` for
`FAIL` when another attempt is allowed. Reject trailing documents.

- [ ] **Step 4: Implement the attempt runner**

The exact sequence is:

1. create attempt directories;
2. render and persist executor prompt;
3. execute runtime in the project directory;
4. persist process result;
5. resolve declared outputs beneath the project directory;
6. calculate SHA-256 and byte size for regular output files;
7. persist artifact metadata without copying project contents;
8. transactionally move task to `VERIFYING`;
9. render and persist verifier prompt with artifact evidence;
10. execute separate verifier runtime;
11. parse and persist verification;
12. transactionally complete, retry, or fail;
13. release project lease only after terminal verification handling.

Artifact path resolution rejects symlink or path traversal escapes from the
canonical project directory. Missing outputs remain verifier evidence rather
than causing Pi-Ramid to make the quality decision itself.

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./internal/engine -run 'Runner|Verifier' -race
go test ./...
```

Expected: lifecycle tests demonstrate a fail-then-pass task with two immutable
attempts.

Commit:

```bash
git add internal/engine internal/store
git commit -m "feat: execute verify and retry task attempts"
```

## Task 10: Recover safely after engine interruption

**Depends on:** Task 9

**Files:**

- Create: `internal/engine/recovery.go`
- Create: `internal/engine/recovery_test.go`
- Modify: `internal/store/sqlite/attempts.go`
- Modify: `internal/store/sqlite/tasks.go`

- [ ] **Step 1: Write recovery tests**

Create persisted fixtures for:

- running attempt with no live process;
- verifying attempt with no live process;
- stale workspace lease;
- retry-wait task whose time elapsed;
- completed task with historical lease corruption;
- database state that cannot be reconciled.

Assert that active orphan attempts become interrupted, stale leases release,
eligible tasks create new attempts, completed history is unchanged, and an
unrecoverable persistence error prevents engine startup.

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/engine -run Recovery
```

Expected: compilation fails because recovery is missing.

- [ ] **Step 3: Implement recovery**

Expose:

```go
type ProcessInspector interface {
	Exists(pid int) bool
}

func Recover(ctx context.Context, st store.Store, inspector ProcessInspector, now time.Time) error
```

Recovery must run before the API listener and scheduler start. It appends events
for every reconciliation and never converts an interrupted attempt to
completed.

- [ ] **Step 4: Verify and commit**

Run:

```bash
go test ./internal/engine -run Recovery -race
go test ./...
```

Expected: all crash fixtures recover deterministically.

Commit:

```bash
git add internal/engine internal/store/sqlite
git commit -m "feat: recover interrupted orchestration state"
```

## Task 11: Expose a versioned HTTP API and SSE event stream

**Depends on:** Task 10

**Files:**

- Create: `internal/api/server.go`
- Create: `internal/api/routes.go`
- Create: `internal/api/events.go`
- Create: `internal/api/client.go`
- Create: `internal/api/server_test.go`
- Modify: `internal/app/service.go`

- [ ] **Step 1: Write API contract tests**

Test these endpoints:

```text
GET    /v1/health
POST   /v1/goals/draft
POST   /v1/goals/{id}/confirm
POST   /v1/goals/{id}/reject
POST   /v1/tasks
GET    /v1/tasks
GET    /v1/tasks/{id}
POST   /v1/tasks/{id}/retry
POST   /v1/tasks/{id}/cancel
GET    /v1/workers
GET    /v1/attempts/{id}/logs?stream=stdout&offset=0&limit=65536
GET    /v1/events
```

Assert JSON content type, bounded log reads, stable error envelopes, request
size limits, context cancellation, and SSE replay from `Last-Event-ID`.

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/api
```

Expected: compilation fails because server and client are missing.

- [ ] **Step 3: Implement API envelopes and routing**

Use standard `http.ServeMux`. Responses follow:

```go
type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
```

Map validation to 400, missing records to 404, illegal lifecycle actions to
409, and internal failures to 500 without leaking secrets or SQL.

- [ ] **Step 4: Implement the shared client**

The client owns base URL construction from host and port, JSON encoding,
timeouts, API error decoding, SSE reconnection, and bounded log retrieval.
CLI and TUI must not call repositories directly.

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./internal/api -race
go test ./...
```

Expected: API and client tests pass against `httptest.Server`.

Commit:

```bash
git add internal/api internal/app
git commit -m "feat: expose orchestration http api"
```

## Task 12: Add foreground and daemon engine commands

**Depends on:** Task 11

**Files:**

- Create: `internal/app/bootstrap.go`
- Create: `internal/cli/start.go`
- Create: `internal/cli/start_test.go`
- Create: `internal/daemon/daemon.go`
- Create: `internal/daemon/daemon_test.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Write startup tests**

Assert:

- `piramid start` runs in foreground;
- `--s` and `--p` override config;
- default address is `127.0.0.1:7433`;
- non-loopback binding prints a warning;
- startup performs migrations and recovery before listening;
- `--d` launches the same executable with an internal child marker;
- daemon PID file is written only after successful listener startup;
- duplicate listener startup fails clearly;
- SIGINT and SIGTERM stop dispatch and close cleanly.

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/cli ./internal/daemon
```

Expected: compilation fails because start and daemon facilities are missing.

- [ ] **Step 3: Implement application bootstrap**

Bootstrap order:

```text
resolve home
load config
open database
apply migrations
construct records and runtimes
recover state
construct services and scheduler
bind TCP listener
start API and scheduler
```

Return a cleanup function that stops dispatch, waits for workers, closes HTTP,
and closes SQLite.

- [ ] **Step 4: Implement daemon re-exec**

Use `os.Executable` and `os.StartProcess`; do not invoke a shell. Redirect
daemon stdout/stderr to files under `~/.piramid/runtime/`. Parent waits for a
bounded readiness handshake before reporting success.

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./internal/cli ./internal/daemon -race
go test ./...
```

Expected: foreground and daemon fixture tests pass without leaving child
processes running.

Commit:

```bash
git add internal/app internal/cli internal/daemon
git commit -m "feat: run piramid in foreground or daemon mode"
```

## Task 13: Complete goal and task-management CLI commands

**Depends on:** Task 12

**Files:**

- Create: `internal/cli/clientflags.go`
- Create: `internal/cli/goal.go`
- Create: `internal/cli/enqueue.go`
- Create: `internal/cli/queue.go`
- Create: `internal/cli/workers.go`
- Create: `internal/cli/inspect.go`
- Create: `internal/cli/retry.go`
- Create: `internal/cli/cancel.go`
- Create: `internal/cli/commands_test.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Write command tests against a fake API**

Assert:

- `goal --project PATH TEXT` drafts and prints task/dependency preview;
- interactive confirmation calls confirm only on affirmative input;
- `--yes` confirms without reading stdin;
- rejected confirmation preserves the draft and does not enqueue;
- `enqueue` sends structured YAML;
- `queue`, `workers`, and `inspect` print stable human-readable tables;
- `retry` and `cancel` require task IDs;
- every client command accepts `--s` and `--p`;
- connection errors mention the attempted address.

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/cli -run Commands
```

Expected: command constructors are undefined.

- [ ] **Step 3: Implement shared client flags**

Defaults are `127.0.0.1` and `7433`. Build `http://HOST:PORT` without silently
changing schemes or accepting malformed ports.

- [ ] **Step 4: Implement goal preview**

The preview includes goal ID, each task ID/title/project, dependencies, DOD
count, maximum attempts, and timeout. It never displays credential environment
values or full prompts.

- [ ] **Step 5: Implement remaining commands**

All state-changing commands call the API. Exit non-zero on API errors.
Human-readable output goes to stdout; diagnostics go to stderr.

- [ ] **Step 6: Verify and commit**

Run:

```bash
go test ./internal/cli -race
go test ./...
```

Expected: all CLI command tests pass.

Commit:

```bash
git add internal/cli
git commit -m "feat: add goal and task cli workflows"
```

## Task 14: Implement read-only `piramid doctor`

**Depends on:** Task 12

**Files:**

- Create: `internal/doctor/check.go`
- Create: `internal/doctor/doctor.go`
- Create: `internal/doctor/doctor_test.go`
- Create: `internal/cli/doctor.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Write doctor tests**

Assert:

- checks report `PASS`, `WARN`, or `FAIL`;
- missing Node.js and Pi are failures with remediation;
- empty policy files are warnings;
- missing home is reported without creating it;
- schema compatibility is inspected without migration;
- default mode never invokes Pi;
- explicit smoke-test mode may invoke a non-mutating Pi probe;
- non-loopback address without authentication is warned;
- no checked file changes content or modification time.

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./internal/doctor
```

Expected: compilation fails because doctor APIs are missing.

- [ ] **Step 3: Implement composable checks**

```go
type Status string

const (
	Pass Status = "PASS"
	Warn Status = "WARN"
	Fail Status = "FAIL"
)

type Result struct {
	Name        string
	Status      Status
	Message     string
	Remediation string
}

type Check interface {
	Run(context.Context) Result
}
```

Checks inspect OS/architecture, home permissions, config, embedded SQLite,
schema compatibility, TCP reachability, Node.js, Pi, runtime templates,
policy files, and optional project Git/GitHub tooling.

- [ ] **Step 4: Add the command**

`piramid doctor` runs checks concurrently where they have no ordering
dependency, prints deterministic grouped output, and exits non-zero when any
check fails.

- [ ] **Step 5: Verify and commit**

Run:

```bash
go test ./internal/doctor ./internal/cli -race
go test ./...
```

Expected: read-only invariants pass.

Commit:

```bash
git add internal/doctor internal/cli
git commit -m "feat: diagnose piramid dependencies safely"
```

## Task 15: Build the operational TUI as an API client

**Depends on:** Tasks 13 and 14

**Files:**

- Create: `internal/tui/model.go`
- Create: `internal/tui/update.go`
- Create: `internal/tui/view.go`
- Create: `internal/tui/styles.go`
- Create: `internal/tui/model_test.go`
- Create: `internal/cli/tui.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Add TUI dependencies**

Run:

```bash
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
```

- [ ] **Step 2: Write state-model tests**

Assert:

- initial load requests tasks and workers;
- event messages update task and worker rows;
- reconnect resumes from the last event ID;
- selected task loads attempts and verification;
- log tail requests are bounded;
- terminal resize changes layout without losing selection;
- goal submission uses the same draft/confirm API;
- no TUI message accesses SQLite or runtime adapters directly.

- [ ] **Step 3: Run tests and confirm failure**

Run:

```bash
go test ./internal/tui
```

Expected: compilation fails because the Bubble Tea model is missing.

- [ ] **Step 4: Implement the model and views**

Required views:

- Queue;
- Running Workers;
- Completed;
- Failed and Blocked;
- Task Details;
- Attempt History;
- Verification Report;
- Live Log;
- Goal Draft Preview.

Keyboard behavior:

```text
q / ctrl+c   quit
tab          next view
shift+tab    previous view
up/down      move selection
enter        inspect
g            submit goal
r            retry selected failed task
x            cancel selected active task
l            toggle stdout/stderr
```

State-changing keys show confirmation before sending API requests.

- [ ] **Step 5: Add `piramid tui`**

The command accepts `--s` and `--p`, constructs the shared API client, and runs
the Bubble Tea program in alternate-screen mode.

- [ ] **Step 6: Verify and commit**

Run:

```bash
go test ./internal/tui ./internal/cli -race
go test ./...
```

Expected: deterministic update/view tests pass without a live terminal.

Commit:

```bash
git add go.mod go.sum internal/tui internal/cli
git commit -m "feat: add operational terminal interface"
```

## Task 16: Prove the full lifecycle and prepare releases

**Depends on:** Task 15

**Files:**

- Create: `test/e2e/fixtures/fake-pi/main.go`
- Create: `test/e2e/lifecycle_test.go`
- Create: `test/e2e/recovery_test.go`
- Create: `docs/task-contract.md`
- Create: `docs/configuration.md`
- Create: `docs/operations.md`
- Create: `README.md`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`
- Modify: `Makefile`

- [ ] **Step 1: Implement a deterministic fake Pi**

The fixture reads its prompt, emits planner YAML for planner prompts, creates a
declared artifact for executor prompts, fails the first verification with an
exact retry prompt, and passes the second verification. Behavior is controlled
only by temporary fixture state.

- [ ] **Step 2: Write the end-to-end lifecycle test**

The test must:

1. initialize a temporary `PIRAMID_HOME`;
2. initialize a temporary Git project;
3. configure fake planner/executor/verifier commands;
4. start the engine on an ephemeral loopback port;
5. submit and confirm a natural-language goal;
6. observe attempt one fail verification;
7. observe attempt two receive exact retry feedback and pass;
8. assert immutable prompts, logs, reports, events, and artifacts;
9. assert task status is `COMPLETED`.

- [ ] **Step 3: Write the restart test**

Terminate the engine during execution, restart it with the same home, and
assert the interrupted attempt is preserved, a new attempt runs, no stale
lease remains, and the final task completes.

- [ ] **Step 4: Document operator contracts**

`README.md` must cover installation, `init`, `doctor`, foreground/daemon
startup, goal submission, direct YAML enqueue, CLI inspection, TUI startup,
default address, and the trusted-single-user security boundary.

The remaining docs must describe every config key, task schema field, lifecycle
state, retry rule, filesystem path, backup requirement, and recovery behavior.

- [ ] **Step 5: Add CI and release builds**

CI runs:

```bash
go test ./... -race
go vet ./...
go build ./cmd/piramid
```

Release builds produce checksummed binaries for current supported combinations
of Linux, macOS, and Windows on amd64 and arm64. The CGo-free SQLite driver
must keep `CGO_ENABLED=0` builds working.

- [ ] **Step 6: Run final verification**

Run:

```bash
go test ./... -race
go vet ./...
CGO_ENABLED=0 go build -trimpath -o bin/piramid ./cmd/piramid
go test ./test/e2e -count=1
```

Expected: all commands pass; no test requires a real Pi account.

- [ ] **Step 7: Commit**

```bash
git add README.md Makefile docs test .github
git commit -m "test: verify piramid v1 end to end"
```

## Final acceptance checklist

- [ ] `piramid init` creates safe machine-wide state and empty prompt files.
- [ ] `piramid doctor` is demonstrably read-only.
- [ ] Foreground and daemon modes run the same engine.
- [ ] CLI and TUI communicate only through the TCP API.
- [ ] Natural-language goals produce reviewable one-task or multi-task plans.
- [ ] Direct task YAML admission remains supported.
- [ ] Every Pi role runs with the canonical project directory as `cmd.Dir`.
- [ ] Workspace leases prevent conflicting project mutation through verification.
- [ ] Executor and verifier are separate Pi invocations.
- [ ] Pi-Ramid never edits verifier retry instructions.
- [ ] SQLite contains metadata while large logs remain files.
- [ ] Every attempt, prompt, report, event, and artifact reference is preserved.
- [ ] Interrupted active work recovers as a new attempt.
- [ ] Dependency failures block descendants.
- [ ] Loopback remains the default and non-loopback emits a security warning.
- [ ] Race, vet, build, lifecycle, and restart tests pass.
