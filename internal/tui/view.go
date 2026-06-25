package tui

import (
	"fmt"
	"strings"

	"github.com/heruujoko/piramid/internal/domain"
)

func (m Model) View() string {
	var builder strings.Builder
	builder.WriteString(titleStyle.Render("Pi-Ramid"))
	builder.WriteString("  ")
	for index, name := range viewNames {
		if view(index) == m.view {
			builder.WriteString(selectedStyle.Render("[" + name + "]"))
		} else {
			builder.WriteString(" " + name + " ")
		}
	}
	builder.WriteString("\n\n")

	if m.goalStage != goalNone {
		builder.WriteString(m.goalView())
	} else if m.pendingAction != "" {
		builder.WriteString(fmt.Sprintf(
			"Confirm %s for %s? [y/N]",
			m.pendingAction,
			m.pendingTaskID,
		))
	} else {
		builder.WriteString(m.currentView())
	}
	if m.err != nil {
		builder.WriteString("\n" + errorStyle.Render("Error: "+m.err.Error()))
	}
	if m.status != "" {
		builder.WriteString("\n" + selectedStyle.Render(m.status))
	}
	builder.WriteString("\n\n")
	builder.WriteString(mutedStyle.Render(
		"tab views • ↑/↓ select • enter inspect • g goal • r retry • x cancel • l logs • q quit",
	))
	return builder.String()
}

func (m Model) currentView() string {
	switch m.view {
	case viewWorkers:
		if len(m.workers) == 0 {
			return "No active workers."
		}
		var lines []string
		for _, worker := range m.workers {
			lines = append(lines, fmt.Sprintf("%s  %-10s  %s  attempt %d",
				worker.ID, worker.Status, worker.TaskID, worker.AttemptID))
		}
		return strings.Join(lines, "\n")
	case viewDetails:
		if m.detail == nil {
			return "Select a task and press enter."
		}
		return fmt.Sprintf(
			"%s — %s\nStatus: %s\nProject: %s\nGoal: %s\nDOD:\n- %s",
			m.detail.ID, m.detail.Title, m.detail.Status, m.detail.ProjectPath,
			m.detail.Goal, strings.Join(m.detail.DOD, "\n- "),
		)
	case viewAttempts:
		return m.attemptsView()
	case viewVerification:
		return m.verificationView()
	case viewLogs:
		if m.detail == nil {
			return "Select a task first."
		}
		if m.logContent == "" {
			return fmt.Sprintf("%s log is empty; press l to load/toggle.", m.logStream)
		}
		return m.logContent
	default:
		return m.tasksView()
	}
}

func (m Model) tasksView() string {
	tasks := m.visibleTasks()
	if len(tasks) == 0 {
		return "No tasks in this view."
	}
	var lines []string
	for index, task := range tasks {
		prefix := "  "
		if index == m.selected {
			prefix = selectedStyle.Render("> ")
		}
		lines = append(lines, fmt.Sprintf("%s%-20s %-12s %d/%d  %s",
			prefix, task.ID, task.Status, task.AttemptCount, task.MaxAttempts, task.Title))
	}
	return strings.Join(lines, "\n")
}

func (m Model) attemptsView() string {
	if m.detail == nil || len(m.detail.Attempts) == 0 {
		return "No attempts."
	}
	var lines []string
	for _, attempt := range m.detail.Attempts {
		exit := "-"
		if attempt.ExitCode != nil {
			exit = fmt.Sprint(*attempt.ExitCode)
		}
		lines = append(lines, fmt.Sprintf("#%d  %-12s worker=%s exit=%s",
			attempt.AttemptNumber, attempt.Status, attempt.WorkerID, exit))
	}
	return strings.Join(lines, "\n")
}

func (m Model) verificationView() string {
	if m.detail == nil {
		return "Select a task first."
	}
	for index := len(m.detail.Attempts) - 1; index >= 0; index-- {
		verification := m.detail.Attempts[index].Verification
		if verification != nil {
			text := fmt.Sprintf("%s\n- %s", verification.Status, strings.Join(verification.Reasons, "\n- "))
			if verification.RetryPrompt != "" {
				text += "\n\nRetry instructions:\n" + verification.RetryPrompt
			}
			return text
		}
	}
	return "No verification report."
}

func (m Model) goalView() string {
	switch m.goalStage {
	case goalStageProject:
		return "New goal\nProject path: " + m.projectInput + "█"
	case goalStageText:
		return "New goal\nProject: " + m.projectInput + "\nGoal: " + m.goalText + "█"
	case goalStagePreview:
		var lines []string
		for _, task := range m.draft.Plan.Tasks {
			lines = append(lines, fmt.Sprintf("%s  %s  depends=%s",
				task.ID, task.Title, strings.Join(task.DependsOn, ",")))
		}
		return "Draft " + m.draft.Goal.ID + "\n" + strings.Join(lines, "\n") +
			"\n\nConfirm? [y/N]"
	default:
		return ""
	}
}

var _ = domain.Task{}
