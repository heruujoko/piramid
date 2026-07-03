# Loop Dashboard / Loop-first pi-ramid Design Reference

This repository is being reworked from a task-first orchestrator into a loop-first orchestrator while preserving the working execution engine.

## Source design docs

The approved design and planning docs live in the adjacent loop-engineering workspace:

- `/Users/herujokoutomo/Code/loop-engineering/docs/specs/2026-07-02-loop-dashboard-design.md`
- `/Users/herujokoutomo/Code/loop-engineering/docs/specs/2026-07-02-loop-dashboard-implementation-plan.md`
- `/Users/herujokoutomo/Code/loop-engineering/docs/specs/2026-07-02-loop-dashboard-task-breakdown.md`

## Locked decisions

- Keep the name **pi-ramid**.
- Ship as **one Go binary** serving a React + Vite frontend.
- Rewrite the domain/API/scheduler top layer around `Pattern → Loop → Fire → Goal → Tasks`.
- Preserve engine internals where possible: worker pool, project leasing, retries, attempt records, SQLite, recovery, and SSE events.
- Store loop definitions as files under a configurable definition root:
  - `patterns/*.yaml`
  - `loops/*.yaml`
- Keep SQLite for runtime state only: fires, goals, tasks, attempts, gates, events.
- Support 5-field UTC cron expressions.
- Support durable mid-run human gates:
  - skill writes `gate.context.md` to `PIRAMID_GATE_CONTEXT`
  - skill exits with code `42`
  - pi-ramid records an open gate, frees the worker, and resumes later from a fresh seeded attempt after a human decision.
- Phase 1 frontend is read-only board + pending-gates badge + gate modal.
- Auth, in-app authoring, timeline, live PTY, and performance work are explicitly deferred.

## M0 baseline

See `docs/baseline-loop-first.md`.
