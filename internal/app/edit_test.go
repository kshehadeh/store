package app

import (
	"context"
	"slices"
	"testing"

	"github.com/kshehadeh/store/internal/provider"
)

type itemEditProvider struct {
	provider.Provider
	detail             provider.Detail
	updatedDescription *string
	updatedTags        []string
	updateCalls        int
}

func (p *itemEditProvider) GetItem(context.Context, string) (provider.Detail, error) {
	return p.detail, nil
}

func (p *itemEditProvider) UpdateItemMetadata(_ context.Context, _ string, description *string, tags []string) error {
	p.updateCalls++
	p.updatedDescription = description
	p.updatedTags = tags
	return nil
}

func TestEditItemReplacesStoreTagsAndPreservesOtherTags(t *testing.T) {
	p := &itemEditProvider{detail: provider.Detail{
		Item: provider.Item{ID: "item-id", Tags: []string{
			"store/meta/managed/v1", "store/meta/type/file", "store/team/old", "personal", "store",
		}},
		Description: "Old description",
	}}
	a := &App{Provider: p}

	if err := a.EditItem(context.Background(), "item-id", "New description", []string{"project/api", "team/new"}); err != nil {
		t.Fatal(err)
	}
	if p.updateCalls != 1 {
		t.Fatalf("UpdateItemMetadata called %d times; want 1", p.updateCalls)
	}
	if p.updatedDescription == nil || *p.updatedDescription != "New description" {
		t.Fatalf("description update = %v", p.updatedDescription)
	}
	want := []string{
		"personal", "store", "store/meta/managed/v1", "store/meta/type/file",
		"store/project/api", "store/team/new",
	}
	if !slices.Equal(p.updatedTags, want) {
		t.Fatalf("tags = %v; want %v", p.updatedTags, want)
	}
}

func TestEditItemNoOpPreservesDescriptionAndRejectsReservedTags(t *testing.T) {
	p := &itemEditProvider{detail: provider.Detail{
		Item:        provider.Item{ID: "item-id", Tags: []string{"store/meta/managed/v1", "store/team/api", "store", "personal"}},
		Description: "Existing",
	}}
	a := &App{Provider: p}

	if err := a.EditItem(context.Background(), "item-id", "Existing", []string{"team/api"}); err != nil {
		t.Fatal(err)
	}
	if p.updateCalls != 0 {
		t.Fatal("unchanged metadata caused an update")
	}
	if err := a.EditItem(context.Background(), "item-id", "Existing", []string{"meta/host/laptop"}); err == nil {
		t.Fatal("reserved tag was accepted")
	}
	if p.updateCalls != 0 {
		t.Fatal("invalid tags caused an update")
	}
}
