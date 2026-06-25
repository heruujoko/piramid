package domain

import "time"

type Input struct {
	Type  string `yaml:"type" json:"type"`
	Value string `yaml:"value" json:"value"`
}

type Output struct {
	Type string `yaml:"type" json:"type"`
	Path string `yaml:"path" json:"path"`
}

type Task struct {
	ID           string        `yaml:"id" json:"id"`
	ParentTaskID string        `yaml:"parent_task_id,omitempty" json:"parent_task_id,omitempty"`
	Title        string        `yaml:"title" json:"title"`
	Goal         string        `yaml:"goal" json:"goal"`
	ProjectPath  string        `yaml:"project_path" json:"project_path"`
	Inputs       []Input       `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Outputs      []Output      `yaml:"expected_outputs,omitempty" json:"expected_outputs,omitempty"`
	DOD          []string      `yaml:"dod" json:"dod"`
	Model        string        `yaml:"model,omitempty" json:"model,omitempty"`
	MaxAttempts  int           `yaml:"max_attempts" json:"max_attempts"`
	Timeout      time.Duration `yaml:"-" json:"-"`
	TimeoutText  string        `yaml:"timeout" json:"timeout"`
	DependsOn    []string      `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
}

type TaskRecord struct {
	Task
	Status       TaskStatus `yaml:"status" json:"status"`
	AttemptCount int        `yaml:"attempt_count" json:"attempt_count"`
	RetryPrompt  string     `yaml:"retry_prompt,omitempty" json:"retry_prompt,omitempty"`
	NextRunAt    time.Time  `yaml:"next_run_at" json:"next_run_at"`
	CreatedAt    time.Time  `yaml:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `yaml:"updated_at" json:"updated_at"`
}

type TaskView struct {
	TaskRecord
	Dependencies []string  `yaml:"dependencies" json:"dependencies"`
	Attempts     []Attempt `yaml:"attempts" json:"attempts"`
}
