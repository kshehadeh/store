package app

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kshehadeh/store/internal/provider"
)

func key(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func TestChoiceModel(t *testing.T) {
	m := newChoiceModel("Pick a field", []string{"username", "password"})
	lines := strings.Split(m.list.View(), "\n")
	for i, line := range lines {
		if strings.Contains(line, "1) username") {
			if i+1 >= len(lines) || !strings.Contains(lines[i+1], "2) password") {
				t.Fatalf("choice items are not rendered on consecutive rows:\n%s", m.list.View())
			}
			break
		}
		if i == len(lines)-1 {
			t.Fatalf("first choice is missing from the list:\n%s", m.list.View())
		}
	}

	next, _ := m.Update(key(tea.KeyDown))
	next, _ = next.Update(key(tea.KeyEnter))
	selected := next.(choiceModel)
	if !selected.done || selected.cancelled || selected.selected != 1 {
		t.Fatalf("selection: %+v", selected)
	}
	if selected.View().Content != "" {
		t.Fatal("finished picker is not cleared")
	}

	// A filtered index is not the index in the original options.
	m.list.SetFilterText("password")
	if m.list.FilterState() == list.Unfiltered {
		t.Fatal("filter was not applied")
	}
	next, _ = m.Update(key(tea.KeyEnter))
	if next.(choiceModel).selected != 1 {
		t.Fatal("filtered picker returned the wrong original index")
	}

	for _, msg := range []tea.KeyPressMsg{key(tea.KeyEscape), {Code: 'c', Mod: tea.ModCtrl}} {
		next, _ := newChoiceModel("Pick", []string{"one"}).Update(msg)
		if !next.(choiceModel).cancelled {
			t.Fatal("picker did not cancel")
		}
	}
}

func TestExplorerModelFiltersAndRunsActions(t *testing.T) {
	items := []provider.Item{
		{ID: "a", Title: "Alpha", Category: "Document", Kind: provider.KindDocument, Tags: []string{"store/team/api"}},
		{ID: "b", Title: "Beta", Category: "Login", Kind: provider.KindFields, Tags: []string{"store/team/web"}},
	}
	m := newExplorerModel("1Password vault Shared", items, "b", "", "")
	if got, ok := m.selectedItem(); !ok || got.ID != "b" {
		t.Fatalf("initial selection = %+v, %t", got, ok)
	}
	if !strings.Contains(m.details.View(), "Beta") || !strings.Contains(m.details.View(), "Login") {
		t.Fatalf("details do not describe the selection:\n%s", m.details.View())
	}

	next, _ := m.Update(tea.PasteMsg{Content: "alpha"})
	m = next.(explorerModel)
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("filter matched %d items; want 1", got)
	}
	if got, ok := m.selectedItem(); !ok || got.ID != "a" {
		t.Fatalf("filtered selection = %+v, %t", got, ok)
	}
	if !strings.Contains(m.details.View(), "Alpha") || !strings.Contains(m.details.View(), "team/api") {
		t.Fatalf("details did not refresh with the filtered selection:\n%s", m.details.View())
	}

	next, _ = m.Update(key(tea.KeyEscape))
	m = next.(explorerModel)
	if m.query != "" || len(m.list.VisibleItems()) != 2 {
		t.Fatalf("Escape did not clear the filter: query=%q items=%d", m.query, len(m.list.VisibleItems()))
	}

	next, _ = m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	m = next.(explorerModel)
	if m.action != explorerRename || m.actionItem.ID != "a" || !m.done {
		t.Fatalf("rename action = %+v", m)
	}

	m = newExplorerModel("vault", items, "a", "", "")
	next, _ = m.Update(tea.PasteMsg{Content: "beta"})
	m = next.(explorerModel)
	if got, ok := m.selectedItem(); !ok || got.ID != "b" {
		t.Fatalf("second filtered selection = %+v, %t", got, ok)
	}
	if !strings.Contains(m.details.View(), "Beta") || !strings.Contains(m.details.View(), "team/web") {
		t.Fatalf("details did not refresh to the second item:\n%s", m.details.View())
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Stored items (2)", "Beta", "Type: login", "Category: Login", "Tags: team/web", "ID: b"} {
		if !strings.Contains(view, want) {
			t.Errorf("browser view after filtering is missing %q:\n%s", want, m.View().Content)
		}
	}
}

func TestExplorerKeepsSelectionOnNonInputMessages(t *testing.T) {
	items := []provider.Item{{ID: "a", Title: "Alpha"}, {ID: "b", Title: "Beta"}}
	m := newExplorerModel("vault", items, "", "", "")
	next, _ := m.Update(key(tea.KeyDown))
	m = next.(explorerModel)
	next, _ = m.Update(struct{}{})
	m = next.(explorerModel)
	if item, ok := m.selectedItem(); !ok || item.ID != "b" {
		t.Fatalf("non-input message reset selection: %+v, %t", item, ok)
	}
}

func TestExplorerLongFilterAndStatus(t *testing.T) {
	prefix := strings.Repeat("a", 80)
	items := []provider.Item{{ID: "left", Title: prefix + "left"}, {ID: "right", Title: prefix + "right"}}
	m := newExplorerModel("vault", items, "", "", "First line\nSecond line")
	next, _ := m.Update(tea.PasteMsg{Content: prefix + "right"})
	m = next.(explorerModel)
	if item, ok := m.selectedItem(); !ok || item.ID != "right" || len(m.list.VisibleItems()) != 1 {
		t.Fatalf("long filter was truncated: %+v, %t", item, ok)
	}
	if view := m.View().Content; lipgloss.Width(view) > 80 || lipgloss.Height(view) > 24 {
		t.Fatalf("long filter or multiline status overflowed the frame:\n%s", view)
	}
}

func TestExplorerNeedsSelectionForItemActions(t *testing.T) {
	m := newExplorerModel("vault", nil, "", "", "")
	next, _ := m.Update(key(tea.KeyEnter))
	if got := next.(explorerModel); got.done {
		t.Fatal("pull exited the browser with no selected item")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if got := next.(explorerModel); got.done {
		t.Fatal("rename exited the browser with no selected item")
	}

	for _, size := range [][2]int{{120, 40}, {100, 28}, {80, 24}, {60, 18}, {40, 12}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(explorerModel)
		view := m.View().Content
		if !strings.Contains(view, "store tui") || !strings.Contains(view, "Ctrl+R") || !strings.Contains(view, "rename") {
			t.Fatalf("browser view at %dx%d is incomplete:\n%s", size[0], size[1], view)
		}
		if w, h := lipgloss.Width(view), lipgloss.Height(view); w > size[0] || h > size[1] {
			t.Fatalf("browser frame is %dx%d, outside terminal %dx%d", w, h, size[0], size[1])
		}
	}
}

func TestInputModel(t *testing.T) {
	m := newInputModel("Save file to", "./.env", false)
	if !m.input.Focused() {
		t.Fatal("input is not focused")
	}
	next, _ := m.Update(tea.PasteMsg{Content: "  /tmp/other.env  "})
	if got := next.(inputModel).value(); got != "/tmp/other.env" {
		t.Fatalf("typed destination = %q", got)
	}
	if got := m.value(); got != "./.env" {
		t.Fatalf("default destination = %q", got)
	}
	next, _ = next.Update(key(tea.KeyEnter))
	if !next.(inputModel).done || next.View().Content != "" {
		t.Fatal("confirmed prompt is not cleared")
	}

	secret := newInputModel("Text (input hidden):", "", true)
	payload := "  hidden-" + strings.Repeat("x", 300) + "  "
	next, _ = secret.Update(tea.PasteMsg{Content: payload})
	secret = next.(inputModel)
	if secret.value() != payload {
		t.Fatal("secret was trimmed or truncated")
	}
	if strings.Contains(secret.View().Content, "hidden-x") || strings.Contains(secret.View().Content, "***") {
		t.Fatal("secret or its length was displayed")
	}
	for _, msg := range []tea.KeyPressMsg{key(tea.KeyEscape), {Code: 'c', Mod: tea.ModCtrl}} {
		next, _ := secret.Update(msg)
		if !next.(inputModel).cancelled || next.View().Content != "" {
			t.Fatal("secret prompt did not cancel and clear")
		}
	}
}

func TestConsoleLineFallback(t *testing.T) {
	t.Setenv("STORE_INTERACTIVE", "1")
	in, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.WriteString("bad\n2\n\n  secret  \n"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	c := NewConsole(in, &out)
	if got, err := c.Choose("Pick", []string{"one", "two"}); err != nil || got != 1 {
		t.Fatalf("choice = %d, %v", got, err)
	}
	if got, err := c.Ask("Save", "./.env"); err != nil || got != "./.env" {
		t.Fatalf("default = %q, %v", got, err)
	}
	if got, err := c.ReadSecret("input hidden: "); err != nil || got != "  secret  " {
		t.Fatalf("secret = %q, %v", got, err)
	}
	if strings.Contains(out.String(), "secret") || strings.Contains(out.String(), "\x1b") {
		t.Fatalf("fallback leaked input or ANSI codes: %q", out.String())
	}
	if _, err := c.Choose("Empty", nil); err == nil {
		t.Fatal("empty picker succeeded")
	}
}
