# Pi-Ramid v1 Design

Date: 2026-06-25

## 1. Purpose

Pi-Ramid is a machine-wide AI work orchestrator. It accepts either structured
task packages or natural-language goals, delegates planning and execution to
separate Pi invocations, verifies results with another Pi invocation, retries
failed work, and preserves a complete execution history.

Pi-Ramid does not execute LLM requests itself. Pi is treated as a replaceable
black-box runtime.

Version 1 includes:

- a headless orchestration engine;
- a CLI;
- an operational TUI;
- a Pi-powered goal intake service;
- concurrent worker scheduling;
- external verification through a separate Pi invocation;
- automatic retries;
- machine-wide, crash-recoverable state;
- human-readable tasks, prompts, logs, reports, and artifacts.

The core engine is independent of CLI and TUI concerns so a GUI can be added in
version 2 without rewriting orchestration.

## 2. Design Decisions

### 2.1 Language and distribution

Pi-Ramid is implemented in Go and distributed as a single executable.

Go was selected because its process APIs and concurrency model fit worker
supervision, it supports simple cross-compilation, and it avoids requiring a
language runtime on a VPS.

### 2.2 Machine-wide home

All Pi-Ramid state lives under:

```text
~/.piramid/
```

This is not project-local state. One engine can orchestrate work across
multiple repositories and directories.

The home directory can be overridden with `PIRAMID_HOME` for testing,
isolation, or alternate deployments.

### 2.3 Persistence

SQLite is the authoritative store for orchestration metadata. The SQLite
engine is compiled into the Go executable through a CGo-free driver.

Large or human-oriented payloads remain ordinary files:

- original goals;
- generated task packages;
- rendered prompts;
- stdout and stderr streams;
- verification reports;
- declared artifacts.

SQLite stores relationships, lifecycle state, paths, hashes, byte sizes,
timestamps, and summaries. It does not store large process logs or artifact
bodies.

### 2.4 Network API

The engine exposes a TCP API used by the CLI and TUI. The default listener is:

```text
127.0.0.1:7433
```

`--s` overrides the host and `--p` overrides the port. Version 1 has no
authentication. Binding to a non-loopback address emits a prominent warning.
Token authentication is deferred.

## 3. Architectural Boundaries

```text
CLI ───────────────┐
TUI ───────────────┼── TCP API ── Application Services
Future GUI ────────┘                    │
                                       ├── Goal Intake Service
                                       │      └── Planner Runtime Adapter
                                       │
                                       └── Core Orchestration Engine
                                              ├── Scheduler
                                              ├── Worker Manager
                                              ├── Retry Engine
                                              ├── Workspace Leases
                                              ├── Executor Runtime Adapter
                                              ├── Verifier Runtime Adapter
                                              └── Persistence
                                                     ├── SQLite
                                                     └── Filesystem
```

### 3.1 Goal intake service

The intake service converts a natural-language goal into a validated immutable
task graph by invoking Pi in a planner role.

Planning is not part of the core orchestration state machine. This preserves a
deterministic engine while making natural-language submission available to the
CLI, TUI, and future GUI through one shared service.

The planner may produce one task or multiple tasks with dependencies.

### 3.2 Core orchestration engine

The engine owns:

- validated task admission;
- dependency-aware scheduling;
- worker allocation;
- process supervision;
- task and attempt state transitions;
- verification dispatch;
- retry policy;
- crash recovery;
- workspace locking;
- execution history.

It does not interpret vague goals, edit generated artifacts, or synthesize
retry instructions.

### 3.3 Runtime adapters

Planning, execution, and verification each use a configurable runtime adapter.
Version 1 provides:

- `pi-cli`, a safe built-in adapter for Pi;
- `command`, a generic process adapter for testing and future compatible
  runtimes.

Adapters implement the same conceptual contract:

```text
prepare invocation
start process
stream stdout and stderr
wait or cancel
return process result
```

Arguments are passed directly to the process without a shell. Templates may
use only documented placeholders:

- `{{prompt}}`
- `{{prompt_file}}`
- `{{workspace}}`
- `{{task_id}}`
- `{{attempt}}`
- `{{model}}`

Unknown placeholders are validation errors.

## 4. Home Directory Layout

`piramid init` creates:

```text
~/.piramid/
  config.yaml
  state.db
  prompts/
    orchestrator.md
    planner.md
    executor.md
    verifier.md
  goals/
  tasks/
  attempts/
  artifacts/
  runtime/
    piramid.pid
```

All prompt files are created empty. Empty prompt files contribute no text to
an invocation. Users may later add machine-wide policy without changing task
packages.

Representative persisted work:

```text
~/.piramid/
  goals/GOAL-20260625-0001/
    goal.yaml
    planner-prompt.md
    planner-stdout.log
    planner-stderr.log
    generated-plan.yaml
  tasks/PR-MAINTAIN-184/
    task.yaml
  attempts/PR-MAINTAIN-184/0001/
    executor-prompt.md
    stdout.log
    stderr.log
    process.json
    verifier-prompt.md
    verifier-stdout.log
    verifier-stderr.log
    verification.yaml
  artifacts/PR-MAINTAIN-184/0001/
```

Files are written atomically where practical by writing a temporary sibling
and renaming it into place.

## 5. Configuration

An illustrative configuration is:

```yaml
version: 1

server:
  host: 127.0.0.1
  port: 7433

workers:
  count: 3

runtime:
  planner:
    adapter: pi-cli
    command: pi
    args: ["-p", "{{prompt}}"]
    timeout: 30m

  executor:
    adapter: pi-cli
    command: pi
    args: ["-p", "{{prompt}}"]
    timeout: 4h

  verifier:
    adapter: pi-cli
    command: pi
    args: ["-p", "{{prompt}}"]
    timeout: 1h

retry:
  default_max_attempts: 3
  initial_delay: 1m
  max_delay: 30m
  backoff: exponential
```

The configuration is loaded and validated before the engine begins accepting
work. Runtime changes require an engine restart in version 1.

## 6. Goal Intake

### 6.1 CLI usage

The primary natural-language interface is:

```bash
piramid goal \
  --project /srv/projects/my-service \
  "Maintain PR https://github.com/acme/my-service/pull/184"
```

If attached to an interactive terminal, Pi-Ramid:

1. resolves the project path to an absolute canonical directory;
2. confirms the directory exists;
3. invokes the configured planner with that directory as its working directory;
4. asks the planner for a structured task graph;
5. validates the graph;
6. displays a concise plan preview;
7. requires confirmation before enqueueing.

`--yes` skips confirmation for automation. A planner failure or invalid plan
does not enqueue partial work.

The original goal, complete planner prompt, planner output, generated task
graph, and hashes are preserved.

### 6.2 Planner output contract

The planner returns a structured document:

```yaml
version: 1
goal_id: GOAL-20260625-0001
tasks:
  - id: PR-MAINTAIN-184
    title: Maintain pull request 184
    goal: |
      Resolve actionable review feedback and failing checks, then leave the
      pull request ready for human review.
    project:
      path: /srv/projects/my-service
    inputs:
      - type: url
        value: https://github.com/acme/my-service/pull/184
    expected_outputs:
      - type: result
        path: .piramid-results/PR-MAINTAIN-184/result.json
    dod:
      - all required checks pass
      - no unresolved actionable review comments remain
      - requested changes are committed and pushed
      - the pull request has no merge conflict
      - no unrelated files are changed
    model: gpt-5.5
    max_attempts: 10
    timeout: 2h
    depends_on: []
```

Each task must have a stable ID, goal, absolute project path, non-empty DOD,
retry limit, and timeout. Dependencies must reference tasks in the same
generated graph or previously admitted completed tasks. Cycles are rejected.

Task packages are immutable after admission. Operational state is stored
separately.

### 6.3 Direct structured submission

Advanced and automated callers may bypass planning:

```bash
piramid enqueue task.yaml
```

This validates and admits an existing task package through the same admission
service used by goal intake.

## 7. Task and Dependency Model

Task hierarchy and scheduling dependencies are distinct:

- `parent_task_id` optionally represents organizational ownership;
- `depends_on` determines scheduling readiness.

This supports both a parent with children and a directed acyclic dependency
graph where a task may wait for multiple predecessors.

A task becomes runnable only when:

- its status is pending or retry-wait has elapsed;
- every dependency is completed;
- its project workspace is not leased by another running task;
- an executor worker slot is available.

If a dependency reaches terminal failure or cancellation, its dependants become
blocked and are not executed automatically.

## 8. Workspace Semantics

Every task contains a required absolute project path. Before invoking planner,
executor, or verifier Pi-Ramid sets the child process working directory to that
path. It does not depend on prompt text to perform `cd`.

Pi-Ramid acquires an exclusive project lease before an executor starts and
holds it through verification. The default lease key is the canonical project
path. This prevents another task from mutating the checkout while results are
being verified.

Planner invocations acquire a shared read lease and wait while an executor and
verifier hold the exclusive lease. Version 1 assumes one Pi-Ramid engine owns
the configured home directory.

Credentials and environment configuration remain external. Task packages do
not contain secrets. Runtime adapters receive an explicitly configured
environment policy, with inherited credentials redacted from persisted logs.

## 9. Execution Lifecycle

### 9.1 Task states

```text
DRAFT
  → PENDING
  → RUNNING
  → VERIFYING
  → COMPLETED

VERIFYING
  → RETRY_WAIT
  → PENDING

PENDING | RUNNING | VERIFYING | RETRY_WAIT
  → CANCELLED

RUNNING | VERIFYING
  → FAILED

PENDING | RETRY_WAIT
  → BLOCKED
```

`DRAFT` applies only to generated plans awaiting confirmation. Admitted tasks
begin at `PENDING`.

### 9.2 Attempts

Every executor run creates a new immutable attempt. An attempt records:

- task ID and attempt number;
- assigned worker;
- runtime and model;
- start and finish times;
- rendered executor prompt and hash;
- process ID and exit code;
- stdout and stderr paths;
- observed artifacts;
- verifier prompt and result;
- terminal attempt outcome.

Previous attempts are never overwritten.

### 9.3 Executor invocation

The executor prompt is assembled deterministically from:

1. `prompts/orchestrator.md`;
2. `prompts/executor.md`;
3. the immutable task package;
4. retry feedback, only when the attempt is a retry.

Pi-Ramid then starts Pi with the task's project directory as the process
working directory and streams stdout and stderr directly to files.

Process exit success means only that execution finished. It does not complete
the task. Every successful executor process must pass verification.

An executor process that exits unsuccessfully still proceeds to verification
when sufficient logs or artifacts exist. The verifier decides whether another
attempt is useful and supplies any changed execution instructions.

A process that cannot start, is interrupted, or times out before meaningful
verification can occur is an operational failure. Pi-Ramid may repeat the
unchanged original invocation under the task's attempt policy. It records the
operational reason but does not generate a retry prompt.

### 9.4 Verification invocation

Verification always uses a separate Pi process and a separate worker phase.
The verifier receives:

- `prompts/orchestrator.md`;
- `prompts/verifier.md`;
- the original immutable task;
- DOD;
- project directory;
- declared and observed artifacts;
- executor process result;
- references to logs and prior attempts.

The verifier is instructed to remain read-only. Pi-Ramid cannot technically
guarantee that a general-purpose agent will not mutate the workspace, so the
verifier policy and audit record make this constraint explicit. Strong
filesystem sandboxing is deferred.

The verifier must return:

```yaml
status: FAIL
reasons:
  - required integration check is failing
retry_prompt: |
  Diagnose and resolve the failing integration check, run the relevant tests,
  commit the change, and push the pull request branch.
```

or:

```yaml
status: PASS
reasons:
  - all required checks pass
  - no actionable review feedback remains
```

The parser accepts only `PASS` or `FAIL`. A malformed verifier response is a
verification-system failure and consumes an attempt unless a future policy
defines infrastructure retries separately.

Pi-Ramid never authors or edits `retry_prompt`. A failed verifier result must
include a non-empty retry prompt when retries remain.

### 9.5 Retry

On `FAIL`, Pi-Ramid atomically:

1. records the verification report;
2. marks the attempt failed;
3. records the exact retry prompt;
4. computes the next eligible time;
5. moves the task to `RETRY_WAIT`.

The next executor attempt receives the unchanged original task plus the
verifier-provided retry prompt. When `max_attempts` is exhausted, the task
becomes `FAILED`.

## 10. Scheduling and Workers

Workers are logical execution slots managed by one engine process. They are
stateless between attempts.

Planner, executor, and verifier jobs share a common runtime supervision
facility but have separate role configuration and status. The scheduler gives
admitted execution work priority over interactive planning only to the extent
needed to prevent planner requests from consuming all worker capacity.

Version 1 scheduling is deterministic:

1. select eligible tasks by next-run time;
2. preserve enqueue order as a tie-breaker;
3. skip tasks whose dependencies or workspace lease are unavailable;
4. assign the first available compatible worker;
5. commit the lease and running transition before process launch.

No distributed workers are included in version 1.

## 11. Storage Model

The schema is expected to include:

- `goals`;
- `tasks`;
- `task_dependencies`;
- `attempts`;
- `workers`;
- `artifacts`;
- `verifications`;
- `workspace_leases`;
- `events`;
- `schema_migrations`.

Foreign keys enforce ownership and dependency references. Task dependencies
are indexed in both directions.

SQLite runs in WAL mode with foreign keys enabled and a busy timeout. All
writes are serialized through a persistence boundary. Transactions remain
small; process log data is never written through the database.

The append-only `events` table records significant lifecycle transitions for
audit and TUI updates. Current state tables remain authoritative for efficient
queries; restart does not require replaying the entire event history.

Pi-Ramid uses a patched SQLite release that includes the WAL-reset correction
published in March 2026.

## 12. Crash Recovery

On startup, the engine:

1. opens the database and applies forward-only migrations;
2. checks filesystem and database consistency;
3. finds tasks or attempts left in active states;
4. checks whether recorded child processes still exist;
5. marks orphaned processes and attempts as interrupted;
6. releases stale workspace leases;
7. schedules eligible interrupted tasks as new attempts;
8. starts the TCP API and scheduler.

Pi-Ramid never treats a pre-crash running attempt as completed. Recovery creates
a new attempt and preserves the interrupted record.

Daemon startup uses a PID file only for operator convenience. SQLite state and
port ownership determine actual engine health.

## 13. Engine Modes and API Clients

### 13.1 Foreground

```bash
piramid start
```

Runs the engine in the foreground with logs attached to the terminal. This is
the default for testing and service-manager deployments.

### 13.2 Background daemon

```bash
piramid start --d
```

Starts the same engine detached in the background. It writes its PID under
`~/.piramid/runtime/`.

### 13.3 Address overrides

```bash
piramid start --d --s 127.0.0.1 --p 7433
piramid queue --s 127.0.0.1 --p 7433
```

All API clients use the same defaults and flags.

The transport is versioned HTTP/JSON over TCP in version 1. This is easy to
inspect, test, and consume from a future GUI. Streaming log and state updates
use server-sent events. The API is operational, not a public multi-tenant
service.

## 14. CLI

Required version 1 commands:

```text
piramid init
piramid doctor
piramid start [--d] [--s HOST] [--p PORT]
piramid goal --project PATH [--yes] GOAL
piramid enqueue TASK.yaml
piramid queue
piramid workers
piramid inspect TASK_ID
piramid retry TASK_ID
piramid cancel TASK_ID
piramid tui
```

`retry` creates a new attempt for a terminal failed task without deleting its
history. It requires remaining policy capacity or an explicit operator
override recorded in the audit trail.

## 15. Doctor

`piramid doctor` is strictly read-only. It performs no installation,
configuration changes, authentication, migrations, or repairs.

Checks include:

- supported OS and architecture;
- Pi-Ramid home existence and permissions;
- config parsing and semantic validation;
- embedded SQLite open and version;
- schema compatibility without applying migrations;
- TCP address and engine reachability;
- Node.js executable and version;
- Pi executable and version;
- Pi readiness/authentication through a non-mutating probe where supported;
- planner, executor, and verifier adapter templates;
- project Git and GitHub CLI dependencies when a project is supplied;
- warnings for empty prompt policy files;
- warnings for non-loopback configuration without authentication.

Results are reported as `PASS`, `WARN`, or `FAIL`, with actionable remediation.
An optional explicit smoke-test flag may invoke Pi, but the default command
does not spend model tokens.

## 16. TUI

The TUI is an API client and contains no scheduler or persistence logic.

Version 1 views:

- queue and blocked work;
- running planner, executor, and verifier jobs;
- worker status;
- completed and failed tasks;
- task dependencies;
- attempt history;
- verification reports;
- live and historical logs.

The TUI may submit goals and approve generated plans through the same
application API as the CLI. It has no general chat or free-form prompt editor.
Machine-wide prompt files are edited outside the TUI.

## 17. Observability and Audit

Every meaningful operation has a stable goal, task, and attempt identity.
Records use UTC timestamps and include:

- submitted goal and submitter interface;
- generated plan and planner runtime details;
- task package hash;
- every state transition;
- worker assignment;
- process invocation excluding secret environment values;
- rendered prompt path and hash;
- process duration and exit status;
- artifact path, size, and checksum;
- verification report;
- retry reason and eligibility time;
- operator actions such as retry and cancel.

Logs are append-only per process. The TUI tails files through API endpoints
that enforce bounded reads rather than loading complete logs into memory.

## 18. Failure Handling

Failure classes are explicit:

- admission failure: invalid goal plan or task package;
- dependency failure: prerequisite failed or was cancelled;
- launch failure: runtime executable or working directory unavailable;
- execution failure: non-zero exit, timeout, or interruption;
- verification failure: valid `FAIL` result;
- verifier-system failure: launch, timeout, or malformed output;
- persistence failure: state cannot be committed safely;
- recovery failure: state cannot be reconciled after restart.

Persistence failure stops new dispatch and reports the engine unhealthy.
Pi-Ramid must not continue executing work that it cannot record.

## 19. Security Boundaries

Version 1 is intended for a trusted single-user machine or VPS.

- The API binds to loopback by default.
- Non-loopback binding is warned but not blocked.
- Authentication and TLS are deferred.
- Commands are invoked without a shell.
- Template placeholders are allow-listed.
- Task packages cannot define arbitrary environment values by default.
- Secrets are not persisted in tasks, prompts, process metadata, or logs by
  Pi-Ramid itself.
- Project paths are canonicalized before use.

Users remain responsible for the permissions granted to Pi, Git, and GitHub
credentials.

## 20. Testing Strategy

### 20.1 Unit tests

- task contract parsing and validation;
- dependency cycle detection;
- deterministic prompt rendering;
- runtime argument expansion;
- state transition guards;
- retry timing and attempt limits;
- scheduler ordering;
- workspace lease behavior;
- verifier response parsing;
- log path and artifact path validation.

### 20.2 Integration tests

- SQLite migrations and foreign keys;
- concurrent readers and serialized writes;
- executor and verifier subprocess supervision;
- stdout/stderr streaming;
- timeout and cancellation;
- engine restart during execution;
- stale lease recovery;
- TCP API behavior;
- CLI against a foreground engine;
- TUI state model against a fake API.

The generic command adapter uses deterministic fixture programs so tests do not
require Pi or model access.

### 20.3 End-to-end tests

An opt-in suite uses Pi to:

1. generate a task from a simple goal;
2. execute it in a temporary project;
3. fail verification once;
4. apply verifier retry feedback;
5. pass verification;
6. restart the engine and confirm complete history remains inspectable.

## 21. Version 1 Acceptance Criteria

Version 1 is complete when it can:

- initialize a machine-wide Pi-Ramid home;
- convert a natural-language project goal into one or more reviewable tasks;
- accept structured task YAML directly;
- schedule independent tasks across multiple projects;
- prevent conflicting writes to the same project directory;
- execute tasks through Pi with the correct project working directory;
- verify each result using a separate read-only Pi role;
- retry failures using only verifier-produced improvement instructions;
- preserve immutable attempts, prompts, logs, reports, and artifact metadata;
- recover safely after an engine interruption;
- operate in foreground and background modes;
- expose the same engine through CLI and TUI clients;
- run unattended on a single VPS.

## 22. Explicit Non-Goals

Version 1 does not include:

- distributed workers;
- a web or desktop GUI;
- multi-user tenancy;
- API authentication or TLS;
- arbitrary workflow authoring;
- interactive chat;
- project-local Pi-Ramid databases;
- verifier filesystem sandboxing;
- continuous event-triggered PR monitoring after a task reaches completion;
- automatic dependency installation or `doctor --fix`;
- direct LLM API calls outside Pi.

Continuous PR maintenance after new external events arrive requires a later
trigger or scheduled-goal feature. A version 1 PR-maintenance task has a
terminal DOD and finishes when that DOD passes.
