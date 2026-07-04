package domain

import "time"

type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

type LoopAutonomy string

const (
	LoopAutonomyL1 LoopAutonomy = "L1"
	LoopAutonomyL2 LoopAutonomy = "L2"
	LoopAutonomyL3 LoopAutonomy = "L3"
)

type LoopTrigger string

const (
	LoopTriggerPiramid LoopTrigger = "piramid"
	LoopTriggerPi      LoopTrigger = "pi"
)

type Pattern struct {
	ID          string       `yaml:"id" json:"id"`
	Name        string       `yaml:"name" json:"name"`
	File        string       `yaml:"file" json:"file"`
	Goal        string       `yaml:"goal" json:"goal"`
	Cadence     string       `yaml:"cadence" json:"cadence"`
	Risk        RiskLevel    `yaml:"risk" json:"risk"`
	Tools       []string     `yaml:"tools" json:"tools"`
	Skills      []string     `yaml:"skills" json:"skills"`
	State       string       `yaml:"state" json:"state"`
	Phases      []string     `yaml:"phases" json:"phases"`
	HumanGates  []string     `yaml:"human_gates" json:"human_gates"`
	Starter     string       `yaml:"starter,omitempty" json:"starter,omitempty"`
	WeekOneMode LoopAutonomy `yaml:"week_one_mode,omitempty" json:"week_one_mode,omitempty"`
	TokenCost   string       `yaml:"token_cost,omitempty" json:"token_cost,omitempty"`
	Cost        PatternCost  `yaml:"cost,omitempty" json:"cost,omitempty"`
}

type PatternCost struct {
	TokensNoop        int  `yaml:"tokens_noop" json:"tokens_noop"`
	TokensReport      int  `yaml:"tokens_report" json:"tokens_report"`
	TokensAction      int  `yaml:"tokens_action" json:"tokens_action"`
	SuggestedDailyCap int  `yaml:"suggested_daily_cap" json:"suggested_daily_cap"`
	EarlyExitRequired bool `yaml:"early_exit_required" json:"early_exit_required"`
}

type Loop struct {
	ID          string       `yaml:"id" json:"id"`
	PatternID   string       `yaml:"pattern" json:"pattern"`
	Active      bool         `yaml:"active" json:"active"`
	Cron        string       `yaml:"cron" json:"cron"`
	Autonomy    LoopAutonomy `yaml:"autonomy" json:"autonomy"`
	Trigger     LoopTrigger  `yaml:"trigger" json:"trigger"`
	Goal        string       `yaml:"goal" json:"goal"`
	ProjectPath string       `yaml:"project_path" json:"project_path"`
	HumanGates  []string     `yaml:"human_gates" json:"human_gates"`
	Token       LoopToken    `yaml:"token" json:"token"`
}

type LoopToken struct {
	DailyCap int `yaml:"daily_cap" json:"daily_cap"`
}

type FireStatus string

const (
	FireScheduled FireStatus = "FIRE_SCHEDULED"
	FireDrafting  FireStatus = "FIRE_DRAFTING"
	FireGated     FireStatus = "FIRE_GATED"
	FireConfirmed FireStatus = "FIRE_CONFIRMED"
	FireRunning   FireStatus = "FIRE_RUNNING"
	FireDone      FireStatus = "FIRE_DONE"
	FireRejected  FireStatus = "FIRE_REJECTED"
	FireDeferred  FireStatus = "FIRE_DEFERRED"
	FireFailed    FireStatus = "FIRE_FAILED"
)

type Fire struct {
	ID          string     `yaml:"id" json:"id"`
	LoopID      string     `yaml:"loop_id" json:"loop_id"`
	GoalID      string     `yaml:"goal_id,omitempty" json:"goal_id,omitempty"`
	Status      FireStatus `yaml:"status" json:"status"`
	ScheduledAt time.Time  `yaml:"scheduled_at" json:"scheduled_at"`
	StartedAt   time.Time  `yaml:"started_at,omitempty" json:"started_at,omitempty"`
	FinishedAt  time.Time  `yaml:"finished_at,omitempty" json:"finished_at,omitempty"`
	CreatedAt   time.Time  `yaml:"created_at" json:"created_at"`
	UpdatedAt   time.Time  `yaml:"updated_at" json:"updated_at"`
	LastError   string     `yaml:"last_error,omitempty" json:"last_error,omitempty"`
}

// TaskGateLinkage is the authoritative fire/goal linkage for a task, derived from
// store state (attempts.task_id -> tasks.goal_id -> fires) rather than from an
// executor-supplied gate context. FireID may be empty for tasks admitted
// outside the loop scheduler (no fire to park); callers must guard on that.
type TaskGateLinkage struct {
	FireID string `yaml:"fire_id,omitempty" json:"fire_id,omitempty"`
	LoopID string `yaml:"loop_id,omitempty" json:"loop_id,omitempty"`
	GoalID string `yaml:"goal_id,omitempty" json:"goal_id,omitempty"`
}

type GateStatus string

const (
	GateOpen     GateStatus = "GATE_OPEN"
	GateResolved GateStatus = "GATE_RESOLVED"
	GateRejected GateStatus = "GATE_REJECTED"
	GateDeferred GateStatus = "GATE_DEFERRED"
)

type GateDecision string

const (
	GateDecisionApprove GateDecision = "approve"
	GateDecisionRoute   GateDecision = "route"
	GateDecisionDefer   GateDecision = "defer"
	GateDecisionReject  GateDecision = "reject"
)

type Gate struct {
	ID           string       `yaml:"id" json:"id"`
	FireID       string       `yaml:"fire_id" json:"fire_id"`
	GoalID       string       `yaml:"goal_id,omitempty" json:"goal_id,omitempty"`
	TaskID       string       `yaml:"task_id,omitempty" json:"task_id,omitempty"`
	AttemptID    string       `yaml:"attempt_id,omitempty" json:"attempt_id,omitempty"`
	Status       GateStatus   `yaml:"status" json:"status"`
	ContextPath  string       `yaml:"context_path" json:"context_path"`
	Context      GateContext  `yaml:"context" json:"context"`
	Decision     GateDecision `yaml:"decision,omitempty" json:"decision,omitempty"`
	DecisionNote string       `yaml:"decision_note,omitempty" json:"decision_note,omitempty"`
	OpenedAt     time.Time    `yaml:"opened_at" json:"opened_at"`
	ResolvedAt   time.Time    `yaml:"resolved_at,omitempty" json:"resolved_at,omitempty"`
	CreatedAt    time.Time    `yaml:"created_at" json:"created_at"`
	UpdatedAt    time.Time    `yaml:"updated_at" json:"updated_at"`
}

type GateContext struct {
	Gate            string         `yaml:"gate" json:"gate"`
	Phase           string         `yaml:"phase" json:"phase"`
	LoopID          string         `yaml:"loop_id" json:"loop_id"`
	FireID          string         `yaml:"fire_id" json:"fire_id"`
	GoalID          string         `yaml:"goal_id,omitempty" json:"goal_id,omitempty"`
	TaskID          string         `yaml:"task_id,omitempty" json:"task_id,omitempty"`
	Summary         string         `yaml:"summary" json:"summary"`
	DecisionOptions []GateDecision `yaml:"decision_options" json:"decision_options"`
	Threads         []GateThread   `yaml:"threads,omitempty" json:"threads,omitempty"`
	Body            string         `yaml:"-" json:"body,omitempty"`
}

type GateThread struct {
	ID       string `yaml:"id" json:"id"`
	Title    string `yaml:"title" json:"title"`
	Location string `yaml:"location,omitempty" json:"location,omitempty"`
	Author   string `yaml:"author,omitempty" json:"author,omitempty"`
	Summary  string `yaml:"summary" json:"summary"`
}

type GateArtifact struct {
	Path    string      `yaml:"path" json:"path"`
	Context GateContext `yaml:"context" json:"context"`
}
