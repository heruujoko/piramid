package definitions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
)

var ErrInvalidRoot = errors.New("definitions root is invalid")

type Snapshot struct {
	Patterns []domain.Pattern `json:"patterns"`
	Loops    []domain.Loop    `json:"loops"`
	LoadedAt time.Time        `json:"loaded_at"`
}

func LoadRoot(root string) (Snapshot, error) {
	if root == "" {
		return Snapshot{}, fmt.Errorf("%w: path is required", ErrInvalidRoot)
	}
	info, err := os.Stat(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%w: %s: %w", ErrInvalidRoot, root, err)
	}
	if !info.IsDir() {
		return Snapshot{}, fmt.Errorf("%w: %s is not a directory", ErrInvalidRoot, root)
	}
	patternsDir := filepath.Join(root, "patterns")
	loopsDir := filepath.Join(root, "loops")
	if err := requireDir(patternsDir); err != nil {
		return Snapshot{}, err
	}
	if err := requireDir(loopsDir); err != nil {
		return Snapshot{}, err
	}

	patterns, err := loadPatterns(patternsDir)
	if err != nil {
		return Snapshot{}, err
	}
	loops, err := loadLoops(loopsDir, patterns)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Patterns: patterns, Loops: loops, LoadedAt: time.Now().UTC()}, nil
}

func requireDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrInvalidRoot, path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrInvalidRoot, path)
	}
	return nil
}

func yamlFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			return nil, fmt.Errorf("%s: definition files must use .yaml or .yml", filepath.Join(dir, entry.Name()))
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}
