package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

func appendEvent(
	ctx context.Context,
	tx *sql.Tx,
	entityType string,
	entityID string,
	eventType string,
	payload any,
	now time.Time,
) error {
	content, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events(entity_type, entity_id, event_type, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, entityType, entityID, eventType, string(content), formatTime(now))
	return err
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
