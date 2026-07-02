package definitions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRootAcceptsValidDefinitions(t *testing.T) {
	snapshot, err := LoadRoot(filepath.Join("..", "..", "test", "definitions", "valid"))
	if err != nil {
		t.Fatalf("LoadRoot() error = %v", err)
	}
	if len(snapshot.Patterns) != 1 {
		t.Fatalf("patterns = %d, want 1", len(snapshot.Patterns))
	}
	if len(snapshot.Loops) != 1 {
		t.Fatalf("loops = %d, want 1", len(snapshot.Loops))
	}
	if snapshot.Patterns[0].ID != "post-merge-cleanup" {
		t.Fatalf("pattern id = %q", snapshot.Patterns[0].ID)
	}
	if snapshot.Loops[0].PatternID != "post-merge-cleanup" {
		t.Fatalf("loop pattern = %q", snapshot.Loops[0].PatternID)
	}
	if snapshot.LoadedAt.IsZero() {
		t.Fatal("LoadedAt is zero")
	}
}

func TestLoadRootAllowsEmptyDirectories(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "patterns"))
	mustMkdir(t, filepath.Join(root, "loops"))

	snapshot, err := LoadRoot(root)
	if err != nil {
		t.Fatalf("LoadRoot() error = %v", err)
	}
	if len(snapshot.Patterns) != 0 || len(snapshot.Loops) != 0 {
		t.Fatalf("snapshot = %+v, want empty definitions", snapshot)
	}
}

func TestLoadRootRejectsMissingRoot(t *testing.T) {
	_, err := LoadRoot(filepath.Join(t.TempDir(), "missing"))
	if err == nil || !strings.Contains(err.Error(), "definitions root") {
		t.Fatalf("LoadRoot() error = %v, want definitions root error", err)
	}
}

func TestLoadRootRejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(t *testing.T, root string)
		contains string
	}{
		{
			name: "bad pattern id",
			mutate: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, "patterns", "post-merge-cleanup.yaml"), strings.Replace(validPatternYAML, "id: post-merge-cleanup", "id: PostMerge", 1))
			},
			contains: "pattern id",
		},
		{
			name: "bad risk",
			mutate: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, "patterns", "post-merge-cleanup.yaml"), strings.Replace(validPatternYAML, "risk: low", "risk: urgent", 1))
			},
			contains: "risk",
		},
		{
			name: "duplicate pattern id",
			mutate: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, "patterns", "copy.yaml"), validPatternYAML)
			},
			contains: "duplicate",
		},
		{
			name: "unknown loop pattern",
			mutate: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, "loops", "post-merge-cleanup.yaml"), strings.Replace(validLoopYAML, "pattern: post-merge-cleanup", "pattern: missing-pattern", 1))
			},
			contains: "unknown pattern",
		},
		{
			name: "bad cron",
			mutate: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, "loops", "post-merge-cleanup.yaml"), strings.Replace(validLoopYAML, "cron: \"0 21 * * *\"", "cron: bad cron", 1))
			},
			contains: "cron",
		},
		{
			name: "duplicate loop id",
			mutate: func(t *testing.T, root string) {
				writeFile(t, filepath.Join(root, "loops", "copy.yaml"), validLoopYAML)
			},
			contains: "duplicate",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := validRoot(t)
			tt.mutate(t, root)
			_, err := LoadRoot(root)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("LoadRoot() error = %v, want substring %q", err, tt.contains)
			}
		})
	}
}

func validRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "patterns"))
	mustMkdir(t, filepath.Join(root, "loops"))
	writeFile(t, filepath.Join(root, "patterns", "post-merge-cleanup.yaml"), validPatternYAML)
	writeFile(t, filepath.Join(root, "loops", "post-merge-cleanup.yaml"), validLoopYAML)
	return root
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validPatternYAML = `id: post-merge-cleanup
name: Post-Merge Cleanup
file: post-merge-cleanup.md
goal: Follow-up tech debt and cleanup after merges to main
cadence: 1d-6h
risk: low
tools: [claude-code, github-actions]
skills: [post-merge-scan, minimal-fix, pr-maintainer]
state: post-merge-state.md
phases: [scan-merges, prioritize, fix-small, ticket-large]
human_gates: [architectural-debt, feature-flags, unresolved-feedback]
week_one_mode: L1
token_cost: low
cost:
  tokens_noop: 5000
  tokens_report: 40000
  tokens_action: 150000
  suggested_daily_cap: 200000
  early_exit_required: false
`

const validLoopYAML = `id: LOOP-POST-MERGE
pattern: post-merge-cleanup
active: true
cron: "0 21 * * *"
autonomy: L1
trigger: piramid
goal: |
  Scan merged PRs, run post-merge cleanup, and route unresolved review feedback through pr-maintainer.
project_path: /tmp/project
human_gates: [unresolved-feedback, architectural-debt]
token:
  daily_cap: 200000
`
