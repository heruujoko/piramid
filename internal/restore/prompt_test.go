package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/heruujoko/piramid/internal/domain"
)

func TestFallbackNote(t *testing.T) {
	cases := map[domain.GateDecision]string{
		domain.GateDecisionApprove: "Approved without note.",
		domain.GateDecisionRoute:   "Routed without note; resume from gate context.",
		domain.GateDecisionDefer:   "Deferred without note.",
		domain.GateDecisionReject:  "Rejected without note.",
	}
	for decision, want := range cases {
		if got := FallbackNote(decision); got != want {
			t.Fatalf("FallbackNote(%s) = %q, want %q", decision, got, want)
		}
	}
}

func TestBuildPromptUsesConciseThreadSummaries(t *testing.T) {
	gate := domain.Gate{
		ID: "GATE-1", FireID: "FIRE-1", TaskID: "TASK-1", AttemptID: "7",
		ContextPath: "/records/TASK-1/0001/gate.context.md",
		Context: domain.GateContext{
			Phase: "execution", Summary: "Needs review", Body: "FULL BODY SHOULD NOT APPEAR",
			Threads: []domain.GateThread{{ID: "T1", Title: "Fix API", Location: "api.go:10", Summary: "Use 400 for invalid input"}},
		},
	}
	prompt := BuildPrompt(BuildInput{
		Gate: gate, Decision: domain.GateDecisionRoute, Note: "Use option B",
		LogTail: LogTail{StdoutPath: "/tmp/stdout", StderrPath: "/tmp/stderr", StdoutTail: "out20", StderrTail: "err20"},
	})
	for _, want := range []string{"Decision: route", "Note: Use option B", "T1", "Fix API", "api.go:10", "Use 400", "out20", "err20", gate.ContextPath} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "FULL BODY SHOULD NOT APPEAR") {
		t.Fatalf("prompt included full gate body:\n%s", prompt)
	}
}

func TestTailFileReturnsLastNLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	var b strings.Builder
	for i := 1; i <= 25; i++ {
		b.WriteString("line ")
		b.WriteString(string(rune('A' + i - 1)))
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	tail := TailFile(path, 20)
	if strings.Contains(tail, "line A") || !strings.Contains(tail, "line F") || !strings.Contains(tail, "line Y") {
		t.Fatalf("tail = %q, want last 20 lines F..Y", tail)
	}
}
