package runtime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExpandArgsPreservesArgumentBoundaries(t *testing.T) {
	args, err := ExpandArgs([]string{
		"-p", "{{prompt}}", "{{prompt_file}}", "{{workspace}}",
		"{{task_id}}", "{{attempt}}", "{{model}}",
	}, TemplateValues{
		Prompt:     "text with spaces; $(unsafe)",
		PromptFile: "/tmp/prompt file.md",
		Workspace:  "/tmp/work space",
		TaskID:     "TASK-1",
		Attempt:    "2",
		Model:      "gpt-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 7 {
		t.Fatalf("len(args) = %d", len(args))
	}
	if args[1] != "text with spaces; $(unsafe)" {
		t.Fatalf("prompt argument = %q", args[1])
	}
	if args[2] != "/tmp/prompt file.md" {
		t.Fatalf("prompt file argument = %q", args[2])
	}
}

func TestExpandArgsRejectsUnknownPlaceholder(t *testing.T) {
	if _, err := ExpandArgs([]string{"{{unknown}}"}, TemplateValues{}); err == nil {
		t.Fatal("ExpandArgs() accepted unknown placeholder")
	}
}

func TestCommandAdapterStreamsOutputAndSetsWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper argument parsing differs on Windows")
	}
	root := t.TempDir()
	stdout := filepath.Join(root, "stdout.log")
	stderr := filepath.Join(root, "stderr.log")
	result, err := NewCommandAdapter().Run(context.Background(), Invocation{
		Command:     os.Args[0],
		Args:        helperArgs("report"),
		WorkingDir:  root,
		Environment: helperEnv("PIRAMID_SAFE_VALUE=visible"),
		Timeout:     5 * time.Second,
		StdoutPath:  stdout,
		StderrPath:  stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.ProcessID <= 0 {
		t.Fatalf("result = %#v", result)
	}
	stdoutContent, err := os.ReadFile(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stdoutContent), root) ||
		!strings.Contains(string(stdoutContent), "visible") {
		t.Fatalf("stdout = %q", stdoutContent)
	}
	stderrContent, err := os.ReadFile(stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stderrContent), "helper stderr") {
		t.Fatalf("stderr = %q", stderrContent)
	}
}

func TestCommandAdapterCapturesNonZeroExit(t *testing.T) {
	result, err := NewCommandAdapter().Run(context.Background(), Invocation{
		Command:     os.Args[0],
		Args:        helperArgs("exit", "7"),
		WorkingDir:  t.TempDir(),
		Environment: helperEnv(),
		Timeout:     5 * time.Second,
		StdoutPath:  filepath.Join(t.TempDir(), "stdout.log"),
		StderrPath:  filepath.Join(t.TempDir(), "stderr.log"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", result.ExitCode)
	}
}

func TestCommandAdapterTimesOutAndTerminatesProcess(t *testing.T) {
	result, err := NewCommandAdapter().Run(context.Background(), Invocation{
		Command:     os.Args[0],
		Args:        helperArgs("sleep"),
		WorkingDir:  t.TempDir(),
		Environment: helperEnv(),
		Timeout:     100 * time.Millisecond,
		StdoutPath:  filepath.Join(t.TempDir(), "stdout.log"),
		StderrPath:  filepath.Join(t.TempDir(), "stderr.log"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.TimedOut || !result.Interrupted {
		t.Fatalf("result = %#v", result)
	}
}

func TestCommandAdapterHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	result, err := NewCommandAdapter().Run(ctx, Invocation{
		Command:     os.Args[0],
		Args:        helperArgs("sleep"),
		WorkingDir:  t.TempDir(),
		Environment: helperEnv(),
		Timeout:     5 * time.Second,
		StdoutPath:  filepath.Join(t.TempDir(), "stdout.log"),
		StderrPath:  filepath.Join(t.TempDir(), "stderr.log"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TimedOut || !result.Interrupted {
		t.Fatalf("result = %#v", result)
	}
}

func TestRedactEnvironmentHidesSecrets(t *testing.T) {
	got := RedactEnvironment([]string{
		"PATH=/bin",
		"GITHUB_TOKEN=secret",
		"API_KEY=key",
		"PASSWORD=hunter2",
		"VISIBLE=value",
	})
	joined := strings.Join(got, "\n")
	if strings.Contains(joined, "secret") || strings.Contains(joined, "hunter2") ||
		strings.Contains(joined, "=key") {
		t.Fatalf("redacted environment leaked a secret: %q", joined)
	}
	if !strings.Contains(joined, "VISIBLE=value") || !strings.Contains(joined, "<redacted>") {
		t.Fatalf("redacted environment = %q", joined)
	}
}

func helperArgs(args ...string) []string {
	return append([]string{"-test.run=TestHelperProcess", "--"}, args...)
}

func helperEnv(extra ...string) []string {
	return append(append(os.Environ(), "GO_WANT_HELPER_PROCESS=1"), extra...)
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	index := 0
	for i, arg := range args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	if index == 0 || index >= len(args) {
		os.Exit(2)
	}
	switch args[index] {
	case "report":
		workingDir, _ := os.Getwd()
		_, _ = os.Stdout.WriteString(workingDir + "\n" + os.Getenv("PIRAMID_SAFE_VALUE") + "\n")
		_, _ = os.Stderr.WriteString("helper stderr\n")
	case "exit":
		if args[index+1] == "7" {
			os.Exit(7)
		}
		os.Exit(3)
	case "sleep":
		time.Sleep(30 * time.Second)
	default:
		os.Exit(4)
	}
	os.Exit(0)
}
