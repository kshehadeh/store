package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kshehadeh/store/internal/archive"
	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/restore"
	"github.com/kshehadeh/store/internal/tags"
)

type PullOptions struct {
	Name  string
	Tags  []string
	ID    string
	To    string
	AsZip bool
	Field string
}

// Pull retrieves one discoverable item. Text goes to stdout unchanged; files
// and folders are restored to a new local path.
func (a *App) Pull(ctx context.Context, o PullOptions) error {
	filters, err := tags.NormalizeAll(o.Tags, false)
	if err != nil {
		return err
	}
	var d provider.Detail
	if o.ID != "" {
		if d, err = a.Provider.GetItem(ctx, o.ID); err != nil {
			return err
		}
		if !tags.Discoverable(d.Tags) {
			return fmt.Errorf("item %s has no store/ tag; add one in 1Password to make it available to store", o.ID)
		}
		if (o.Name != "" && d.Title != o.Name) || !tags.MatchesAll(d.Tags, filters) {
			return fmt.Errorf("item %s does not match the given name and tags", o.ID)
		}
	} else {
		if o.Name == "" && len(filters) == 0 && !a.Console.Interactive {
			return errors.New("specify a NAME, --tag, or --id when the terminal is not interactive")
		}
		item, err := a.selectItem(ctx, o.Name, filters)
		if err != nil {
			return err
		}
		if d, err = a.Provider.GetItem(ctx, item.ID); err != nil {
			return err
		}
	}
	return a.deliver(ctx, d, o)
}

func (a *App) selectItem(ctx context.Context, name string, filters []string) (provider.Item, error) {
	recent := name == "" && len(filters) == 0
	found, err := a.discover(ctx, filters)
	if err != nil {
		return provider.Item{}, err
	}
	var cands []provider.Item
	for _, it := range found {
		if name == "" || it.Title == name {
			cands = append(cands, it)
		}
	}
	if recent {
		// discover already orders by title and ID, providing stable ties and
		// keeping items without an update timestamp after dated items.
		sort.SliceStable(cands, func(i, j int) bool {
			return cands[i].Updated.After(cands[j].Updated)
		})
	}
	switch len(cands) {
	case 0:
		if recent {
			return provider.Item{}, errors.New("no stored items are available; use `store keep` or add a store/ tag to an item")
		}
		return provider.Item{}, fmt.Errorf("no stored item matches %s; run `store list` to see what is available", describeQuery(name, filters))
	case 1:
		if !recent {
			return cands[0], nil
		}
	}
	labels := make([]string, len(cands))
	for i, c := range cands {
		label := fmt.Sprintf("%s  [%s]", c.Title, typeLabel(c))
		if recent && !c.Updated.IsZero() {
			label += "  updated " + c.Updated.Local().Format("2006-01-02 15:04")
		}
		if h := hostOf(c.Tags); h != "" {
			label += "  from " + h
		}
		if t := displayTags(c.Tags); t != "" {
			label += "  " + t
		}
		labels[i] = label + "  (" + c.ID + ")"
	}
	if !a.Console.Interactive {
		return provider.Item{}, fmt.Errorf("%d items match %s; rerun with --id to choose one:\n  %s", len(cands), describeQuery(name, filters), strings.Join(labels, "\n  "))
	}
	prompt := fmt.Sprintf("%d items match %s:", len(cands), describeQuery(name, filters))
	if recent {
		prompt = "Choose an item to pull (most recently updated first):"
	}
	idx, err := a.Console.Choose(prompt, labels)
	if err != nil {
		return provider.Item{}, err
	}
	return cands[idx], nil
}

func describeQuery(name string, filters []string) string {
	var parts []string
	if name != "" {
		parts = append(parts, fmt.Sprintf("name %q", name))
	}
	for _, f := range filters {
		parts = append(parts, "tag "+tags.Short(f))
	}
	return strings.Join(parts, " and ")
}

func (a *App) deliver(ctx context.Context, d provider.Detail, o PullOptions) error {
	if o.Field != "" && d.Kind != provider.KindFields {
		return fmt.Errorf("--field applies only to items with fields; %q is a %s", d.Title, typeLabel(d.Item))
	}
	kind := typeLabel(d.Item)
	if o.AsZip && (d.Kind != provider.KindDocument || kind != tags.TypeFolder) {
		return fmt.Errorf("--as-zip applies only to folders; %q is a %s", d.Title, kind)
	}
	textName := safeFileName(d.Title) + ".txt"
	switch d.Kind {
	case provider.KindDocument:
		switch {
		case kind == tags.TypeText:
			data, err := a.downloadBytes(ctx, d.ID)
			if err != nil {
				return err
			}
			return a.emitText(data, o.To, textName)
		case kind == tags.TypeFolder && !o.AsZip:
			return a.pullFolder(ctx, d, o.To)
		default:
			return a.pullFile(ctx, d, o.To, documentFileName(d))
		}
	case provider.KindNote:
		note, err := a.Provider.ReadNote(ctx, d.ID)
		if err != nil {
			return err
		}
		return a.emitText([]byte(note), o.To, textName)
	default:
		f, err := a.chooseField(d, o.Field)
		if err != nil {
			return err
		}
		v, err := a.Provider.ReadField(ctx, d.ID, f.ID)
		if err != nil {
			return err
		}
		return a.emitText([]byte(v), o.To, textName)
	}
}

func documentFileName(d provider.Detail) string {
	if d.FileName != "" {
		if n := safeFileName(filepath.Base(d.FileName)); n != "item" {
			return n
		}
	}
	return safeFileName(d.Title)
}

func fieldName(f provider.Field) string {
	if f.Section != "" {
		return f.Section + "." + f.Label
	}
	return f.Label
}

// chooseField selects a field by ID, label, or section.label. Without an
// explicit choice it asks, and never picks a field on the user's behalf.
func (a *App) chooseField(d provider.Detail, want string) (provider.Field, error) {
	if len(d.Fields) == 0 {
		return provider.Field{}, fmt.Errorf("%q has no fields to pull", d.Title)
	}
	names := make([]string, len(d.Fields))
	for i, f := range d.Fields {
		names[i] = fieldName(f)
	}
	if want == "" {
		if !a.Console.Interactive {
			return provider.Field{}, fmt.Errorf("%q is a %s; choose a field with --field (available: %s)", d.Title, d.Category, strings.Join(names, ", "))
		}
		idx, err := a.Console.Choose(fmt.Sprintf("%q is a %s. Which field?", d.Title, d.Category), names)
		if err != nil {
			return provider.Field{}, err
		}
		return d.Fields[idx], nil
	}
	for _, f := range d.Fields {
		if f.ID == want {
			return f, nil
		}
	}
	var matches []provider.Field
	for _, f := range d.Fields {
		if strings.EqualFold(f.Label, want) || strings.EqualFold(fieldName(f), want) {
			matches = append(matches, f)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return provider.Field{}, fmt.Errorf("%q has no field %q (available: %s)", d.Title, want, strings.Join(names, ", "))
	default:
		return provider.Field{}, fmt.Errorf("several fields on %q match %q; use section.label or a field ID", d.Title, want)
	}
}

// destination resolves --to, or asks for a path when it is absent.
func (a *App) destination(to, defaultName, what string) (string, error) {
	if to == "" {
		if !a.Console.Interactive {
			return "", fmt.Errorf("--to PATH is required to save a %s when the terminal is not interactive", what)
		}
		ans, err := a.Console.Ask(fmt.Sprintf("Save %s to", what), "."+string(filepath.Separator)+defaultName)
		if err != nil {
			return "", err
		}
		to = ans
	}
	return restore.Resolve(to, defaultName)
}

func (a *App) emitText(data []byte, to, defaultName string) error {
	if to == "" {
		_, err := a.Stdout.Write(data)
		return err
	}
	final, err := restore.Resolve(to, defaultName)
	if err != nil {
		return err
	}
	stage, err := restore.NewStage(final)
	if err != nil {
		return err
	}
	defer stage.Cleanup()
	p := stage.Path("payload")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return err
	}
	if err := stage.PublishFile(p); err != nil {
		return err
	}
	a.Console.Info("Saved to %s", final)
	return nil
}

func (a *App) downloadBytes(ctx context.Context, id string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "store-pull-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "payload")
	if err := a.Provider.DownloadDocument(ctx, id, p); err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func (a *App) pullFile(ctx context.Context, d provider.Detail, to, defaultName string) error {
	final, err := a.destination(to, defaultName, "file")
	if err != nil {
		return err
	}
	stage, err := restore.NewStage(final)
	if err != nil {
		return err
	}
	defer stage.Cleanup()
	p := stage.Path("payload")
	if err := a.Provider.DownloadDocument(ctx, d.ID, p); err != nil {
		return err
	}
	if err := stage.PublishFile(p); err != nil {
		return err
	}
	a.Console.Info("Saved %q to %s", d.Title, final)
	return nil
}

func (a *App) pullFolder(ctx context.Context, d provider.Detail, to string) error {
	name := strings.TrimSuffix(documentFileName(d), ".zip")
	if name == "" {
		name = safeFileName(d.Title)
	}
	final, err := a.destination(to, name, "folder")
	if err != nil {
		return err
	}
	stage, err := restore.NewStage(final)
	if err != nil {
		return err
	}
	defer stage.Cleanup()
	zipPath := stage.Path("archive.zip")
	if err := a.Provider.DownloadDocument(ctx, d.ID, zipPath); err != nil {
		return err
	}
	tree := stage.Path("tree")
	if err := archive.Extract(zipPath, tree, archive.DefaultLimits); err != nil {
		return fmt.Errorf("cannot restore %q: %w", d.Title, err)
	}
	if err := stage.PublishDir(tree); err != nil {
		return err
	}
	a.Console.Info("Restored folder %q to %s", d.Title, final)
	return nil
}
