package app

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kshehadeh/store/internal/provider"
)

type metadataProvider struct {
	provider.Provider
	items []provider.Item
}

func (p metadataProvider) ListItems(context.Context) ([]provider.Item, error) {
	return p.items, nil
}

func TestRecentPickerOrder(t *testing.T) {
	newer := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	older := newer.Add(-time.Hour)
	item := func(id, title string, updated time.Time) provider.Item {
		return provider.Item{ID: id, Title: title, Updated: updated, Kind: provider.KindNote, Tags: []string{"store/test"}}
	}
	p := metadataProvider{items: []provider.Item{
		item("missing", "No timestamp", time.Time{}),
		item("old", "A older", older),
		item("new", "Z newest", newer),
		item("tie-b", "Same time", older),
		item("tie-a", "Same time", older),
		{ID: "unrelated", Title: "Unrelated", Updated: newer.Add(time.Hour)},
	}}
	var out bytes.Buffer
	a := &App{Provider: p, Console: &Console{Interactive: true, Out: &out, in: bufio.NewReader(strings.NewReader("1\n"))}}
	got, err := a.selectItem(context.Background(), "", nil)
	if err != nil || got.ID != "new" {
		t.Fatalf("selected %q, %v", got.ID, err)
	}
	labels := out.String()
	for _, want := range []string{"1) Z newest", "2) A older", "3) Same time  [note]  updated", "(tie-a)", "4) Same time", "(tie-b)", "5) No timestamp"} {
		if !strings.Contains(labels, want) {
			t.Errorf("picker missing %q:\n%s", want, labels)
		}
	}
	if strings.Contains(labels, "Unrelated") {
		t.Fatal("picker included an item without a store tag")
	}
	if strings.Index(labels, "(tie-a)") > strings.Index(labels, "(tie-b)") {
		t.Fatal("equal timestamps were not ordered by ID")
	}
	// A new keep refreshes an older item's timestamp, moving it to the top.
	p.items[1].Updated = newer.Add(time.Hour)
	a.Console.in = bufio.NewReader(strings.NewReader("1\n"))
	got, err = a.selectItem(context.Background(), "", nil)
	if err != nil || got.ID != "old" {
		t.Fatalf("updated item did not move to the top: %q, %v", got.ID, err)
	}
}
