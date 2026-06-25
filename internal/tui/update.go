package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/heruujoko/piramid/internal/domain"
)

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tasksLoadedMsg:
		m.tasks, m.err = msg.tasks, msg.err
		if m.selected >= len(m.visibleTasks()) {
			m.selected = max(len(m.visibleTasks())-1, 0)
		}
		return m, nil
	case workersLoadedMsg:
		m.workers, m.err = msg.workers, msg.err
		return m, nil
	case detailLoadedMsg:
		m.err = msg.err
		if msg.err == nil {
			m.detail = &msg.task
			m.view = viewDetails
		}
		return m, nil
	case logLoadedMsg:
		m.err = msg.err
		if msg.err == nil {
			m.logContent += msg.chunk.Content
			m.logOffset = msg.chunk.NextOffset
		}
		return m, nil
	case eventMsg:
		m.lastEventID = msg.event.ID
		return m, tea.Batch(m.loadTasksCmd(), m.loadWorkersCmd(), m.subscribeCmd())
	case reconnectMsg:
		m.err = msg.err
		return m, m.subscribeCmd()
	case draftLoadedMsg:
		m.err = msg.err
		if msg.err == nil {
			m.draft = &msg.draft
			m.goalStage = goalStagePreview
		}
		return m, nil
	case actionMsg:
		m.err, m.status = msg.err, msg.status
		if msg.err == nil {
			m.goalStage = goalNone
			m.draft = nil
		}
		return m, tea.Batch(m.loadTasksCmd(), m.loadWorkersCmd())
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.goalStage != goalNone {
		return m.updateGoalKey(key)
	}
	if m.pendingAction != "" {
		return m.updateConfirmationKey(key)
	}
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.view = (m.view + 1) % view(len(viewNames))
		m.selected = 0
	case "shift+tab":
		m.view = (m.view - 1 + view(len(viewNames))) % view(len(viewNames))
		m.selected = 0
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected+1 < len(m.visibleTasks()) {
			m.selected++
		}
	case "enter":
		tasks := m.visibleTasks()
		if m.selected < len(tasks) {
			return m, m.loadDetailCmd(tasks[m.selected].ID)
		}
	case "g":
		m.goalStage = goalStageProject
		m.projectInput, m.goalText = "", ""
	case "r":
		tasks := m.visibleTasks()
		if m.selected < len(tasks) {
			m.pendingAction = "retry"
			m.pendingTaskID = tasks[m.selected].ID
		}
	case "x":
		tasks := m.visibleTasks()
		if m.selected < len(tasks) {
			m.pendingAction = "cancel"
			m.pendingTaskID = tasks[m.selected].ID
		}
	case "l":
		if m.logStream == "stdout" {
			m.logStream = "stderr"
		} else {
			m.logStream = "stdout"
		}
		m.logOffset, m.logContent = 0, ""
		return m, m.loadLogCmd()
	}
	return m, nil
}

func (m Model) updateConfirmationKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch strings.ToLower(key.String()) {
	case "y":
		action, taskID := m.pendingAction, m.pendingTaskID
		m.pendingAction, m.pendingTaskID = "", ""
		return m, func() tea.Msg {
			var err error
			if action == "retry" {
				err = m.client.RetryTask(context.Background(), taskID, false)
			} else {
				err = m.client.CancelTask(context.Background(), taskID)
			}
			return actionMsg{status: action + " requested for " + taskID, err: err}
		}
	case "n", "esc":
		m.pendingAction, m.pendingTaskID = "", ""
	}
	return m, nil
}

func (m Model) updateGoalKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.String() == "esc" {
		m.goalStage = goalNone
		return m, nil
	}
	if m.goalStage == goalStagePreview {
		switch strings.ToLower(key.String()) {
		case "y":
			draftID := m.draft.Goal.ID
			return m, func() tea.Msg {
				err := m.client.ConfirmGoal(context.Background(), draftID)
				return actionMsg{status: "enqueued " + draftID, err: err}
			}
		case "n":
			draftID := m.draft.Goal.ID
			return m, func() tea.Msg {
				err := m.client.RejectGoal(context.Background(), draftID)
				return actionMsg{status: "rejected " + draftID, err: err}
			}
		}
		return m, nil
	}
	switch key.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		if m.goalStage == goalStageProject && len(m.projectInput) > 0 {
			m.projectInput = m.projectInput[:len(m.projectInput)-1]
		}
		if m.goalStage == goalStageText && len(m.goalText) > 0 {
			m.goalText = m.goalText[:len(m.goalText)-1]
		}
	case tea.KeyEnter:
		if m.goalStage == goalStageProject && strings.TrimSpace(m.projectInput) != "" {
			m.goalStage = goalStageText
		} else if m.goalStage == goalStageText && strings.TrimSpace(m.goalText) != "" {
			return m, m.draftGoalCmd()
		}
	case tea.KeyRunes:
		if m.goalStage == goalStageProject {
			m.projectInput += string(key.Runes)
		} else {
			m.goalText += string(key.Runes)
		}
	}
	return m, nil
}

func (m Model) visibleTasks() []domain.TaskView {
	filtered := make([]domain.TaskView, 0, len(m.tasks))
	for _, task := range m.tasks {
		include := false
		switch m.view {
		case viewCompleted:
			include = task.Status == domain.TaskCompleted
		case viewFailed:
			include = task.Status == domain.TaskFailed || task.Status == domain.TaskBlocked ||
				task.Status == domain.TaskCancelled
		case viewWorkers:
			include = false
		default:
			include = task.Status != domain.TaskCompleted && task.Status != domain.TaskFailed &&
				task.Status != domain.TaskCancelled
		}
		if include {
			filtered = append(filtered, task)
		}
	}
	return filtered
}
