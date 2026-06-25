package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

func (s *Store) SaveGoalDraft(
	ctx context.Context,
	goal domain.Goal,
	paths storepkg.PersistedPaths,
) error {
	now := goal.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO goals(
			id, text, project_path, status, goal_path, plan_path, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, goal.ID, goal.Text, goal.ProjectPath, goal.Status, paths.GoalPath, paths.PlanPath,
		formatTime(now), formatTime(now)); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "goal", goal.ID, "GOAL_DRAFTED", nil, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateGoalStatus(
	ctx context.Context,
	goalID string,
	status domain.GoalStatus,
	now time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE goals SET status = ?, updated_at = ? WHERE id = ?
	`, status, formatTime(now), goalID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return sql.ErrNoRows
	}
	if err := appendEvent(ctx, tx, "goal", goalID, "GOAL_STATUS_CHANGED",
		map[string]any{"status": status}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AdmitPlan(
	ctx context.Context,
	goal domain.Goal,
	plan domain.Plan,
	paths storepkg.PersistedPaths,
) error {
	if err := domain.ValidatePlan(&plan); err != nil {
		return err
	}
	if goal.ID != plan.GoalID {
		return fmt.Errorf("goal id %s does not match plan goal_id %s", goal.ID, plan.GoalID)
	}
	now := goal.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingStatus string
	err = tx.QueryRowContext(ctx, "SELECT status FROM goals WHERE id = ?", goal.ID).Scan(&existingStatus)
	switch {
	case err == sql.ErrNoRows:
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO goals(
				id, text, project_path, status, goal_path, plan_path, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, goal.ID, goal.Text, goal.ProjectPath, domain.GoalConfirmed,
			paths.GoalPath, paths.PlanPath, formatTime(now), formatTime(now)); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if existingStatus != string(domain.GoalDraft) {
			return fmt.Errorf("goal %s cannot be admitted from status %s", goal.ID, existingStatus)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE goals
			SET status = ?, goal_path = ?, plan_path = ?, updated_at = ?
			WHERE id = ?
		`, domain.GoalConfirmed, paths.GoalPath, paths.PlanPath, formatTime(now), goal.ID); err != nil {
			return err
		}
	}

	for _, task := range plan.Tasks {
		taskJSON, err := json.Marshal(task)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tasks(
				id, goal_id, parent_task_id, title, goal_text, project_path,
				task_path, task_hash, task_json, status, model, max_attempts,
				timeout_seconds, attempt_count, created_at, updated_at
			) VALUES (?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		`, task.ID, goal.ID, task.Title, task.Goal, task.ProjectPath,
			paths.TaskPaths[task.ID], paths.TaskHashes[task.ID], string(taskJSON),
			domain.TaskPending, task.Model, task.MaxAttempts, int64(task.Timeout.Seconds()),
			formatTime(now), formatTime(now)); err != nil {
			return err
		}
	}
	for _, task := range plan.Tasks {
		if task.ParentTaskID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks SET parent_task_id = ? WHERE id = ?
		`, task.ParentTaskID, task.ID); err != nil {
			return err
		}
	}
	for _, task := range plan.Tasks {
		for _, dependency := range task.DependsOn {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO task_dependencies(task_id, depends_on_task_id) VALUES (?, ?)
			`, task.ID, dependency); err != nil {
				return err
			}
		}
	}
	if err := appendEvent(ctx, tx, "goal", goal.ID, "GOAL_ADMITTED",
		map[string]any{"task_count": len(plan.Tasks)}, now); err != nil {
		return err
	}
	return tx.Commit()
}
