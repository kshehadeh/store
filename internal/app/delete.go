package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kshehadeh/store/internal/tags"
)

// Delete asks for confirmation before deleting a discoverable item by name or ID.
// It returns the deleted title and whether the user confirmed the deletion.
func (a *App) Delete(ctx context.Context, name, id string) (string, bool, error) {
	if (name == "") == (id == "") {
		return "", false, errors.New("specify either a NAME or --id")
	}
	if a.Console == nil || !a.Console.Interactive {
		return "", false, errors.New("deletion requires interactive confirmation")
	}
	if id == "" {
		item, err := a.selectItem(ctx, name, nil)
		if err != nil {
			return "", false, err
		}
		id = item.ID
	}

	item, err := a.Provider.GetItem(ctx, id)
	if err != nil {
		return "", false, err
	}
	if item.ID != id {
		return "", false, fmt.Errorf("no stored item has ID %q", id)
	}
	if !tags.Discoverable(item.Tags) {
		return "", false, fmt.Errorf("item %q has no store/ tag and cannot be deleted with store", item.ID)
	}
	prompt := fmt.Sprintf("Delete %q (%q) from %s? Type yes to confirm", item.Title, item.ID, a.Location)
	answer, err := a.Console.Ask(prompt, "no")
	if errors.Is(err, errCancelled) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !strings.EqualFold(strings.TrimSpace(answer), "yes") {
		return "", false, nil
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if err := a.Provider.DeleteItem(ctx, item.ID); err != nil {
		return "", false, err
	}
	return item.Title, true, nil
}
