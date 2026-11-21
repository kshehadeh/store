package app

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func key(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code}
}

func TestChoiceModel(t *testing.T) {
	m := newChoiceModel("Pick a field", []string{"username", "password"})
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
