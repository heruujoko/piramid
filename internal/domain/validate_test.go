package domain

import (
	"strings"
	"testing"
	"time"
)

func validPlan() Plan {
	return Plan{
		Version: 1,
		GoalID:  "GOAL-1",
		Tasks: []Task{{
			ID:          "TASK-1",
			Title:       "Maintain PR",
			Goal:        "Leave the pull request ready for review.",
			ProjectPath: "/tmp/project",
			DOD:         []string{"required checks pass"},
			MaxAttempts: 3,
			Timeout:     time.Hour,
			TimeoutText: "1h",
		}},
	}
}

func TestValidatePlanAcceptsValidSingleTask(t *testing.T) {
	plan := validPlan()

	if err := ValidatePlan(&plan); err != nil {
		t.Fatalf("ValidatePlan() error = %v", err)
	}
	if plan.Tasks[0].Timeout != time.Hour {
		t.Fatalf("Timeout = %s, want 1h", plan.Tasks[0].Timeout)
	}
}

func TestValidatePlanRejectsInvalidTasks(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*Plan)
		contains string
	}{
		{
			name: "empty task id",
			mutate: func(plan *Plan) {
				plan.Tasks[0].ID = ""
			},
			contains: "task id",
		},
		{
			name: "relative project path",
			mutate: func(plan *Plan) {
				plan.Tasks[0].ProjectPath = "relative/project"
			},
			contains: "TASK-1 project_path",
		},
		{
			name: "unclean project path",
			mutate: func(plan *Plan) {
				plan.Tasks[0].ProjectPath = "/tmp/project/../other"
			},
			contains: "TASK-1 project_path",
		},
		{
			name: "empty definition of done",
			mutate: func(plan *Plan) {
				plan.Tasks[0].DOD = nil
			},
			contains: "TASK-1 dod",
		},
		{
			name: "unknown dependency",
			mutate: func(plan *Plan) {
				plan.Tasks[0].DependsOn = []string{"TASK-404"}
			},
			contains: "TASK-1 depends_on",
		},
		{
			name: "unknown parent",
			mutate: func(plan *Plan) {
				plan.Tasks[0].ParentTaskID = "TASK-404"
			},
			contains: "TASK-1 parent_task_id",
		},
		{
			name: "duplicate task id",
			mutate: func(plan *Plan) {
				plan.Tasks = append(plan.Tasks, plan.Tasks[0])
			},
			contains: "TASK-1 id",
		},
		{
			name: "non-positive timeout",
			mutate: func(plan *Plan) {
				plan.Tasks[0].Timeout = 0
				plan.Tasks[0].TimeoutText = "0s"
			},
			contains: "TASK-1 timeout",
		},
		{
			name: "invalid timeout",
			mutate: func(plan *Plan) {
				plan.Tasks[0].TimeoutText = "later"
			},
			contains: "TASK-1 timeout",
		},
		{
			name: "non-positive maximum attempts",
			mutate: func(plan *Plan) {
				plan.Tasks[0].MaxAttempts = 0
			},
			contains: "TASK-1 max_attempts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := validPlan()
			tt.mutate(&plan)

			err := ValidatePlan(&plan)
			if err == nil {
				t.Fatal("ValidatePlan() error = nil")
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("ValidatePlan() error = %q, want substring %q", err, tt.contains)
			}
		})
	}
}

func TestValidatePlanRejectsDependencyCycle(t *testing.T) {
	plan := validPlan()
	second := plan.Tasks[0]
	second.ID = "TASK-2"
	second.DependsOn = []string{"TASK-1"}
	plan.Tasks[0].DependsOn = []string{"TASK-2"}
	plan.Tasks = append(plan.Tasks, second)

	err := ValidatePlan(&plan)
	if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
		t.Fatalf("ValidatePlan() error = %v, want dependency cycle", err)
	}
}

func TestCanTransitionAllowsOnlyLifecycleEdges(t *testing.T) {
	allowed := [][2]TaskStatus{
		{TaskPending, TaskRunning},
		{TaskPending, TaskCancelled},
		{TaskPending, TaskBlocked},
		{TaskRunning, TaskVerifying},
		{TaskRunning, TaskFailed},
		{TaskRunning, TaskCancelled},
		{TaskVerifying, TaskCompleted},
		{TaskVerifying, TaskRetryWait},
		{TaskVerifying, TaskFailed},
		{TaskVerifying, TaskCancelled},
		{TaskRetryWait, TaskPending},
		{TaskRetryWait, TaskCancelled},
		{TaskRetryWait, TaskBlocked},
	}
	for _, edge := range allowed {
		if !CanTransition(edge[0], edge[1]) {
			t.Errorf("CanTransition(%s, %s) = false", edge[0], edge[1])
		}
	}

	if CanTransition(TaskCompleted, TaskRunning) {
		t.Error("completed task transitioned to running")
	}
	if CanTransition(TaskPending, TaskCompleted) {
		t.Error("pending task transitioned directly to completed")
	}
}
