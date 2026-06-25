package cli

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/heruujoko/piramid/internal/app"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/intake"
)

type commandAPI struct {
	mu    sync.Mutex
	paths []string
}

func (a *commandAPI) handler(writer http.ResponseWriter, request *http.Request) {
	a.mu.Lock()
	a.paths = append(a.paths, request.URL.Path)
	a.mu.Unlock()
	writer.Header().Set("Content-Type", "application/json")
	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/v1/goals/draft":
		_ = json.NewEncoder(writer).Encode(intake.Draft{
			Goal: domain.Goal{ID: "GOAL-1"},
			Plan: domain.Plan{Version: 1, GoalID: "GOAL-1", Tasks: []domain.Task{{
				ID: "TASK-1", Title: "Maintain PR", ProjectPath: "/tmp/project",
				DOD: []string{"checks pass"}, MaxAttempts: 3, TimeoutText: "1h",
			}}},
		})
	case request.Method == http.MethodGet && request.URL.Path == "/v1/tasks":
		_ = json.NewEncoder(writer).Encode([]domain.TaskView{{TaskRecord: domain.TaskRecord{
			Task: domain.Task{
				ID: "TASK-1", ProjectPath: "/tmp/project", MaxAttempts: 3,
			},
			Status: domain.TaskPending, AttemptCount: 1,
		}}})
	case request.Method == http.MethodGet && request.URL.Path == "/v1/tasks/TASK-1":
		_ = json.NewEncoder(writer).Encode(domain.TaskView{TaskRecord: domain.TaskRecord{
			Task: domain.Task{ID: "TASK-1"}, Status: domain.TaskPending,
		}})
	case request.Method == http.MethodGet && request.URL.Path == "/v1/workers":
		_ = json.NewEncoder(writer).Encode([]app.WorkerView{{
			ID: "pi-worker-01", Status: "running", TaskID: "TASK-1", AttemptID: 9,
		}})
	default:
		writer.WriteHeader(http.StatusNoContent)
	}
}

func commandServer(t *testing.T) (*commandAPI, string, int) {
	t.Helper()
	api := &commandAPI{}
	server := httptest.NewServer(http.HandlerFunc(api.handler))
	t.Cleanup(server.Close)
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return api, host, port
}

func executeCommand(t *testing.T, commandName string, commandArgs []string, stdin string) (string, error) {
	t.Helper()
	root := NewRootCommand()
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append([]string{commandName}, commandArgs...))
	err := root.Execute()
	return output.String(), err
}

func TestGoalCommandPreviewsAndConfirmsWithYes(t *testing.T) {
	api, host, port := commandServer(t)
	output, err := executeCommand(t, "goal", []string{
		"--project", "/tmp/project", "--yes", "--s", host, "--p", strconv.Itoa(port),
		"Maintain this PR",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "TASK-1") || !strings.Contains(output, "Enqueued goal GOAL-1") {
		t.Fatalf("output = %q", output)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if !containsPath(api.paths, "/v1/goals/GOAL-1/confirm") {
		t.Fatalf("paths = %v", api.paths)
	}
}

func TestGoalCommandRejectsOnNegativeConfirmation(t *testing.T) {
	api, host, port := commandServer(t)
	_, err := executeCommand(t, "goal", []string{
		"--project", "/tmp/project", "--s", host, "--p", strconv.Itoa(port),
		"Maintain this PR",
	}, "n\n")
	if err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if !containsPath(api.paths, "/v1/goals/GOAL-1/reject") ||
		containsPath(api.paths, "/v1/goals/GOAL-1/confirm") {
		t.Fatalf("paths = %v", api.paths)
	}
}

func TestEnqueueQueueWorkersInspectRetryAndCancelCommands(t *testing.T) {
	api, host, port := commandServer(t)
	taskPath := filepath.Join(t.TempDir(), "task.yaml")
	taskYAML := `id: TASK-2
title: Direct task
goal: Do work
project_path: /tmp/project
dod: [done]
max_attempts: 3
timeout: 1h
`
	if err := os.WriteFile(taskPath, []byte(taskYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	address := []string{"--s", host, "--p", strconv.Itoa(port)}
	commands := []struct {
		name string
		args []string
		want string
	}{
		{"enqueue", append([]string{taskPath}, address...), "Enqueued 1 task"},
		{"queue", address, "TASK-1"},
		{"workers", address, "pi-worker-01"},
		{"inspect", append([]string{"TASK-1"}, address...), "id: TASK-1"},
		{"retry", append([]string{"TASK-1", "--override"}, address...), "Retry queued"},
		{"cancel", append([]string{"TASK-1"}, address...), "Cancelled"},
	}
	for _, tt := range commands {
		t.Run(tt.name, func(t *testing.T) {
			output, err := executeCommand(t, tt.name, tt.args, "")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, tt.want) {
				t.Fatalf("output = %q, want %q", output, tt.want)
			}
		})
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	for _, path := range []string{
		"/v1/tasks", "/v1/tasks/TASK-1/retry", "/v1/tasks/TASK-1/cancel",
	} {
		if !containsPath(api.paths, path) {
			t.Fatalf("missing path %s in %v", path, api.paths)
		}
	}
}

func TestClientCommandsRequireTaskIDs(t *testing.T) {
	for _, name := range []string{"inspect", "retry", "cancel"} {
		if _, err := executeCommand(t, name, nil, ""); err == nil {
			t.Fatalf("%s accepted missing task ID", name)
		}
	}
}

func TestConnectionErrorIncludesAttemptedAddress(t *testing.T) {
	_, err := executeCommand(t, "queue", []string{"--s", "127.0.0.1", "--p", "1"}, "")
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("error = %v", err)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
