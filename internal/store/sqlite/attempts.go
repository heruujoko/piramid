package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

func (s *Store) StartAttempt(
	ctx context.Context,
	input storepkg.StartAttemptInput,
) (domain.Attempt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Attempt{}, err
	}
	defer tx.Rollback()

	var status, projectPath, model string
	var attemptCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT status, project_path, model, attempt_count FROM tasks WHERE id = ?
	`, input.TaskID).Scan(&status, &projectPath, &model, &attemptCount); err != nil {
		return domain.Attempt{}, err
	}
	if !domain.CanTransition(domain.TaskStatus(status), domain.TaskRunning) {
		return domain.Attempt{}, fmt.Errorf("task %s cannot transition from %s to RUNNING", input.TaskID, status)
	}

	var leaseCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM workspace_leases WHERE project_path = ?
	`, projectPath).Scan(&leaseCount); err != nil {
		return domain.Attempt{}, err
	}
	if leaseCount > 0 {
		return domain.Attempt{}, fmt.Errorf("project %s is leased", projectPath)
	}

	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	attemptNumber := attemptCount + 1
	result, err := tx.ExecContext(ctx, `
		INSERT INTO attempts(
			task_id, attempt_number, worker_id, status, runtime, model,
			prompt_path, prompt_hash, stdout_path, stderr_path, started_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.TaskID, attemptNumber, input.WorkerID, domain.AttemptRunning,
		input.Runtime, model, input.PromptPath, input.PromptHash,
		input.StdoutPath, input.StderrPath, formatTime(startedAt))
	if err != nil {
		return domain.Attempt{}, err
	}
	attemptID, err := result.LastInsertId()
	if err != nil {
		return domain.Attempt{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspace_leases(
			project_path, mode, holder_type, holder_id, acquired_at
		) VALUES (?, 'WRITE', 'attempt', ?, ?)
	`, projectPath, fmt.Sprint(attemptID), formatTime(startedAt)); err != nil {
		return domain.Attempt{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, attempt_count = ?, next_run_at = NULL, updated_at = ?
		WHERE id = ?
	`, domain.TaskRunning, attemptNumber, formatTime(startedAt), input.TaskID); err != nil {
		return domain.Attempt{}, err
	}
	if err := appendEvent(ctx, tx, "task", input.TaskID, "TASK_STARTED",
		map[string]any{"attempt_id": attemptID, "attempt_number": attemptNumber}, startedAt); err != nil {
		return domain.Attempt{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Attempt{}, err
	}
	return domain.Attempt{
		ID:            attemptID,
		TaskID:        input.TaskID,
		AttemptNumber: attemptNumber,
		WorkerID:      input.WorkerID,
		Status:        domain.AttemptRunning,
		StartedAt:     startedAt,
	}, nil
}

func (s *Store) MoveToVerification(ctx context.Context, input storepkg.FinishExecutionInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE attempts SET status = ?, process_id = ?, exit_code = ?, finished_at = ?
		WHERE id = ? AND task_id = ?
	`, domain.AttemptVerifying, input.ProcessID, input.ExitCode,
		formatTime(input.FinishedAt), input.AttemptID, input.TaskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, updated_at = ? WHERE id = ? AND status = ?
	`, domain.TaskVerifying, formatTime(input.FinishedAt), input.TaskID, domain.TaskRunning); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", input.TaskID, "TASK_VERIFYING",
		map[string]any{"attempt_id": input.AttemptID}, input.FinishedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinishVerification(ctx context.Context, input storepkg.FinishVerificationInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	reason := ""
	if len(input.Verification.Reasons) > 0 {
		reason = input.Verification.Reasons[0]
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO verifications(attempt_id, status, report_path, reason_summary, retry_prompt)
		VALUES (?, ?, ?, ?, ?)
	`, input.AttemptID, input.Verification.Status, input.ReportPath,
		reason, input.Verification.RetryPrompt); err != nil {
		return err
	}
	taskStatus := domain.TaskCompleted
	attemptStatus := domain.AttemptCompleted
	var nextRun any
	eventType := "TASK_COMPLETED"
	if input.Verification.Status == domain.VerificationFail {
		taskStatus = domain.TaskRetryWait
		attemptStatus = domain.AttemptFailed
		nextRun = formatTime(input.NextRunAt)
		eventType = "TASK_RETRY_SCHEDULED"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE attempts SET status = ?, finished_at = ? WHERE id = ? AND task_id = ?
	`, attemptStatus, formatTime(input.FinishedAt), input.AttemptID, input.TaskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, next_run_at = ?, updated_at = ?
		WHERE id = ? AND status = ?
	`, taskStatus, nextRun, formatTime(input.FinishedAt), input.TaskID, domain.TaskVerifying); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM workspace_leases WHERE holder_type = 'attempt' AND holder_id = ?
	`, fmt.Sprint(input.AttemptID)); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", input.TaskID, eventType,
		map[string]any{"attempt_id": input.AttemptID}, input.FinishedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordOperationalFailure(
	ctx context.Context,
	input storepkg.OperationalFailureInput,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE attempts SET status = ?, failure_class = ?, finished_at = ? WHERE id = ?
	`, domain.AttemptFailed, input.FailureClass, formatTime(input.FinishedAt), input.AttemptID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = ?, next_run_at = ?, updated_at = ? WHERE id = ?
	`, domain.TaskRetryWait, formatTime(input.NextRunAt), formatTime(input.FinishedAt), input.TaskID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM workspace_leases WHERE holder_type = 'attempt' AND holder_id = ?
	`, fmt.Sprint(input.AttemptID)); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "task", input.TaskID, "OPERATIONAL_FAILURE",
		map[string]any{"attempt_id": input.AttemptID, "class": input.FailureClass}, input.FinishedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecoverActive(
	ctx context.Context,
	now time.Time,
) ([]storepkg.InterruptedAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, COALESCE(process_id, 0) FROM attempts
		WHERE status IN (?, ?)
	`, domain.AttemptRunning, domain.AttemptVerifying)
	if err != nil {
		return nil, err
	}
	var interrupted []storepkg.InterruptedAttempt
	for rows.Next() {
		var item storepkg.InterruptedAttempt
		if err := rows.Scan(&item.AttemptID, &item.TaskID, &item.ProcessID); err != nil {
			rows.Close()
			return nil, err
		}
		interrupted = append(interrupted, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, item := range interrupted {
		if _, err := tx.ExecContext(ctx, `
			UPDATE attempts SET status = ?, failure_class = 'interrupted', finished_at = ? WHERE id = ?
		`, domain.AttemptInterrupted, formatTime(now), item.AttemptID); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks SET status = ?, next_run_at = ?, updated_at = ? WHERE id = ?
		`, domain.TaskPending, formatTime(now), formatTime(now), item.TaskID); err != nil {
			return nil, err
		}
		if err := appendEvent(ctx, tx, "task", item.TaskID, "ATTEMPT_INTERRUPTED",
			map[string]any{"attempt_id": item.AttemptID}, now); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM workspace_leases WHERE mode = 'WRITE'"); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return interrupted, nil
}

var _ = sql.ErrNoRows
