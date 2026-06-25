package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

func (s *Store) GetTask(ctx context.Context, id string) (domain.TaskView, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT task_json, status, attempt_count,
		       COALESCE((
		           SELECT v.retry_prompt
		           FROM verifications v
		           JOIN attempts a ON a.id = v.attempt_id
		           WHERE a.task_id = tasks.id
		           ORDER BY a.attempt_number DESC
		           LIMIT 1
		       ), ''),
		       COALESCE(next_run_at, ''), created_at, updated_at
		FROM tasks WHERE id = ?
	`, id)
	var (
		taskJSON, status, retryPrompt, nextRunAt, createdAt, updatedAt string
		attemptCount                                                   int
	)
	if err := row.Scan(
		&taskJSON, &status, &attemptCount, &retryPrompt,
		&nextRunAt, &createdAt, &updatedAt,
	); err != nil {
		return domain.TaskView{}, err
	}
	var task domain.Task
	if err := json.Unmarshal([]byte(taskJSON), &task); err != nil {
		return domain.TaskView{}, err
	}
	if err := domain.NormalizeTask(&task); err != nil {
		return domain.TaskView{}, err
	}
	view := domain.TaskView{
		TaskRecord: domain.TaskRecord{
			Task:         task,
			Status:       domain.TaskStatus(status),
			AttemptCount: attemptCount,
			RetryPrompt:  retryPrompt,
			NextRunAt:    parseTime(nextRunAt),
			CreatedAt:    parseTime(createdAt),
			UpdatedAt:    parseTime(updatedAt),
		},
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT depends_on_task_id FROM task_dependencies
		WHERE task_id = ? ORDER BY depends_on_task_id
	`, id)
	if err != nil {
		return domain.TaskView{}, err
	}
	for rows.Next() {
		var dependency string
		if err := rows.Scan(&dependency); err != nil {
			rows.Close()
			return domain.TaskView{}, err
		}
		view.Dependencies = append(view.Dependencies, dependency)
	}
	if err := rows.Close(); err != nil {
		return domain.TaskView{}, err
	}

	attemptRows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.task_id, a.attempt_number, a.worker_id, a.status,
		       a.started_at, COALESCE(a.finished_at, ''), a.exit_code,
		       COALESCE(v.status, ''), COALESCE(v.reason_summary, ''),
		       COALESCE(v.retry_prompt, ''), COALESCE(v.report_path, '')
		FROM attempts a
		LEFT JOIN verifications v ON v.attempt_id = a.id
		WHERE a.task_id = ? ORDER BY a.attempt_number
	`, id)
	if err != nil {
		return domain.TaskView{}, err
	}
	defer attemptRows.Close()
	for attemptRows.Next() {
		var attempt domain.Attempt
		var startedAt, finishedAt, verificationStatus, reason, retryPrompt string
		var exitCode sql.NullInt64
		if err := attemptRows.Scan(
			&attempt.ID, &attempt.TaskID, &attempt.AttemptNumber,
			&attempt.WorkerID, &attempt.Status, &startedAt,
			&finishedAt, &exitCode, &verificationStatus, &reason,
			&retryPrompt, &attempt.ReportPath,
		); err != nil {
			return domain.TaskView{}, err
		}
		attempt.StartedAt = parseTime(startedAt)
		attempt.FinishedAt = parseTime(finishedAt)
		if exitCode.Valid {
			value := int(exitCode.Int64)
			attempt.ExitCode = &value
		}
		if verificationStatus != "" {
			attempt.Verification = &domain.Verification{
				Status:      domain.VerificationStatus(verificationStatus),
				Reasons:     []string{reason},
				RetryPrompt: retryPrompt,
			}
		}
		view.Attempts = append(view.Attempts, attempt)
	}
	return view, attemptRows.Err()
}

func (s *Store) ListTasks(ctx context.Context, filter storepkg.TaskFilter) ([]domain.TaskView, error) {
	query := "SELECT id FROM tasks"
	args := make([]any, 0, len(filter.Statuses)+1)
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, len(filter.Statuses))
		for i, status := range filter.Statuses {
			placeholders[i] = "?"
			args = append(args, status)
		}
		query += " WHERE status IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY created_at, id"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	views := make([]domain.TaskView, 0, len(ids))
	for _, id := range ids {
		view, err := s.GetTask(ctx, id)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Store) ListRunnable(ctx context.Context, now time.Time, limit int) ([]domain.TaskRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT task_json, status, attempt_count,
		       COALESCE((
		           SELECT v.retry_prompt
		           FROM verifications v
		           JOIN attempts a ON a.id = v.attempt_id
		           WHERE a.task_id = t.id
		           ORDER BY a.attempt_number DESC
		           LIMIT 1
		       ), ''),
		       COALESCE(next_run_at, ''), created_at, updated_at
		FROM tasks t
		WHERE t.status IN (?, ?)
		  AND (t.next_run_at IS NULL OR t.next_run_at <= ?)
		  AND NOT EXISTS (
		      SELECT 1
		      FROM task_dependencies d
		      JOIN tasks dependency ON dependency.id = d.depends_on_task_id
		      WHERE d.task_id = t.id AND dependency.status <> ?
		  )
		  AND NOT EXISTS (
		      SELECT 1 FROM workspace_leases l WHERE l.project_path = t.project_path
		  )
		ORDER BY COALESCE(t.next_run_at, t.created_at), t.created_at, t.id
		LIMIT ?
	`, domain.TaskPending, domain.TaskRetryWait, formatTime(now), domain.TaskCompleted, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []domain.TaskRecord
	for rows.Next() {
		var record domain.TaskRecord
		var taskJSON, status, retryPrompt, nextRunAt, createdAt, updatedAt string
		if err := rows.Scan(
			&taskJSON, &status, &record.AttemptCount, &retryPrompt,
			&nextRunAt, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(taskJSON), &record.Task); err != nil {
			return nil, err
		}
		if err := domain.NormalizeTask(&record.Task); err != nil {
			return nil, err
		}
		record.Status = domain.TaskStatus(status)
		record.RetryPrompt = retryPrompt
		record.NextRunAt = parseTime(nextRunAt)
		record.CreatedAt = parseTime(createdAt)
		record.UpdatedAt = parseTime(updatedAt)
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *Store) ReconcileBlocked(ctx context.Context, now time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		WITH RECURSIVE blocked(id) AS (
			SELECT d.task_id
			FROM task_dependencies d
			JOIN tasks dependency ON dependency.id = d.depends_on_task_id
			WHERE dependency.status IN (?, ?, ?)
			UNION
			SELECT d.task_id
			FROM task_dependencies d
			JOIN blocked b ON b.id = d.depends_on_task_id
		)
		SELECT DISTINCT t.id
		FROM blocked b
		JOIN tasks t ON t.id = b.id
		WHERE t.status IN (?, ?)
		ORDER BY t.id
	`, domain.TaskFailed, domain.TaskCancelled, domain.TaskBlocked,
		domain.TaskPending, domain.TaskRetryWait)
	if err != nil {
		return 0, err
	}
	var taskIDs []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			rows.Close()
			return 0, err
		}
		taskIDs = append(taskIDs, taskID)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, taskID := range taskIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?
		`, domain.TaskBlocked, formatTime(now), taskID); err != nil {
			return 0, err
		}
		if err := appendEvent(ctx, tx, "task", taskID, "TASK_BLOCKED",
			map[string]any{"reason": "terminal dependency"}, now); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(taskIDs), nil
}

func (s *Store) AcquireReadLease(ctx context.Context, projectPath, holderID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var writeCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM workspace_leases WHERE project_path = ? AND mode = 'WRITE'
	`, projectPath).Scan(&writeCount); err != nil {
		return err
	}
	if writeCount > 0 {
		return fmt.Errorf("%w: %s", storepkg.ErrProjectLeased, projectPath)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO workspace_leases(project_path, mode, holder_type, holder_id, acquired_at)
		VALUES (?, 'READ', 'planner', ?, ?)
	`, projectPath, holderID, formatTime(time.Now())); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReleaseReadLease(ctx context.Context, projectPath, holderID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM workspace_leases
		WHERE project_path = ? AND mode = 'READ' AND holder_type = 'planner' AND holder_id = ?
	`, projectPath, holderID)
	return err
}

var _ = sql.ErrNoRows
