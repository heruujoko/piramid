package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/heruujoko/piramid/internal/api"
	"github.com/heruujoko/piramid/internal/app"
	"github.com/heruujoko/piramid/internal/domain"
	"github.com/heruujoko/piramid/internal/intake"
	storepkg "github.com/heruujoko/piramid/internal/store"
)

const maxLogChunk = 64 << 10

type Client interface {
	ListTasks(context.Context) ([]domain.TaskView, error)
	ListWorkers(context.Context) ([]app.WorkerView, error)
	GetTask(context.Context, string) (domain.TaskView, error)
	ReadLog(context.Context, int64, string, int64, int) (api.LogChunk, error)
	DraftGoal(context.Context, intake.DraftRequest) (intake.Draft, error)
	ConfirmGoal(context.Context, string) error
	RejectGoal(context.Context, string) error
	RetryTask(context.Context, string, bool) error
	CancelTask(context.Context, string) error
	StreamEvents(context.Context, int64) (<-chan storepkg.Event, <-chan error)
}

type view int

const (
	viewQueue view = iota
	viewWorkers
	viewCompleted
	viewFailed
	viewDetails
	viewAttempts
	viewVerification
	viewLogs
)

var viewNames = []string{
	"Queue", "Workers", "Completed", "Failed/Blocked",
	"Task Details", "Attempts", "Verification", "Logs",
}

type goalStage int

const (
	goalNone goalStage = iota
	goalStageProject
	goalStageText
	goalStagePreview
)

type Model struct {
	client        Client
	tasks         []domain.TaskView
	workers       []app.WorkerView
	detail        *domain.TaskView
	selected      int
	view          view
	width         int
	height        int
	lastEventID   int64
	logStream     string
	logOffset     int64
	logContent    string
	status        string
	err           error
	goalStage     goalStage
	projectInput  string
	goalText      string
	draft         *intake.Draft
	pendingAction string
	pendingTaskID string
}

func NewModel(client Client) Model {
	return Model{client: client, logStream: "stdout"}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadTasksCmd(), m.loadWorkersCmd(), m.subscribeCmd())
}

type tasksLoadedMsg struct {
	tasks []domain.TaskView
	err   error
}

type workersLoadedMsg struct {
	workers []app.WorkerView
	err     error
}

type detailLoadedMsg struct {
	task domain.TaskView
	err  error
}

type logLoadedMsg struct {
	chunk api.LogChunk
	err   error
}

type eventMsg struct {
	event storepkg.Event
}

type reconnectMsg struct {
	err error
}

type draftLoadedMsg struct {
	draft intake.Draft
	err   error
}

type actionMsg struct {
	status string
	err    error
}

func (m Model) loadTasksCmd() tea.Cmd {
	return func() tea.Msg {
		tasks, err := m.client.ListTasks(context.Background())
		return tasksLoadedMsg{tasks: tasks, err: err}
	}
}

func (m Model) loadWorkersCmd() tea.Cmd {
	return func() tea.Msg {
		workers, err := m.client.ListWorkers(context.Background())
		return workersLoadedMsg{workers: workers, err: err}
	}
}

func (m Model) loadDetailCmd(taskID string) tea.Cmd {
	return func() tea.Msg {
		task, err := m.client.GetTask(context.Background(), taskID)
		return detailLoadedMsg{task: task, err: err}
	}
}

func (m Model) loadLogCmd() tea.Cmd {
	if m.detail == nil || len(m.detail.Attempts) == 0 {
		return nil
	}
	attemptID := m.detail.Attempts[len(m.detail.Attempts)-1].ID
	stream := m.logStream
	offset := m.logOffset
	return func() tea.Msg {
		chunk, err := m.client.ReadLog(
			context.Background(), attemptID, stream, offset, maxLogChunk,
		)
		return logLoadedMsg{chunk: chunk, err: err}
	}
}

func (m Model) subscribeCmd() tea.Cmd {
	after := m.lastEventID
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		events, errs := m.client.StreamEvents(ctx, after)
		select {
		case event, ok := <-events:
			if !ok {
				return reconnectMsg{}
			}
			return eventMsg{event: event}
		case err, ok := <-errs:
			if !ok {
				return reconnectMsg{}
			}
			return reconnectMsg{err: err}
		}
	}
}

func (m Model) draftGoalCmd() tea.Cmd {
	request := intake.DraftRequest{ProjectPath: m.projectInput, GoalText: m.goalText}
	return func() tea.Msg {
		draft, err := m.client.DraftGoal(context.Background(), request)
		return draftLoadedMsg{draft: draft, err: err}
	}
}
