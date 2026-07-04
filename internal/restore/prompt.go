package restore

import (
	"fmt"
	"os"
	"strings"

	"github.com/heruujoko/piramid/internal/domain"
)

type LogTail struct {
	StdoutPath string
	StderrPath string
	StdoutTail string
	StderrTail string
}

type BuildInput struct {
	Gate     domain.Gate
	Decision domain.GateDecision
	Note     string
	LogTail  LogTail
}

func FallbackNote(decision domain.GateDecision) string {
	switch decision {
	case domain.GateDecisionApprove:
		return "Approved without note."
	case domain.GateDecisionRoute:
		return "Routed without note; resume from gate context."
	case domain.GateDecisionDefer:
		return "Deferred without note."
	case domain.GateDecisionReject:
		return "Rejected without note."
	default:
		return "Decision recorded without note."
	}
}

func BuildPrompt(input BuildInput) string {
	note := strings.TrimSpace(input.Note)
	if note == "" {
		note = FallbackNote(input.Decision)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Resume this task from a human gate.\n\n")
	fmt.Fprintf(&b, "Decision: %s\n", input.Decision)
	fmt.Fprintf(&b, "Note: %s\n\n", note)
	fmt.Fprintf(&b, "Gate:\n")
	fmt.Fprintf(&b, "- id: %s\n", input.Gate.ID)
	fmt.Fprintf(&b, "- phase: %s\n", input.Gate.Context.Phase)
	fmt.Fprintf(&b, "- summary: %s\n", input.Gate.Context.Summary)
	fmt.Fprintf(&b, "- context path: %s\n\n", input.Gate.ContextPath)
	fmt.Fprintf(&b, "Threads:\n")
	if len(input.Gate.Context.Threads) == 0 {
		fmt.Fprintf(&b, "- none\n")
	}
	for _, thread := range input.Gate.Context.Threads {
		fmt.Fprintf(&b, "- id: %s\n  title: %s\n  location: %s\n  summary: %s\n",
			thread.ID, thread.Title, thread.Location, thread.Summary)
	}
	fmt.Fprintf(&b, "\nPrevious attempt:\n")
	fmt.Fprintf(&b, "- attempt id: %s\n", input.Gate.AttemptID)
	fmt.Fprintf(&b, "- stdout path: %s\n", input.LogTail.StdoutPath)
	fmt.Fprintf(&b, "- stderr path: %s\n", input.LogTail.StderrPath)
	fmt.Fprintf(&b, "- last 20 stdout lines:\n%s\n", emptyTail(input.LogTail.StdoutTail))
	fmt.Fprintf(&b, "- last 20 stderr lines:\n%s\n\n", emptyTail(input.LogTail.StderrTail))
	fmt.Fprintf(&b, "Instruction:\n")
	fmt.Fprintf(&b, "Resume from the paused phase. Do not repeat completed ledger work unless needed.\n")
	fmt.Fprintf(&b, "Use the full gate context file if more detail is required.\n")
	return b.String()
}

func TailFile(path string, lines int) string {
	if strings.TrimSpace(path) == "" || lines <= 0 {
		return "(log unavailable)"
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "(log unavailable: " + err.Error() + ")"
	}
	parts := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	if len(parts) == 1 && parts[0] == "" {
		return "(empty log)"
	}
	return strings.Join(parts, "\n")
}

func emptyTail(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(empty log)"
	}
	return value
}
