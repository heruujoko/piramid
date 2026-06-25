package home

import (
	"os"
	"path/filepath"
)

type Paths struct {
	Root      string
	Config    string
	Database  string
	Prompts   string
	Goals     string
	Tasks     string
	Attempts  string
	Artifacts string
	Runtime   string
}

func NewPaths(root string) Paths {
	return Paths{
		Root:      root,
		Config:    filepath.Join(root, "config.yaml"),
		Database:  filepath.Join(root, "state.db"),
		Prompts:   filepath.Join(root, "prompts"),
		Goals:     filepath.Join(root, "goals"),
		Tasks:     filepath.Join(root, "tasks"),
		Attempts:  filepath.Join(root, "attempts"),
		Artifacts: filepath.Join(root, "artifacts"),
		Runtime:   filepath.Join(root, "runtime"),
	}
}

func Resolve() (Paths, error) {
	root := os.Getenv("PIRAMID_HOME")
	if root == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		root = filepath.Join(userHome, ".piramid")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, err
	}
	return NewPaths(filepath.Clean(absolute)), nil
}
