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
  - id: E2E-TASK
    title: Create deterministic result
    goal: Create result.txt with the verified content
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
