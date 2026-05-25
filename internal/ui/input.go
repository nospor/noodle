package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type InputModel struct {
	textInput textinput.Model
	prompt    string
	state     *AppState
	width     int
	height    int
	mode      string // "url" or "title"
}

type InputSubmitMsg struct {
	Value string
	Mode  string
}

type InputCancelMsg struct{}

func NewInputModel(state *AppState, prompt string, mode string) *InputModel {
	ti := textinput.New()
	ti.Placeholder = prompt
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 50

	return &InputModel{
		textInput: ti,
		prompt:    prompt,
		state:     state,
		mode:      mode,
	}
}

func (m *InputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *InputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			value := strings.TrimSpace(m.textInput.Value())
			// Allow empty values for optional fields (title)
			return m, func() tea.Msg {
				return InputSubmitMsg{Value: value, Mode: m.mode}
			}

		case "esc", "ctrl+c":
			return m, func() tea.Msg {
				return InputCancelMsg{}
			}
		}

		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *InputModel) View() string {
	// Dialog box
	dialogWidth := 60
	dialogHeight := 5
	dialogBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(dialogWidth - 4).
		Height(dialogHeight - 2).
		Render(
			lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("170")).
				Render(m.prompt) + "\n\n" +
				m.textInput.View() + "\n\n" +
				lipgloss.NewStyle().Foreground(lipgloss.Color("241")).
					Render("Press Enter to submit, Esc to cancel"),
		)

	// Center the dialog
	centered := lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		dialogBox,
	)

	return centered
}
