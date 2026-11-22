package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kshehadeh/store/internal/tags"
)

// Rename changes the title of a discoverable item without changing its
// content, tags, or other fields.
func (a *App) Rename(ctx context.Context, id, title string) error {
	title = strings.TrimSpace(title)
	if id == "" {
		return errors.New("item ID is required")
	}
	if title == "" {
		return errors.New("item name cannot be empty")
	}
	if strings.ContainsAny(title, "\r\n") {
		return errors.New("item name cannot contain line breaks")
	}

	item, err := a.Provider.GetItem(ctx, id)
	if err != nil {
		return err
	}
	if !tags.Discoverable(item.Tags) {
		return fmt.Errorf("item %s has no store/ tag and cannot be renamed with store", id)
	}
	if item.Title == title {
		return nil
	}
	return a.Provider.RenameItem(ctx, id, title)
}
