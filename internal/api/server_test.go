package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/heruujoko/piramid/internal/app"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/intake"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

type fakeApplication struct {
	draft       intake.Draft
	tasks       []domain.TaskView
	workers     []app.WorkerView
	events      []storepkg.Event
	logContent  []byte
	confirmed   string
	rejected    string
	retried     string
	cancelled   string
	lastEventID int64
	eventCalls  int

	// Phase 6 test data
	loops      []domain.LoopView
	fires      []domain.FireView
	gates      []domain.GateSummary
	gateDetail domain.GateDetail
	gateError  error
	resolved   string
	webhookCalls []webhookCall
}

type webhookCall struct {
	eventType string
	signature string
	payload   []byte
}

type cancelOnFlushWriter struct {
	header http.Header
	body   bytes.Buffer
	cancel context.CancelFunc
	mu     sync.Mutex
	status int
}

func (w *cancelOnFlushWriter) Header() http.Header {
	return w.header
}

func (w *cancelOnFlushWriter) WriteHeader(status int) {
	w.status = status
}

func (w *cancelOnFlushWriter) Write(content []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.Write(content)
}

func (w *cancelOnFlushWriter) Flush() {
	w.cancel()
}

func (w *cancelOnFlushWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}

func (f *fakeApplication) Health(context.Context) error { return nil }
func (f *fakeApplication) DraftGoal(context.Context, intake.DraftRequest) (intake.Draft, error) {
	return f.draft, nil
}
func (f *fakeApplication) ConfirmGoal(_ context.Context, id string) error {
	f.confirmed = id
	return nil
}
func (f *fakeApplication) RejectGoal(_ context.Context, id string) error {
	f.rejected = id
	return nil
}
func (f *fakeApplication) Enqueue(context.Context, domain.Plan) error { return nil }
func (f *fakeApplication) ListTasks(context.Context, storepkg.TaskFilter) ([]domain.TaskView, error) {
	return f.tasks, nil
}
func (f *fakeApplication) GetTask(_ context.Context, id string) (domain.TaskView, error) {
	if len(f.tasks) == 0 || id == "missing" {
		return domain.TaskView{}, app.ErrNotFound
	}
	return f.tasks[0], nil
}
func (f *fakeApplication) RetryTask(_ context.Context, id string, _ bool) error {
	f.retried = id
	return nil
}
func (f *fakeApplication) CancelTask(_ context.Context, id string) error {
	f.cancelled = id
	return nil
}
func (f *fakeApplication) ListWorkers(context.Context) ([]app.WorkerView, error) {
	return f.workers, nil
}
func (f *fakeApplication) ReadAttemptLog(
	context.Context, int64, string, int64, int,
) ([]byte, int64, error) {
	return f.logContent, int64(len(f.logContent)), nil
}
func (f *fakeApplication) ListEvents(_ context.Context, after int64, _ int) ([]storepkg.Event, error) {
	f.lastEventID = after
	f.eventCalls++
	return f.events, nil
}
func (f *fakeApplication) ListLoops(context.Context) ([]domain.LoopView, error) { return f.loops, nil }
func (f *fakeApplication) ListLoopFires(_ context.Context, _ string) ([]domain.FireView, error) {
	return f.fires, nil
}
func (f *fakeApplication) ListOpenGates(context.Context) ([]domain.GateSummary, error) { return f.gates, nil }
func (f *fakeApplication) GetGate(_ context.Context, _ string) (domain.GateDetail, error) {
	if f.gateError != nil {
		return domain.GateDetail{}, f.gateError
	}
	return f.gateDetail, nil
}
func (f *fakeApplication) ResolveGate(_ context.Context, id string, input domain.GateDecisionInput) error {
	switch domain.GateDecision(input.Decision) {
	case domain.GateDecisionApprove, domain.GateDecisionRoute,
		domain.GateDecisionDefer, domain.GateDecisionReject:
	default:
		return app.ErrInvalid
	}
	f.resolved = id
	return nil
}
func (f *fakeApplication) HandleGitHubWebhook(_ context.Context, eventType string, signature string, payload []byte) error {
	f.webhookCalls = append(f.webhookCalls, webhookCall{
		eventType: eventType, signature: signature, payload: payload,
	})
	return nil
}

func TestServerExposesHealthAndTaskEndpoints(t *testing.T) {
	application := &fakeApplication{
		tasks: []domain.TaskView{{TaskRecord: domain.TaskRecord{
			Task: domain.Task{ID: "TASK-1"}, Status: domain.TaskPending,
		}}},
	}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	for _, path := range []string{"/v1/health", "/v1/tasks", "/v1/tasks/TASK-1", "/v1/workers"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s status = %d", path, response.StatusCode)
		}
		if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/json") {
			t.Fatalf("%s content type = %q", path, contentType)
		}
		response.Body.Close()
	}
}

func TestServerHandlesGoalLifecycleAndTaskActions(t *testing.T) {
	application := &fakeApplication{draft: intake.Draft{
		Goal: domain.Goal{ID: "GOAL-1"},
		Plan: domain.Plan{Version: 1, GoalID: "GOAL-1"},
	}}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	postJSON(t, server.URL+"/v1/goals/draft", map[string]any{
		"goal_text": "maintain PR", "project_path": "/tmp/project",
	}, http.StatusCreated)
	postJSON(t, server.URL+"/v1/goals/GOAL-1/confirm", map[string]any{}, http.StatusNoContent)
	postJSON(t, server.URL+"/v1/goals/GOAL-1/reject", map[string]any{}, http.StatusNoContent)
	postJSON(t, server.URL+"/v1/tasks/TASK-1/retry", map[string]any{"override": true}, http.StatusNoContent)
	postJSON(t, server.URL+"/v1/tasks/TASK-1/cancel", map[string]any{}, http.StatusNoContent)

	if application.confirmed != "GOAL-1" || application.rejected != "GOAL-1" ||
		application.retried != "TASK-1" || application.cancelled != "TASK-1" {
		t.Fatalf("actions: %#v", application)
	}
}

func TestServerBoundsLogReads(t *testing.T) {
	application := &fakeApplication{logContent: []byte("hello")}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/attempts/9/logs?stream=stdout&offset=0&limit=999999")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}

	response, err = http.Get(server.URL + "/v1/attempts/9/logs?stream=stderr&offset=0&limit=10")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestServerUsesStableErrorEnvelope(t *testing.T) {
	server := httptest.NewServer(NewServer(&fakeApplication{}))
	defer server.Close()
	response, err := http.Get(server.URL + "/v1/tasks/missing")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var envelope ErrorResponse
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error.Code != "not_found" || envelope.Error.Message == "" {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestServerRejectsOversizedRequest(t *testing.T) {
	server := httptest.NewServer(NewServer(&fakeApplication{}))
	defer server.Close()
	body := strings.NewReader(`{"goal_text":"` + strings.Repeat("x", maxRequestBytes+1) + `"}`)
	response, err := http.Post(server.URL+"/v1/goals/draft", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", response.StatusCode)
	}
}

func TestSSEReplaysFromLastEventID(t *testing.T) {
	application := &fakeApplication{events: []storepkg.Event{{
		ID: 8, EntityType: "task", EntityID: "TASK-1", EventType: "TASK_STARTED",
		PayloadJSON: `{"attempt":1}`, CreatedAt: time.Now().UTC(),
	}}}
	request := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	request.Header.Set("Last-Event-ID", "7")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request = request.WithContext(ctx)
	writer := &cancelOnFlushWriter{header: make(http.Header), cancel: cancel}
	NewServer(application).ServeHTTP(writer, request)
	content := writer.String()
	if application.lastEventID != 7 || !strings.Contains(content, "id: 8") {
		t.Fatalf("last=%d content=%q", application.lastEventID, content)
	}
}

func TestClientDecodesAPIAndErrors(t *testing.T) {
	application := &fakeApplication{tasks: []domain.TaskView{{
		TaskRecord: domain.TaskRecord{Task: domain.Task{ID: "TASK-1"}},
	}}}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()
	client := NewClient(server.URL, time.Second)

	tasks, err := client.ListTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "TASK-1" {
		t.Fatalf("tasks = %#v", tasks)
	}
	_, err = client.GetTask(context.Background(), "missing")
	if err == nil {
		t.Fatal("GetTask() error = nil")
	}
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotFound {
		t.Fatalf("error = %#v", err)
	}
}

func TestClientParsesSSE(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "id: 2\nevent: task\ndata: {\"event_type\":\"TASK_STARTED\"}\n\n")
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client := NewClient(server.URL, time.Second)
	events, errs := client.StreamEvents(context.Background(), 1)
	select {
	case event := <-events:
		if event.ID != 2 || event.EventType != "TASK_STARTED" {
			t.Fatalf("event = %#v", event)
		}
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE")
	}
}

func postJSON(t *testing.T, url string, value any, wantStatus int) {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Post(url, "application/json", bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		scanner := bufio.NewScanner(response.Body)
		scanner.Scan()
		t.Fatalf("%s status = %d, want %d: %s", url, response.StatusCode, wantStatus, scanner.Text())
	}
}

// --- Phase 6 behavioral tests ---

func TestListLoopsReturnsLoopsWithLatestFire(t *testing.T) {
	application := &fakeApplication{
		loops: []domain.LoopView{
			{
				ID:        "pr-review",
				PatternID: "pr-review-pattern",
				Active:    true,
				Cron:      "0 */6 * * *",
				Autonomy:  "L1",
				LatestFire: &domain.FireSummary{
					ID:          "FIRE-01",
					LoopID:      "pr-review",
					Status:      "FIRE_RUNNING",
					ScheduledAt: "2026-07-04T12:00:00Z",
				},
			},
		},
	}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/loops")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	var loops []domain.LoopView
	if err := json.NewDecoder(response.Body).Decode(&loops); err != nil {
		t.Fatal(err)
	}
	if len(loops) != 1 {
		t.Fatalf("len(loops) = %d, want 1", len(loops))
	}
	if loops[0].ID != "pr-review" {
		t.Fatalf("loops[0].ID = %q, want pr-review", loops[0].ID)
	}
	if loops[0].LatestFire == nil || loops[0].LatestFire.ID != "FIRE-01" {
		t.Fatalf("loops[0].LatestFire = %v, want FIRE-01", loops[0].LatestFire)
	}
}

func TestListLoopFiresReturnsFiresForLoop(t *testing.T) {
	application := &fakeApplication{
		fires: []domain.FireView{
			{
				ID:          "FIRE-01",
				LoopID:      "pr-review",
				GoalID:      "GOAL-01",
				Status:      "FIRE_DONE",
				ScheduledAt: "2026-07-04T12:00:00Z",
				StartedAt:   "2026-07-04T12:01:00Z",
				LastError:   "",
			},
			{
				ID:          "FIRE-02",
				LoopID:      "pr-review",
				Status:      "FIRE_RUNNING",
				ScheduledAt: "2026-07-04T18:00:00Z",
			},
		},
	}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/loops/pr-review/fires")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	var fires []domain.FireView
	if err := json.NewDecoder(response.Body).Decode(&fires); err != nil {
		t.Fatal(err)
	}
	if len(fires) != 2 {
		t.Fatalf("len(fires) = %d, want 2", len(fires))
	}
	if fires[0].ID != "FIRE-01" || fires[1].ID != "FIRE-02" {
		t.Fatalf("fires = %v", fires)
	}
}

func TestListGatesReturnsOpenGates(t *testing.T) {
	application := &fakeApplication{
		gates: []domain.GateSummary{
			{
				ID:       "GATE-01",
				Gate:     "review",
				Phase:    "pr-summary",
				FireID:   "FIRE-01",
				LoopID:   "pr-review",
				Summary:  "Needs human review: 3 threads",
				OpenedAt: "2026-07-04T12:05:00Z",
			},
		},
	}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/gates")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	var gates []domain.GateSummary
	if err := json.NewDecoder(response.Body).Decode(&gates); err != nil {
		t.Fatal(err)
	}
	if len(gates) != 1 {
		t.Fatalf("len(gates) = %d, want 1", len(gates))
	}
	if gates[0].ID != "GATE-01" || gates[0].Summary != "Needs human review: 3 threads" {
		t.Fatalf("gates[0] = %v", gates[0])
	}
}

func TestGetGateReturnsDetail(t *testing.T) {
	application := &fakeApplication{
		gateDetail: domain.GateDetail{
			ID:              "GATE-01",
			Gate:            "review",
			Phase:           "pr-summary",
			FireID:          "FIRE-01",
			LoopID:          "pr-review",
			GoalID:          "GOAL-01",
			TaskID:          "TASK-01",
			AttemptID:       "3",
			Summary:         "Needs human review: 3 threads",
			OpenedAt:        "2026-07-04T12:05:00Z",
			DecisionOptions: []string{"approve", "route", "defer", "reject"},
			Threads: []domain.GateThreadView{
				{ID: "t1", Title: "Thread 1", Summary: "Issue in auth module"},
			},
			Body: "## Decision needed\n\nPlease review the threads below.",
		},
	}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/gates/GATE-01")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	var detail domain.GateDetail
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID != "GATE-01" || detail.Gate != "review" {
		t.Fatalf("detail = %v", detail)
	}
	if len(detail.DecisionOptions) != 4 || detail.DecisionOptions[0] != "approve" {
		t.Fatalf("DecisionOptions = %v", detail.DecisionOptions)
	}
	if len(detail.Threads) != 1 || detail.Threads[0].ID != "t1" {
		t.Fatalf("Threads = %v", detail.Threads)
	}
	if detail.Body != "## Decision needed\n\nPlease review the threads below." {
		t.Fatalf("Body = %q", detail.Body)
	}
}

func TestGetGateReturns404ForMissing(t *testing.T) {
	application := &fakeApplication{gateError: app.ErrNotFound}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	response, err := http.Get(server.URL + "/v1/gates/missing")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.StatusCode)
	}
}

func TestResolveGateAcceptsValidDecision(t *testing.T) {
	application := &fakeApplication{}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	postJSON(t, server.URL+"/v1/gates/GATE-01/decision", map[string]any{
		"decision": "approve",
		"note":     "looks good",
	}, http.StatusNoContent)

	if application.resolved != "GATE-01" {
		t.Fatalf("resolved = %q, want GATE-01", application.resolved)
	}
}

func TestResolveGateRejectsInvalidDecision(t *testing.T) {
	application := &fakeApplication{}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	response, err := http.Post(
		server.URL+"/v1/gates/GATE-01/decision",
		"application/json",
		strings.NewReader(`{"decision":"invalid"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	// invalid decision returns 400 from the service layer
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
}

func TestGitHubWebhookRoutesToApplication(t *testing.T) {
	application := &fakeApplication{}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	payload := `{"action":"opened","repository":{"full_name":"owner/repo"}}`
	request, _ := http.NewRequest(
		http.MethodPost,
		server.URL+"/v1/webhooks/github",
		strings.NewReader(payload),
	)
	request.Header.Set("X-GitHub-Event", "pull_request")
	request.Header.Set("X-Hub-Signature-256", "sha256=abc123")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if len(application.webhookCalls) != 1 {
		t.Fatalf("webhookCalls = %d, want 1", len(application.webhookCalls))
	}
	call := application.webhookCalls[0]
	if call.eventType != "pull_request" {
		t.Fatalf("eventType = %q", call.eventType)
	}
	if call.signature != "sha256=abc123" {
		t.Fatalf("signature = %q", call.signature)
	}
	if string(call.payload) != payload {
		t.Fatalf("payload = %q", string(call.payload))
	}
}

func TestGitHubWebhookMissingHeadersStillRoutes(t *testing.T) {
	// Headers may be absent — the application layer handles validation.
	application := &fakeApplication{}
	server := httptest.NewServer(NewServer(application))
	defer server.Close()

	r, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/webhooks/github", strings.NewReader(`{}`))
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
}
