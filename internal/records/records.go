package records

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/home"
	"gopkg.in/yaml.v3"
)

type FileRecord struct {
	Path   string
	SHA256 string
	Size   int64
}

type GoalPaths struct {
	Root          string
	Goal          string
	PlannerPrompt string
	PlannerStdout string
	PlannerStderr string
	Plan          string
}

type AttemptPaths struct {
	Root           string
	ExecutorPrompt string
	Stdout         string
	Stderr         string
	Process        string
	VerifierPrompt string
	VerifierStdout string
	VerifierStderr string
	Verification   string
	Artifacts      string
	GateContext    string
}

type Store struct {
	paths home.Paths
}

func New(paths home.Paths) *Store {
	return &Store{paths: paths}
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validateIdentifier(id string) error {
	if !identifierPattern.MatchString(id) || id == "." || id == ".." {
		return fmt.Errorf("invalid identifier %q", id)
	}
	return nil
}

func (s *Store) Paths() home.Paths {
	return s.paths
}

func (s *Store) GoalPaths(goalID string) (GoalPaths, error) {
	if err := validateIdentifier(goalID); err != nil {
		return GoalPaths{}, err
	}
	root := filepath.Join(s.paths.Goals, goalID)
	return GoalPaths{
		Root:          root,
		Goal:          filepath.Join(root, "goal.yaml"),
		PlannerPrompt: filepath.Join(root, "planner-prompt.md"),
		PlannerStdout: filepath.Join(root, "planner-stdout.log"),
		PlannerStderr: filepath.Join(root, "planner-stderr.log"),
		Plan:          filepath.Join(root, "generated-plan.yaml"),
	}, nil
}

func (s *Store) WriteGoal(goal domain.Goal) (FileRecord, error) {
	paths, err := s.GoalPaths(goal.ID)
	if err != nil {
		return FileRecord{}, err
	}
	content, err := marshalYAML(goal)
	if err != nil {
		return FileRecord{}, err
	}
	return s.writeImmutable(paths.Goal, content)
}

func (s *Store) WritePlan(goalID string, plan domain.Plan) (FileRecord, error) {
	paths, err := s.GoalPaths(goalID)
	if err != nil {
		return FileRecord{}, err
	}
	content, err := marshalYAML(plan)
	if err != nil {
		return FileRecord{}, err
	}
	return s.writeImmutable(paths.Plan, content)
}

func (s *Store) WriteTask(task domain.Task) (FileRecord, error) {
	if err := validateIdentifier(task.ID); err != nil {
		return FileRecord{}, err
	}
	content, err := marshalYAML(task)
	if err != nil {
		return FileRecord{}, err
	}
	return s.writeImmutable(filepath.Join(s.paths.Tasks, task.ID, "task.yaml"), content)
}

func (s *Store) CreateAttempt(taskID string, number int) (AttemptPaths, error) {
	if err := validateIdentifier(taskID); err != nil {
		return AttemptPaths{}, err
	}
	if number < 1 {
		return AttemptPaths{}, fmt.Errorf("attempt number must be positive")
	}
	root := filepath.Join(s.paths.Attempts, taskID, fmt.Sprintf("%04d", number))
	if err := s.ensureInsideHome(root); err != nil {
		return AttemptPaths{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return AttemptPaths{}, err
	}
	artifactRoot := filepath.Join(s.paths.Artifacts, taskID, fmt.Sprintf("%04d", number))
	if err := os.MkdirAll(artifactRoot, 0o700); err != nil {
		return AttemptPaths{}, err
	}
	return AttemptPaths{
		Root:           root,
		ExecutorPrompt: filepath.Join(root, "executor-prompt.md"),
		Stdout:         filepath.Join(root, "stdout.log"),
		Stderr:         filepath.Join(root, "stderr.log"),
		Process:        filepath.Join(root, "process.json"),
		VerifierPrompt: filepath.Join(root, "verifier-prompt.md"),
		VerifierStdout: filepath.Join(root, "verifier-stdout.log"),
		VerifierStderr: filepath.Join(root, "verifier-stderr.log"),
		Verification:   filepath.Join(root, "verification.yaml"),
		Artifacts:      artifactRoot,
		GateContext:    filepath.Join(root, "gate.context.md"),
	}, nil
}

func (s *Store) WriteVerification(
	paths AttemptPaths,
	verification domain.Verification,
) (FileRecord, error) {
	content, err := marshalYAML(verification)
	if err != nil {
		return FileRecord{}, err
	}
	return s.writeImmutable(paths.Verification, content)
}

func (s *Store) WriteImmutable(path string, content []byte) (FileRecord, error) {
	return s.writeImmutable(path, content)
}

func (s *Store) writeImmutable(path string, content []byte) (FileRecord, error) {
	if err := s.ensureInsideHome(path); err != nil {
		return FileRecord{}, err
	}
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, content) {
			return FileRecord{}, fmt.Errorf("immutable record %s already exists with different content", path)
		}
		return record(path, existing), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return FileRecord{}, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return FileRecord{}, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return FileRecord{}, err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return FileRecord{}, err
	}
	if _, err := file.Write(content); err != nil {
		file.Close()
		return FileRecord{}, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return FileRecord{}, err
	}
	if err := file.Close(); err != nil {
		return FileRecord{}, err
	}
	if err := os.Rename(temp, path); err != nil {
		return FileRecord{}, err
	}
	return record(path, content), nil
}

func (s *Store) ensureInsideHome(path string) error {
	cleanRoot := filepath.Clean(s.paths.Root)
	cleanPath := filepath.Clean(path)
	relative, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return err
	}
	if relative == ".." || filepath.IsAbs(relative) ||
		len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("path %s escapes Pi-Ramid home", path)
	}
	return nil
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

func record(path string, content []byte) FileRecord {
	return FileRecord{Path: path, SHA256: Hash(content), Size: int64(len(content))}
}

func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
