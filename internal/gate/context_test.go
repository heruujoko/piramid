package gate

import (
	"errors"
	"testing"

	"github.com/heruujoko/piramid/internal/domain"
)

func TestParseContextValid(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
summary: Review the plan before execution
decision_options:
  - approve
  - route
  - defer
  - reject
---
This is the markdown body with context details.

It can span multiple paragraphs.
`
	gc, err := ParseContext(content)
	if err != nil {
		t.Fatal(err)
	}
	if gc.Gate != "review" {
		t.Fatalf("gate = %q, want review", gc.Gate)
	}
	if gc.Phase != "pre-execution" {
		t.Fatalf("phase = %q, want pre-execution", gc.Phase)
	}
	if gc.LoopID != "loop-1" {
		t.Fatalf("loop_id = %q, want loop-1", gc.LoopID)
	}
	if gc.FireID != "fire-1" {
		t.Fatalf("fire_id = %q, want fire-1", gc.FireID)
	}
	if gc.Summary != "Review the plan before execution" {
		t.Fatalf("summary = %q", gc.Summary)
	}
	if len(gc.DecisionOptions) != 4 {
		t.Fatalf("decision_options = %d, want 4", len(gc.DecisionOptions))
	}
	if gc.Body != "This is the markdown body with context details.\n\nIt can span multiple paragraphs." {
		t.Fatalf("body = %q", gc.Body)
	}
}

func TestParseContextMissingFrontMatter(t *testing.T) {
	content := `just some text without front-matter`
	_, err := ParseContext(content)
	if err == nil || err.Error() != "gate context: missing front-matter" {
		t.Fatalf("error = %v, want missing front-matter", err)
	}
}

func TestParseContextMissingRequiredField(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
# summary is missing
decision_options:
  - approve
---
Body text
`
	_, err := ParseContext(content)
	if err == nil || err.Error() != "gate context: summary is required" {
		t.Fatalf("error = %v, want summary is required", err)
	}
}

func TestParseContextMissingDecisionOptions(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
summary: Review
---
Body
`
	_, err := ParseContext(content)
	if err == nil || err.Error() != "gate context: decision_options is required" {
		t.Fatalf("error = %v, want decision_options is required", err)
	}
}

func TestParseContextInvalidDecisionOption(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
summary: Review
decision_options:
  - approve
  - invalid_option
---
Body
`
	_, err := ParseContext(content)
	if err == nil || !isInvalidDecision(err) {
		t.Fatalf("error = %v, want invalid decision option", err)
	}
}

func TestParseContextBodyPreservedExactly(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
summary: Review
decision_options:
  - approve
---

## Section 1

Some *markdown* content.

- list item
- another item
`
	gc, err := ParseContext(content)
	if err != nil {
		t.Fatal(err)
	}
	expected := "## Section 1\n\nSome *markdown* content.\n\n- list item\n- another item"
	if gc.Body != expected {
		t.Fatalf("body = %q, want %q", gc.Body, expected)
	}
}

func TestParseContextPreservesLeadingWhitespace(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
summary: Review
decision_options:
  - approve
---
    code block
    indented content
`
	gc, err := ParseContext(content)
	if err != nil {
		t.Fatal(err)
	}
	expected := "    code block\n    indented content"
	if gc.Body != expected {
		t.Fatalf("body = %q, want %q", gc.Body, expected)
	}
}

func TestParseContextAcceptsCRLFLineEndings(t *testing.T) {
	content := "---\r\ngate: review\r\nphase: pre-execution\r\nloop_id: loop-1\r\nfire_id: fire-1\r\nsummary: Review\r\ndecision_options:\r\n  - approve\r\n---\r\nBody text\r\n"
	gc, err := ParseContext(content)
	if err != nil {
		t.Fatal(err)
	}
	if gc.Gate != "review" {
		t.Fatalf("gate = %q, want review", gc.Gate)
	}
	if gc.Body != "Body text" {
		t.Fatalf("body = %q, want 'Body text'", gc.Body)
	}
}

func TestParseContextAllowsOptionalFields(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
summary: Review
decision_options:
  - approve
threads:
  - id: thread-1
    title: Design review
    location: internal/gate/context.go
    author: alice
    summary: Check the parsing logic
---
Body
`
	gc, err := ParseContext(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(gc.Threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(gc.Threads))
	}
	if gc.Threads[0].ID != "thread-1" {
		t.Fatalf("thread[0].id = %q", gc.Threads[0].ID)
	}
	if gc.Threads[0].Title != "Design review" {
		t.Fatalf("thread[0].title = %q", gc.Threads[0].Title)
	}
}

func isInvalidDecision(err error) bool {
	return errors.Is(err, ErrInvalidDecision)
}

func TestParseContextEmptyContent(t *testing.T) {
	_, err := ParseContext("")
	if err == nil || err.Error() != "gate context: missing front-matter" {
		t.Fatalf("error = %v, want missing front-matter", err)
	}
}

func TestParseContextOnlyFrontMatterNoBody(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
summary: Review
decision_options:
  - approve
---`
	gc, err := ParseContext(content)
	if err != nil {
		t.Fatal(err)
	}
	if gc.Body != "" {
		t.Fatalf("body = %q, want empty", gc.Body)
	}
}

func TestParseContextGoalIDAndTaskID(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
goal_id: goal-1
task_id: task-1
summary: Review
decision_options:
  - approve
---
Body
`
	gc, err := ParseContext(content)
	if err != nil {
		t.Fatal(err)
	}
	if gc.GoalID != "goal-1" {
		t.Fatalf("goal_id = %q, want goal-1", gc.GoalID)
	}
	if gc.TaskID != "task-1" {
		t.Fatalf("task_id = %q, want task-1", gc.TaskID)
	}
}

func TestParseContextRejectsOnlyDashes(t *testing.T) {
	content := `---
---
`
	_, err := ParseContext(content)
	if err == nil {
		t.Fatal("expected error for empty front-matter")
	}
}

func TestParseContextRejectsInvalidYAML(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
summary: Review
decision_options: [approve, invalid
---
Body
`
	_, err := ParseContext(content)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// Ensure the parsed GateContext can be used with domain types.
func TestParseContextRoundTripWithDomain(t *testing.T) {
	content := `---
gate: review
phase: pre-execution
loop_id: loop-1
fire_id: fire-1
summary: Review the plan
decision_options:
  - approve
  - reject
---
Body text
`
	gc, err := ParseContext(content)
	if err != nil {
		t.Fatal(err)
	}

	// Verify it can populate a domain.Gate
	gate := domain.Gate{
		Context: gc,
	}
	if gate.Context.Gate != "review" {
		t.Fatalf("gate.Context.Gate = %q", gate.Context.Gate)
	}
	if gate.Context.Body != "Body text" {
		t.Fatalf("gate.Context.Body = %q", gate.Context.Body)
	}
}
