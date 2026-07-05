# Pi-Ramid

Pi-Ramid is a lightweight, machine-wide AI work orchestrator. It now supports a loop-first control plane while preserving the working execution engine underneath.

The core runtime model is:

```text
Pattern -> Loop -> Fire -> Goal -> Task -> Attempt
                                 \
                                  -> Gate
```

Pi-Ramid stores state under `~/.piramid` by default. Set `PIRAMID_HOME` to use an isolated location.

## Requirements

- Node.js
- Pi available as `pi`
- Git and GitHub CLI for pull-request maintenance workflows

SQLite is embedded in the Pi-Ramid binary and requires no system installation.

## Build

```bash
make test
make build
```

The executable is written to `bin/piramid`.

## Quick start

```bash
piramid init
piramid doctor
piramid start
```

Foreground mode is appropriate for testing and service managers. To detach:

```bash
piramid start --d
```

The default API address is `127.0.0.1:7433`. Override it consistently on the engine and clients:

```bash
piramid start --d --s 127.0.0.1 --p 7433
piramid queue --s 127.0.0.1 --p 7433
```

Version 1 has no API authentication or TLS. Keep the listener on loopback, or protect non-loopback access with an external firewall, VPN, or SSH tunnel.

## Loop-first overview

Loop-first pi-ramid splits source-of-truth and runtime state:

- file-backed definitions:
  - `patterns/*.yaml`
  - `loops/*.yaml`
- SQLite runtime state:
  - fires
  - goals
  - tasks
  - attempts
  - gates
  - events

Loops are cron-scheduled automation instances that reference reusable patterns. When a loop is due, pi-ramid creates a fire and a draft goal, then continues through the normal task execution pipeline.

## Human gates

pi-ramid supports durable mid-run human gates.

The contract is:

1. executor reads `PIRAMID_GATE_CONTEXT`
2. executor writes `gate.context.md` to that exact path
3. executor exits with code `42`
4. pi-ramid records an open gate, parks the attempt/fire, and releases the worker
5. a human resolves the gate through the API or frontend
6. pi-ramid resumes from a fresh seeded attempt

Phase 1 includes a React + Vite frontend embedded in the Go binary with:

- lifecycle board
- pending-gates header badge
- gate modal
- SSE-backed live refresh

## Submit a goal

```bash
piramid goal \
  --project /srv/projects/my-service \
  "Maintain PR https://github.com/acme/my-service/pull/184"
```

Pi-Ramid invokes Pi in the planner role, displays the generated task graph, and requires confirmation before admission. Use `--yes` for unattended submission.

The planner, executor, and verifier always run with the task's canonical project directory as their process working directory.

## Submit structured YAML

```bash
piramid enqueue task.yaml
```

Both a single task and a complete version 1 task graph are accepted. See [docs/task-contract.md](docs/task-contract.md).

## Operate work

```bash
piramid queue
piramid workers
piramid inspect TASK-021
piramid retry TASK-021
piramid retry TASK-021 --override
piramid cancel TASK-021
piramid tui
```

The CLI and TUI are TCP API clients. They do not contain scheduler or storage logic.

## Storage

SQLite contains lifecycle metadata and relationships. Human-readable files contain goals, task packages, rendered prompts, stdout/stderr logs, verification reports, artifact metadata:

```text
~/.piramid/
  config.yaml
  state.db
  prompts/
  goals/
  tasks/
  attempts/
  artifacts/
  runtime/
```

Back up the entire directory while the engine is stopped. See [docs/operations.md](docs/operations.md) for recovery and service guidance.

## Documentation

- [Configuration](docs/configuration.md)
- [Task contract](docs/task-contract.md)
- [Gate contract](docs/gate-contract.md)
- [Definitions](docs/definitions.md)
- [HTTP API](docs/api.md)
- [Operations and recovery](docs/operations.md)
- [Loop-first design reference](docs/loop-dashboard-design.md)
- [Loop-first baseline](docs/baseline-loop-first.md)
