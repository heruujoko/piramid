# HTTP API

The pi-ramid daemon exposes a loop-first HTTP API on the configured server address. Version 1 is unauthenticated; keep it on loopback unless protected externally.

Base path: `/v1`

## Health

### `GET /v1/health`

Returns daemon health.

```json
{ "status": "ok" }
```

## Goals

### `POST /v1/goals/draft`

Draft a goal into a plan.

Request:

```json
{
  "goal_text": "Maintain PR #182",
  "project_path": "/srv/projects/piramid"
}
```

### `POST /v1/goals/{id}/confirm`

Confirm a drafted goal and admit its tasks.

### `POST /v1/goals/{id}/reject`

Reject a drafted goal.

## Tasks

### `POST /v1/tasks`

Submit a structured YAML/JSON plan.

### `GET /v1/tasks`

List tasks.

### `GET /v1/tasks/{id}`

Get a task plus attempts.

### `POST /v1/tasks/{id}/retry`

Retry a task.

Request:

```json
{ "override": true }
```

### `POST /v1/tasks/{id}/cancel`

Cancel a task.

## Workers

### `GET /v1/workers`

List active workers.

## Attempt logs

### `GET /v1/attempts/{attemptID}/logs?stream=stdout&offset=0&limit=65536`

Reads a log chunk.

Allowed streams:

- `stdout`
- `stderr`
- `verifier-stdout`
- `verifier-stderr`

## Events

### `GET /v1/events`

Server-Sent Events stream of runtime events.

- header `Last-Event-ID` is supported for resume
- event id is the store event id
- event type is the entity type

Frontend phase 1 uses this stream to refresh board state.

## Loops

### `GET /v1/loops`

Returns file-backed loops plus latest fire summary.

Example:

```json
[
  {
    "id": "LOOP-PR-MAINTAINER",
    "pattern_id": "pr-maintainer",
    "active": true,
    "cron": "*/30 * * * *",
    "autonomy": "L1",
    "latest_fire": {
      "id": "FIRE-...",
      "loop_id": "LOOP-PR-MAINTAINER",
      "status": "FIRE_RUNNING",
      "scheduled_at": "2026-07-05T00:00:00Z"
    }
  }
]
```

### `GET /v1/loops/{id}/fires`

Returns recent fires for a loop.

## Gates

### `GET /v1/gates`

Returns open gates.

### `GET /v1/gates/{id}`

Returns gate detail including decision options, parsed threads, and markdown body.

### `POST /v1/gates/{id}/decision`

Resolve a gate.

Request:

```json
{
  "decision": "approve",
  "note": "Continue"
}
```

Allowed decisions:

- `approve`
- `route`
- `defer`
- `reject`

## Webhooks

### `POST /v1/webhooks/github`

GitHub webhook entrypoint. Signature verification uses `X-Hub-Signature-256` and the configured shared secret.

Matched events create fires/goals through the same loop scheduler path.

## Error format

Errors use this envelope:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "..."
  }
}
```

Common codes:

- `invalid_request`
- `not_found`
- `conflict`
- `internal_error`
