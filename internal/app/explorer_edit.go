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
	m.editing = true
	m.editItem = item
	m.editError = ""
	m.editFocus = 0

	m.editDescriptionInput = textinput.New()
	m.editDescriptionInput.Prompt = "> "
	m.editDescriptionInput.Placeholder = "No description"
	m.editDescriptionInput.SetValue(description)
	m.editDescriptionInput.Focus()

	m.editTagsInput = textinput.New()
	m.editTagsInput.Prompt = "> "
	m.editTagsInput.Placeholder = "project/api, environment/dev"
	m.editTagsInput.SetValue(strings.Join(editableStoreTags(item.Tags), ", "))
	m.editTagsInput.Blur()

	m.filter.Blur()
	m.resizeEdit()
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
			m.filter.Focus()
			return *m, nil
		case "tab", "shift+tab":
			m.editFocus = 1 - m.editFocus
			m.focusEditInput()
			return *m, nil
		case "enter", "ctrl+s":
			userTags, err := parseEditTags(m.editTagsInput.Value())
			if err != nil {
				m.editError = err.Error()
				return *m, nil
			}
			m.editedDescription = m.editDescriptionInput.Value()
			m.editedTags = userTags
			m.actionItem = m.editItem
			m.action = explorerSaveEdit
			m.done = true
			return *m, tea.Quit
		}
	}

	var cmd tea.Cmd
	if m.editFocus == 0 {
		m.editDescriptionInput, cmd = m.editDescriptionInput.Update(msg)
	} else {
		m.editTagsInput, cmd = m.editTagsInput.Update(msg)
	}
	return *m, cmd
}

func (m *explorerModel) focusEditInput() {
	if m.editFocus == 0 {
		m.editDescriptionInput.Focus()
		m.editTagsInput.Blur()
		return
	}
	m.editDescriptionInput.Blur()
	m.editTagsInput.Focus()
}

func (m *explorerModel) resizeEdit() {
	// Inputs reserve their "> " prompt and a trailing cursor cell so long
	// values scroll instead of wrapping inside the modal.
	width := max(1, editModalInnerWidth(m.width)-3)
	for _, input := range []*textinput.Model{&m.editDescriptionInput, &m.editTagsInput} {
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
	contentWidth := editModalWidth(m.width)
	innerWidth := editModalInnerWidth(m.width)
	rows := []string{
		explorerHeaderStyle.Render("Edit selected item"),
		ansi.Truncate("Item: "+m.editItem.Title, innerWidth, "…"),
		explorerDetailLabel.Render("Description"),
		m.editDescriptionInput.View(),
		explorerDetailLabel.Render("Store tags (comma-separated)"),
		m.editTagsInput.View(),
	}
	if m.editError != "" {
		rows = append(rows, ansi.Truncate(m.editError, innerWidth, "…"))
	}
	rows = append(rows, explorerHintStyle.Render(ansi.Truncate("Tab switch • Enter save • Esc cancel", innerWidth, "…")))

	verticalPadding := 1
	if len(rows)+4 > m.height {
		verticalPadding = 0
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("69")).
		Background(lipgloss.Color("235")).
		Padding(verticalPadding, 1).
		Width(contentWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
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
