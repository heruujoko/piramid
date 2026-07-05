# Loop and Pattern Definitions

pi-ramid reads loop-first automation from files instead of storing authoring state in SQLite.

## Root layout

A definition root contains two directories:

```text
<root>/
  patterns/
    *.yaml
  loops/
    *.yaml
```

Demo definitions live in `demo/definitions/`.

## Pattern files

Pattern files define reusable automation shapes.

Example:

```yaml
id: pr-maintainer
name: PR Maintainer
file: pr-maintainer.md
goal: Monitor open PRs, address review feedback, and maintain PR hygiene
cadence: 30m-2h
risk: low
tools: [claude-code, github-actions]
skills: [pr-review, minimal-fix, pr-maintainer]
state: pr-maintainer-state.md
phases: [scan-prs, triage-feedback, apply-fixes, verify-ci]
human_gates: [unresolved-feedback, breaking-change]
week_one_mode: L1
token_cost: medium
cost:
  tokens_noop: 10000
  tokens_report: 50000
  tokens_action: 200000
  suggested_daily_cap: 500000
  early_exit_required: false
```

Key fields:

- `id` — stable identifier referenced by loops
- `goal` — default intent of the automation
- `skills` — expected skill/tooling surface
- `phases` — human-readable phase labels
- `human_gates` — allowed gate names for this pattern

## Loop files

Loop files define scheduled runtime instances of patterns.

Example:

```yaml
id: LOOP-PR-MAINTAINER
pattern: pr-maintainer
active: true
cron: "*/30 * * * *"
autonomy: L1
trigger: piramid
goal: |
  Monitor open PRs in the piramid repository, triage review feedback,
  apply approved fixes, and keep PRs moving toward merge.
project_path: /home/pi/piramid
human_gates: [unresolved-feedback, breaking-change]
token:
  daily_cap: 500000
```

Key fields:

- `pattern` — references a pattern `id`
- `cron` — 5-field UTC cron schedule
- `autonomy` — `L1`, `L2`, or `L3`
- `trigger` — runtime trigger source (`piramid` or `pi`)
- `project_path` — absolute workspace path for generated tasks
- `token.daily_cap` — per-loop token budget

## Loading model

Definitions are loaded through `definitions.LoadRoot(...)` and exposed to:

- the loop scheduler
- the HTTP API (`GET /v1/loops`)
- webhook matching logic

SQLite is runtime state only. Definitions remain file-backed.

## Validation rules

Current validation enforces:

- required ids and goal text
- valid autonomy and trigger enums
- valid 5-field cron expressions
- existing pattern references
- positive token caps

The frontend phase-1 app is read-only. It consumes loop/gate/fire state from the API; it does not author definitions in-app.

## Runtime relationship

Definitions create runtime state in this order:

```text
Pattern -> Loop -> Fire -> Goal -> Task -> Attempt
```

Where:

- pattern/loop = file-backed source of truth
- fire/goal/task/attempt/gate/event = SQLite/runtime state

## Example roots

- `test/definitions/valid/` — fixture root for validation tests
- `demo/definitions/` — sample root for manual demos and E2E stories
