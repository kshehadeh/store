package app

import (
	"context"
	"testing"

	"github.com/kshehadeh/store/internal/provider"
)

type renameProvider struct {
	provider.Provider
	detail   provider.Detail
	renamed  string
	renameID string
}

func (p *renameProvider) GetItem(context.Context, string) (provider.Detail, error) {
	return p.detail, nil
}

func (p *renameProvider) RenameItem(_ context.Context, id, title string) error {
	p.renameID, p.renamed = id, title
	return nil
}

func TestRenameChangesOnlyDiscoverableItemTitle(t *testing.T) {
	p := &renameProvider{detail: provider.Detail{Item: provider.Item{
		ID: "item-id", Title: "old title", Tags: []string{"store/project"},
	}}}
	a := &App{Provider: p}

	if err := a.Rename(context.Background(), "item-id", "  new title  "); err != nil {
		t.Fatal(err)
	}
	if p.renameID != "item-id" || p.renamed != "new title" {
		t.Fatalf("rename call = (%q, %q)", p.renameID, p.renamed)
	}
}

func TestRenameRejectsInvalidOrUndiscoverableItems(t *testing.T) {
	p := &renameProvider{detail: provider.Detail{Item: provider.Item{
		ID: "item-id", Title: "old title", Tags: []string{"other"},
	}}}
	a := &App{Provider: p}
	for _, title := range []string{"", " \t ", "bad\nname"} {
		if err := a.Rename(context.Background(), "item-id", title); err == nil {
			t.Errorf("Rename(%q) succeeded", title)
		}
	}
	if err := a.Rename(context.Background(), "item-id", "new title"); err == nil {
		t.Fatal("renamed an item without a store tag")
	}
	if p.renameID != "" {
		t.Fatalf("called provider RenameItem for an invalid item: %q", p.renameID)
	}
}
