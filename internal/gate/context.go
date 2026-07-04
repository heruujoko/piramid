package gate

import (
	"fmt"
	"strings"

	"github.com/heruujoko/piramid/internal/domain"
	"gopkg.in/yaml.v3"
)

const (
	frontMatterOpen  = "---"
	frontMatterClose = "\n---"
)

var (
	ErrMissingFrontMatter = fmt.Errorf("gate context: missing front-matter")
	ErrInvalidDecision    = fmt.Errorf("gate context: invalid decision option")
)

// ParseContext parses a gate.context.md file content.
// The file must have YAML front-matter delimited by "---" lines,
// followed by a Markdown body.
func ParseContext(content string) (domain.GateContext, error) {
	front, body, ok := splitFrontMatter(content)
	if !ok {
		return domain.GateContext{}, ErrMissingFrontMatter
	}

	var gc domain.GateContext
	if err := yaml.Unmarshal([]byte(front), &gc); err != nil {
		return domain.GateContext{}, fmt.Errorf("gate context: %w", err)
	}

	gc.Body = strings.TrimRight(body, "\n\r\t ")

	if err := validate(gc); err != nil {
		return domain.GateContext{}, err
	}

	return gc, nil
}

// splitFrontMatter splits content into front-matter YAML and body.
// Returns (frontMatter, body, ok). The first "---" line starts the
// front-matter, the second "---" line ends it. Everything after is body.
// Line endings are normalized to LF before parsing.
func splitFrontMatter(content string) (string, string, bool) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, frontMatterOpen) {
		return "", "", false
	}
	rest := content[len(frontMatterOpen):]
	// Find the closing delimiter on its own line. Using "\n---\n" ensures
	// we don't match "---" embedded inside YAML content (e.g. in a string).
	closeIdx := strings.Index(rest, "\n---\n")
	if closeIdx < 0 {
		// Also accept "---" at end of content (no trailing newline).
		if strings.HasSuffix(rest, "\n---") {
			closeIdx = len(rest) - len(frontMatterClose)
		} else {
			return "", "", false
		}
	}
	front := strings.TrimSpace(rest[:closeIdx])
	body := strings.TrimLeft(rest[closeIdx+len(frontMatterClose):], "\n\r")
	return front, body, true
}

func validate(gc domain.GateContext) error {
	required := []struct {
		field string
		value string
	}{
		{"gate", gc.Gate},
		{"phase", gc.Phase},
		{"summary", gc.Summary},
	}
	for _, r := range required {
		if strings.TrimSpace(r.value) == "" {
			return fmt.Errorf("gate context: %s is required", r.field)
		}
	}

	if len(gc.DecisionOptions) == 0 {
		return fmt.Errorf("gate context: decision_options is required")
	}

	validDecisions := map[domain.GateDecision]bool{
		domain.GateDecisionApprove: true,
		domain.GateDecisionRoute:   true,
		domain.GateDecisionDefer:   true,
		domain.GateDecisionReject:  true,
	}
	for _, d := range gc.DecisionOptions {
		if !validDecisions[d] {
			return fmt.Errorf("%w: %q", ErrInvalidDecision, d)
		}
	}

	return nil
}
