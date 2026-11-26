package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	keys "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/tags"
)

type addStep int

const (
	addInactive addStep = iota
	addChooseType
	addChoosePath
	addEnterText
	addEnterDetails
)

var addKinds = []string{tags.TypeFile, tags.TypeFolder, tags.TypeText}

// addFlow is the state of adding an item: choose a type, choose a path or
// enter text, then fill in the details in the edit modal.
type addFlow struct {
	step      addStep
	kindIndex int
	picker    pathPicker
	path      string
	text      textarea.Model
	showText  bool
	textErr   string
}

func (f addFlow) kind() string { return addKinds[f.kindIndex] }

// textValue is the entered text, exactly as it will be stored.
func (f addFlow) textValue() string {
	return strings.ReplaceAll(f.text.Value(), string(tabMark), "\t")
}

// tabMark stands in for tabs inside the text area, which would otherwise
// replace them with spaces. It is a Unicode noncharacter, reserved for
// internal use such as this.
const tabMark = '﷐'

// maxTextLines is the text area's own limit; it drops lines beyond it.
const maxTextLines = 10_000

// textAreaPaste prepares pasted text for the text area, or returns an error if
// the text area could not hold it unchanged.
func textAreaPaste(s string, existingLines int) (string, error) {
	if strings.ContainsRune(s, '\r') {
		return "", errors.New("paste contains carriage returns; add it as a file to preserve its line endings")
	}
	for _, r := range s {
		if r == tabMark || r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\t') {
			return "", errors.New("not plain text; add it as a file instead")
		}
	}
	if existingLines+strings.Count(s, "\n") > maxTextLines {
		return "", errors.New("too many lines; add it as a file instead")
	}
	return strings.ReplaceAll(s, "\t", string(tabMark)), nil
}

func newAddText() textarea.Model {
	text := textarea.New()
	text.Prompt = ""
	text.ShowLineNumbers = false
	text.MaxHeight = 0
	// Enter continues, so a stray newline never ends up in a stored secret.
	text.KeyMap.InsertNewline = keys.NewBinding(keys.WithKeys("ctrl+j", "shift+enter", "alt+enter"))
	// The built-in Ctrl+V bypasses textAreaPaste. Terminal paste still works.
	text.KeyMap.Paste = keys.NewBinding()
	text.Focus()
	return text
}

// adding reports whether the edit modal is filling in a new item.
func (m explorerModel) adding() bool { return m.add.step == addEnterDetails }

func (m *explorerModel) openAdd() {
	m.add = addFlow{step: addChooseType}
	m.status = ""
	m.filter.Blur()
}

func (m *explorerModel) closeAdd() {
	m.add = addFlow{}
	m.filter.Focus()
}

func (m *explorerModel) openAddSource() {
	if m.add.kind() == tags.TypeText {
		m.add.step = addEnterText
		m.add.text = newAddText()
		m.add.showText = false
		m.add.textErr = ""
	} else {
		dir, err := os.Getwd()
		if err != nil {
			dir = string(filepath.Separator)
		}
		m.add.step = addChoosePath
		m.add.picker = newPathPicker(m.add.kind() == tags.TypeFolder, dir)
	}
	m.resizeAdd()
}

func (m *explorerModel) backToAddSource() {
	m.add.step = addChoosePath
	if m.add.kind() == tags.TypeText {
		m.add.step = addEnterText
	}
	m.resizeAdd()
}

func (m *explorerModel) openAddDetails() {
	m.add.step = addEnterDetails
	name := ""
	if m.add.kind() != tags.TypeText {
		if name = filepath.Base(m.add.path); name == string(filepath.Separator) {
			name = ""
		}
	}
	m.openEditInputs(name, "", "")
}

// resumeEdit returns to the details modal after a save fails.
func (m *explorerModel) resumeEdit(err error) {
	m.action = explorerNoAction
	m.done = false
	m.editError = err.Error()
	m.editFocus = 0
	m.focusEditInput()
}

// editCreatedItem switches from creating content to editing the saved item.
func (m *explorerModel) editCreatedItem(result keepResult, description string, userTags []string, err error) {
	m.editItem = provider.Item{ID: result.id, Title: result.name}
	m.openEditInputs("", description, strings.Join(userTags, ", "))
	m.resumeEdit(err)
}

func (m explorerModel) addKeepOptions() KeepOptions {
	o := KeepOptions{Name: m.editNameInput.Value(), Tags: m.editedTags, New: true}
	if m.editedDescription != "" {
		o.Description = &m.editedDescription
	}
	if m.add.kind() == tags.TypeText {
		text := m.add.textValue()
		o.Text = true
		o.TextContent = &text
	} else {
		o.Path = m.add.path
	}
	return o
}

func (m explorerModel) updateAdd(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "ctrl+c":
			m.finish(explorerQuit)
			return m, tea.Quit
		case "esc":
			if m.add.step == addChooseType {
				m.closeAdd()
			} else {
				m.add = addFlow{step: addChooseType, kindIndex: m.add.kindIndex}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	switch m.add.step {
	case addChooseType:
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "up":
				m.add.kindIndex = max(0, m.add.kindIndex-1)
			case "down":
				m.add.kindIndex = min(len(addKinds)-1, m.add.kindIndex+1)
			case "enter":
				m.openAddSource()
			}
		}
	case addChoosePath:
		var path string
		if path, cmd = m.add.picker.update(msg); path != "" {
			m.add.path = path
			m.openAddDetails()
		}
	case addEnterText:
		cmd = m.updateAddText(msg)
	}
	return m, cmd
}

func (m *explorerModel) updateAddText(msg tea.Msg) tea.Cmd {
	switch input := msg.(type) {
	case tea.KeyPressMsg:
		m.add.textErr = ""
		switch input.String() {
		case "enter":
			if m.add.text.Value() == "" {
				m.add.textErr = "no text was provided"
				return nil
			}
			m.openAddDetails()
			return nil
		case "ctrl+t":
			m.add.showText = !m.add.showText
			return nil
		}
	case tea.PasteMsg:
		content, err := textAreaPaste(input.Content, m.add.text.LineCount())
		if err != nil {
			m.add.textErr = err.Error()
			return nil
		}
		m.add.textErr = ""
		msg = tea.PasteMsg{Content: content}
	}
	var cmd tea.Cmd
	m.add.text, cmd = m.add.text.Update(msg)
	return cmd
}

// addBodyHeight is the number of rows for the path list or the text. Both
// modals also have a border and up to five fixed rows.
func (m explorerModel) addBodyHeight() int { return max(1, min(m.height, 20)-7) }

func (m *explorerModel) resizeAdd() {
	width := editModalInnerWidth(m.width)
	switch {
	case m.add.kind() == tags.TypeText && m.add.step >= addEnterText:
		m.add.text.SetWidth(width)
		m.add.text.SetHeight(min(10, m.addBodyHeight()))
	case m.add.kind() != tags.TypeText && m.add.step >= addChoosePath:
		m.add.picker.setSize(width, m.addBodyHeight())
	}
}

func (m explorerModel) addSourceRow(width int) string {
	if m.add.kind() == tags.TypeText {
		return "Text entered (hidden)"
	}
	const label = "Path: "
	return label + shortPath(m.add.path, width-len(label))
}

func (m explorerModel) addModalView() string {
	width := editModalInnerWidth(m.width)
	fit := func(s string) string { return ansi.Truncate(singleLine(s), width, "…") }
	hint := func(s string) string { return explorerHintStyle.Render(fit(s)) }

	var rows []string
	switch m.add.step {
	case addChooseType:
		rows = []string{explorerHeaderStyle.Render("Add item")}
		for i, kind := range addKinds {
			label := strings.ToUpper(kind[:1]) + kind[1:]
			if i == m.add.kindIndex {
				rows = append(rows, promptStyle.Render("> "+label))
			} else {
				rows = append(rows, "  "+label)
			}
		}
		rows = append(rows, hint("↑↓ select • Enter choose • Esc cancel"))
	case addChoosePath:
		p := m.add.picker
		rows = []string{
			explorerHeaderStyle.Render("Choose a " + m.add.kind()),
			shortPath(p.dir, width),
			ansi.Truncate(p.filter.View(), width, ""),
			p.list.View(),
			fit(p.err),
			hint(p.hint()),
		}
	case addEnterText:
		body := "Nothing entered"
		if m.add.showText {
			body = strings.ReplaceAll(m.add.text.View(), string(tabMark), "→")
		} else if m.add.text.Value() != "" {
			body = "Text entered (hidden)"
		}
		toggle := "show"
		if m.add.showText {
			toggle = "hide"
		}
		rows = []string{
			explorerHeaderStyle.Render("Enter text"),
			body,
			fit(m.add.textErr),
			hint("Enter next • Ctrl+T " + toggle + " • Esc back • Ctrl+J newline"),
		}
	}
	return m.modalFrame(rows)
}
