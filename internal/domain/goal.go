package domain

import "time"

type GoalStatus string

const (
	GoalDraft     GoalStatus = "DRAFT"
	GoalConfirmed GoalStatus = "CONFIRMED"
	GoalRejected  GoalStatus = "REJECTED"
)

type Goal struct {
	ID          string     `yaml:"id" json:"id"`
	FireID      string     `yaml:"fire_id,omitempty" json:"fire_id,omitempty"`
	Text        string     `yaml:"text" json:"text"`
	ProjectPath string     `yaml:"project_path" json:"project_path"`
	Status      GoalStatus `yaml:"status" json:"status"`
	CreatedAt   time.Time  `yaml:"created_at" json:"created_at"`
}

type Plan struct {
	Version int    `yaml:"version" json:"version"`
	GoalID  string `yaml:"goal_id" json:"goal_id"`
	Tasks   []Task `yaml:"tasks" json:"tasks"`
}
