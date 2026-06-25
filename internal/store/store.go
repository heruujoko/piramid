package store

import (
	"context"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
)

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

type Store interface {
	SaveGoalDraft(context.Context, domain.Goal, PersistedPaths) error
	UpdateGoalStatus(context.Context, string, domain.GoalStatus, time.Time) error
	AdmitPlan(context.Context, domain.Goal, domain.Plan, PersistedPaths) error
	ListRunnable(context.Context, time.Time, int) ([]domain.TaskRecord, error)
	AcquireReadLease(context.Context, string, string) error
	ReleaseReadLease(context.Context, string, string) error
	StartAttempt(context.Context, StartAttemptInput) (domain.Attempt, error)
	MoveToVerification(context.Context, FinishExecutionInput) error
	FinishVerification(context.Context, FinishVerificationInput) error
	RecordOperationalFailure(context.Context, OperationalFailureInput) error
	RecoverActive(context.Context, time.Time) ([]InterruptedAttempt, error)
	GetTask(context.Context, string) (domain.TaskView, error)
	ListTasks(context.Context, TaskFilter) ([]domain.TaskView, error)
}
