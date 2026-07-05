package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	prompt := argument("-p")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "missing -p prompt")
		os.Exit(2)
	}
	switch {
	case strings.Contains(prompt, "--- GOAL REQUEST ---"):
		planner(prompt)
	case strings.Contains(prompt, "--- TASK PACKAGE ---"):
		executor(prompt)
	case strings.Contains(prompt, "--- VERIFICATION INPUT ---"):
		verifier()
	default:
		fmt.Fprintln(os.Stderr, "unknown prompt role")
		os.Exit(3)
	}
}

func argument(name string) string {
	for index := 1; index+1 < len(os.Args); index++ {
		if os.Args[index] == name {
			return os.Args[index+1]
		}
	}
	return ""
}

func planner(prompt string) {
	goalID := lineValue(prompt, "Goal ID:")
	project := lineValue(prompt, "Project:")
	fmt.Printf(`version: 1
goal_id: %s
tasks:
  - id: GATE-TASK
    title: Create deterministic result with gate
    goal: Create result.txt with the verified content, then gate for human review
    project_path: %s
    expected_outputs:
      - type: file
        path: result.txt
    dod:
      - result.txt contains verified
    max_attempts: 3
    timeout: 30s
`, goalID, project)
}

func executor(prompt string) {
	content := "first attempt\n"
	if strings.Contains(prompt, "--- RETRY FEEDBACK ---") {
		content = "verified\n"
	}
	if err := os.WriteFile(filepath.Join(".", "result.txt"), []byte(content), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	fmt.Println("wrote result.txt")

	if !strings.Contains(prompt, "--- RETRY FEEDBACK ---") &&
		!strings.Contains(prompt, "--- RESTORE PROMPT ---") {
		gatePath := os.Getenv("PIRAMID_GATE_CONTEXT")
		if gatePath == "" {
			gatePath = "gate.context.md"
		}
		gateCtx := buildGateContext()
		if err := os.WriteFile(gatePath, []byte(gateCtx), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(5)
		}
		fmt.Fprintln(os.Stderr, "gate.context.md written, exiting with code 42")
		os.Exit(42)
	}
}

func buildGateContext() string {
	lines := []string{
		"---",
		"gate: review",
		"phase: executor",
	}
	if fireID := os.Getenv("PIRAMID_FIRE_ID"); fireID != "" {
		lines = append(lines, "fire_id: "+fireID)
	}
	if loopID := os.Getenv("PIRAMID_LOOP_ID"); loopID != "" {
		lines = append(lines, "loop_id: "+loopID)
	}
	if goalID := os.Getenv("PIRAMID_GOAL_ID"); goalID != "" {
		lines = append(lines, "goal_id: "+goalID)
	}
	lines = append(lines,
		`summary: "Human review needed: verify result.txt content"`,
		"decision_options:",
		"  - approve",
		"  - route",
		"  - defer",
		"  - reject",
		"---",
		"## Thread Ledger",
		"",
		"1. **[author-note]: Verify result.txt content is correct**",
		`   - result.txt — check content is "verified"`,
	)
	return strings.Join(lines, "\n") + "\n"
}

func verifier() {
	content, err := os.ReadFile("result.txt")
	if err != nil {
		fmt.Println("status: FAIL")
		fmt.Println("reasons: [result.txt is missing]")
		fmt.Println("retry_prompt: Create result.txt with the exact content verified.")
		return
	}
	if strings.TrimSpace(string(content)) != "verified" {
		fmt.Println("status: FAIL")
		fmt.Println("reasons: [result.txt has unverified content]")
		fmt.Println("retry_prompt: Replace result.txt content with the exact word verified.")
		return
	}
	fmt.Println("status: PASS")
	fmt.Println("reasons: [result.txt contains verified]")
}

func lineValue(content, prefix string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
