package domain

import "time"

type AttemptStatus string

const (
	AttemptRunning     AttemptStatus = "RUNNING"
	AttemptVerifying   AttemptStatus = "VERIFYING"
	AttemptCompleted   AttemptStatus = "COMPLETED"
	AttemptFailed      AttemptStatus = "FAILED"
	AttemptInterrupted AttemptStatus = "INTERRUPTED"
	AttemptGated       AttemptStatus = "GATED"
)

type Attempt struct {
	ID            int64         `yaml:"id" json:"id"`
	TaskID        string        `yaml:"task_id" json:"task_id"`
	AttemptNumber int           `yaml:"attempt_number" json:"attempt_number"`
	WorkerID      string        `yaml:"worker_id" json:"worker_id"`
	Status        AttemptStatus `yaml:"status" json:"status"`
	StartedAt     time.Time     `yaml:"started_at" json:"started_at"`
	FinishedAt    time.Time     `yaml:"finished_at,omitempty" json:"finished_at,omitempty"`
	ExitCode      *int          `yaml:"exit_code,omitempty" json:"exit_code,omitempty"`
	Verification  *Verification `yaml:"verification,omitempty" json:"verification,omitempty"`
	ReportPath    string        `yaml:"report_path,omitempty" json:"report_path,omitempty"`
}

type VerificationStatus string

const (
	VerificationPass VerificationStatus = "PASS"
	VerificationFail VerificationStatus = "FAIL"
)

type Verification struct {
	Status      VerificationStatus `yaml:"status" json:"status"`
	Reasons     []string           `yaml:"reasons" json:"reasons"`
	RetryPrompt string             `yaml:"retry_prompt,omitempty" json:"retry_prompt,omitempty"`
}
