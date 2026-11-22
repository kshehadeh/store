package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kshehadeh/store/internal/tags"
)

// EditItem changes a discoverable item's description and store-owned user tags.
// Other 1Password tags and store metadata tags are preserved.
func (a *App) EditItem(ctx context.Context, id, description string, userTags []string) error {
	if id == "" {
		return errors.New("item ID is required")
	}
	detail, err := a.Provider.GetItem(ctx, id)
	if err != nil {
		return err
	}
	if !tags.Discoverable(detail.Tags) {
		return fmt.Errorf("item %s has no store/ tag and cannot be edited with store", id)
	}

	normalizedTags, err := tags.NormalizeAll(userTags, true)
	if err != nil {
		return err
	}
	preservedTags := make([]string, 0, len(detail.Tags))
	for _, tag := range detail.Tags {
		if tag == tags.Root || !tags.IsStore(tag) || tags.IsMeta(tag) {
			preservedTags = append(preservedTags, tag)
		}
	}
	updatedTags := tags.Merge(nil, preservedTags, normalizedTags)
	currentTags := tags.Merge(nil, detail.Tags)
	if description == detail.Description && slices.Equal(updatedTags, currentTags) {
		return nil
	}

	var descriptionUpdate *string
	if description != detail.Description {
		descriptionUpdate = &description
	}
	return a.Provider.UpdateItemMetadata(ctx, id, descriptionUpdate, updatedTags)
}
