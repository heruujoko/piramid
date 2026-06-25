# Operations and Recovery

## Engine modes

Foreground:

```bash
piramid start
```

Background:

```bash
piramid start --d
```

The daemon writes its PID and logs under `~/.piramid/runtime`. For production,
foreground mode under systemd, launchd, or another service manager is
preferred because restart policy and logs remain externally observable.

## Health and diagnostics

```bash
piramid doctor
piramid doctor --project /srv/projects/my-service
piramid doctor --smoke-test
```

Doctor is read-only. The default command does not invoke Pi or spend model
tokens. `--smoke-test` explicitly performs a non-mutating Pi request.

## State and logs

SQLite is authoritative for goals, tasks, dependencies, attempts, leases,
events, and artifact indexes. Large logs remain files:

```text
attempts/TASK-ID/0001/
  executor-prompt.md
  stdout.log
  stderr.log
  process.json
  verifier-prompt.md
  verifier-stdout.log
  verifier-stderr.log
  verification.yaml
```

Use `piramid inspect`, the TUI, or ordinary tools such as `less`, `tail`, and
`rg`.

## Crash recovery

On startup Pi-Ramid:

1. opens SQLite and applies forward-only migrations;
2. inspects active attempts;
3. refuses startup if a recorded child process is still alive, preventing
   duplicate mutation;
4. marks orphaned attempts `INTERRUPTED`;
5. releases stale project leases;
6. returns interrupted tasks to scheduling as new attempts;
7. binds the API only after recovery succeeds.

An interrupted attempt is never rewritten as successful. Its prompt and logs
remain in history.

## Workspace concurrency

Executor and verifier phases hold one exclusive lease keyed by canonical
project path. The lease remains held through verification. Planner invocations
use shared read leases and wait behind an exclusive lease.

Avoid running multiple Pi-Ramid engines against the same home directory.
Version 1 is a single-engine, single-user design.

## Backup and restore

Stop the engine before backup:

```bash
cp -a ~/.piramid /secure/backup/piramid
```

Restore the complete directory, preserving permissions. Do not restore only
`state.db`; human-readable prompts, logs, verification reports, and hashes are
part of the audit record.

## Security

- Loopback is the default.
- Version 1 has no built-in authentication or TLS.
- Runtime arguments are executed without a shell.
- Environment values with secret-like names are redacted from recorded
  metadata.
- Task packages must not contain credentials.

Use filesystem permissions, a firewall, VPN, or SSH tunnel when operating on a
VPS.
