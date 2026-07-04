package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/prompt"
	"github.com/heruujoko/piramid/internal/records"
	runtimepkg "github.com/heruujoko/piramid/internal/runtime"
	storepkg "github.com/heruujoko/piramid/internal/store"
	"gopkg.in/yaml.v3"
)

type RunnerStore interface {
	PrepareAttempt(context.Context, storepkg.AttemptPathsInput) error
	PrepareVerifier(context.Context, int64, string, string) error
	RecordArtifacts(context.Context, int64, []storepkg.ArtifactRecord) error
	MoveToVerification(context.Context, storepkg.FinishExecutionInput) error
	FinishVerification(context.Context, storepkg.FinishVerificationInput) error
	RecordOperationalFailure(context.Context, storepkg.OperationalFailureInput) error
}

type RoleRuntime struct {
	Command     string
	Args        []string
	Environment []string
	Timeout     time.Duration
}

type RunnerConfig struct {
	Store           RunnerStore
	Records         *records.Store
	Executor        runtimepkg.Adapter
	Verifier        runtimepkg.Adapter
	ExecutorRuntime RoleRuntime
	VerifierRuntime RoleRuntime
	Now             func() time.Time
	RetryDelay      func(attemptNumber int) time.Duration
}

type Runner struct {
	store           RunnerStore
	records         *records.Store
	executor        runtimepkg.Adapter
	verifier        runtimepkg.Adapter
	executorRuntime RoleRuntime
	verifierRuntime RoleRuntime
	now             func() time.Time
	retryDelay      func(int) time.Duration
}

type artifactEvidence struct {
	Path         string `yaml:"path"`
	Status       string `yaml:"status"`
	AbsolutePath string `yaml:"absolute_path,omitempty"`
	SHA256       string `yaml:"sha256,omitempty"`
	SizeBytes    int64  `yaml:"size_bytes,omitempty"`
	Reason       string `yaml:"reason,omitempty"`
}

type verifierInput struct {
	Task             domain.Task        `yaml:"task"`
	DefinitionOfDone []string           `yaml:"dod"`
	Process          runtimepkg.Result  `yaml:"process"`
	Artifacts        []artifactEvidence `yaml:"artifacts"`
	StdoutPath       string             `yaml:"stdout_path"`
	StderrPath       string             `yaml:"stderr_path"`
	OutputContract   map[string]any     `yaml:"output_contract"`
}

func NewRunner(config RunnerConfig) *Runner {
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	retryDelay := config.RetryDelay
	if retryDelay == nil {
		retryDelay = func(attempt int) time.Duration {
			delay := time.Minute << max(attempt-1, 0)
			if delay > 30*time.Minute {
				return 30 * time.Minute
			}
			return delay
		}
	}
	return &Runner{
		store:           config.Store,
		records:         config.Records,
		executor:        config.Executor,
		verifier:        config.Verifier,
		executorRuntime: config.ExecutorRuntime,
		verifierRuntime: config.VerifierRuntime,
		now:             now,
		retryDelay:      retryDelay,
	}
}

func (r *Runner) Run(ctx context.Context, dispatch Dispatch) error {
	defer dispatch.Done()
	task := dispatch.Task
	attempt := dispatch.Attempt
	paths, err := r.records.CreateAttempt(task.ID, attempt.AttemptNumber)
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "attempt_records", err)
	}
	taskBody, err := marshalYAML(task.Task)
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "task_encoding", err)
	}
	executorPrompt, executorHash, err := r.renderRolePrompt(
		prompt.RoleExecutor,
		taskBody,
		task.RetryPrompt,
	)
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "executor_prompt", err)
	}
	if _, err := r.records.WriteImmutable(paths.ExecutorPrompt, executorPrompt); err != nil {
		return r.operationalFailure(ctx, task, attempt, "executor_prompt", err)
	}
	if err := r.store.PrepareAttempt(ctx, storepkg.AttemptPathsInput{
		AttemptID: attempt.ID, PromptPath: paths.ExecutorPrompt, PromptHash: executorHash,
		StdoutPath: paths.Stdout, StderrPath: paths.Stderr,
	}); err != nil {
		return r.operationalFailure(ctx, task, attempt, "attempt_prepare", err)
	}

	executorRuntime := r.executorRuntime
	executorRuntime.Timeout = task.Timeout
	executorRuntime.Environment = append(
		append([]string(nil), executorRuntime.Environment...),
		"PIRAMID_GATE_CONTEXT="+paths.GateContext,
	)
	executorResult, err := r.invoke(
		ctx, r.executor, executorRuntime, task, attempt,
		executorPrompt, paths.ExecutorPrompt, paths.Stdout, paths.Stderr,
	)
	if writeErr := r.writeProcessResult(paths.Process, executorResult); writeErr != nil && err == nil {
		err = writeErr
	}
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "executor_launch", err)
	}
	if executorResult.TimedOut {
		return r.operationalFailure(
			ctx, task, attempt, "executor_timeout", errors.New("executor timed out"),
		)
	}
	if executorResult.Interrupted {
		return r.operationalFailure(
			ctx, task, attempt, "executor_interrupted", errors.New("executor was interrupted"),
		)
	}

	evidence, artifactRecords := discoverArtifacts(task.ProjectPath, task.Outputs)
	if err := r.store.RecordArtifacts(ctx, attempt.ID, artifactRecords); err != nil {
		return r.operationalFailure(ctx, task, attempt, "artifact_recording", err)
	}
	finishedAt := executorResult.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = r.now()
	}
	if err := r.store.MoveToVerification(ctx, storepkg.FinishExecutionInput{
		TaskID: task.ID, AttemptID: attempt.ID, ProcessID: executorResult.ProcessID,
		ExitCode: executorResult.ExitCode, FinishedAt: finishedAt,
	}); err != nil {
		return r.operationalFailure(ctx, task, attempt, "verification_transition", err)
	}

	verificationBody, err := marshalYAML(verifierInput{
		Task:             task.Task,
		DefinitionOfDone: task.DOD,
		Process:          executorResult,
		Artifacts:        evidence,
		StdoutPath:       paths.Stdout,
		StderrPath:       paths.Stderr,
		OutputContract: map[string]any{
			"status":       "PASS or FAIL",
			"reasons":      []string{"one or more concrete reasons"},
			"retry_prompt": "required on FAIL when retries remain; empty on PASS",
		},
	})
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "verifier_input", err)
	}
	verifierPrompt, _, err := r.renderRolePrompt(prompt.RoleVerifier, verificationBody, "")
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "verifier_prompt", err)
	}
	if _, err := r.records.WriteImmutable(paths.VerifierPrompt, verifierPrompt); err != nil {
		return r.operationalFailure(ctx, task, attempt, "verifier_prompt", err)
	}
	if err := r.store.PrepareVerifier(
		ctx, attempt.ID, paths.VerifierStdout, paths.VerifierStderr,
	); err != nil {
		return r.operationalFailure(ctx, task, attempt, "verifier_prepare", err)
	}
	verifierResult, err := r.invoke(
		ctx, r.verifier, r.verifierRuntime, task, attempt,
		verifierPrompt, paths.VerifierPrompt, paths.VerifierStdout, paths.VerifierStderr,
	)
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "verifier_launch", err)
	}
	if verifierResult.ExitCode != 0 {
		return r.operationalFailure(
			ctx, task, attempt, "verifier_exit",
			fmt.Errorf("verifier exited with code %d", verifierResult.ExitCode),
		)
	}
	output, err := os.Open(paths.VerifierStdout)
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "verifier_output", err)
	}
	verification, parseErr := ParseVerification(
		output,
		attempt.AttemptNumber < task.MaxAttempts,
	)
	closeErr := output.Close()
	if parseErr != nil {
		return r.operationalFailure(ctx, task, attempt, "verifier_response", parseErr)
	}
	if closeErr != nil {
		return r.operationalFailure(ctx, task, attempt, "verifier_output", closeErr)
	}
	report, err := r.records.WriteVerification(paths, verification)
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "verification_record", err)
	}
	now := r.now()
	return r.store.FinishVerification(ctx, storepkg.FinishVerificationInput{
		TaskID:       task.ID,
		AttemptID:    attempt.ID,
		Verification: verification,
		ReportPath:   report.Path,
		NextRunAt:    now.Add(r.retryDelay(attempt.AttemptNumber)),
		FinishedAt:   now,
	})
}

func (r *Runner) invoke(
	ctx context.Context,
	adapter runtimepkg.Adapter,
	config RoleRuntime,
	task domain.TaskRecord,
	attempt domain.Attempt,
	rendered []byte,
	promptPath string,
	stdoutPath string,
	stderrPath string,
) (runtimepkg.Result, error) {
	args, err := runtimepkg.ExpandArgs(config.Args, runtimepkg.TemplateValues{
		Prompt:     string(rendered),
		PromptFile: promptPath,
		Workspace:  task.ProjectPath,
		TaskID:     task.ID,
		Attempt:    strconv.Itoa(attempt.AttemptNumber),
		Model:      task.Model,
	})
	if err != nil {
		return runtimepkg.Result{}, err
	}
	return adapter.Run(ctx, runtimepkg.Invocation{
		Command:     config.Command,
		Args:        args,
		WorkingDir:  task.ProjectPath,
		Environment: config.Environment,
		Timeout:     config.Timeout,
		StdoutPath:  stdoutPath,
		StderrPath:  stderrPath,
	})
}

func (r *Runner) renderRolePrompt(
	role prompt.Role,
	body []byte,
	retryFeedback string,
) ([]byte, string, error) {
	orchestrator, err := os.ReadFile(filepath.Join(r.records.Paths().Prompts, "orchestrator.md"))
	if err != nil {
		return nil, "", err
	}
	rolePolicy, err := os.ReadFile(filepath.Join(r.records.Paths().Prompts, string(role)+".md"))
	if err != nil {
		return nil, "", err
	}
	content, hash := prompt.Render(prompt.RenderInput{
		Role: role, Orchestrator: orchestrator, RolePolicy: rolePolicy,
		Body: body, RetryFeedback: retryFeedback,
	})
	return content, hash, nil
}

func (r *Runner) writeProcessResult(path string, result runtimepkg.Result) error {
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	_, err = r.records.WriteImmutable(path, content)
	return err
}

func (r *Runner) operationalFailure(
	ctx context.Context,
	task domain.TaskRecord,
	attempt domain.Attempt,
	class string,
	cause error,
) error {
	now := r.now()
	stateErr := r.store.RecordOperationalFailure(ctx, storepkg.OperationalFailureInput{
		TaskID:       task.ID,
		AttemptID:    attempt.ID,
		FailureClass: class,
		FinishedAt:   now,
		NextRunAt:    now.Add(r.retryDelay(attempt.AttemptNumber)),
	})
	if stateErr != nil {
		return errors.Join(cause, stateErr)
	}
	return cause
}

func discoverArtifacts(project string, outputs []domain.Output) (
	[]artifactEvidence,
	[]storepkg.ArtifactRecord,
) {
	evidence := make([]artifactEvidence, 0, len(outputs))
	records := make([]storepkg.ArtifactRecord, 0, len(outputs))
	cleanProject := filepath.Clean(project)
	for _, output := range outputs {
		item := artifactEvidence{Path: output.Path}
		if filepath.IsAbs(output.Path) {
			item.Status = "rejected"
			item.Reason = "absolute output path is not allowed"
			evidence = append(evidence, item)
			continue
		}
		candidate := filepath.Join(cleanProject, filepath.Clean(output.Path))
		if !inside(cleanProject, candidate) {
			item.Status = "rejected"
			item.Reason = "output path escapes project"
			evidence = append(evidence, item)
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			item.Status = "missing"
			item.Reason = err.Error()
			evidence = append(evidence, item)
			continue
		}
		if !inside(cleanProject, resolved) {
			item.Status = "rejected"
			item.Reason = "resolved output path escapes project"
			evidence = append(evidence, item)
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			item.Status = "missing"
			if err != nil {
				item.Reason = err.Error()
			} else {
				item.Reason = "output is not a regular file"
			}
			evidence = append(evidence, item)
			continue
		}
		content, err := os.ReadFile(resolved)
		if err != nil {
			item.Status = "unreadable"
			item.Reason = err.Error()
			evidence = append(evidence, item)
			continue
		}
		sum := sha256.Sum256(content)
		item.Status = "present"
		item.AbsolutePath = resolved
		item.SHA256 = hex.EncodeToString(sum[:])
		item.SizeBytes = info.Size()
		evidence = append(evidence, item)
		records = append(records, storepkg.ArtifactRecord{
			RelativePath: output.Path,
			AbsolutePath: resolved,
			SHA256:       item.SHA256,
			SizeBytes:    item.SizeBytes,
		})
	}
	return evidence, records
}

func inside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != ".." && !filepath.IsAbs(relative) &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func marshalYAML(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
