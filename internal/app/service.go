package app

import (
	"context"
	"errors"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/intake"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid request")
)

type WorkerView struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	TaskID    string `json:"task_id,omitempty"`
	AttemptID int64  `json:"attempt_id,omitempty"`
}

type Application interface {
	Health(context.Context) error
	DraftGoal(context.Context, intake.DraftRequest) (intake.Draft, error)
	ConfirmGoal(context.Context, string) error
	RejectGoal(context.Context, string) error
	Enqueue(context.Context, domain.Plan) error
	ListTasks(context.Context, storepkg.TaskFilter) ([]domain.TaskView, error)
	GetTask(context.Context, string) (domain.TaskView, error)
	RetryTask(context.Context, string, bool) error
	CancelTask(context.Context, string) error
	ListWorkers(context.Context) ([]WorkerView, error)
	ReadAttemptLog(context.Context, int64, string, int64, int) ([]byte, int64, error)
	ListEvents(context.Context, int64, int) ([]storepkg.Event, error)
}

type Service struct {
	Intake *intake.Service
}
