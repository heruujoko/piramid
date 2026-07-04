package engine

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"github.com/heruujoko/piramid/internal/gate"
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
	GetTaskGateLinkage(context.Context, string) (domain.TaskGateLinkage, error)
	CreateGate(context.Context, domain.Gate) (domain.Gate, error)
	UpdateFireStatus(context.Context, string, domain.FireStatus, time.Time) error
}

type RoleRuntime struct {
	Command     string
	Args        []string
	Environment []string
	Timeout     time.Duration
}

// GateExitCode is the exit code an executor uses to signal a mid-run gate.
// The runner intercepts it before verification and opens a gate row instead.
const GateExitCode = 42

type RunnerConfig struct {
	Store           RunnerStore
	Records         *records.Store
	Executor        runtimepkg.Adapter
	Verifier        runtimepkg.Adapter
	ExecutorRuntime RoleRuntime
	VerifierRuntime RoleRuntime
	Now             func() time.Time
	RetryDelay      func(attemptNumber int) time.Duration
	GateIDGenerator func(now time.Time) string
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
	gateIDGenerator func(time.Time) string
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
	gateIDGenerator := config.GateIDGenerator
	if gateIDGenerator == nil {
		gateIDGenerator = defaultGateIDGenerator
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
		gateIDGenerator: gateIDGenerator,
	}
}

// defaultGateIDGenerator produces a gate ID that is unique even when two
// executor attempts open a gate in the same wall-clock second. It keeps the
// sortable, human-readable GATE-YYYYMMDDHHMMSS prefix (mirroring the looprunner
// ID convention without a loop ID, since the runner does not carry one) and
// appends a short crypto-random suffix so concurrent attempts never collide on
// the gates.id primary key, which previously caused CreateGate to fail and the
// runner to record a spurious gate_create operational failure.
func defaultGateIDGenerator(now time.Time) string {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		// rand.Read failing is exceptional; fall back to nanoseconds so the
		// ID still differs across near-simultaneous calls instead of
		// degrading to second-precision collisions.
		return fmt.Sprintf("GATE-%s-%09d", now.Format("20060102150405"), now.Nanosecond())
	}
	return fmt.Sprintf("GATE-%s-%x", now.Format("20060102150405"), suffix[:])
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

	// Derive the authoritative fire/goal linkage from store state once: the
	// executor receives PIRAMID_FIRE_ID/PIRAMID_GOAL_ID/PIRAMID_LOOP_ID so a real
	// executor can populate gate.context.md without guessing, and handleGate
	// later trusts THIS linkage (not the executor-written context) to park the
	// correct fire -- eliminating the risk of parking the wrong fire.
	linkage, err := r.store.GetTaskGateLinkage(ctx, task.ID)
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "gate_linkage", err)
	}

	executorRuntime := r.executorRuntime
	executorRuntime.Timeout = task.Timeout
	// Seed from the process environment when the configured executor env is empty,
	// mirroring the command adapter's inherit-on-empty fallback. Appending the gate
	// path would otherwise make the slice non-empty and drop HOME/PATH/credentials.
	baseEnv := executorRuntime.Environment
	if len(baseEnv) == 0 {
		baseEnv = os.Environ()
	}
	gateEnv := append(
		append([]string(nil), baseEnv...),
		"PIRAMID_GATE_CONTEXT="+paths.GateContext,
	)
	if linkage.FireID != "" {
		gateEnv = append(gateEnv, "PIRAMID_FIRE_ID="+linkage.FireID)
	}
	if linkage.LoopID != "" {
		gateEnv = append(gateEnv, "PIRAMID_LOOP_ID="+linkage.LoopID)
	}
	if linkage.GoalID != "" {
		gateEnv = append(gateEnv, "PIRAMID_GOAL_ID="+linkage.GoalID)
	}
	executorRuntime.Environment = gateEnv
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

	// Exit 42 signals a mid-run gate: the executor wrote a gate.context.md at
	// PIRAMID_GATE_CONTEXT and wants a human/bot decision before continuing.
	// Skip verification entirely; open a gate row and park the fire as gated.
	if executorResult.ExitCode == GateExitCode {
		return r.handleGate(ctx, task, attempt, paths, linkage)
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

// handleGate processes an exit-42 executor result: parse the gate context the
// executor wrote at PIRAMID_GATE_CONTEXT, create an open gate row linked to the
// fire/goal/task/attempt, and move the fire into FIRE_GATED. Returns nil on
// success so the worker is released without invoking the verifier.
func (r *Runner) handleGate(
	ctx context.Context,
	task domain.TaskRecord,
	attempt domain.Attempt,
	paths records.AttemptPaths,
	linkage domain.TaskGateLinkage,
) error {
	content, err := os.ReadFile(paths.GateContext)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return r.operationalFailure(ctx, task, attempt, "gate_context_invalid",
				fmt.Errorf("executor exited %d but wrote no gate.context.md at %s: %w",
					GateExitCode, paths.GateContext, err))
		}
		return r.operationalFailure(ctx, task, attempt, "gate_context_invalid", err)
	}
	gateContext, err := gate.ParseContext(string(content))
	if err != nil {
		return r.operationalFailure(ctx, task, attempt, "gate_context_invalid", err)
	}
	now := r.now()
	// The gate row's fire/goal foreign keys come from the store-derived
	// linkage (authoritative), not from gateContext (executor-supplied and
	// therefore untrusted for linkage). The parsed gateContext is still stored
	// verbatim in gate.Context as an audit record of what the executor saw.
	fireID := linkage.FireID
	if fireID == "" {
		fireID = gateContext.FireID
	}
	goalID := linkage.GoalID
	if goalID == "" {
		goalID = gateContext.GoalID
	}
	gateRow := domain.Gate{
		ID:          r.gateIDGenerator(now),
		FireID:      fireID,
		GoalID:      goalID,
		TaskID:      task.ID,
		AttemptID:   strconv.FormatInt(attempt.ID, 10),
		Status:      domain.GateOpen,
		ContextPath: paths.GateContext,
		Context:     gateContext,
		OpenedAt:    now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := r.store.CreateGate(ctx, gateRow); err != nil {
		return r.operationalFailure(ctx, task, attempt, "gate_create", err)
	}
	// Only park a fire when the task has one (loop-scheduler admits fires;
	// manually-admitted tasks have no fire to park). Trusting linkage.FireID
	// here means a mis-identified or omitted fire_id in gate.context.md can no
	// longer park the wrong fire or emit a spurious fire_gated failure.
	if linkage.FireID != "" {
		if err := r.store.UpdateFireStatus(ctx, linkage.FireID, domain.FireGated, now); err != nil {
			return r.operationalFailure(ctx, task, attempt, "fire_gated", err)
		}
	}
	return nil
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
