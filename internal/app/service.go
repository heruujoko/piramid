package app

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/heruujoko/piramid/internal/definitions"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/intake"
	"github.com/heruujoko/piramid/internal/records"
	storepkg "github.com/heruujoko/piramid/internal/store"
	"github.com/heruujoko/piramid/internal/webhook"
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
	ListLoops(context.Context) ([]domain.LoopView, error)
	ListLoopFires(context.Context, string) ([]domain.FireView, error)
	ListOpenGates(context.Context) ([]domain.GateSummary, error)
	GetGate(context.Context, string) (domain.GateDetail, error)
	ResolveGate(context.Context, string, domain.GateDecisionInput) error
	HandleGitHubWebhook(context.Context, string, string, []byte) error
}

type WorkerProvider interface {
	ListWorkers(context.Context) ([]WorkerView, error)
	CancelTask(string)
}

// DefinitionsProvider loads loop/pattern definitions from a file-backed root.
type DefinitionsProvider interface {
	Load(context.Context) (definitions.Snapshot, error)
}

type Service struct {
	Intake        *intake.Service
	Store         storepkg.Store
	Records       *records.Store
	Workers       WorkerProvider
	Definitions   DefinitionsProvider
	WebhookSecret string
	Now           func() time.Time
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
	draft, err := s.Intake.Draft(ctx, request)
	return draft, mapStoreError(err)
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

// --- Loop methods ---

func (s *Service) ListLoops(ctx context.Context) ([]domain.LoopView, error) {
	if s.Definitions == nil {
		return nil, fmt.Errorf("definitions not configured")
	}
	snapshot, err := s.Definitions.Load(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	views := make([]domain.LoopView, 0, len(snapshot.Loops))
	for _, loop := range snapshot.Loops {
		view := domain.LoopView{
			ID:        loop.ID,
			PatternID: loop.PatternID,
			Active:    loop.Active,
			Cron:      loop.Cron,
			Autonomy:  string(loop.Autonomy),
		}
		fire, err := s.Store.GetLatestFireByLoop(ctx, loop.ID)
		if err == nil {
			view.LatestFire = &domain.FireSummary{
				ID:          fire.ID,
				LoopID:      fire.LoopID,
				Status:      string(fire.Status),
				ScheduledAt: formatTime(fire.ScheduledAt),
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) ListLoopFires(ctx context.Context, loopID string) ([]domain.FireView, error) {
	fires, err := s.Store.ListFires(ctx, loopID, 50)
	if err != nil {
		return nil, mapStoreError(err)
	}
	views := make([]domain.FireView, 0, len(fires))
	for _, f := range fires {
		views = append(views, domain.FireView{
			ID:          f.ID,
			LoopID:      f.LoopID,
			GoalID:      f.GoalID,
			Status:      string(f.Status),
			ScheduledAt: formatTime(f.ScheduledAt),
			StartedAt:   formatTime(f.StartedAt),
			LastError:   f.LastError,
		})
	}
	return views, nil
}

// --- Gate methods ---

func (s *Service) ListOpenGates(ctx context.Context) ([]domain.GateSummary, error) {
	gates, err := s.Store.ListOpenGates(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	summaries := make([]domain.GateSummary, 0, len(gates))
	for _, g := range gates {
		summaries = append(summaries, domain.GateSummary{
			ID:       g.ID,
			Gate:     g.Context.Gate,
			Phase:    g.Context.Phase,
			FireID:   g.FireID,
			LoopID:   g.Context.LoopID,
			Summary:  g.Context.Summary,
			OpenedAt: formatTime(g.OpenedAt),
		})
	}
	return summaries, nil
}

func (s *Service) GetGate(ctx context.Context, gateID string) (domain.GateDetail, error) {
	gate, err := s.Store.GetGate(ctx, gateID)
	if err != nil {
		return domain.GateDetail{}, mapStoreError(err)
	}
	detail := domain.GateDetail{
		ID:        gate.ID,
		Gate:      gate.Context.Gate,
		Phase:     gate.Context.Phase,
		FireID:    gate.FireID,
		LoopID:    gate.Context.LoopID,
		GoalID:    gate.GoalID,
		TaskID:    gate.TaskID,
		AttemptID: gate.AttemptID,
		Summary:   gate.Context.Summary,
		OpenedAt:  formatTime(gate.OpenedAt),
		Body:      gate.Context.Body,
	}
	for _, opt := range gate.Context.DecisionOptions {
		detail.DecisionOptions = append(detail.DecisionOptions, string(opt))
	}
	threads := make([]domain.GateThreadView, 0, len(gate.Context.Threads))
	for _, t := range gate.Context.Threads {
		threads = append(threads, domain.GateThreadView{
			ID:       t.ID,
			Title:    t.Title,
			Location: t.Location,
			Author:   t.Author,
			Summary:  t.Summary,
		})
	}
	detail.Threads = threads
	return detail, nil
}

func (s *Service) ResolveGate(ctx context.Context, gateID string, input domain.GateDecisionInput) error {
	if input.Decision == "" {
		return fmt.Errorf("%w: decision is required", ErrInvalid)
	}
	decision := domain.GateDecision(input.Decision)
	switch decision {
	case domain.GateDecisionApprove,
		domain.GateDecisionRoute,
		domain.GateDecisionDefer,
		domain.GateDecisionReject:
	default:
		return fmt.Errorf("%w: invalid decision: %s", ErrInvalid, input.Decision)
	}
	return mapStoreError(s.Store.ResolveGate(ctx, storepkg.ResolveGateInput{
		ID:       gateID,
		Decision: decision,
		Note:     input.Note,
		Now:      s.now(),
	}))
}

// --- GitHub webhook ---

func (s *Service) HandleGitHubWebhook(ctx context.Context, eventType string, signature string, payload []byte) error {
	if err := webhook.VerifySignature(s.WebhookSecret, payload, signature); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	event, err := webhook.ParseEvent(payload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	eventKey := webhook.BuildEventKey(eventType, event)

	if s.Definitions == nil {
		return fmt.Errorf("definitions not configured")
	}
	snapshot, err := s.Definitions.Load(ctx)
	if err != nil {
		return err
	}
	matched := webhook.MatchLoops(eventKey, event.Repository.FullName, snapshot.Loops)
	for _, loop := range matched {
		// Create a fire for each matched loop via the same path as the loop scheduler.
		now := s.now()
		goalID := fmt.Sprintf("GOAL-%s-%s", safeID(loop.ID), now.Format("20060102150405"))
		fireID := fmt.Sprintf("FIRE-%s-%s", safeID(loop.ID), now.Format("20060102150405"))

		fire := domain.Fire{
			ID:          fireID,
			LoopID:      loop.ID,
			GoalID:      goalID,
			Status:      domain.FireScheduled,
			ScheduledAt: now,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		goal := domain.Goal{
			ID:          goalID,
			FireID:      fireID,
			Text:        loop.Goal,
			ProjectPath: loop.ProjectPath,
			Status:      domain.GoalDraft,
			CreatedAt:   now,
		}
		_, _ = s.Records.WriteGoal(goal) // best-effort record
		if err := s.Store.SaveGoalDraft(ctx, goal, storepkg.PersistedPaths{}); err != nil {
			return err
		}
		if _, err := s.Store.CreateFire(ctx, fire); err != nil {
			return err
		}
	}
	return nil
}

func safeID(value string) string {
	return hex.EncodeToString([]byte(value))
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
