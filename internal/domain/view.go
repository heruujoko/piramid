package domain

// LoopView is the API representation of a loop with its latest fire.
type LoopView struct {
	ID         string       `json:"id"`
	PatternID  string       `json:"pattern_id"`
	Active     bool         `json:"active"`
	Cron       string       `json:"cron"`
	Autonomy   string       `json:"autonomy"`
	LatestFire *FireSummary `json:"latest_fire,omitempty"`
}

// FireSummary is a lightweight fire view for embedding.
type FireSummary struct {
	ID          string `json:"id"`
	LoopID      string `json:"loop_id"`
	Status      string `json:"status"`
	ScheduledAt string `json:"scheduled_at"`
}

// FireView is the detailed fire API representation.
type FireView struct {
	ID          string `json:"id"`
	LoopID      string `json:"loop_id"`
	GoalID      string `json:"goal_id,omitempty"`
	Status      string `json:"status"`
	ScheduledAt string `json:"scheduled_at"`
	StartedAt   string `json:"started_at,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

// GateSummary is the gate list API representation.
type GateSummary struct {
	ID       string `json:"id"`
	Gate     string `json:"gate"`
	Phase    string `json:"phase"`
	FireID   string `json:"fire_id"`
	LoopID   string `json:"loop_id"`
	Summary  string `json:"summary"`
	OpenedAt string `json:"opened_at"`
}

// GateDetail is the full gate API representation including context.
type GateDetail struct {
	ID              string              `json:"id"`
	Gate            string              `json:"gate"`
	Phase           string              `json:"phase"`
	FireID          string              `json:"fire_id"`
	LoopID          string              `json:"loop_id"`
	GoalID          string              `json:"goal_id,omitempty"`
	TaskID          string              `json:"task_id,omitempty"`
	AttemptID       string              `json:"attempt_id,omitempty"`
	Summary         string              `json:"summary"`
	OpenedAt        string              `json:"opened_at"`
	DecisionOptions []string            `json:"decision_options"`
	Threads         []GateThreadView    `json:"threads,omitempty"`
	Body            string              `json:"body,omitempty"`
}

// GateThreadView is the thread representation in a gate detail.
type GateThreadView struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location string `json:"location,omitempty"`
	Author   string `json:"author,omitempty"`
	Summary  string `json:"summary"`
}

// GateDecisionInput is the request body for POST /v1/gates/{id}/decision.
type GateDecisionInput struct {
	Decision string `json:"decision"`
	Note     string `json:"note,omitempty"`
}
