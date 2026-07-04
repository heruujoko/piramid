package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

func (s *Store) RetryTask(
	ctx context.Context,
	taskID string,
	override bool,
	now time.Time,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	var attempts, maxAttempts int
	if err := tx.QueryRowContext(ctx, `
		SELECT status, attempt_count, max_attempts FROM tasks WHERE id = ?
	`, taskID).Scan(&status, &attempts, &maxAttempts); err != nil {
		return err
	}
	if status != string(domain.TaskFailed) {
		return fmt.Errorf("%w: task %s is %s", storepkg.ErrInvalidState, taskID, status)
	}
	if attempts >= maxAttempts && !override {
		return fmt.Errorf("%w: task %s exhausted attempts", storepkg.ErrInvalidState, taskID)
	}
	if attempts >= maxAttempts {
		maxAttempts = attempts + 1
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, max_attempts = ?, next_run_at = ?, updated_at = ?
		WHERE id = ?
	`, domain.TaskPending, maxAttempts, formatTime(now), formatTime(now), taskID); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", taskID, "OPERATOR_RETRY",
		map[string]any{"override": override}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelTask(ctx context.Context, taskID string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = ?", taskID).Scan(&status); err != nil {
		return err
	}
	switch domain.TaskStatus(status) {
	case domain.TaskPending, domain.TaskRunning, domain.TaskVerifying, domain.TaskRetryWait:
	default:
		return fmt.Errorf("%w: task %s is %s", storepkg.ErrInvalidState, taskID, status)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, next_run_at = NULL, updated_at = ? WHERE id = ?
	`, domain.TaskCancelled, formatTime(now), taskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE attempts
		SET status = ?, failure_class = 'cancelled', finished_at = ?
		WHERE task_id = ? AND status IN (?, ?)
	`, domain.AttemptInterrupted, formatTime(now), taskID,
		domain.AttemptRunning, domain.AttemptVerifying); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM workspace_leases
		WHERE holder_type = 'attempt'
		  AND holder_id IN (SELECT CAST(id AS TEXT) FROM attempts WHERE task_id = ?)
	`, taskID); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", taskID, "OPERATOR_CANCEL", nil, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetAttemptLogPaths(
	ctx context.Context,
	attemptID int64,
) (storepkg.AttemptLogPaths, error) {
	var paths storepkg.AttemptLogPaths
	err := s.db.QueryRowContext(ctx, `
		SELECT stdout_path, stderr_path, verifier_stdout_path, verifier_stderr_path
		FROM attempts WHERE id = ?
	`, attemptID).Scan(
		&paths.Stdout,
		&paths.Stderr,
		&paths.VerifierStdout,
		&paths.VerifierStderr,
	)
	return paths, err
}

func (s *Store) ListEvents(
	ctx context.Context,
	afterID int64,
	limit int,
) ([]storepkg.Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, entity_type, entity_id, event_type, payload_json, created_at
		FROM events WHERE id > ? ORDER BY id LIMIT ?
	`, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []storepkg.Event
	for rows.Next() {
		var event storepkg.Event
		var createdAt string
		if err := rows.Scan(
			&event.ID, &event.EntityType, &event.EntityID,
			&event.EventType, &event.PayloadJSON, &createdAt,
		); err != nil {
			return nil, err
		}
		event.CreatedAt = parseTime(createdAt)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) ResumeGatedTask(ctx context.Context, in storepkg.ResumeGatedTaskInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	var attempts, maxAttempts int
	if err := tx.QueryRowContext(ctx, `
		SELECT status, attempt_count, max_attempts FROM tasks WHERE id = ?
	`, in.TaskID).Scan(&status, &attempts, &maxAttempts); err != nil {
		return err
	}
	if status != string(domain.TaskGated) {
		return fmt.Errorf("%w: task %s is %s", storepkg.ErrInvalidState, in.TaskID, status)
	}
	if attempts >= maxAttempts && !in.Override {
		return fmt.Errorf("%w: task %s exhausted attempts", storepkg.ErrInvalidState, in.TaskID)
	}
	if attempts >= maxAttempts {
		maxAttempts = attempts + 1
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, max_attempts = ?, resume_prompt = ?, next_run_at = ?, updated_at = ?
		WHERE id = ?
	`, domain.TaskPending, maxAttempts, in.RestorePrompt, formatTime(now), formatTime(now), in.TaskID); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", in.TaskID, "GATE_RESUME",
		map[string]any{"override": in.Override}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GateAttempt(ctx context.Context, in storepkg.GateAttemptInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = ?", in.TaskID).Scan(&status); err != nil {
		return err
	}
	if status != string(domain.TaskRunning) {
		return fmt.Errorf("%w: task %s is %s", storepkg.ErrInvalidState, in.TaskID, status)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE attempts SET status = ?, finished_at = ? WHERE id = ? AND task_id = ?
	`, domain.AttemptGated, formatTime(now), in.AttemptID, in.TaskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, updated_at = ? WHERE id = ? AND status = ?
	`, domain.TaskGated, formatTime(now), in.TaskID, domain.TaskRunning); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM workspace_leases WHERE holder_type = 'attempt' AND holder_id = ?
	`, fmt.Sprint(in.AttemptID)); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", in.TaskID, "TASK_GATED",
		map[string]any{"attempt_id": in.AttemptID}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetTaskStatus(ctx context.Context, taskID string, status domain.TaskStatus, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var current string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM tasks WHERE id = ?", taskID).Scan(&current); err != nil {
		return err
	}
	if !domain.CanTransition(domain.TaskStatus(current), status) {
		return fmt.Errorf("%w: task %s cannot transition from %s to %s", storepkg.ErrInvalidState, taskID, current, status)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, resume_prompt = '', next_run_at = NULL, updated_at = ? WHERE id = ?
	`, status, formatTime(now), taskID); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", taskID, "TASK_STATUS_CHANGED",
		map[string]any{"status": status}, now); err != nil {
		return err
	}
	return tx.Commit()
}

var _ = sql.ErrNoRows
