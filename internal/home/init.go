package home

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/heruujoko/piramid/internal/config"
)

func Init(paths Paths) error {
	for _, directory := range []string{
		paths.Root,
		paths.Prompts,
		paths.Goals,
		paths.Tasks,
		paths.Attempts,
		paths.Artifacts,
		paths.Runtime,
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(directory, 0o700); err != nil {
			return err
		}
	}

	for _, name := range []string{"orchestrator.md", "planner.md", "executor.md", "verifier.md"} {
		if err := createExclusive(filepath.Join(paths.Prompts, name), nil); err != nil {
			return err
		}
	}
	if err := createExclusive(paths.Database, nil); err != nil {
		return err
	}

	content, err := config.Marshal(config.Default())
	if err != nil {
		return err
	}
	return createExclusive(paths.Config, content)
}

func createExclusive(path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
