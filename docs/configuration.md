# Configuration

Pi-Ramid reads `~/.piramid/config.yaml`, or
`$PIRAMID_HOME/config.yaml` when `PIRAMID_HOME` is set. Configuration changes
take effect after an engine restart.

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

## Server

- `host`: TCP bind address. The default is loopback. Version 1 warns on
  non-loopback addresses because authentication and TLS are deferred.
- `port`: TCP port from 1 through 65535.

CLI and TUI clients use `--s` and `--p` to select the engine address.

## Workers

`workers.count` controls concurrent executor slots. A project workspace lease
still prevents two tasks from mutating the same canonical project directory.

## Runtime roles

Planner, executor, and verifier are independently configurable.

- `adapter`: `pi-cli` or `command`.
- `command`: executable path or name resolved through `PATH`.
- `args`: direct process arguments. Pi-Ramid never invokes a shell.
- `timeout`: Go duration such as `30m`, `4h`, or `45s`.

Allowed argument placeholders:

- `{{prompt}}`
- `{{prompt_file}}`
- `{{workspace}}`
- `{{task_id}}`
- `{{attempt}}`
- `{{model}}`

Unknown placeholders make configuration invalid.

## Retry

- `default_max_attempts`: fallback attempt limit for planned work.
- `initial_delay`: first automatic retry delay.
- `max_delay`: retry delay ceiling.
- `backoff`: version 1 supports `exponential`.

Verifier-provided retry instructions are persisted and passed unchanged to the
next executor attempt. Operational failures repeat the original invocation
without inventing artifact-quality instructions.

## Prompt policies

`piramid init` creates these empty files:

```text
prompts/orchestrator.md
prompts/planner.md
prompts/executor.md
prompts/verifier.md
```

Empty files contribute no prompt content. Edit them to add machine-wide or
role-specific rules. Each rendered attempt prompt and hash is preserved.
