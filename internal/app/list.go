package app

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/kshehadeh/store/internal/tags"
)

type ListOptions struct {
	Tags []string
	JSON bool
}

type listEntry struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Category string     `json:"category"`
	Type     string     `json:"type"`
	Host     string     `json:"host,omitempty"`
	Source   string     `json:"source,omitempty"`
	Tags     []string   `json:"tags"`
	Updated  *time.Time `json:"updated,omitempty"`
}

// List shows discoverable items matching every tag filter. It never reads
// secret values.
func (a *App) List(ctx context.Context, o ListOptions) error {
	filters, err := tags.NormalizeAll(o.Tags, false)
	if err != nil {
		return err
	}
	items, err := a.discover(ctx, filters)
	if err != nil {
		return err
	}
	if o.JSON {
		entries := make([]listEntry, 0, len(items))
		for _, it := range items {
			e := listEntry{ID: it.ID, Name: it.Title, Category: it.Category, Type: typeLabel(it), Host: hostOf(it.Tags), Source: sourceOf(it.Tags), Tags: []string{}}
			for _, t := range it.Tags {
				if tags.IsStore(t) {
					e.Tags = append(e.Tags, t)
				}
			}
			if !it.Updated.IsZero() {
				u := it.Updated
				e.Updated = &u
			}
			entries = append(entries, e)
		}
		enc := json.NewEncoder(a.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}
	if len(items) == 0 {
		fmt.Fprintln(a.Console.Out, "No stored items match.")
		return nil
	}
	tw := tabwriter.NewWriter(a.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTYPE\tHOST\tSOURCE\tTAGS\tUPDATED\tID")
	for _, it := range items {
		updated := "-"
		if !it.Updated.IsZero() {
			updated = it.Updated.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", it.Title, typeLabel(it), orDash(hostOf(it.Tags)), orDash(sourceOf(it.Tags)), orDash(displayTags(it.Tags)), updated, it.ID)
	}
	return tw.Flush()
}
