# Phase 6 — HTTP API Design

**Date:** 2026-07-04
**Status:** Approved
**Branch:** `phase-6-http-api-github-webhooks`

## Overview

Expose loop/fire/gate read endpoints and gate decision posting over HTTP, plus a GitHub App webhook receiver that triggers loop fires from PR events.

## Architecture

```
POST /v1/webhooks/github ─→ verify HMAC ─→ match loop triggers ─→ Fire + Goal
GET  /v1/loops           ─→ definitions.Snapshot + store.GetLatestFireByLoop
GET  /v1/loops/{id}/fires ─→ store.ListFires
GET  /v1/gates            ─→ store.ListOpenGates
GET  /v1/gates/{id}       ─→ store.GetGate
POST /v1/gates/{id}/decision → store.ResolveGate
SSE  /v1/events           ─→ already works, just passes through fire/gate events
```

## Application Interface

Add to `app.Application` in `internal/app/service.go`:

```go
ListLoops(ctx context.Context) ([]LoopView, error)
ListLoopFires(ctx context.Context, loopID string) ([]FireView, error)
ListOpenGates(ctx context.Context) ([]GateSummary, error)
GetGate(ctx context.Context, gateID string) (GateDetail, error)
ResolveGate(ctx context.Context, gateID string, input GateDecisionInput) error
HandleGitHubWebhook(ctx context.Context, eventType string, signature string, payload []byte) error
```

## View Models (DTOs)

New file `internal/domain/view.go` (or inline in `internal/app/service.go`):

```go
type LoopView struct {
    ID         string       `json:"id"`
    PatternID  string       `json:"pattern_id"`
    Active     bool         `json:"active"`
    Cron       string       `json:"cron"`
    Autonomy   string       `json:"autonomy"`
    LatestFire *FireSummary `json:"latest_fire,omitempty"`
}

type FireSummary struct {
    ID          string `json:"id"`
    LoopID      string `json:"loop_id"`
    Status      string `json:"status"`
    ScheduledAt string `json:"scheduled_at"`
}

type FireView struct {
    FireSummary
    GoalID    string `json:"goal_id,omitempty"`
    StartedAt string `json:"started_at,omitempty"`
    LastError string `json:"last_error,omitempty"`
}

type GateSummary struct {
    ID       string `json:"id"`
    Gate     string `json:"gate"`
    Phase    string `json:"phase"`
    FireID   string `json:"fire_id"`
    LoopID   string `json:"loop_id"`
    Summary  string `json:"summary"`
    OpenedAt string `json:"opened_at"`
}

type GateDetail struct {
    GateSummary
    GoalID          string              `json:"goal_id,omitempty"`
    TaskID          string              `json:"task_id,omitempty"`
    AttemptID       string              `json:"attempt_id,omitempty"`
    DecisionOptions []string            `json:"decision_options"`
    Threads         []GateThreadView    `json:"threads,omitempty"`
    Body            string              `json:"body,omitempty"`
}

type GateThreadView struct {
    ID       string `json:"id"`
    Title    string `json:"title"`
    Location string `json:"location,omitempty"`
    Author   string `json:"author,omitempty"`
    Summary  string `json:"summary"`
}

type GateDecisionInput struct {
    Decision string `json:"decision"`
    Note     string `json:"note,omitempty"`
}
```

## Domain Changes

### `internal/domain/loop.go` — add triggers

```go
type GitHubTrigger struct {
    Repos  []string `yaml:"repos" json:"repos"`
    Events []string `yaml:"events" json:"events"`
}

type Triggers struct {
    GitHub *GitHubTrigger `yaml:"github,omitempty" json:"github,omitempty"`
}
```

Add `Triggers Triggers` field to `Loop` struct.

### `internal/config/config.go` — add webhook secret

```go
type LoopsConfig struct {
    DefinitionRoot      string `yaml:"definition_root"`
    GitHubWebhookSecret string `yaml:"github_webhook_secret"`
}
```

## Service Wiring

`Service` gains a new dependency: `defs DefinitionsProvider`:

```go
type DefinitionsProvider interface {
    Load(ctx context.Context) (definitions.Snapshot, error)
}
```

Added to `NewService` constructor. In bootstrap, the `definitionsSource` is passed to both the loop scheduler and the service.

For `HandleGitHubWebhook`, Service needs the webhook secret from config. Pass it via `NewService` or a setter.

## GitHub Webhook Handler

New file `internal/api/webhook.go`:

```go
func (s *Server) handleGitHubWebhook(writer http.ResponseWriter, request *http.Request) {
    // Read body (limited to 1MB)
    // Pass X-Hub-Signature-256 + X-GitHub-Event + body to application.HandleGitHubWebhook
    // Return 200 or error
}
```

## GitHub Webhook Processing

New file `internal/webhook/github.go`:

```go
func Process(eventType string, signature string, secret string, payload []byte, loops []domain.Loop) ([]domain.Loop, error)
```

Steps:
1. Verify `sha256=HMAC-SHA256(secret, payload)` matches signature
2. Parse JSON to extract `action`, `repository.full_name`, `pull_request.merged`
3. Build event key: `pull_request.{action}` or `pull_request.closed.{merged/unmerged}`
4. Match against each loop's `triggers.github.repos` and `triggers.github.events`
5. Return matching loops

## API Route Registration

Add to `ServeHTTP` switch:

```go
case request.Method == http.MethodPost && path == "v1/webhooks/github":
    s.handleGitHubWebhook(writer, request)
case request.Method == http.MethodGet && path == "v1/loops":
    s.listLoops(writer, request)
case request.Method == http.MethodGet && len(parts) == 4 && parts[1] == "loops" && parts[3] == "fires":
    s.listLoopFires(writer, request, parts[2])
case request.Method == http.MethodGet && path == "v1/gates":
    s.listGates(writer, request)
case request.Method == http.MethodGet && len(parts) == 3 && parts[1] == "gates":
    s.getGate(writer, request, parts[2])
case request.Method == http.MethodPost && len(parts) == 4 && parts[1] == "gates" && parts[3] == "decision":
    s.resolveGate(writer, request, parts[2])
```

## Files to Create

| File | Purpose |
|------|---------|
| `internal/api/loops.go` | `listLoops`, `listLoopFires` handlers |
| `internal/api/gates.go` | `listGates`, `getGate`, `resolveGate` handlers |
| `internal/api/webhook.go` | `handleGitHubWebhook` handler |
| `internal/webhook/github.go` | HMAC verify, event parser, loop matcher |
| `internal/webhook/github_test.go` | Tests for webhook parsing and matching |

## Files to Modify

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `GitHubWebhookSecret` to `LoopsConfig` |
| `internal/domain/loop.go` | Add `Triggers` struct + field to `Loop` |
| `internal/definitions/loops.go` | No change needed (YAML unmarshaling handles new fields) |
| `internal/app/service.go` | Add 6 methods + `DefinitionsProvider` dep |
| `internal/api/server.go` | Register 6 new routes |
| `internal/bootstrap/bootstrap.go` | Pass definitionsSource + webhook secret to Service |

## Verification

- `go test ./...` passes
- `GET /v1/loops` returns loops with latest fire status
- `GET /v1/gates` returns open gates
- `POST /v1/gates/{id}/decision` resolves gate
- `POST /v1/webhooks/github` with valid PR payload → matching loops fire
- Invalid GitHub signature → 401
- Unmatched webhook event → 200 (no-op)
