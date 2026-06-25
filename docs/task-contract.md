# Task Contract

Pi-Ramid accepts a version 1 plan containing one or more immutable tasks:

```yaml
version: 1
goal_id: GOAL-184
tasks:
  - id: PR-MAINTAIN-184
    parent_task_id: ""
    title: Maintain pull request 184
    goal: Leave the pull request ready for human review.
    project_path: /srv/projects/my-service
    inputs:
      - type: url
        value: https://github.com/acme/my-service/pull/184
    expected_outputs:
      - type: file
        path: .piramid-results/PR-MAINTAIN-184/result.json
    dod:
      - all required checks pass
      - no actionable review comments remain
    model: gpt-5.5
    max_attempts: 10
    timeout: 2h
    depends_on: []
```

`piramid enqueue` also accepts one task without the outer plan. The CLI wraps
it in a direct-submission goal.

## Fields

- `version`: must be `1`.
- `goal_id`: stable identifier shared by the task graph.
- `tasks`: non-empty task list.
- `id`: unique task identifier.
- `parent_task_id`: optional organizational parent. It does not control
  scheduling.
- `title`: short operator-facing label.
- `goal`: terminal execution objective.
- `project_path`: absolute, clean project directory. Pi-Ramid uses it as
  `cmd.Dir` for every Pi invocation.
- `inputs`: typed references supplied to the executor.
- `expected_outputs`: declared project-relative output files.
- `dod`: non-empty, measurable definition of done.
- `model`: optional runtime model value.
- `max_attempts`: positive total attempt limit.
- `timeout`: positive Go duration.
- `depends_on`: scheduling prerequisites in the same graph.

Task IDs must be unique, dependency references must exist, and the dependency
graph must be acyclic.

## Parent and dependency relationships

`parent_task_id` groups work for inspection. `depends_on` gates scheduling. A
task may depend on several predecessors regardless of its organizational
parent.

If a dependency fails, is cancelled, or becomes blocked, all reachable pending
descendants become `BLOCKED`.

## Lifecycle

```text
PENDING → RUNNING → VERIFYING → COMPLETED
                         │
                         └→ RETRY_WAIT → PENDING

PENDING/RUNNING/VERIFYING/RETRY_WAIT → CANCELLED
PENDING/RETRY_WAIT                    → BLOCKED
RUNNING/VERIFYING                     → FAILED
```

Every executor run creates an immutable attempt. Executor process success does
not complete a task; a separate verifier Pi process must return `PASS`.

## Verification response

```yaml
status: FAIL
reasons:
  - required integration check is failing
retry_prompt: |
  Diagnose the failing integration check, apply the correction, run tests,
  commit, and push the branch.
```

`status` is strictly `PASS` or `FAIL`. Reasons are required. `retry_prompt` is
required for `FAIL` while attempts remain and must be empty for `PASS`.

## Artifacts

Declared output paths must remain beneath the canonical project directory.
Pi-Ramid rejects absolute paths, traversal, and symlink escapes. It stores
path, SHA-256, and byte size metadata in SQLite while leaving artifact content
in the project.
