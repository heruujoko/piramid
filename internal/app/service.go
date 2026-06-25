package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/intake"
	"github.com/heruujoko/piramid/internal/records"
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

type WorkerProvider interface {
	ListWorkers(context.Context) ([]WorkerView, error)
	CancelTask(string)
}

type Service struct {
	Intake  *intake.Service
	Store   storepkg.Store
	Records *records.Store
	Workers WorkerProvider
	Now     func() time.Time
}

func NewService(
	intakeService *intake.Service,
	st storepkg.Store,
	recordStore *records.Store,
	workers WorkerProvider,
) *Service {
	return &Service{
		Intake: intakeService, Store: st, Records: recordStore, Workers: workers,
		Now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) Health(ctx context.Context) error {
	return s.Store.Health(ctx)
}

func (s *Service) DraftGoal(
	ctx context.Context,
	request intake.DraftRequest,
) (intake.Draft, error) {
	return s.Intake.Draft(ctx, request)
}

func (s *Service) ConfirmGoal(ctx context.Context, goalID string) error {
	return mapStoreError(s.Intake.Confirm(ctx, goalID))
}

func (s *Service) RejectGoal(ctx context.Context, goalID string) error {
	return mapStoreError(s.Intake.Reject(ctx, goalID))
}

func (s *Service) Enqueue(ctx context.Context, plan domain.Plan) error {
	if err := domain.ValidatePlan(&plan); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	project := plan.Tasks[0].ProjectPath
	goal := domain.Goal{
		ID: plan.GoalID, Text: "Direct structured task submission",
		ProjectPath: project, Status: domain.GoalConfirmed, CreatedAt: s.now(),
	}
	goalRecord, err := s.Records.WriteGoal(goal)
	if err != nil {
		return err
	}
	planRecord, err := s.Records.WritePlan(goal.ID, plan)
	if err != nil {
		return err
	}
	taskPaths := make(map[string]string, len(plan.Tasks))
	taskHashes := make(map[string]string, len(plan.Tasks))
	for _, task := range plan.Tasks {
		record, err := s.Records.WriteTask(task)
		if err != nil {
			return err
		}
		taskPaths[task.ID] = record.Path
		taskHashes[task.ID] = record.SHA256
	}
	return mapStoreError(s.Store.AdmitPlan(ctx, goal, plan, storepkg.PersistedPaths{
		GoalPath: goalRecord.Path, PlanPath: planRecord.Path,
		TaskPaths: taskPaths, TaskHashes: taskHashes,
	}))
}

func (s *Service) ListTasks(
	ctx context.Context,
	filter storepkg.TaskFilter,
) ([]domain.TaskView, error) {
	tasks, err := s.Store.ListTasks(ctx, filter)
	return tasks, mapStoreError(err)
}

func (s *Service) GetTask(ctx context.Context, taskID string) (domain.TaskView, error) {
	task, err := s.Store.GetTask(ctx, taskID)
	return task, mapStoreError(err)
}

func (s *Service) RetryTask(ctx context.Context, taskID string, override bool) error {
	return mapStoreError(s.Store.RetryTask(ctx, taskID, override, s.now()))
}

func (s *Service) CancelTask(ctx context.Context, taskID string) error {
	if s.Workers != nil {
		s.Workers.CancelTask(taskID)
	}
	return mapStoreError(s.Store.CancelTask(ctx, taskID, s.now()))
}

func (s *Service) ListWorkers(ctx context.Context) ([]WorkerView, error) {
	if s.Workers == nil {
		return []WorkerView{}, nil
	}
	return s.Workers.ListWorkers(ctx)
}

func (s *Service) ReadAttemptLog(
	ctx context.Context,
	attemptID int64,
	stream string,
	offset int64,
	limit int,
) ([]byte, int64, error) {
	paths, err := s.Store.GetAttemptLogPaths(ctx, attemptID)
	if err != nil {
		return nil, offset, mapStoreError(err)
	}
	path := map[string]string{
		"stdout":          paths.Stdout,
		"stderr":          paths.Stderr,
		"verifier-stdout": paths.VerifierStdout,
		"verifier-stderr": paths.VerifierStderr,
	}[stream]
	if path == "" {
		return nil, offset, ErrNotFound
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, offset, ErrNotFound
	}
	if err != nil {
		return nil, offset, err
	}
	defer file.Close()
	content := make([]byte, limit)
	count, err := file.ReadAt(content, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, offset, err
	}
	return content[:count], offset + int64(count), nil
}

func (s *Service) ListEvents(
	ctx context.Context,
	afterID int64,
	limit int,
) ([]storepkg.Event, error) {
	events, err := s.Store.ListEvents(ctx, afterID, limit)
	return events, mapStoreError(err)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func mapStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case errors.Is(err, storepkg.ErrInvalidState), errors.Is(err, storepkg.ErrProjectLeased):
		return fmt.Errorf("%w: %v", ErrConflict, err)
	default:
		return err
	}
}
