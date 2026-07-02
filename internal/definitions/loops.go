package definitions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/heruujoko/piramid/internal/cron"
	"github.com/heruujoko/piramid/internal/domain"
	"gopkg.in/yaml.v3"
)

func loadLoops(dir string, patterns []domain.Pattern) ([]domain.Loop, error) {
	files, err := yamlFiles(dir)
	if err != nil {
		return nil, err
	}
	patternIDs := make(map[string]struct{}, len(patterns))
	for _, pattern := range patterns {
		patternIDs[pattern.ID] = struct{}{}
	}
	loops := make([]domain.Loop, 0, len(files))
	seen := make(map[string]string, len(files))
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var loop domain.Loop
		if err := yaml.Unmarshal(content, &loop); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := validateLoop(loop, patternIDs); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if first, exists := seen[loop.ID]; exists {
			return nil, fmt.Errorf("loop %s id: duplicate in %s and %s", loop.ID, first, path)
		}
		seen[loop.ID] = path
		loops = append(loops, loop)
	}
	return loops, nil
}

func validateLoop(loop domain.Loop, patternIDs map[string]struct{}) error {
	if strings.TrimSpace(loop.ID) == "" {
		return fmt.Errorf("loop id: is required")
	}
	if strings.TrimSpace(loop.PatternID) == "" {
		return fmt.Errorf("loop %s pattern: is required", loop.ID)
	}
	if _, ok := patternIDs[loop.PatternID]; !ok {
		return fmt.Errorf("loop %s pattern: unknown pattern %s", loop.ID, loop.PatternID)
	}
	if strings.TrimSpace(loop.Cron) == "" {
		return fmt.Errorf("loop %s cron: is required", loop.ID)
	}
	if err := cron.Validate(loop.Cron); err != nil {
		return fmt.Errorf("loop %s cron: %w", loop.ID, err)
	}
	switch loop.Autonomy {
	case domain.LoopAutonomyL1, domain.LoopAutonomyL2, domain.LoopAutonomyL3:
	default:
		return fmt.Errorf("loop %s autonomy: must be L1, L2, or L3", loop.ID)
	}
	switch loop.Trigger {
	case domain.LoopTriggerPiramid, domain.LoopTriggerPi:
	default:
		return fmt.Errorf("loop %s trigger: must be piramid or pi", loop.ID)
	}
	if strings.TrimSpace(loop.Goal) == "" {
		return fmt.Errorf("loop %s goal: is required", loop.ID)
	}
	if !filepath.IsAbs(loop.ProjectPath) || filepath.Clean(loop.ProjectPath) != loop.ProjectPath {
		return fmt.Errorf("loop %s project_path: must be an absolute clean path", loop.ID)
	}
	if len(loop.HumanGates) == 0 {
		return fmt.Errorf("loop %s human_gates: at least one value is required", loop.ID)
	}
	for _, gate := range loop.HumanGates {
		if strings.TrimSpace(gate) == "" {
			return fmt.Errorf("loop %s human_gates: values cannot be empty", loop.ID)
		}
	}
	if loop.Token.DailyCap <= 0 {
		return fmt.Errorf("loop %s token.daily_cap: must be positive", loop.ID)
	}
	return nil
}
