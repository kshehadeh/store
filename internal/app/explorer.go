package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kshehadeh/store/internal/provider"
)

type explorerAction int

const (
	explorerNoAction explorerAction = iota
	explorerQuit
	explorerPull
	explorerRename
)

type storedItem struct {
	item provider.Item
}

func (i storedItem) Title() string       { return i.item.Title }
func (i storedItem) Description() string { return "" }
func (i storedItem) FilterValue() string {
	return strings.Join([]string{
		i.item.Title,
		i.item.Category,
		typeLabel(i.item),
		displayTags(i.item.Tags),
		hostOf(i.item.Tags),
		sourceOf(i.item.Tags),
	}, " ")
}

type explorerModel struct {
	location string
	list     list.Model
	filter   textinput.Model
	details  viewport.Model

	width  int
	height int
	status string
	query  string

	action     explorerAction
	actionItem provider.Item
	done       bool
}

var (
	explorerHeaderStyle = lipgloss.NewStyle().Bold(true)
	explorerHintStyle   = lipgloss.NewStyle().Faint(true)
	explorerDetailLabel = lipgloss.NewStyle().Bold(true)
	explorerPaneStyle   = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("62"))
)

func newExplorerModel(location string, items []provider.Item, selectedID, query, status string) explorerModel {
	listItems := make([]list.Item, len(items))
	for i, item := range items {
		listItems[i] = storedItem{item: item}
	}

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)
	itemList := list.New(listItems, delegate, 30, 12)
	itemList.Title = fmt.Sprintf("Stored items (%d)", len(items))
	itemList.Styles.TitleBar = itemList.Styles.TitleBar.PaddingBottom(0)
	itemList.SetShowStatusBar(false)
	itemList.SetShowHelp(false)
	itemList.SetShowPagination(false)
	itemList.SetShowFilter(false)
	itemList.FilterInput.CharLimit = 0
	itemList.DisableQuitKeybindings()
	filter := textinput.New()
	filter.Prompt = "Filter: "
	filter.Placeholder = "Type to filter stored items"
	filter.Focus()
	filter.SetWidth(76)
	if query != "" {
		filter.SetValue(query)
		filter.CursorEnd()
		itemList.SetFilterText(query)
	}
	for i, item := range itemList.VisibleItems() {
		if selected, ok := item.(storedItem); ok && selected.item.ID == selectedID {
			itemList.Select(i)
			break
		}
	}

	model := explorerModel{
		location: location,
		list:     itemList,
		filter:   filter,
		details:  viewport.New(),
		query:    query,
		status:   status,
	}
	model.details.SoftWrap = true
	model.resize(80, 24)
	model.syncDetails()
	return model
}

func (m explorerModel) Init() tea.Cmd { return textinput.Blink }

func (m explorerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			m.finish(explorerQuit)
			return m, tea.Quit
		case "ctrl+r":
			if !m.finish(explorerRename) {
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			if !m.finish(explorerPull) {
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.filter.Value() != "" {
				m.filter.Reset()
				m.list.SetFilterText("")
				m.query = ""
				m.syncDetails()
				return m, nil
			}
			m.finish(explorerQuit)
			return m, tea.Quit
		case "up", "down":
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			m.syncDetails()
			return m, cmd
		case "pgup", "pgdown":
			var cmd tea.Cmd
			m.details, cmd = m.details.Update(msg)
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if query := m.filter.Value(); query != m.query {
		m.query = query
		m.list.SetFilterText(m.query)
		m.syncDetails()
	}
	return m, cmd
}

func (m explorerModel) View() tea.View {
	view := tea.NewView("")
	view.AltScreen = true
	if m.done {
		return view
	}
	if m.width < 40 || m.height < 12 {
		view.Content = ansi.Truncate("Resize terminal to at least 40×12. Ctrl+C quits.", m.width, "")
		return view
	}

	header, footer, bodyHeight := m.frameParts()
	view.Content = lipgloss.JoinVertical(lipgloss.Left, header, m.panes(bodyHeight), footer)
	return view
}

func (m explorerModel) frameParts() (header, footer string, bodyHeight int) {
	width := max(1, m.width)
	header = lipgloss.JoinVertical(
		lipgloss.Left,
		explorerHeaderStyle.Width(width).Render(ansi.Truncate("store tui  •  "+m.location, width, "…")),
		ansi.Truncate(m.filter.View(), width, ""),
	)
	status := strings.NewReplacer("\r", " ", "\n", " ").Replace(m.status)
	if status == "" {
		status = " "
	}
	footer = lipgloss.JoinVertical(
		lipgloss.Left,
		explorerHintStyle.Width(width).Render("↑↓ select  Enter pull  Ctrl+R rename  Esc clear/quit  Ctrl+C quit"),
		lipgloss.NewStyle().Width(width).Render(ansi.Truncate(status, width, "…")),
	)
	return header, footer, max(1, m.height-lipgloss.Height(header)-lipgloss.Height(footer))
}

func (m *explorerModel) finish(action explorerAction) bool {
	if action != explorerQuit {
		item, ok := m.selectedItem()
		if !ok {
			return false
		}
		m.actionItem = item
	}
	m.action = action
	m.done = true
	return true
}

func (m explorerModel) selectedItem() (provider.Item, bool) {
	selected, ok := m.list.SelectedItem().(storedItem)
	if !ok {
		return provider.Item{}, false
	}
	return selected.item, true
}

func (m *explorerModel) syncDetails() {
	item, ok := m.selectedItem()
	if !ok {
		m.details.SetContent("No stored items match this filter.")
		return
	}

	updated := "-"
	if !item.Updated.IsZero() {
		updated = item.Updated.Local().Format("2006-01-02 15:04")
	}
	content := []string{
		explorerHeaderStyle.Render(item.Title),
		"",
		detailRow("Type", typeLabel(item)),
		detailRow("Category", orDash(item.Category)),
		detailRow("Host", orDash(hostOf(item.Tags))),
		detailRow("Source", orDash(sourceOf(item.Tags))),
		detailRow("Tags", orDash(displayTags(item.Tags))),
		detailRow("Updated", updated),
		detailRow("ID", item.ID),
	}
	m.details.SetContent(strings.Join(content, "\n"))
	m.details.GotoTop()
}

func detailRow(label, value string) string {
	return explorerDetailLabel.Render(label+": ") + value
}

func (m *explorerModel) resize(width, height int) {
	m.width = max(1, width)
	m.height = max(1, height)
	m.filter.SetWidth(max(1, m.width-lipgloss.Width(m.filter.Prompt)-1))

	_, _, bodyHeight := m.frameParts()
	if m.width >= 76 {
		leftWidth := max(24, (m.width-1)*34/100)
		rightWidth := max(1, m.width-leftWidth-1)
		m.list.SetSize(max(1, leftWidth-2), max(1, bodyHeight-2))
		m.details.SetWidth(max(1, rightWidth-2))
		m.details.SetHeight(max(1, bodyHeight-2))
		return
	}

	leftHeight := max(4, bodyHeight/2)
	rightHeight := max(3, bodyHeight-leftHeight)
	m.list.SetSize(max(1, m.width-2), max(1, leftHeight-2))
	m.details.SetWidth(max(1, m.width-2))
	m.details.SetHeight(max(1, rightHeight-2))
}

func (m explorerModel) panes(bodyHeight int) string {
	width := max(1, m.width)
	if width >= 76 {
		leftWidth := max(24, (width-1)*34/100)
		rightWidth := max(1, width-leftWidth-1)
		left := explorerPaneStyle.
			Width(leftWidth).
			Height(bodyHeight).
			Render(m.list.View())
		right := explorerPaneStyle.
			Width(rightWidth).
			Height(bodyHeight).
			Render(m.details.View())
		return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
	}

	leftHeight := max(4, bodyHeight/2)
	rightHeight := max(3, bodyHeight-leftHeight)
	left := explorerPaneStyle.
		Width(width).
		Height(leftHeight).
		Render(m.list.View())
	right := explorerPaneStyle.
		Width(width).
		Height(rightHeight).
		Render(m.details.View())
	return lipgloss.JoinVertical(lipgloss.Left, left, right)
}

// TUI opens an interactive item browser. Actions run after the browser
// releases the terminal, then the browser resumes with refreshed metadata.
func (a *App) TUI(ctx context.Context) error {
	if a.Console == nil || !a.Console.tty {
		return errors.New("store tui requires an interactive terminal")
	}
	items, err := a.discover(ctx, nil)
	if err != nil {
		return err
	}

	selectedID, query, status := "", "", ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err := a.Console.runTUI(newExplorerModel(a.Location, items, selectedID, query, status))
		if err != nil {
			return err
		}
		model := result.(explorerModel)
		if model.action == explorerQuit || model.action == explorerNoAction {
			return nil
		}
		selectedID = model.actionItem.ID
		query = model.query
		switch model.action {
		case explorerPull:
			err = a.Pull(ctx, PullOptions{ID: model.actionItem.ID})
			if err != nil {
				status = err.Error()
			} else {
				status = fmt.Sprintf("Pulled %q.", model.actionItem.Title)
			}
		case explorerRename:
			title, askErr := a.Console.Ask("Rename item", model.actionItem.Title)
			if askErr != nil {
				status = askErr.Error()
				continue
			}
			err = a.Rename(ctx, model.actionItem.ID, title)
			if err != nil {
				status = err.Error()
				continue
			}
			items, err = a.discover(ctx, nil)
			if err != nil {
				return err
			}
			query = ""
			status = fmt.Sprintf("Renamed %q.", strings.TrimSpace(title))
		}
	}
}
