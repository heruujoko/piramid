package prompt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

type Role string

const (
	RolePlanner  Role = "planner"
	RoleExecutor Role = "executor"
	RoleVerifier Role = "verifier"
)

type RenderInput struct {
	Role          Role
	Orchestrator  []byte
	RolePolicy    []byte
	Body          []byte
	RetryFeedback string
}

func Render(input RenderInput) ([]byte, string) {
	sections := make([]string, 0, 4)
	if policy := strings.TrimSpace(string(input.Orchestrator)); policy != "" {
		sections = append(sections, policy)
	}
	if policy := strings.TrimSpace(string(input.RolePolicy)); policy != "" {
		sections = append(sections, policy)
	}
	sections = append(sections, heading(input.Role)+"\n"+strings.TrimSpace(string(input.Body)))
	if retry := strings.TrimSpace(input.RetryFeedback); retry != "" {
		sections = append(sections, "--- RETRY FEEDBACK ---\n"+retry)
	}
	content := []byte(strings.Join(sections, "\n\n") + "\n")
	return content, Hash(content)
}

func heading(role Role) string {
	switch role {
	case RolePlanner:
		return "--- GOAL REQUEST ---"
	case RoleVerifier:
		return "--- VERIFICATION INPUT ---"
	default:
		return "--- TASK PACKAGE ---"
	}
}

func Hash(content []byte) string {
	sum := sha256.Sum256(bytes.Clone(content))
	return hex.EncodeToString(sum[:])
}
