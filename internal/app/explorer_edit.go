package app

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/tags"
)

func (m *explorerModel) openEdit(item provider.Item, description string) {
	m.editItem = item
	m.openEditInputs("", description, strings.Join(editableStoreTags(item.Tags), ", "))
}

func (m *explorerModel) openEditInputs(name, description, storeTags string) {
	m.editing = true
	m.editError = ""
	m.editFocus = 0
	m.editNameInput = newEditInput("Item name", name)
	m.editDescriptionInput = newEditInput("No description", description)
	m.editTagsInput = newEditInput("project/api, environment/dev", storeTags)
	m.filter.Blur()
	m.focusEditInput()
	m.resizeEdit()
}

func newEditInput(placeholder, value string) textinput.Model {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = placeholder
	input.SetValue(value)
	return input
}

// editInputs returns the modal's inputs in tab order. The name can be set only
// while adding an item.
func (m *explorerModel) editInputs() []*textinput.Model {
	inputs := []*textinput.Model{&m.editDescriptionInput, &m.editTagsInput}
	if m.adding() {
		inputs = append([]*textinput.Model{&m.editNameInput}, inputs...)
	}
	return inputs
}

func (m *explorerModel) updateEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "ctrl+c":
			m.finish(explorerQuit)
			return *m, tea.Quit
		case "esc":
			m.editing = false
			m.editError = ""
			if m.adding() {
				m.backToAddSource()
				return *m, nil
			}
			m.filter.Focus()
			return *m, nil
		case "tab", "shift+tab":
			count := len(m.editInputs())
			step := 1
			if key.String() == "shift+tab" {
				step = count - 1
			}
			m.editFocus = (m.editFocus + step) % count
			m.focusEditInput()
			return *m, nil
		case "enter", "ctrl+s":
			if m.adding() && strings.TrimSpace(m.editNameInput.Value()) == "" {
				m.editError = "item name cannot be empty"
				m.editFocus = 0
				m.focusEditInput()
				return *m, nil
			}
			userTags, err := parseEditTags(m.editTagsInput.Value())
			if err != nil {
				m.editError = err.Error()
				return *m, nil
			}
			m.editedDescription = m.editDescriptionInput.Value()
			m.editedTags = userTags
			m.done = true
			if m.adding() {
				m.action = explorerCreate
				return *m, tea.Quit
			}
			m.actionItem = m.editItem
			m.action = explorerSaveEdit
			return *m, tea.Quit
		}
	}

	var cmd tea.Cmd
	input := m.editInputs()[m.editFocus]
	*input, cmd = input.Update(msg)
	return *m, cmd
}

func (m *explorerModel) focusEditInput() {
	for i, input := range m.editInputs() {
		if i == m.editFocus {
			input.Focus()
		} else {
			input.Blur()
		}
	}
}

func (m *explorerModel) resizeEdit() {
	// Inputs reserve their "> " prompt and a trailing cursor cell so long
	// values scroll instead of wrapping inside the modal.
	width := max(1, editModalInnerWidth(m.width)-3)
	for _, input := range m.editInputs() {
		input.SetWidth(width)
		// SetWidth keeps stale scroll offsets. Visiting both ends forces the
		// input to recompute them before restoring the cursor.
		pos := input.Position()
		input.CursorEnd()
		input.CursorStart()
		input.SetCursor(pos)
	}
}

// editModalWidth is the modal's total width; lipgloss counts border and
// horizontal padding inside it.
func editModalWidth(termWidth int) int { return max(16, min(68, termWidth-4)) }

func editModalInnerWidth(termWidth int) int { return editModalWidth(termWidth) - 4 }

func (m explorerModel) editModalView() string {
	innerWidth := editModalInnerWidth(m.width)
	rows := []string{
		explorerHeaderStyle.Render("Edit selected item"),
		ansi.Truncate("Item: "+m.editItem.Title, innerWidth, "…"),
	}
	cancelHint := "Esc cancel"
	if m.adding() {
		rows = []string{
			explorerHeaderStyle.Render("Add " + m.add.kind()),
			m.addSourceRow(innerWidth),
			explorerDetailLabel.Render("Name"),
			m.editNameInput.View(),
		}
		cancelHint = "Esc back"
	}
	rows = append(rows,
		explorerDetailLabel.Render("Description"),
		m.editDescriptionInput.View(),
		explorerDetailLabel.Render("Store tags (comma-separated)"),
		m.editTagsInput.View(),
	)
	if m.editError != "" {
		rows = append(rows, ansi.Truncate(singleLine(m.editError), innerWidth, "…"))
	}
	rows = append(rows, explorerHintStyle.Render(ansi.Truncate("Tab switch • Enter save • "+cancelHint, innerWidth, "…")))
	return m.modalFrame(rows)
}

func (m explorerModel) modalFrame(rows []string) string {
	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	verticalPadding := 1
	if lipgloss.Height(content)+4 > m.height {
		verticalPadding = 0
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("69")).
		Background(lipgloss.Color("235")).
		Padding(verticalPadding, 1).
		Width(editModalWidth(m.width)).
		Render(content)
}

func overlayContent(background, foreground string, width, height int) string {
	baseLines := strings.Split(background, "\n")
	overlayLines := strings.Split(foreground, "\n")
	overlayWidth := lipgloss.Width(foreground)
	startX := max(0, (width-overlayWidth)/2)
	startY := max(0, (height-len(overlayLines))/2)
	output := make([]string, height)

	for y := 0; y < height; y++ {
		line := ""
		if y < len(baseLines) {
			line = ansi.Truncate(baseLines[y], width, "")
		}
		lineWidth := lipgloss.Width(line)
		if lineWidth < width {
			line += strings.Repeat(" ", width-lineWidth)
		}
		overlayIndex := y - startY
		if overlayIndex >= 0 && overlayIndex < len(overlayLines) {
			overlayLine := ansi.Truncate(overlayLines[overlayIndex], overlayWidth, "")
			overlayLineWidth := lipgloss.Width(overlayLine)
			if overlayLineWidth < overlayWidth {
				overlayLine += strings.Repeat(" ", overlayWidth-overlayLineWidth)
			}
			rightStart := min(width, startX+overlayWidth)
			line = ansi.Cut(line, 0, startX) + overlayLine + ansi.Cut(line, rightStart, width)
		}
		output[y] = line
	}
	return strings.Join(output, "\n")
}

func editableStoreTags(itemTags []string) []string {
	var out []string
	for _, tag := range itemTags {
		if tags.IsStore(tag) && !tags.IsMeta(tag) && tag != tags.Root {
			out = append(out, tags.Short(tag))
		}
	}
	return out
}

func parseEditTags(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return tags.NormalizeAll(parts, true)
}
