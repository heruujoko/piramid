package prompt

import (
	"strings"
	"testing"
)

func TestRenderOrdersPoliciesBodyAndRetryFeedback(t *testing.T) {
	content, hash := Render(RenderInput{
		Role:          RoleExecutor,
		Orchestrator:  []byte("orchestrator policy\n"),
		RolePolicy:    []byte("executor policy\n"),
		Body:          []byte("version: 1\nid: TASK-1\n"),
		RetryFeedback: "fix the checks",
	})
	want := `orchestrator policy

executor policy

--- TASK PACKAGE ---
version: 1
id: TASK-1

--- RETRY FEEDBACK ---
fix the checks
`
	if string(content) != want {
		t.Fatalf("content:\n%s\nwant:\n%s", content, want)
	}
	if hash != Hash(content) {
		t.Fatalf("hash = %q, want %q", hash, Hash(content))
	}
}

func TestRenderOmitsEmptyPoliciesAndFirstAttemptRetrySection(t *testing.T) {
	content, _ := Render(RenderInput{
		Role: RoleExecutor,
		Body: []byte("task"),
	})
	if strings.HasPrefix(string(content), "\n") {
		t.Fatalf("content starts with blank line: %q", content)
	}
	if strings.Contains(string(content), "RETRY FEEDBACK") {
		t.Fatalf("retry section present: %q", content)
	}
	if string(content) != "--- TASK PACKAGE ---\ntask\n" {
		t.Fatalf("content = %q", content)
	}
}

func TestRenderUsesRoleSpecificBodyHeading(t *testing.T) {
	tests := []struct {
		role    Role
		heading string
	}{
		{RolePlanner, "--- GOAL REQUEST ---"},
		{RoleExecutor, "--- TASK PACKAGE ---"},
		{RoleVerifier, "--- VERIFICATION INPUT ---"},
	}
	for _, tt := range tests {
		content, _ := Render(RenderInput{Role: tt.role, Body: []byte("body")})
		if !strings.Contains(string(content), tt.heading) {
			t.Fatalf("%s content = %q", tt.role, content)
		}
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	input := RenderInput{
		Role:         RoleVerifier,
		Orchestrator: []byte(" global \n"),
		RolePolicy:   []byte(" role \n"),
		Body:         []byte(" input \n"),
	}
	firstContent, firstHash := Render(input)
	secondContent, secondHash := Render(input)
	if string(firstContent) != string(secondContent) || firstHash != secondHash {
		t.Fatal("repeated rendering differed")
	}
}
