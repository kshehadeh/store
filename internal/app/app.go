// Package app implements store's commands independently of any provider.
package app

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/tags"
)

type App struct {
	Provider provider.Provider
	// Location describes the configured vault for messages.
	Location string
	Console  *Console
	Stdout   io.Writer
	// Hostname is recorded in metadata tags for items this computer keeps.
	Hostname string
}

// discover returns the items with a store/ tag that match every filter,
// sorted by title.
func (a *App) discover(ctx context.Context, filters []string) ([]provider.Item, error) {
	all, err := a.Provider.ListItems(ctx)
	if err != nil {
		return nil, err
	}
	var out []provider.Item
	for _, it := range all {
		if tags.Discoverable(it.Tags) && tags.MatchesAll(it.Tags, filters) {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Title != out[j].Title {
			return out[i].Title < out[j].Title
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// typeLabel is the content type shown to users.
func typeLabel(it provider.Item) string {
	if it.Kind == provider.KindDocument {
		if t := tags.TypeOf(it.Tags); t != "" {
			return t
		}
		return tags.TypeFile
	}
	if it.Kind == provider.KindNote {
		return "note"
	}
	return strings.ToLower(it.Category)
}

// displayTags lists an item's user labels in short form. Automatic
// store/meta/ tags are left out; known ones are shown in their own columns.
func displayTags(ts []string) string {
	var out []string
	for _, t := range ts {
		if !tags.IsStore(t) || tags.IsMeta(t) {
			continue
		}
		out = append(out, tags.Short(t))
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func hostOf(ts []string) string { return tags.MetaValue(ts, "host") }

// sourceOf renders the recorded source directory as a path. Tags drop the
// leading slash, so it is restored unless the path starts with a drive letter.
func sourceOf(ts []string) string {
	v := tags.MetaValue(ts, "source")
	if v == "" {
		return ""
	}
	if first, _, _ := strings.Cut(v, "/"); strings.HasSuffix(first, ":") {
		return v
	}
	return "/" + v
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// safeFileName turns an item title into a single path segment.
func safeFileName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r < 0x20 || r == 0x7f {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	if s == "" || s == "." || s == ".." {
		return "item"
	}
	return s
}
