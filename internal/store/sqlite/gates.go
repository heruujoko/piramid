package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

// gateResolvedStatus maps a decision to the terminal gate status it produces.
func gateResolvedStatus(decision domain.GateDecision) domain.GateStatus {
	switch decision {
	case domain.GateDecisionDefer:
		return domain.GateDeferred
	case domain.GateDecisionReject:
		return domain.GateRejected
	default:
		return domain.GateResolved
	}
}

func (s *Store) CreateGate(ctx context.Context, gate domain.Gate) (domain.Gate, error) {
	now := gate.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	gate.CreatedAt = now
	gate.UpdatedAt = now
	if gate.OpenedAt.IsZero() {
		gate.OpenedAt = now
	}
	if gate.Status == "" {
		gate.Status = domain.GateOpen
	}

	body := gate.Context.Body
	front := gate.Context
	front.Body = ""
	contextJSON, err := json.Marshal(front)
	if err != nil {
		return domain.Gate{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Gate{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO gates(
			id, fire_id, goal_id, task_id, attempt_id, status, context_path,
			context_json, context_body, decision, decision_note, opened_at,
			resolved_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?)
	`, gate.ID, gate.FireID, nullableString(gate.GoalID), nullableString(gate.TaskID),
		nullableString(gate.AttemptID), gate.Status, gate.ContextPath,
		string(contextJSON), body, gate.DecisionNote, formatTime(gate.OpenedAt),
		nullableTime(gate.ResolvedAt), formatTime(gate.CreatedAt), formatTime(gate.UpdatedAt)); err != nil {
		return domain.Gate{}, err
	}
	if err := appendEvent(ctx, tx, "gate", gate.ID, "GATE_OPENED",
		map[string]any{"fire_id": gate.FireID}, now); err != nil {
		return domain.Gate{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Gate{}, err
	}
	return gate, nil
}

func (s *Store) GetGate(ctx context.Context, id string) (domain.Gate, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, fire_id, goal_id, task_id, attempt_id, status, context_path,
		       context_json, context_body, decision, decision_note, opened_at,
		       resolved_at, created_at, updated_at
		FROM gates WHERE id = ?
	`, id)
	return scanGate(row)
}

func (s *Store) ListOpenGates(ctx context.Context) ([]domain.Gate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, fire_id, goal_id, task_id, attempt_id, status, context_path,
		       context_json, context_body, decision, decision_note, opened_at,
		       resolved_at, created_at, updated_at
		FROM gates WHERE status = ? ORDER BY opened_at ASC, id ASC
	`, domain.GateOpen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	gates := []domain.Gate{}
	for rows.Next() {
		gate, err := scanGate(rows)
		if err != nil {
			return nil, err
		}
		gates = append(gates, gate)
	}
	return gates, rows.Err()
}

func (s *Store) ResolveGate(ctx context.Context, in storepkg.ResolveGateInput) error {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentStatus string
	err = tx.QueryRowContext(ctx, "SELECT status FROM gates WHERE id = ?", in.ID).Scan(&currentStatus)
	if err != nil {
		return err
	}
	if currentStatus != string(domain.GateOpen) {
		return storepkg.ErrInvalidState
	}

	resolved := gateResolvedStatus(in.Decision)
	if _, err := tx.ExecContext(ctx, `
		UPDATE gates
		SET status = ?, decision = ?, decision_note = ?, resolved_at = ?, updated_at = ?
		WHERE id = ?
	`, resolved, in.Decision, in.Note, formatTime(now), formatTime(now), in.ID); err != nil {
		return err
	}
	if err := appendEvent(ctx, tx, "gate", in.ID, "GATE_RESOLVED",
		map[string]any{"decision": in.Decision, "note": in.Note, "status": resolved}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func scanGate(row scanner) (domain.Gate, error) {
	var (
		gate        domain.Gate
		goalID      sql.NullString
		taskID      sql.NullString
		attemptID   sql.NullString
		contextJSON string
		contextBody string
		decision    sql.NullString
		openedAt    string
		resolvedAt  sql.NullString
		createdAt   string
		updatedAt   string
	)
	if err := row.Scan(
		&gate.ID, &gate.FireID, &goalID, &taskID, &attemptID, &gate.Status,
		&gate.ContextPath, &contextJSON, &contextBody, &decision, &gate.DecisionNote,
		&openedAt, &resolvedAt, &createdAt, &updatedAt,
	); err != nil {
		return domain.Gate{}, err
	}
	gate.GoalID = goalID.String
	gate.TaskID = taskID.String
	gate.AttemptID = attemptID.String
	gate.Decision = domain.GateDecision(decision.String)
	gate.OpenedAt = parseTime(openedAt)
	gate.ResolvedAt = parseTime(resolvedAt.String)
	gate.CreatedAt = parseTime(createdAt)
	gate.UpdatedAt = parseTime(updatedAt)

	if contextJSON != "" {
		var gc domain.GateContext
		if err := json.Unmarshal([]byte(contextJSON), &gc); err != nil {
			return domain.Gate{}, err
		}
		gate.Context = gc
	}
	gate.Context.Body = contextBody
	return gate, nil
}
