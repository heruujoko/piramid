package domain

type TaskStatus string

const (
	TaskPending   TaskStatus = "PENDING"
	TaskRunning   TaskStatus = "RUNNING"
	TaskVerifying TaskStatus = "VERIFYING"
	TaskRetryWait TaskStatus = "RETRY_WAIT"
	TaskCompleted TaskStatus = "COMPLETED"
	TaskFailed    TaskStatus = "FAILED"
	TaskBlocked   TaskStatus = "BLOCKED"
	TaskCancelled TaskStatus = "CANCELLED"
)

var allowedTaskTransitions = map[TaskStatus]map[TaskStatus]struct{}{
	TaskPending: {
		TaskRunning:   {},
		TaskBlocked:   {},
		TaskCancelled: {},
	},
	TaskRunning: {
		TaskVerifying: {},
		TaskFailed:    {},
		TaskCancelled: {},
	},
	TaskVerifying: {
		TaskCompleted: {},
		TaskRetryWait: {},
		TaskFailed:    {},
		TaskCancelled: {},
	},
	TaskRetryWait: {
		TaskPending:   {},
		TaskBlocked:   {},
		TaskCancelled: {},
	},
}

func CanTransition(from, to TaskStatus) bool {
	_, ok := allowedTaskTransitions[from][to]
	return ok
}
