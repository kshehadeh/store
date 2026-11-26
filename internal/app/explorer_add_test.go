package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/tags"
)

func send(t *testing.T, m explorerModel, msgs ...tea.Msg) explorerModel {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(explorerModel)
	}
	return m
}

var ctrlN = tea.KeyPressMsg{Code: 'n', Mod: tea.ModCtrl}

// addTree makes the working directory hold .env, notes.txt, and conf/app.yml.
func addTree(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env", "notes.txt", "conf/app.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

func entryTitles(p pathPicker) []string {
	var out []string
	for _, item := range p.list.Items() {
		out = append(out, item.(pathEntry).Title())
	}
	return out
}

func TestAddFile(t *testing.T) {
	dir := addTree(t)
	m := send(t, newExplorerModel("vault", nil, "", "", ""), ctrlN)
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Add item", "> File", "Folder", "Text"} {
		if !strings.Contains(view, want) {
			t.Errorf("type chooser is missing %q:\n%s", want, view)
		}
	}

	m = send(t, m, key(tea.KeyEnter))
	if got := entryTitles(m.add.picker); !slices.Equal(got, []string{"conf/", ".env", "notes.txt"}) {
		t.Fatalf("file picker entries = %q", got)
	}
	// Enter and Right open folders; Left returns with the folder selected.
	m = send(t, m, key(tea.KeyEnter))
	if m.add.picker.dir != filepath.Join(dir, "conf") || m.add.step != addChoosePath {
		t.Fatalf("Enter did not open the folder: %q", m.add.picker.dir)
	}
	m = send(t, m, key(tea.KeyLeft))
	if got, _ := m.add.picker.list.SelectedItem().(pathEntry); m.add.picker.dir != dir || got.name != "conf" {
		t.Fatalf("Left went to %q with %q selected", m.add.picker.dir, got.name)
	}
	m = send(t, m, tea.PasteMsg{Content: "ENV"})
	if got := entryTitles(m.add.picker); !slices.Equal(got, []string{".env"}) {
		t.Fatalf("filtered entries = %q", got)
	}
	m = send(t, m, key(tea.KeyEnter))
	if !m.adding() || !m.editing || m.add.path != filepath.Join(dir, ".env") || m.editNameInput.Value() != ".env" {
		t.Fatalf("details modal: step=%v path=%q name=%q", m.add.step, m.add.path, m.editNameInput.Value())
	}

	// The name is required, and reserved tags are rejected.
	m.editNameInput.SetValue("  ")
	m = send(t, m, key(tea.KeyEnter))
	if m.done || m.editError == "" {
		t.Fatal("blank name was accepted")
	}
	m.editNameInput.SetValue("root-env")
	m = send(t, m, key(tea.KeyTab), tea.PasteMsg{Content: "Local"}, key(tea.KeyTab), tea.PasteMsg{Content: "meta/x"}, key(tea.KeyEnter))
	if m.done || !strings.Contains(m.editError, "reserved") {
		t.Fatalf("reserved tag was accepted: %q", m.editError)
	}
	m.editTagsInput.SetValue("project/api, team/web")
	m = send(t, m, key(tea.KeyEnter))
	if !m.done || m.action != explorerCreate {
		t.Fatalf("save action = %v, done = %v", m.action, m.done)
	}
	o := m.addKeepOptions()
	if o.Path != filepath.Join(dir, ".env") || o.Text || !o.New || o.Name != "root-env" ||
		o.Description == nil || *o.Description != "Local" ||
		!slices.Equal(o.Tags, []string{"store/project/api", "store/team/web"}) {
		t.Fatalf("keep options = %+v", o)
	}
}

func TestAddFolder(t *testing.T) {
	dir := addTree(t)
	start := send(t, newExplorerModel("vault", nil, "", "", ""), ctrlN, key(tea.KeyDown), key(tea.KeyEnter))
	if got := entryTitles(start.add.picker); !slices.Equal(got, []string{"./ (this folder)", "conf/"}) {
		t.Fatalf("folder picker entries = %q", got)
	}
	if m := send(t, start, key(tea.KeyEnter)); m.add.path != dir || m.editNameInput.Value() != filepath.Base(dir) {
		t.Fatalf("this folder chose %q named %q", m.add.path, m.editNameInput.Value())
	}
	if m := send(t, start, key(tea.KeyDown), key(tea.KeyEnter)); m.add.path != filepath.Join(dir, "conf") || !m.adding() {
		t.Fatalf("Enter chose %q", m.add.path)
	}
	if m := send(t, start, key(tea.KeyDown), key(tea.KeyRight)); m.add.picker.dir != filepath.Join(dir, "conf") || m.adding() {
		t.Fatalf("Right opened %q", m.add.picker.dir)
	}
}

func openAddText(t *testing.T) explorerModel {
	t.Helper()
	return send(t, newExplorerModel("vault", nil, "", "", ""), ctrlN, key(tea.KeyDown), key(tea.KeyDown), key(tea.KeyEnter))
}

func TestAddTextIsHiddenAndExact(t *testing.T) {
	m := send(t, openAddText(t), key(tea.KeyEnter))
	if m.adding() || m.add.textErr == "" {
		t.Fatal("empty text was accepted")
	}
	m = send(t, m, tea.PasteMsg{Content: "  SECRET\tone\n\ntwo\n"},
		tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if view := m.View().Content; strings.Contains(view, "SECRET") || !strings.Contains(view, "Text entered (hidden)") {
		t.Fatalf("text was displayed:\n%s", view)
	}
	ctrlT := tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	if view := ansi.Strip(send(t, m, ctrlT).View().Content); !strings.Contains(view, "SECRET→one") {
		t.Fatalf("Ctrl+T did not show the text:\n%s", view)
	}
	m = send(t, m, key(tea.KeyEnter))
	if !m.adding() || strings.Contains(m.View().Content, "SECRET") || m.editNameInput.Value() != "" {
		t.Fatalf("details modal is wrong:\n%s", m.View().Content)
	}
	// Going back keeps the text.
	m = send(t, m, key(tea.KeyEscape), key(tea.KeyEnter), tea.PasteMsg{Content: "token"}, key(tea.KeyEnter))
	o := m.addKeepOptions()
	if !m.done || !o.Text || o.Path != "" || o.Name != "token" || o.Description != nil {
		t.Fatalf("keep options = %+v", o)
	}
	if want := "  SECRET\tone\n\ntwo\n\nx"; *o.TextContent != want {
		t.Fatalf("text = %q; want %q", *o.TextContent, want)
	}
}

func TestAddTextRejectsCarriageReturns(t *testing.T) {
	for _, content := range []string{"first\r\nsecond", "first\rsecond"} {
		m := send(t, openAddText(t), tea.PasteMsg{Content: "existing"}, tea.PasteMsg{Content: content})
		if m.add.textValue() != "existing" || !strings.Contains(m.add.textErr, "line endings") {
			t.Fatalf("paste changed the input to %q, error = %q", m.add.textValue(), m.add.textErr)
		}
	}
}

func TestAddTextRejectsWhatItCannotKeep(t *testing.T) {
	for _, bad := range []string{"a\x1b[0m", "a\x00", "a\ufdd0", "a\xff", strings.Repeat("\n", maxTextLines)} {
		m := send(t, openAddText(t), tea.PasteMsg{Content: bad})
		if m.add.textErr == "" || m.add.text.Value() != "" {
			t.Errorf("paste of %.10q was accepted", bad)
		}
	}
	content := strings.Repeat("x\n", maxTextLines-1)
	if m := send(t, openAddText(t), tea.PasteMsg{Content: content}); m.add.textErr != "" || m.add.textValue() != content {
		t.Errorf("long paste was changed: %q", m.add.textErr)
	}
}

func TestAddEscapeStepsBack(t *testing.T) {
	addTree(t)
	m := send(t, newExplorerModel("vault", nil, "", "", ""), ctrlN, key(tea.KeyEnter), tea.PasteMsg{Content: "notes"}, key(tea.KeyEnter))
	for _, want := range []addStep{addChoosePath, addChooseType, addInactive} {
		m = send(t, m, key(tea.KeyEscape))
		if m.add.step != want || m.editing || m.done {
			t.Fatalf("step = %v, editing = %v, done = %v; want step %v", m.add.step, m.editing, m.done, want)
		}
	}
	if !m.filter.Focused() {
		t.Fatal("filter did not regain focus")
	}
}

func TestAddPickerReportsUnreadableFolder(t *testing.T) {
	dir := addTree(t)
	p := newPathPicker(false, dir)
	p.open(filepath.Join(dir, "gone"), "")
	if p.err == "" || p.dir != dir || len(p.entries) != 3 {
		t.Fatalf("picker = %q in %q with %d entries", p.err, p.dir, len(p.entries))
	}
}

func TestAddModalsFitTerminalSizes(t *testing.T) {
	dir := addTree(t)
	for i := range 40 {
		if err := os.WriteFile(filepath.Join(dir, strings.Repeat("long", 20)+string(rune('a'+i))), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base := newExplorerModel("vault", nil, "", "", "")
	picker := send(t, base, ctrlN, key(tea.KeyEnter))
	picker.add.picker.err = strings.Repeat("error ", 30)
	text := send(t, openAddText(t), tea.PasteMsg{Content: strings.Repeat("\tline of text\n", 60)}, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})
	text.add.textErr = strings.Repeat("error ", 30)
	details := send(t, picker, tea.PasteMsg{Content: "long"}, key(tea.KeyEnter))
	details.editError = "a multi-line\nerror " + strings.Repeat("x", 100)
	for name, m := range map[string]explorerModel{"type": send(t, base, ctrlN), "picker": picker, "text": text, "details": details} {
		for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 18}, {40, 12}} {
			m = send(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			modal := m.addModalView()
			if m.editing {
				modal = m.editModalView()
			}
			if w, h := lipgloss.Width(modal), lipgloss.Height(modal); w > size[0] || h > size[1] {
				t.Errorf("%s modal at %v is %dx%d:\n%s", name, size, w, h, modal)
			}
			if !strings.Contains(ansi.Strip(modal), "╰") {
				t.Errorf("%s modal at %v lost its border", name, size)
			}
		}
	}
}

type keepProvider struct {
	provider.Provider
	items     []provider.Item
	created   provider.Document
	content   string
	err       error
	partialID string
}

func (p *keepProvider) ListItems(context.Context) ([]provider.Item, error) { return p.items, nil }

func (p *keepProvider) CreateDocument(_ context.Context, doc provider.Document) (string, error) {
	if p.err != nil {
		return p.partialID, p.err
	}
	data, err := io.ReadAll(doc.Content)
	p.created, p.content = doc, string(data)
	return "new-id", err
}

func TestPartialAddRetriesAsEdit(t *testing.T) {
	for _, tc := range []struct {
		id  string
		err error
	}{
		{"saved-id", errors.New("description failed")},
		{"", provider.ErrCreatedWithoutID},
	} {
		p := &keepProvider{partialID: tc.id, err: tc.err}
		a := &App{Provider: p}
		text, description := "value", "description"
		result, err := a.keep(context.Background(), KeepOptions{
			Name: "token", Text: true, TextContent: &text, Description: &description, New: true,
		})
		if !errors.Is(err, tc.err) || !result.created || result.id != tc.id {
			t.Fatalf("partial creation = %+v, %v", result, err)
		}
		if result.id == "" {
			continue
		}
		m := newExplorerModel("vault", nil, "", "", "")
		m.editCreatedItem(result, description, []string{"store/team/api"}, err)
		m = send(t, m, key(tea.KeyEnter))
		if m.action != explorerSaveEdit || m.actionItem.ID != tc.id || m.editedDescription != description ||
			!slices.Equal(m.editedTags, []string{"store/team/api"}) {
			t.Fatalf("retry action = %v, item = %+v", m.action, m.actionItem)
		}
	}
}

func TestKeepNew(t *testing.T) {
	// A store-managed document would be replaced by a plain keep.
	p := &keepProvider{items: []provider.Item{{ID: "a", Title: "taken", Kind: provider.KindDocument, Tags: []string{tags.Managed}}}}
	a := &App{Provider: p, Hostname: "laptop"}
	text := "line\tone\n"
	o := KeepOptions{Text: true, TextContent: &text, Name: "taken", Tags: []string{"team/api"}, New: true}
	if _, err := a.keep(context.Background(), o); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing name: %v", err)
	}
	o.Name = "fresh"
	r, err := a.keep(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if r.id != "new-id" || !r.created || p.content != text || !tags.Has(p.created.Tags, "store/team/api") || !tags.Has(p.created.Tags, "store/meta/type/text") {
		t.Fatalf("result = %+v, document = %+v, content = %q", r, p.created, p.content)
	}
}

func TestResumeAddKeepsEntries(t *testing.T) {
	m := send(t, openAddText(t), tea.PasteMsg{Content: "value"}, key(tea.KeyEnter), tea.PasteMsg{Content: "taken"}, key(tea.KeyEnter))
	m.resumeEdit(errors.New("already exists"))
	if m.done || !m.adding() || m.editNameInput.Value() != "taken" || m.add.textValue() != "value" {
		t.Fatalf("resumed model lost its entries: %+v", m.add)
	}
	if !strings.Contains(m.View().Content, "already exists") {
		t.Fatal("error is not shown")
	}
	m = send(t, m, tea.PasteMsg{Content: "-2"}, key(tea.KeyEnter))
	if !m.done || m.addKeepOptions().Name != "taken-2" {
		t.Fatalf("retry name = %q", m.addKeepOptions().Name)
	}
}
