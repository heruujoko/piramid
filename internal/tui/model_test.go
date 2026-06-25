package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/heruujoko/piramid/internal/api"
	"github.com/heruujoko/piramid/internal/app"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/intake"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

type fakeClient struct {
	tasks          []domain.TaskView
	workers        []app.WorkerView
	draft          intake.Draft
	taskLoads      int
	workerLoads    int
	detailLoads    int
	logLimit       int
	streamAfter    int64
	draftRequests  int
	confirmedGoals []string
	rejectedGoals  []string
	cancelledTasks []string
}

func (f *fakeClient) ListTasks(context.Context) ([]domain.TaskView, error) {
	f.taskLoads++
	return f.tasks, nil
}
func (f *fakeClient) ListWorkers(context.Context) ([]app.WorkerView, error) {
	f.workerLoads++
	return f.workers, nil
}
func (f *fakeClient) GetTask(context.Context, string) (domain.TaskView, error) {
	f.detailLoads++
	return f.tasks[0], nil
}
func (f *fakeClient) ReadLog(
	_ context.Context, _ int64, _ string, _ int64, limit int,
) (api.LogChunk, error) {
	f.logLimit = limit
	return api.LogChunk{Content: "log", NextOffset: 3}, nil
}
func (f *fakeClient) DraftGoal(context.Context, intake.DraftRequest) (intake.Draft, error) {
	f.draftRequests++
	return f.draft, nil
}
func (f *fakeClient) ConfirmGoal(_ context.Context, id string) error {
	f.confirmedGoals = append(f.confirmedGoals, id)
	return nil
}
func (f *fakeClient) RejectGoal(_ context.Context, id string) error {
	f.rejectedGoals = append(f.rejectedGoals, id)
	return nil
}
func (f *fakeClient) RetryTask(context.Context, string, bool) error { return nil }
func (f *fakeClient) CancelTask(_ context.Context, id string) error {
	f.cancelledTasks = append(f.cancelledTasks, id)
	return nil
}
func (f *fakeClient) StreamEvents(_ context.Context, after int64) (<-chan storepkg.Event, <-chan error) {
	f.streamAfter = after
	events := make(chan storepkg.Event, 1)
	errs := make(chan error, 1)
	events <- storepkg.Event{ID: after + 1, EventType: "TASK_STARTED"}
	close(events)
	close(errs)
	return events, errs
}

func baseTask() domain.TaskView {
	return domain.TaskView{TaskRecord: domain.TaskRecord{
		Task:   domain.Task{ID: "TASK-1", Title: "Work", MaxAttempts: 3},
		Status: domain.TaskPending,
	}}
}

func runBatch(message tea.Msg) {
	batch, ok := message.(tea.BatchMsg)
	if !ok {
		return
	}
	for _, command := range batch {
		if command != nil {
			_ = command()
		}
	}
}

func TestInitRequestsTasksWorkersAndEvents(t *testing.T) {
	client := &fakeClient{tasks: []domain.TaskView{baseTask()}}
	model := NewModel(client)
	command := model.Init()
	if command == nil {
		t.Fatal("Init() command = nil")
	}
	runBatch(command())
	if client.taskLoads != 1 || client.workerLoads != 1 || client.streamAfter != 0 {
		t.Fatalf("loads tasks=%d workers=%d stream=%d",
			client.taskLoads, client.workerLoads, client.streamAfter)
	}
}

func TestMessagesUpdateRowsAndPreserveSelection(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client)
	model.selected = 0
	updated, _ := model.Update(tasksLoadedMsg{tasks: []domain.TaskView{baseTask()}})
	next := updated.(Model)
	if len(next.tasks) != 1 || next.selected != 0 {
		t.Fatalf("model = %#v", next)
	}
	updated, _ = next.Update(workersLoadedMsg{workers: []app.WorkerView{{ID: "worker-1"}}})
	next = updated.(Model)
	if len(next.workers) != 1 {
		t.Fatalf("workers = %#v", next.workers)
	}
}

func TestEventTracksLastIDAndReconnectUsesIt(t *testing.T) {
	client := &fakeClient{}
	model := NewModel(client)
	updated, command := model.Update(eventMsg{event: storepkg.Event{ID: 42}})
	next := updated.(Model)
	if next.lastEventID != 42 || command == nil {
		t.Fatalf("last=%d command=%v", next.lastEventID, command)
	}
	runBatch(command())
	if client.streamAfter != 42 {
		t.Fatalf("stream after = %d", client.streamAfter)
	}
}

func TestEnterLoadsSelectedTaskDetails(t *testing.T) {
	client := &fakeClient{tasks: []domain.TaskView{baseTask()}}
	model := NewModel(client)
	model.tasks = client.tasks
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("detail command = nil")
	}
	_ = command()
	if client.detailLoads != 1 {
		t.Fatalf("detail loads = %d", client.detailLoads)
	}
}

func TestLogTailRequestIsBounded(t *testing.T) {
	client := &fakeClient{tasks: []domain.TaskView{{
		TaskRecord: domain.TaskRecord{Task: domain.Task{ID: "TASK-1"}},
		Attempts:   []domain.Attempt{{ID: 9}},
	}}}
	model := NewModel(client)
	model.detail = &client.tasks[0]
	model.view = viewLogs
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if command == nil {
		t.Fatal("log command = nil")
	}
	_ = command()
	if client.logLimit != maxLogChunk {
		t.Fatalf("log limit = %d", client.logLimit)
	}
}

func TestResizeKeepsSelection(t *testing.T) {
	model := NewModel(&fakeClient{})
	model.selected = 2
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	next := updated.(Model)
	if next.width != 120 || next.height != 40 || next.selected != 2 {
		t.Fatalf("model = %#v", next)
	}
}

func TestGoalWorkflowUsesDraftAndConfirmAPI(t *testing.T) {
	client := &fakeClient{draft: intake.Draft{Goal: domain.Goal{ID: "GOAL-1"}}}
	model := NewModel(client)
	model.goalStage = goalStageProject
	model.projectInput = "/tmp/project"
	model.goalText = "Maintain PR"
	command := model.draftGoalCmd()
	message := command()
	updated, _ := model.Update(message)
	next := updated.(Model)
	if client.draftRequests != 1 || next.goalStage != goalStagePreview {
		t.Fatalf("requests=%d stage=%d", client.draftRequests, next.goalStage)
	}
	_, confirm := next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if confirm == nil {
		t.Fatal("confirm command = nil")
	}
	_ = confirm()
	if len(client.confirmedGoals) != 1 || client.confirmedGoals[0] != "GOAL-1" {
		t.Fatalf("confirmed = %v", client.confirmedGoals)
	}
}

func TestStateChangingKeysRequireConfirmation(t *testing.T) {
	client := &fakeClient{tasks: []domain.TaskView{baseTask()}}
	model := NewModel(client)
	model.tasks = client.tasks
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	next := updated.(Model)
	if command != nil || next.pendingAction != "cancel" || len(client.cancelledTasks) != 0 {
		t.Fatalf("pending=%q command=%v cancelled=%v",
			next.pendingAction, command, client.cancelledTasks)
	}
	updated, command = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if command == nil {
		t.Fatal("confirmed action command = nil")
	}
	_ = updated
	_ = command()
	if len(client.cancelledTasks) != 1 || client.cancelledTasks[0] != "TASK-1" {
		t.Fatalf("cancelled = %v", client.cancelledTasks)
	}
}
