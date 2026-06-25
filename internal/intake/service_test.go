package intake

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/home"
	"github.com/heruujoko/piramid/internal/records"
	runtimepkg "github.com/heruujoko/piramid/internal/runtime"
	sqlitestore "github.com/heruujoko/piramid/internal/store/sqlite"
)

type fakePlanner struct {
	output     string
	result     runtimepkg.Result
	err        error
	invocation runtimepkg.Invocation
}

func (f *fakePlanner) Run(_ context.Context, invocation runtimepkg.Invocation) (runtimepkg.Result, error) {
	f.invocation = invocation
	if err := os.WriteFile(invocation.StdoutPath, []byte(f.output), 0o600); err != nil {
		return runtimepkg.Result{}, err
	}
	if err := os.WriteFile(invocation.StderrPath, nil, 0o600); err != nil {
		return runtimepkg.Result{}, err
	}
	return f.result, f.err
}

func validPlannerOutput(project string) string {
	return `version: 1
goal_id: GOAL-1
tasks:
  - id: TASK-1
    title: Inspect PR
    goal: Inspect the pull request
    project_path: ` + project + `
    dod:
      - inspection complete
    max_attempts: 3
    timeout: 1h
  - id: TASK-2
    title: Fix PR
    goal: Fix actionable issues
    project_path: ` + project + `
    dod:
      - checks pass
    max_attempts: 3
    timeout: 2h
    depends_on: [TASK-1]
`
}

func newTestService(t *testing.T, planner *fakePlanner) (*Service, *sqlitestore.Store, home.Paths, string) {
	t.Helper()
	paths := home.NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	if err := home.Init(paths); err != nil {
		t.Fatal(err)
	}
	repository, err := sqlitestore.Open(paths.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	project, err = filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(ServiceConfig{
		Repository: repository,
		Records:    records.New(paths),
		Planner:    planner,
		Runtime: RuntimeConfig{
			Command: "pi",
			Args:    []string{"-p", "{{prompt}}"},
			Timeout: time.Minute,
		},
		IDGenerator: func() string { return "GOAL-1" },
	})
	return service, repository, paths, project
}

func TestDraftInvokesPlannerInCanonicalProjectAndPersistsEvidence(t *testing.T) {
	planner := &fakePlanner{}
	service, repository, paths, project := newTestService(t, planner)
	planner.output = validPlannerOutput(project)

	draft, err := service.Draft(context.Background(), DraftRequest{
		GoalText:    "Maintain this PR",
		ProjectPath: project + string(filepath.Separator) + ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	if draft.Goal.ProjectPath != project || planner.invocation.WorkingDir != project {
		t.Fatalf("project paths: goal=%q invocation=%q want=%q",
			draft.Goal.ProjectPath, planner.invocation.WorkingDir, project)
	}
	if !strings.Contains(strings.Join(planner.invocation.Args, "\n"), "Maintain this PR") {
		t.Fatalf("planner args do not contain goal: %#v", planner.invocation.Args)
	}
	if len(draft.Plan.Tasks) != 2 || draft.Plan.Tasks[1].DependsOn[0] != "TASK-1" {
		t.Fatalf("plan = %#v", draft.Plan)
	}
	for _, path := range []string{
		filepath.Join(paths.Goals, "GOAL-1", "goal.yaml"),
		filepath.Join(paths.Goals, "GOAL-1", "planner-prompt.md"),
		filepath.Join(paths.Goals, "GOAL-1", "planner-stdout.log"),
		filepath.Join(paths.Goals, "GOAL-1", "planner-stderr.log"),
		filepath.Join(paths.Goals, "GOAL-1", "generated-plan.yaml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected evidence %s: %v", path, err)
		}
	}
	if _, err := repository.GetTask(context.Background(), "TASK-1"); err == nil {
		t.Fatal("task admitted before confirmation")
	}
}

func TestConfirmAtomicallyAdmitsGeneratedPlan(t *testing.T) {
	planner := &fakePlanner{}
	service, repository, _, project := newTestService(t, planner)
	planner.output = validPlannerOutput(project)
	if _, err := service.Draft(context.Background(), DraftRequest{
		GoalText: "Maintain this PR", ProjectPath: project,
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.Confirm(context.Background(), "GOAL-1"); err != nil {
		t.Fatal(err)
	}
	task, err := repository.GetTask(context.Background(), "TASK-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Dependencies) != 1 || task.Dependencies[0] != "TASK-1" {
		t.Fatalf("dependencies = %#v", task.Dependencies)
	}
}

func TestRejectPreservesDraftWithoutAdmittingTasks(t *testing.T) {
	planner := &fakePlanner{}
	service, repository, paths, project := newTestService(t, planner)
	planner.output = validPlannerOutput(project)
	if _, err := service.Draft(context.Background(), DraftRequest{
		GoalText: "Maintain this PR", ProjectPath: project,
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.Reject(context.Background(), "GOAL-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.Goals, "GOAL-1", "generated-plan.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.GetTask(context.Background(), "TASK-1"); err == nil {
		t.Fatal("rejected draft admitted a task")
	}
}

func TestDraftRejectsInvalidPlannerOutputWithoutAdmission(t *testing.T) {
	tests := []struct {
		name   string
		output func(string) string
	}{
		{"invalid yaml", func(string) string { return "tasks: [" }},
		{"cycle", func(project string) string {
			return strings.Replace(validPlannerOutput(project),
				"depends_on: [TASK-1]", "depends_on: [TASK-2]", 1)
		}},
		{"relative project", func(project string) string {
			return strings.Replace(validPlannerOutput(project), project, "relative", 1)
		}},
		{"trailing document", func(project string) string {
			return validPlannerOutput(project) + "---\nextra: true\n"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planner := &fakePlanner{}
			service, repository, _, project := newTestService(t, planner)
			planner.output = tt.output(project)
			if _, err := service.Draft(context.Background(), DraftRequest{
				GoalText: "Maintain this PR", ProjectPath: project,
			}); err == nil {
				t.Fatal("Draft() error = nil")
			}
			if _, err := repository.GetTask(context.Background(), "TASK-1"); err == nil {
				t.Fatal("invalid plan admitted a task")
			}
		})
	}
}

func TestDraftRejectsPlannerFailure(t *testing.T) {
	planner := &fakePlanner{result: runtimepkg.Result{ExitCode: 9}}
	service, _, _, project := newTestService(t, planner)
	planner.output = validPlannerOutput(project)

	if _, err := service.Draft(context.Background(), DraftRequest{
		GoalText: "Maintain this PR", ProjectPath: project,
	}); err == nil {
		t.Fatal("Draft() accepted non-zero planner exit")
	}
}

func TestParsePlanRequiresKnownFields(t *testing.T) {
	_, err := ParsePlan(strings.NewReader(`version: 1
goal_id: GOAL-1
mystery: true
tasks: []
`))
	if err == nil {
		t.Fatal("ParsePlan() accepted unknown field")
	}
}

var _ = domain.Goal{}
