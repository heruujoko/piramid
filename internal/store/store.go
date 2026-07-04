package store

import (
	"context"
	"errors"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
)

var ErrProjectLeased = errors.New("project workspace is leased")
var ErrInvalidState = errors.New("invalid lifecycle state")

type PersistedPaths struct {
	GoalPath   string
	PlanPath   string
	TaskPaths  map[string]string
	TaskHashes map[string]string
}

type StartAttemptInput struct {
	TaskID     string
	WorkerID   string
	Runtime    string
	PromptPath string
	PromptHash string
	StdoutPath string
	StderrPath string
	StartedAt  time.Time
}

type AttemptPathsInput struct {
	AttemptID  int64
	PromptPath string
	PromptHash string
	StdoutPath string
	StderrPath string
}

type ArtifactRecord struct {
	RelativePath string
	AbsolutePath string
	SHA256       string
	SizeBytes    int64
}

type FinishExecutionInput struct {
	TaskID     string
	AttemptID  int64
	ProcessID  int
	ExitCode   int
	FinishedAt time.Time
}

type FinishVerificationInput struct {
	TaskID       string
	AttemptID    int64
	Verification domain.Verification
	ReportPath   string
	NextRunAt    time.Time
	FinishedAt   time.Time
}

type OperationalFailureInput struct {
	TaskID       string
	AttemptID    int64
	FailureClass string
	FinishedAt   time.Time
	NextRunAt    time.Time
}

type InterruptedAttempt struct {
	AttemptID int64
	TaskID    string
	ProcessID int
}

type TaskFilter struct {
	Statuses []domain.TaskStatus
	Limit    int
}

type Event struct {
	ID          int64     `json:"id"`
	EntityType  string    `json:"entity_type"`
	EntityID    string    `json:"entity_id"`
	EventType   string    `json:"event_type"`
	PayloadJSON string    `json:"payload_json"`
	CreatedAt   time.Time `json:"created_at"`
}

type AttemptLogPaths struct {
	Stdout         string
	Stderr         string
	VerifierStdout string
	VerifierStderr string
}

type Store interface {
	Health(context.Context) error
	SaveGoalDraft(context.Context, domain.Goal, PersistedPaths) error
	UpdateGoalStatus(context.Context, string, domain.GoalStatus, time.Time) error
	AdmitPlan(context.Context, domain.Goal, domain.Plan, PersistedPaths) error
	ReconcileBlocked(context.Context, time.Time) (int, error)
	ListRunnable(context.Context, time.Time, int) ([]domain.TaskRecord, error)
	AcquireReadLease(context.Context, string, string) error
	ReleaseReadLease(context.Context, string, string) error
	StartAttempt(context.Context, StartAttemptInput) (domain.Attempt, error)
	PrepareAttempt(context.Context, AttemptPathsInput) error
	PrepareVerifier(context.Context, int64, string, string) error
	RecordArtifacts(context.Context, int64, []ArtifactRecord) error
	MoveToVerification(context.Context, FinishExecutionInput) error
	FinishVerification(context.Context, FinishVerificationInput) error
	RecordOperationalFailure(context.Context, OperationalFailureInput) error
	ListActiveAttempts(context.Context) ([]InterruptedAttempt, error)
	RecoverActive(context.Context, time.Time) ([]InterruptedAttempt, error)
	GetTask(context.Context, string) (domain.TaskView, error)
	ListTasks(context.Context, TaskFilter) ([]domain.TaskView, error)
	RetryTask(context.Context, string, bool, time.Time) error
	CancelTask(context.Context, string, time.Time) error
	GetAttemptLogPaths(context.Context, int64) (AttemptLogPaths, error)
	ListEvents(context.Context, int64, int) ([]Event, error)
	CreateFire(context.Context, domain.Fire) (domain.Fire, error)
	UpdateFireStatus(context.Context, string, domain.FireStatus, time.Time) error
	ListFires(context.Context, string, int) ([]domain.Fire, error)
	GetLatestFireByLoop(context.Context, string) (domain.Fire, error)
	GetTaskGateLinkage(context.Context, string) (domain.TaskGateLinkage, error)
	CreateGate(context.Context, domain.Gate) (domain.Gate, error)
	GetGate(context.Context, string) (domain.Gate, error)
	ListOpenGates(context.Context) ([]domain.Gate, error)
	ResolveGate(context.Context, ResolveGateInput) error
}

type ResolveGateInput struct {
	ID       string
	Decision domain.GateDecision
	Note     string
	Now      time.Time
}
