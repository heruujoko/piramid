# Pi-Ramid

Pi-Ramid is a lightweight, machine-wide AI work orchestrator. It delegates
planning, execution, and verification to separate Pi processes while it owns
scheduling, retries, worker allocation, persistence, recovery, and audit
history.

Pi-Ramid stores state under `~/.piramid` by default. Set `PIRAMID_HOME` to use
an isolated location.

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

The default API address is `127.0.0.1:7433`. Override it consistently on the
engine and clients:

```bash
piramid start --d --s 127.0.0.1 --p 7433
piramid queue --s 127.0.0.1 --p 7433
```

Version 1 has no API authentication or TLS. Keep the listener on loopback, or
protect non-loopback access with an external firewall, VPN, or SSH tunnel.

## Submit a goal

```bash
piramid goal \
  --project /srv/projects/my-service \
  "Maintain PR https://github.com/acme/my-service/pull/184"
```

Pi-Ramid invokes Pi in the planner role, displays the generated task graph, and
requires confirmation before admission. Use `--yes` for unattended submission.

The planner, executor, and verifier always run with the task's canonical
project directory as their process working directory.

## Submit structured YAML

```bash
piramid enqueue task.yaml
```

Both a single task and a complete version 1 task graph are accepted. See
[docs/task-contract.md](docs/task-contract.md).

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

The CLI and TUI are TCP API clients. They do not contain scheduler or storage
logic.

## Storage

SQLite contains lifecycle metadata and relationships. Human-readable files
contain goals, task packages, rendered prompts, stdout/stderr logs,
verification reports, and artifact metadata:

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

Back up the entire directory while the engine is stopped. See
[docs/operations.md](docs/operations.md) for recovery and service guidance.

## Documentation

- [Configuration](docs/configuration.md)
- [Task contract](docs/task-contract.md)
- [Operations and recovery](docs/operations.md)
- [v1 design](docs/superpowers/specs/2026-06-25-piramid-v1-design.md)
- [Implementation tracker](docs/superpowers/plans/2026-06-25-piramid-v1-implementation-plan.md)
