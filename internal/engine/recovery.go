package engine

import (
	"context"
	"fmt"
	"time"

	storepkg "github.com/heruujoko/piramid/internal/store"
)

type ProcessInspector interface {
	Exists(pid int) bool
}

type RecoveryStore interface {
	ListActiveAttempts(context.Context) ([]storepkg.InterruptedAttempt, error)
	RecoverActive(context.Context, time.Time) ([]storepkg.InterruptedAttempt, error)
}

func Recover(
	ctx context.Context,
	st RecoveryStore,
	inspector ProcessInspector,
	now time.Time,
) error {
	active, err := st.ListActiveAttempts(ctx)
	if err != nil {
		return fmt.Errorf("list active attempts: %w", err)
	}
	for _, attempt := range active {
		if attempt.ProcessID > 0 && inspector.Exists(attempt.ProcessID) {
			return fmt.Errorf(
				"attempt %d for task %s still has live process %d",
				attempt.AttemptID,
				attempt.TaskID,
				attempt.ProcessID,
			)
		}
	}
	if _, err := st.RecoverActive(ctx, now); err != nil {
		return fmt.Errorf("recover active attempts: %w", err)
	}
	return nil
}
