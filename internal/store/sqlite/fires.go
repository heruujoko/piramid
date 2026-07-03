package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
)

const defaultFireListLimit = 100

// fireTerminalStatuses are statuses at which a fire is considered finished.
var fireTerminalStatuses = map[domain.FireStatus]bool{
	domain.FireDone:     true,
	domain.FireRejected: true,
	domain.FireDeferred: true,
	domain.FireFailed:   true,
}

func (s *Store) CreateFire(ctx context.Context, fire domain.Fire) (domain.Fire, error) {
	now := fire.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	fire.CreatedAt = now
	fire.UpdatedAt = now
	if fire.Status == "" {
		fire.Status = domain.FireScheduled
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Fire{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fires(
			id, loop_id, goal_id, status, scheduled_at, started_at, finished_at,
			last_error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, fire.ID, fire.LoopID, nullableString(fire.GoalID), fire.Status,
		formatTime(fire.ScheduledAt), nullableTime(fire.StartedAt), nullableTime(fire.FinishedAt),
		fire.LastError, formatTime(fire.CreatedAt), formatTime(fire.UpdatedAt)); err != nil {
		return domain.Fire{}, err
	}
	if err := appendEvent(ctx, tx, "fire", fire.ID, "FIRE_CREATED",
		map[string]any{"loop_id": fire.LoopID, "status": fire.Status}, now); err != nil {
		return domain.Fire{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Fire{}, err
	}
	return fire, nil
}

func (s *Store) UpdateFireStatus(
	ctx context.Context,
	id string,
	status domain.FireStatus,
	now time.Time,
) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	setStarted := status == domain.FireRunning
	setFinished := fireTerminalStatuses[status]

	result, err := tx.ExecContext(ctx, `
		UPDATE fires
		SET status = ?,
		    started_at = CASE WHEN ? AND (started_at IS NULL OR started_at = '') THEN ? ELSE started_at END,
		    finished_at = CASE WHEN ? THEN ? ELSE finished_at END,
		    updated_at = ?
		WHERE id = ?
	`, status, setStarted, formatTime(now), setFinished, formatTime(now), formatTime(now), id)
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
	if err := appendEvent(ctx, tx, "fire", id, "FIRE_STATUS_CHANGED",
		map[string]any{"status": status}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListFires(ctx context.Context, loopID string, limit int) ([]domain.Fire, error) {
	if limit <= 0 {
		limit = defaultFireListLimit
	}
	query := `
		SELECT id, loop_id, goal_id, status, scheduled_at, started_at, finished_at,
		       last_error, created_at, updated_at
		FROM fires
	`
	args := []any{}
	if loopID != "" {
		query += " WHERE loop_id = ?"
		args = append(args, loopID)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fires := []domain.Fire{}
	for rows.Next() {
		fire, err := scanFire(rows)
		if err != nil {
			return nil, err
		}
		fires = append(fires, fire)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortFiresByLatestScheduled(fires)
	if len(fires) > limit {
		fires = fires[:limit]
	}
	return fires, nil
}

func (s *Store) GetLatestFireByLoop(ctx context.Context, loopID string) (domain.Fire, error) {
	fires, err := s.ListFires(ctx, loopID, 1)
	if err != nil {
		return domain.Fire{}, err
	}
	if len(fires) == 0 {
		return domain.Fire{}, sql.ErrNoRows
	}
	return fires[0], nil
}

func sortFiresByLatestScheduled(fires []domain.Fire) {
	sort.Slice(fires, func(i, j int) bool {
		if fires[i].ScheduledAt.Equal(fires[j].ScheduledAt) {
			return fires[i].ID > fires[j].ID
		}
		return fires[i].ScheduledAt.After(fires[j].ScheduledAt)
	})
}

type scanner interface {
	Scan(dest ...any) error
}

func scanFire(row scanner) (domain.Fire, error) {
	var (
		fire       domain.Fire
		goalID     sql.NullString
		scheduled  string
		startedAt  sql.NullString
		finishedAt sql.NullString
		createdAt  string
		updatedAt  string
	)
	if err := row.Scan(
		&fire.ID, &fire.LoopID, &goalID, &fire.Status, &scheduled,
		&startedAt, &finishedAt, &fire.LastError, &createdAt, &updatedAt,
	); err != nil {
		return domain.Fire{}, err
	}
	fire.GoalID = goalID.String
	fire.ScheduledAt = parseTime(scheduled)
	fire.StartedAt = parseTime(startedAt.String)
	fire.FinishedAt = parseTime(finishedAt.String)
	fire.CreatedAt = parseTime(createdAt)
	fire.UpdatedAt = parseTime(updatedAt)
	return fire, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}
