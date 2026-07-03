package cron

import (
	"testing"
	"time"
)

func TestNextAfter(t *testing.T) {
	tests := []struct {
		name  string
		expr  string
		after string
		want  string
	}{
		{"quarter hour", "*/15 * * * *", "2026-07-02T09:07:12Z", "2026-07-02T09:15:00Z"},
		{"six hour boundary", "0 */6 * * *", "2026-07-02T07:59:00Z", "2026-07-02T12:00:00Z"},
		{"daily", "30 9 * * *", "2026-07-02T09:30:00Z", "2026-07-03T09:30:00Z"},
		{"range step", "0 9-17/2 * * 1-5", "2026-07-02T10:00:00Z", "2026-07-02T11:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			after := mustParse(t, tt.after)
			got, err := NextAfter(tt.expr, after)
			if err != nil {
				t.Fatalf("NextAfter() error = %v", err)
			}
			if got != mustParse(t, tt.want) {
				t.Fatalf("NextAfter() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseRejectsInvalidExpressions(t *testing.T) {
	for _, expr := range []string{
		"* * * *",
		"60 * * * *",
		"* 24 * * *",
		"* * 0 * *",
		"* * * 13 *",
		"* * * * 8",
		"*/0 * * * *",
		"10-5 * * * *",
		"bad * * * *",
	} {
		t.Run(expr, func(t *testing.T) {
			if err := Validate(expr); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
	}
}

func mustParse(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
