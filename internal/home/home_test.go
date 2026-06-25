package home

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveUsesPiramidHomeOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "state")
	t.Setenv("PIRAMID_HOME", override)

	paths, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if paths.Root != override {
		t.Fatalf("Root = %q, want %q", paths.Root, override)
	}
}

func TestResolveDefaultsToDotPiramidInUserHome(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("PIRAMID_HOME", "")
	t.Setenv("HOME", userHome)

	paths, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(userHome, ".piramid")
	if paths.Root != want {
		t.Fatalf("Root = %q, want %q", paths.Root, want)
	}
}

func TestInitCreatesLayoutAndEmptyPromptFiles(t *testing.T) {
	paths := NewPaths(filepath.Join(t.TempDir(), ".piramid"))

	if err := Init(paths); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{
		paths.Root, paths.Prompts, paths.Goals, paths.Tasks,
		paths.Attempts, paths.Artifacts, paths.Runtime,
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %o, want 700", dir, info.Mode().Perm())
		}
	}

	for _, name := range []string{"orchestrator.md", "planner.md", "executor.md", "verifier.md"} {
		path := filepath.Join(paths.Prompts, name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if len(content) != 0 {
			t.Fatalf("%s is not empty", path)
		}
	}

	databaseInfo, err := os.Stat(paths.Database)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if databaseInfo.IsDir() || databaseInfo.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %s, want regular file 0600", databaseInfo.Mode())
	}
}

func TestInitIsIdempotentAndPreservesConfig(t *testing.T) {
	paths := NewPaths(filepath.Join(t.TempDir(), ".piramid"))
	if err := Init(paths); err != nil {
		t.Fatal(err)
	}
	const custom = "version: 1\nserver:\n  host: custom\n"
	if err := os.WriteFile(paths.Config, []byte(custom), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Init(paths); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != custom {
		t.Fatalf("config overwritten: %q", content)
	}
}
