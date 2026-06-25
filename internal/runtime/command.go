package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CommandAdapter struct {
	gracePeriod time.Duration
}

func NewCommandAdapter() *CommandAdapter {
	return &CommandAdapter{gracePeriod: 2 * time.Second}
}

func NewPiCLIAdapter() Adapter {
	return NewCommandAdapter()
}

func (a *CommandAdapter) Run(ctx context.Context, invocation Invocation) (Result, error) {
	if strings.TrimSpace(invocation.Command) == "" {
		return Result{}, fmt.Errorf("command is required")
	}
	if invocation.Timeout <= 0 {
		return Result{}, fmt.Errorf("timeout must be positive")
	}
	if invocation.WorkingDir == "" {
		return Result{}, fmt.Errorf("working directory is required")
	}
	if err := os.MkdirAll(filepath.Dir(invocation.StdoutPath), 0o700); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(invocation.StderrPath), 0o700); err != nil {
		return Result{}, err
	}
	stdout, err := os.OpenFile(invocation.StdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Result{}, err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(invocation.StderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Result{}, err
	}
	defer stderr.Close()

	cmd := exec.Command(invocation.Command, invocation.Args...)
	cmd.Dir = invocation.WorkingDir
	cmd.Env = invocation.Environment
	if len(cmd.Env) == 0 {
		cmd.Env = os.Environ()
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	configureProcessGroup(cmd)

	result := Result{StartedAt: time.Now().UTC(), ExitCode: -1}
	if err := cmd.Start(); err != nil {
		result.FinishedAt = time.Now().UTC()
		return result, err
	}
	result.ProcessID = cmd.Process.Pid

	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()

	timer := time.NewTimer(invocation.Timeout)
	defer timer.Stop()
	var waitErr error
	select {
	case waitErr = <-wait:
	case <-ctx.Done():
		result.Interrupted = true
		waitErr = a.stop(cmd, wait)
	case <-timer.C:
		result.TimedOut = true
		result.Interrupted = true
		waitErr = a.stop(cmd, wait)
	}
	result.FinishedAt = time.Now().UTC()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err := stdout.Sync(); err != nil {
		return result, err
	}
	if err := stderr.Sync(); err != nil {
		return result, err
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			return result, waitErr
		}
	}
	return result, nil
}

func (a *CommandAdapter) stop(cmd *exec.Cmd, wait <-chan error) error {
	_ = terminateProcessGroup(cmd)
	timer := time.NewTimer(a.gracePeriod)
	defer timer.Stop()
	select {
	case err := <-wait:
		return err
	case <-timer.C:
		_ = killProcessGroup(cmd)
		return <-wait
	}
}

func RedactEnvironment(environment []string) []string {
	redacted := make([]string, 0, len(environment))
	for _, item := range environment {
		name, value, found := strings.Cut(item, "=")
		if !found {
			redacted = append(redacted, item)
			continue
		}
		upper := strings.ToUpper(name)
		if strings.Contains(upper, "TOKEN") || strings.Contains(upper, "PASSWORD") ||
			strings.Contains(upper, "SECRET") || strings.Contains(upper, "AUTH") ||
			strings.HasSuffix(upper, "_KEY") {
			value = "<redacted>"
		}
		redacted = append(redacted, name+"="+value)
	}
	return redacted
}
