package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262")).Render
)

// ProgressModel represents a progress bar model
type ProgressModel struct {
	progress progress.Model
	percent  float64
	message  string
}

// NewProgressModel creates a new progress bar model
func NewProgressModel() ProgressModel {
	return ProgressModel{
		progress: progress.New(progress.WithDefaultGradient()),
		percent:  0.0,
		message:  "",
	}
}

// Init initializes the progress model
func (m ProgressModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the progress model
func (m ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.progress.Width = msg.Width - 4
		if m.progress.Width > 80 {
			m.progress.Width = 80
		}
		return m, nil
	case ProgressMsg:
		m.percent = msg.Percent
		m.message = msg.Message
		if m.percent >= 1.0 {
			return m, tea.Quit
		}
		return m, nil
	default:
		return m, nil
	}
}

// View renders the progress bar
func (m ProgressModel) View() string {
	pad := strings.Repeat(" ", 2)
	return "\n" +
		pad + m.progress.ViewAs(m.percent) + "\n" +
		pad + helpStyle(m.message) + "\n"
}

// ProgressMsg is a message type for updating progress
type ProgressMsg struct {
	Percent float64
	Message string
}

// ShowProgress displays a progress bar with the given percentage and message
func ShowProgress(percent float64, message string) {
	p := tea.NewProgram(NewProgressModel())
	p.Send(ProgressMsg{Percent: percent, Message: message})
}

// SimpleProgress displays a simple text-based progress indicator
func SimpleProgress(current, total int, message string) string {
	percent := float64(current) / float64(total) * 100
	bar := "["
	filled := int(percent / 5)
	for i := 0; i < 20; i++ {
		if i < filled {
			bar += "="
		} else {
			bar += " "
		}
	}
	bar += "]"
	return fmt.Sprintf("%s %s %.0f%% (%d/%d)", message, bar, percent, current, total)
}
