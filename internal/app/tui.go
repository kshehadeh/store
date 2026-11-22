package app

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var errCancelled = errors.New("cancelled")

var promptStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
var hintStyle = lipgloss.NewStyle().Faint(true)

func (c *Console) runTUI(m tea.Model) (tea.Model, error) {
	return tea.NewProgram(m, tea.WithInput(c.input), tea.WithOutput(c.Out)).Run()
}

type choiceItem struct {
	label string
	index int
}

func (i choiceItem) Title() string       { return fmt.Sprintf("%d) %s", i.index+1, i.label) }
func (i choiceItem) Description() string { return "" }
func (i choiceItem) FilterValue() string { return i.label }

func choiceListHeight(itemCount int) int {
	return min(itemCount+7, 20)
}

type choiceModel struct {
	list      list.Model
	selected  int
	done      bool
	cancelled bool
}

func newChoiceModel(prompt string, options []string) choiceModel {
	items := make([]list.Item, len(options))
	for i, label := range options {
		items[i] = choiceItem{label: label, index: i}
	}
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)
	l := list.New(items, delegate, 80, choiceListHeight(len(items)))
	l.Title = prompt
	l.SetShowStatusBar(false)
	l.DisableQuitKeybindings()
	return choiceModel{list: l, selected: -1}
}

func (m choiceModel) Init() tea.Cmd { return nil }

func (m choiceModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(max(1, msg.Width-2), max(1, min(msg.Height-2, choiceListHeight(len(m.list.Items())))))
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			m.done, m.cancelled = true, true
			return m, tea.Quit
		case "esc", "q":
			if !m.list.SettingFilter() && m.list.FilterState() == list.Unfiltered {
				m.done, m.cancelled = true, true
				return m, tea.Quit
			}
		case "enter":
			if !m.list.SettingFilter() {
				if item, ok := m.list.SelectedItem().(choiceItem); ok {
					m.selected, m.done = item.index, true
					return m, tea.Quit
				}
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m choiceModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	return tea.NewView(m.list.View() + "\n" + hintStyle.Render("enter select • esc clear/cancel"))
}

func (c *Console) chooseItem(prompt string, options []string) (int, error) {
	result, err := c.runTUI(newChoiceModel(prompt, options))
	if err != nil {
		return -1, err
	}
	m := result.(choiceModel)
	if m.cancelled || m.selected < 0 {
		return -1, errCancelled
	}
	return m.selected, nil
}

type inputModel struct {
	input     textinput.Model
	prompt    string
	def       string
	secret    bool
	done      bool
	cancelled bool
}

func newInputModel(prompt, def string, secret bool) inputModel {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = def
	input.SetWidth(76)
	if secret {
		input.EchoMode = textinput.EchoNone
	}
	input.Focus()
	return inputModel{input: input, prompt: strings.TrimSpace(prompt), def: def, secret: secret}
}

func (m inputModel) Init() tea.Cmd { return textinput.Blink }

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.input.SetWidth(max(1, msg.Width-4))
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.done, m.cancelled = true, true
			return m, tea.Quit
		case "enter":
			m.done = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m inputModel) View() tea.View {
	if m.done {
		return tea.NewView("")
	}
	prompt := m.prompt
	if m.def != "" {
		prompt += " [" + m.def + "]"
	}
	return tea.NewView(promptStyle.Render(prompt) + "\n" + m.input.View() + "\n" + hintStyle.Render("enter confirm • esc cancel"))
}

func (m inputModel) value() string {
	value := m.input.Value()
	if !m.secret {
		value = strings.TrimSpace(value)
		if value == "" {
			value = m.def
		}
	}
	return value
}

func (c *Console) promptInput(prompt, def string, secret bool) (string, error) {
	result, err := c.runTUI(newInputModel(prompt, def, secret))
	if err != nil {
		return "", err
	}
	m := result.(inputModel)
	if m.cancelled {
		return "", errCancelled
	}
	return m.value(), nil
}
