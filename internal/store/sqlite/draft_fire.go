package sqlite

import (
	"context"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

func (s *Store) CreateDraftFire(
	ctx context.Context,
	goal domain.Goal,
	paths storepkg.PersistedPaths,
	fire domain.Fire,
) error {
	now := goal.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	fireNow := fire.CreatedAt
	if fireNow.IsZero() {
		fireNow = now
	}
	if fire.UpdatedAt.IsZero() {
		fire.UpdatedAt = fireNow
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
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fires(id, loop_id, goal_id, status, scheduled_at, started_at, finished_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, fire.ID, fire.LoopID, nullableString(fire.GoalID), fire.Status, formatTime(fire.ScheduledAt),
		nullableTime(fire.StartedAt), nullableTime(fire.FinishedAt), formatTime(fireNow), formatTime(fire.UpdatedAt)); err != nil {
		return err
	}
	payload := map[string]any{
		"loop_id":      fire.LoopID,
		"goal_id":      fire.GoalID,
		"status":       fire.Status,
		"scheduled_at": fire.ScheduledAt,
	}
	if err := appendEvent(ctx, tx, "fire", fire.ID, "FIRE_CREATED", payload, fireNow); err != nil {
		return err
	}
	return tx.Commit()
}

var _ interface {
	CreateDraftFire(context.Context, domain.Goal, storepkg.PersistedPaths, domain.Fire) error
} = (*Store)(nil)
