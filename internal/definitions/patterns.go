package definitions

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/heruujoko/piramid/internal/domain"
	"gopkg.in/yaml.v3"
)

var patternIDRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func loadPatterns(dir string) ([]domain.Pattern, error) {
	files, err := yamlFiles(dir)
	if err != nil {
		return nil, err
	}
	patterns := make([]domain.Pattern, 0, len(files))
	seen := make(map[string]string, len(files))
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var pattern domain.Pattern
		if err := yaml.Unmarshal(content, &pattern); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := validatePattern(pattern); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if first, exists := seen[pattern.ID]; exists {
			return nil, fmt.Errorf("pattern %s id: duplicate in %s and %s", pattern.ID, first, path)
		}
		seen[pattern.ID] = path
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

func validatePattern(pattern domain.Pattern) error {
	if !patternIDRe.MatchString(strings.TrimSpace(pattern.ID)) {
		return fmt.Errorf("pattern id: must match ^[a-z][a-z0-9-]*$")
	}
	if len(strings.TrimSpace(pattern.Name)) < 3 {
		return fmt.Errorf("pattern %s name: must be at least 3 characters", pattern.ID)
	}
	if strings.TrimSpace(pattern.File) == "" || !strings.HasSuffix(pattern.File, ".md") {
		return fmt.Errorf("pattern %s file: must be a .md file", pattern.ID)
	}
	if strings.TrimSpace(pattern.Goal) == "" {
		return fmt.Errorf("pattern %s goal: is required", pattern.ID)
	}
	if strings.TrimSpace(pattern.Cadence) == "" {
		return fmt.Errorf("pattern %s cadence: is required", pattern.ID)
	}
	switch pattern.Risk {
	case domain.RiskLow, domain.RiskMedium, domain.RiskHigh:
	default:
		return fmt.Errorf("pattern %s risk: must be low, medium, or high", pattern.ID)
	}
	if err := requireNonEmptyStrings("tools", pattern.ID, pattern.Tools); err != nil {
		return err
	}
	if err := requireNonEmptyStrings("skills", pattern.ID, pattern.Skills); err != nil {
		return err
	}
	if strings.TrimSpace(pattern.State) == "" || !strings.HasSuffix(pattern.State, ".md") {
		return fmt.Errorf("pattern %s state: must be a .md file", pattern.ID)
	}
	if err := requireNonEmptyStrings("phases", pattern.ID, pattern.Phases); err != nil {
		return err
	}
	if len(pattern.Phases) < 2 {
		return fmt.Errorf("pattern %s phases: at least two phases are required", pattern.ID)
	}
	if err := requireNonEmptyStrings("human_gates", pattern.ID, pattern.HumanGates); err != nil {
		return err
	}
	return nil
}

func requireNonEmptyStrings(field, id string, values []string) error {
	if len(values) == 0 {
		return fmt.Errorf("pattern %s %s: at least one value is required", id, field)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("pattern %s %s: values cannot be empty", id, field)
		}
	}
	return nil
}
