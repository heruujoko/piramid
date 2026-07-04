package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/heruujoko/piramid/internal/app"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/intake"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

const (
	maxRequestBytes = 1 << 20
	maxLogRead      = 64 << 10
)

type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type Server struct {
	application app.Application
}

func NewServer(application app.Application) http.Handler {
	return &Server{application: application}
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	path := strings.Trim(request.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "v1" {
		writeError(writer, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	switch {
	case request.Method == http.MethodGet && path == "v1/health":
		s.health(writer, request)
	case request.Method == http.MethodPost && path == "v1/goals/draft":
		s.draftGoal(writer, request)
	case request.Method == http.MethodPost && len(parts) == 4 &&
		parts[1] == "goals" && parts[3] == "confirm":
		s.goalAction(writer, request, parts[2], true)
	case request.Method == http.MethodPost && len(parts) == 4 &&
		parts[1] == "goals" && parts[3] == "reject":
		s.goalAction(writer, request, parts[2], false)
	case request.Method == http.MethodPost && path == "v1/tasks":
		s.enqueue(writer, request)
	case request.Method == http.MethodGet && path == "v1/tasks":
		s.listTasks(writer, request)
	case request.Method == http.MethodGet && len(parts) == 3 && parts[1] == "tasks":
		s.getTask(writer, request, parts[2])
	case request.Method == http.MethodPost && len(parts) == 4 &&
		parts[1] == "tasks" && parts[3] == "retry":
		s.retryTask(writer, request, parts[2])
	case request.Method == http.MethodPost && len(parts) == 4 &&
		parts[1] == "tasks" && parts[3] == "cancel":
		s.cancelTask(writer, request, parts[2])
	case request.Method == http.MethodGet && path == "v1/workers":
		s.listWorkers(writer, request)
	case request.Method == http.MethodGet && len(parts) == 4 &&
		parts[1] == "attempts" && parts[3] == "logs":
		s.readLog(writer, request, parts[2])
	case request.Method == http.MethodGet && path == "v1/events":
		s.events(writer, request)
	// --- Loop/Fire endpoints ---
	case request.Method == http.MethodGet && path == "v1/loops":
		s.listLoops(writer, request)
	case request.Method == http.MethodGet && len(parts) == 4 &&
		parts[1] == "loops" && parts[3] == "fires":
		s.listLoopFires(writer, request, parts[2])
	// --- Gate endpoints ---
	case request.Method == http.MethodGet && path == "v1/gates":
		s.listGates(writer, request)
	case request.Method == http.MethodGet && len(parts) == 3 && parts[1] == "gates":
		s.getGate(writer, request, parts[2])
	case request.Method == http.MethodPost && len(parts) == 4 &&
		parts[1] == "gates" && parts[3] == "decision":
		s.resolveGate(writer, request, parts[2])
	// --- GitHub webhook ---
	case request.Method == http.MethodPost && path == "v1/webhooks/github":
		s.handleGitHubWebhook(writer, request)
	default:
		writeError(writer, http.StatusNotFound, "not_found", "endpoint not found")
	}
}

func (s *Server) health(writer http.ResponseWriter, request *http.Request) {
	if err := s.application.Health(request.Context()); err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) draftGoal(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		GoalText    string `json:"goal_text"`
		ProjectPath string `json:"project_path"`
	}
	if err := decodeJSON(writer, request, &input); err != nil {
		return
	}
	draft, err := s.application.DraftGoal(request.Context(), intake.DraftRequest{
		GoalText: input.GoalText, ProjectPath: input.ProjectPath,
	})
	if err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, draft)
}

func (s *Server) goalAction(
	writer http.ResponseWriter,
	request *http.Request,
	goalID string,
	confirm bool,
) {
	var err error
	if confirm {
		err = s.application.ConfirmGoal(request.Context(), goalID)
	} else {
		err = s.application.RejectGoal(request.Context(), goalID)
	}
	if err != nil {
		handleError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) enqueue(writer http.ResponseWriter, request *http.Request) {
	var plan domain.Plan
	if err := decodeJSON(writer, request, &plan); err != nil {
		return
	}
	if err := s.application.Enqueue(request.Context(), plan); err != nil {
		handleError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusCreated)
}

func (s *Server) listTasks(writer http.ResponseWriter, request *http.Request) {
	tasks, err := s.application.ListTasks(request.Context(), storepkg.TaskFilter{})
	if err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, tasks)
}

func (s *Server) getTask(writer http.ResponseWriter, request *http.Request, taskID string) {
	task, err := s.application.GetTask(request.Context(), taskID)
	if err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, task)
}

func (s *Server) retryTask(writer http.ResponseWriter, request *http.Request, taskID string) {
	var input struct {
		Override bool `json:"override"`
	}
	if err := decodeJSON(writer, request, &input); err != nil {
		return
	}
	if err := s.application.RetryTask(request.Context(), taskID, input.Override); err != nil {
		handleError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) cancelTask(writer http.ResponseWriter, request *http.Request, taskID string) {
	if err := decodeOptionalObject(writer, request); err != nil {
		return
	}
	if err := s.application.CancelTask(request.Context(), taskID); err != nil {
		handleError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (s *Server) listWorkers(writer http.ResponseWriter, request *http.Request) {
	workers, err := s.application.ListWorkers(request.Context())
	if err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, workers)
}

func (s *Server) readLog(writer http.ResponseWriter, request *http.Request, attemptText string) {
	attemptID, err := strconv.ParseInt(attemptText, 10, 64)
	if err != nil || attemptID < 1 {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid attempt ID")
		return
	}
	stream := request.URL.Query().Get("stream")
	if stream != "stdout" && stream != "stderr" &&
		stream != "verifier-stdout" && stream != "verifier-stderr" {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid log stream")
		return
	}
	offset, err := strconv.ParseInt(defaultQuery(request, "offset", "0"), 10, 64)
	if err != nil || offset < 0 {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid log offset")
		return
	}
	limit, err := strconv.Atoi(defaultQuery(request, "limit", "65536"))
	if err != nil || limit < 1 || limit > maxLogRead {
		writeError(writer, http.StatusBadRequest, "invalid_request", "log limit must be 1..65536")
		return
	}
	content, nextOffset, err := s.application.ReadAttemptLog(
		request.Context(), attemptID, stream, offset, limit,
	)
	if err != nil {
		handleError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"content": string(content), "next_offset": nextOffset,
	})
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeError(writer, http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
		} else {
			writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
		}
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(writer, http.StatusBadRequest, "invalid_request", "request must contain one JSON value")
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func decodeOptionalObject(writer http.ResponseWriter, request *http.Request) error {
	if request.Body == nil || request.ContentLength == 0 {
		return nil
	}
	var input map[string]any
	return decodeJSON(writer, request, &input)
}

func defaultQuery(request *http.Request, key, fallback string) string {
	value := request.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	return value
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	var response ErrorResponse
	response.Error.Code = code
	response.Error.Message = message
	writeJSON(writer, status, response)
}

func handleError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, app.ErrNotFound):
		writeError(writer, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, app.ErrConflict):
		writeError(writer, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, app.ErrInvalid):
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		writeError(writer, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
