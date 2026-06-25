package intake

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/prompt"
	"github.com/heruujoko/piramid/internal/records"
	runtimepkg "github.com/heruujoko/piramid/internal/runtime"
	storepkg "github.com/heruujoko/piramid/internal/store"
	"gopkg.in/yaml.v3"
)

type Repository interface {
	SaveGoalDraft(context.Context, domain.Goal, storepkg.PersistedPaths) error
	UpdateGoalStatus(context.Context, string, domain.GoalStatus, time.Time) error
	AdmitPlan(context.Context, domain.Goal, domain.Plan, storepkg.PersistedPaths) error
	AcquireReadLease(context.Context, string, string) error
	ReleaseReadLease(context.Context, string, string) error
}

type RuntimeConfig struct {
	Command     string
	Args        []string
	Environment []string
	Timeout     time.Duration
}

type ServiceConfig struct {
	Repository  Repository
	Records     *records.Store
	Planner     runtimepkg.Adapter
	Runtime     RuntimeConfig
	IDGenerator func() string
	Now         func() time.Time
}

type Service struct {
	repository  Repository
	records     *records.Store
	planner     runtimepkg.Adapter
	runtime     RuntimeConfig
	idGenerator func() string
	now         func() time.Time
}

type DraftRequest struct {
	GoalText    string
	ProjectPath string
}

type Draft struct {
	Goal  domain.Goal
	Plan  domain.Plan
	Paths records.GoalPaths
}

func NewService(config ServiceConfig) *Service {
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		repository:  config.Repository,
		records:     config.Records,
		planner:     config.Planner,
		runtime:     config.Runtime,
		idGenerator: config.IDGenerator,
		now:         now,
	}
}

func (s *Service) Draft(ctx context.Context, request DraftRequest) (Draft, error) {
	if strings.TrimSpace(request.GoalText) == "" {
		return Draft{}, fmt.Errorf("goal text is required")
	}
	project, err := canonicalDirectory(request.ProjectPath)
	if err != nil {
		return Draft{}, err
	}
	goalID := s.idGenerator()
	if strings.TrimSpace(goalID) == "" {
		return Draft{}, fmt.Errorf("goal ID generator returned an empty ID")
	}
	goal := domain.Goal{
		ID:          goalID,
		Text:        strings.TrimSpace(request.GoalText),
		ProjectPath: project,
		Status:      domain.GoalDraft,
		CreatedAt:   s.now(),
	}
	goalPaths, err := s.records.GoalPaths(goalID)
	if err != nil {
		return Draft{}, err
	}
	goalRecord, err := s.records.WriteGoal(goal)
	if err != nil {
		return Draft{}, err
	}
	if err := s.repository.SaveGoalDraft(ctx, goal, storepkg.PersistedPaths{
		GoalPath: goalRecord.Path,
		PlanPath: goalPaths.Plan,
	}); err != nil {
		return Draft{}, err
	}

	if err := s.repository.AcquireReadLease(ctx, project, goalID); err != nil {
		return Draft{}, err
	}
	defer s.repository.ReleaseReadLease(context.WithoutCancel(ctx), project, goalID)

	body := plannerBody(goal)
	orchestrator, err := os.ReadFile(filepath.Join(s.records.Paths().Prompts, "orchestrator.md"))
	if err != nil {
		return Draft{}, err
	}
	rolePolicy, err := os.ReadFile(filepath.Join(s.records.Paths().Prompts, "planner.md"))
	if err != nil {
		return Draft{}, err
	}
	rendered, _ := prompt.Render(prompt.RenderInput{
		Role:         prompt.RolePlanner,
		Orchestrator: orchestrator,
		RolePolicy:   rolePolicy,
		Body:         []byte(body),
	})
	if _, err := s.records.WriteImmutable(goalPaths.PlannerPrompt, rendered); err != nil {
		return Draft{}, err
	}
	args, err := runtimepkg.ExpandArgs(s.runtime.Args, runtimepkg.TemplateValues{
		Prompt:     string(rendered),
		PromptFile: goalPaths.PlannerPrompt,
		Workspace:  project,
		TaskID:     goalID,
		Attempt:    "1",
	})
	if err != nil {
		return Draft{}, err
	}
	result, err := s.planner.Run(ctx, runtimepkg.Invocation{
		Command:     s.runtime.Command,
		Args:        args,
		WorkingDir:  project,
		Environment: s.runtime.Environment,
		Timeout:     s.runtime.Timeout,
		StdoutPath:  goalPaths.PlannerStdout,
		StderrPath:  goalPaths.PlannerStderr,
	})
	if err != nil {
		return Draft{}, err
	}
	if result.ExitCode != 0 {
		return Draft{}, fmt.Errorf("planner exited with code %d", result.ExitCode)
	}
	output, err := os.ReadFile(goalPaths.PlannerStdout)
	if err != nil {
		return Draft{}, err
	}
	plan, err := ParsePlan(bytes.NewReader(output))
	if err != nil {
		return Draft{}, fmt.Errorf("parse planner output: %w", err)
	}
	if plan.GoalID != goalID {
		return Draft{}, fmt.Errorf("planner goal_id %s does not match %s", plan.GoalID, goalID)
	}
	for _, task := range plan.Tasks {
		if task.ProjectPath != project {
			return Draft{}, fmt.Errorf("task %s project_path must equal goal project", task.ID)
		}
	}
	if _, err := s.records.WritePlan(goalID, plan); err != nil {
		return Draft{}, err
	}
	return Draft{Goal: goal, Plan: plan, Paths: goalPaths}, nil
}

func (s *Service) Confirm(ctx context.Context, goalID string) error {
	goal, plan, paths, err := s.loadDraft(goalID)
	if err != nil {
		return err
	}
	taskPaths := make(map[string]string, len(plan.Tasks))
	taskHashes := make(map[string]string, len(plan.Tasks))
	for _, task := range plan.Tasks {
		record, err := s.records.WriteTask(task)
		if err != nil {
			return err
		}
		taskPaths[task.ID] = record.Path
		taskHashes[task.ID] = record.SHA256
	}
	goal.Status = domain.GoalConfirmed
	return s.repository.AdmitPlan(ctx, goal, plan, storepkg.PersistedPaths{
		GoalPath:   paths.Goal,
		PlanPath:   paths.Plan,
		TaskPaths:  taskPaths,
		TaskHashes: taskHashes,
	})
}

func (s *Service) Reject(ctx context.Context, goalID string) error {
	return s.repository.UpdateGoalStatus(ctx, goalID, domain.GoalRejected, s.now())
}

func (s *Service) loadDraft(goalID string) (
	domain.Goal,
	domain.Plan,
	records.GoalPaths,
	error,
) {
	paths, err := s.records.GoalPaths(goalID)
	if err != nil {
		return domain.Goal{}, domain.Plan{}, records.GoalPaths{}, err
	}
	goalContent, err := os.ReadFile(paths.Goal)
	if err != nil {
		return domain.Goal{}, domain.Plan{}, records.GoalPaths{}, err
	}
	var goal domain.Goal
	if err := yaml.Unmarshal(goalContent, &goal); err != nil {
		return domain.Goal{}, domain.Plan{}, records.GoalPaths{}, err
	}
	planFile, err := os.Open(paths.Plan)
	if err != nil {
		return domain.Goal{}, domain.Plan{}, records.GoalPaths{}, err
	}
	defer planFile.Close()
	plan, err := ParsePlan(planFile)
	if err != nil {
		return domain.Goal{}, domain.Plan{}, records.GoalPaths{}, err
	}
	return goal, plan, paths, nil
}

func canonicalDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project path %s is not a directory", resolved)
	}
	return filepath.Clean(resolved), nil
}

func plannerBody(goal domain.Goal) string {
	return `Create an executable Pi-Ramid task graph for the following goal.
Return YAML only, with no prose or Markdown fence.

Required schema:
version: 1
goal_id: ` + goal.ID + `
tasks:
  - id: TASK-ID
    title: Short title
    goal: Terminal execution goal
    project_path: ` + goal.ProjectPath + `
    inputs: []
    expected_outputs: []
    dod:
      - measurable terminal criterion
    model: optional-model
    max_attempts: 3
    timeout: 1h
    depends_on: []

Rules:
- Produce one task for simple work or multiple dependency-linked tasks for complex work.
- Use only the absolute project path shown above.
- Every task needs non-empty terminal DOD, max_attempts, and timeout.
- Dependencies must reference task IDs in this graph and must be acyclic.
- Never include credentials or secrets.

Goal:
` + goal.Text + `

Goal ID: ` + goal.ID + `
Project: ` + goal.ProjectPath + `
Attempt: ` + strconv.Itoa(1)
}
