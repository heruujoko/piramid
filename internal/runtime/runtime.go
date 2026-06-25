package runtime

import (
	"context"
	"time"
)

type Invocation struct {
	Command     string
	Args        []string
	WorkingDir  string
	Environment []string
	Timeout     time.Duration
	StdoutPath  string
	StderrPath  string
}

type Result struct {
	ProcessID   int       `json:"process_id"`
	ExitCode    int       `json:"exit_code"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	TimedOut    bool      `json:"timed_out"`
	Interrupted bool      `json:"interrupted"`
}

type Adapter interface {
	Run(context.Context, Invocation) (Result, error)
}
