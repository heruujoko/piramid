package definitions

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func helloPatternYAML() string {
	return "id: hello\nname: Hello World\nfile: hello.md\ngoal: say hello\ncadence: manual\nrisk: low\ntools: [echo]\nskills: [greeting]\nstate: hello-state.md\nphases: [prepare, execute]\nhuman_gates: [review]\n"
}

func helloLoopYAML(projectPath string) string {
	return "id: loop-1\npattern: hello\nactive: true\ncron: '* * * * *'\nautonomy: L1\ntrigger: piramid\ngoal: say hello\nproject_path: " + projectPath + "\nhuman_gates: [review]\ntoken:\n  daily_cap: 200000\n"
}

func TestCachedSourceReloadsWhenFileChanges(t *testing.T) {
	root := t.TempDir()
	mustDir(t, filepath.Join(root, "patterns"))
	mustDir(t, filepath.Join(root, "loops"))

	writeFile(t, filepath.Join(root, "patterns", "p.yaml"), helloPatternYAML())
	writeFile(t, filepath.Join(root, "loops", "l.yaml"), helloLoopYAML(root))

	cs, err := NewCachedSource(root)
	if err != nil {
		t.Fatal(err)
	}

	// Initial load should have 1 pattern, 1 loop
	snap, err := cs.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Patterns) != 1 || snap.Patterns[0].ID != "hello" {
		t.Fatalf("patterns = %d, want 1 (hello)", len(snap.Patterns))
	}
	if len(snap.Loops) != 1 || snap.Loops[0].ID != "loop-1" {
		t.Fatalf("loops = %d, want 1 (loop-1)", len(snap.Loops))
	}

	// Same stamp → returns cached (no new LoadRoot)
	firstAt := snap.LoadedAt
	snap2, err := cs.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snap2.LoadedAt.Equal(firstAt) {
		t.Fatalf("LoadedAt changed without file change: %v != %v", snap2.LoadedAt, firstAt)
	}

	// Change a file → reload
	time.Sleep(time.Millisecond * 10) // ensure mtime advances
	writeFile(t, filepath.Join(root, "patterns", "p.yaml"), "id: hello\nname: Hello World v2\nfile: hello.md\ngoal: say hello\ncadence: manual\nrisk: low\ntools: [echo]\nskills: [greeting]\nstate: hello-state.md\nphases: [prepare, execute]\nhuman_gates: [review]\n")

	snap3, err := cs.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap3.LoadedAt.Equal(firstAt) {
		t.Fatal("LoadedAt did not update after file change")
	}
	if snap3.Patterns[0].Name != "Hello World v2" {
		t.Fatalf("pattern name = %q, want updated value", snap3.Patterns[0].Name)
	}

	// Add a new loop file → reload (use different root so it's not a duplicate)
	time.Sleep(time.Millisecond * 10)
	root2 := t.TempDir()
	writeFile(t, filepath.Join(root, "loops", "l2.yaml"), "id: loop-2\npattern: hello\nactive: true\ncron: '0 * * * *'\nautonomy: L1\ntrigger: piramid\ngoal: say hello\nproject_path: "+root2+"\nhuman_gates: [review]\ntoken:\n  daily_cap: 200000\n")

	snap4, err := cs.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap4.Loops) != 2 {
		t.Fatalf("loops after new file = %d, want 2", len(snap4.Loops))
	}
}

func TestCachedSourceKeepsPreviousOnInvalidReload(t *testing.T) {
	root := t.TempDir()
	mustDir(t, filepath.Join(root, "patterns"))
	mustDir(t, filepath.Join(root, "loops"))

	writeFile(t, filepath.Join(root, "patterns", "p.yaml"), helloPatternYAML())
	writeFile(t, filepath.Join(root, "loops", "l.yaml"), helloLoopYAML(root))

	cs, err := NewCachedSource(root)
	if err != nil {
		t.Fatal(err)
	}

	// Corrupt a file
	time.Sleep(time.Millisecond * 10)
	writeFile(t, filepath.Join(root, "loops", "l.yaml"), "garbage {{{")

	snap, err := cs.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Loops) != 1 || snap.Loops[0].ID != "loop-1" {
		t.Fatalf("invalid reload should keep previous snapshot, got %d loops", len(snap.Loops))
	}

	// Fix the file → reload works again
	time.Sleep(time.Millisecond * 10)
	root2 := t.TempDir()
	writeFile(t, filepath.Join(root, "loops", "l.yaml"), "id: loop-3\npattern: hello\nactive: true\ncron: '0 * * * *'\nautonomy: L1\ntrigger: piramid\ngoal: say hello\nproject_path: "+root2+"\nhuman_gates: [review]\ntoken:\n  daily_cap: 200000\n")

	snap2, err := cs.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap2.Loops) != 1 || snap2.Loops[0].ID != "loop-3" {
		t.Fatalf("after fix: loops = %d, id = %q, want 1 loop-3", len(snap2.Loops), snap2.Loops[0].ID)
	}
}

func TestCachedSourceContextCancelledReturnsCurrent(t *testing.T) {
	root := t.TempDir()
	mustDir(t, filepath.Join(root, "patterns"))
	mustDir(t, filepath.Join(root, "loops"))

	writeFile(t, filepath.Join(root, "patterns", "p.yaml"), helloPatternYAML())
	writeFile(t, filepath.Join(root, "loops", "l.yaml"), helloLoopYAML(root))

	cs, err := NewCachedSource(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	snap, err := cs.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Patterns) == 0 {
		t.Fatal("cancelled context should return current snapshot, not empty")
	}
}

func TestNewCachedSourceRejectsInvalidRoot(t *testing.T) {
	_, err := NewCachedSource("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for non-existent root")
	}
}

func TestStampProducesDeterministicString(t *testing.T) {
	root := t.TempDir()
	mustDir(t, filepath.Join(root, "patterns"))
	mustDir(t, filepath.Join(root, "loops"))
	writeFile(t, filepath.Join(root, "patterns", "a.yaml"), "id: a\nname: A File\nfile: a.md\ngoal: do a\ncadence: manual\nrisk: low\ntools: [cat]\nskills: [a]\nstate: a-state.md\nphases: [one, two]\nhuman_gates: [review]\n")

	cs, err := NewCachedSource(root)
	if err != nil {
		t.Fatal(err)
	}
	stamp1, err := cs.stamp()
	if err != nil {
		t.Fatal(err)
	}
	stamp2, err := cs.stamp()
	if err != nil {
		t.Fatal(err)
	}
	if stamp1 != stamp2 {
		t.Fatalf("stamp not deterministic:\n%s\n%s", stamp1, stamp2)
	}
}

func mustDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
}
