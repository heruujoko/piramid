package domain

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func ValidatePlan(plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is required")
	}
	if plan.Version != 1 {
		return fmt.Errorf("plan version: must be 1")
	}
	if strings.TrimSpace(plan.GoalID) == "" {
		return fmt.Errorf("plan goal_id: is required")
	}
	if len(plan.Tasks) == 0 {
		return fmt.Errorf("plan tasks: at least one task is required")
	}

	tasks := make(map[string]*Task, len(plan.Tasks))
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		if err := validateTask(task); err != nil {
			return err
		}
		if _, exists := tasks[task.ID]; exists {
			return fmt.Errorf("task %s id: duplicate", task.ID)
		}
		tasks[task.ID] = task
	}

	for _, task := range plan.Tasks {
		if task.ParentTaskID != "" {
			if task.ParentTaskID == task.ID {
				return fmt.Errorf("task %s parent_task_id: cannot reference itself", task.ID)
			}
			if _, exists := tasks[task.ParentTaskID]; !exists {
				return fmt.Errorf("task %s parent_task_id: unknown task %s", task.ID, task.ParentTaskID)
			}
		}
		for _, dependency := range task.DependsOn {
			if _, exists := tasks[dependency]; !exists {
				return fmt.Errorf("task %s depends_on: unknown task %s", task.ID, dependency)
			}
		}
	}

	if err := validateAcyclic(tasks); err != nil {
		return err
	}
	return nil
}

func NormalizeTask(task *Task) error {
	return validateTask(task)
}

func validateTask(task *Task) error {
	task.ID = strings.TrimSpace(task.ID)
	if task.ID == "" {
		return fmt.Errorf("task id: is required")
	}
	if strings.TrimSpace(task.Goal) == "" {
		return fmt.Errorf("task %s goal: is required", task.ID)
	}
	if !filepath.IsAbs(task.ProjectPath) || filepath.Clean(task.ProjectPath) != task.ProjectPath {
		return fmt.Errorf("task %s project_path: must be an absolute clean path", task.ID)
	}
	if len(task.DOD) == 0 {
		return fmt.Errorf("task %s dod: at least one criterion is required", task.ID)
	}
	for _, criterion := range task.DOD {
		if strings.TrimSpace(criterion) == "" {
			return fmt.Errorf("task %s dod: criteria cannot be empty", task.ID)
		}
	}
	if task.MaxAttempts <= 0 {
		return fmt.Errorf("task %s max_attempts: must be positive", task.ID)
	}

	if strings.TrimSpace(task.TimeoutText) != "" {
		timeout, err := time.ParseDuration(task.TimeoutText)
		if err != nil {
			return fmt.Errorf("task %s timeout: %w", task.ID, err)
		}
		task.Timeout = timeout
	} else if task.Timeout > 0 {
		task.TimeoutText = task.Timeout.String()
	}
	if task.Timeout <= 0 {
		return fmt.Errorf("task %s timeout: must be positive", task.ID)
	}
	return nil
}

func validateAcyclic(tasks map[string]*Task) error {
	const (
		unvisited = iota
		visiting
		visited
	)
	colors := make(map[string]int, len(tasks))

	var visit func(string) error
	visit = func(id string) error {
		switch colors[id] {
		case visiting:
			return fmt.Errorf("task %s depends_on: dependency cycle detected", id)
		case visited:
			return nil
		}
		colors[id] = visiting
		for _, dependency := range tasks[id].DependsOn {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		colors[id] = visited
		return nil
	}

	for id := range tasks {
		if colors[id] == unvisited {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}
