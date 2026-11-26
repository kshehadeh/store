package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/kshehadeh/store/internal/archive"
	"github.com/kshehadeh/store/internal/provider"
	"github.com/kshehadeh/store/internal/restore"
	"github.com/kshehadeh/store/internal/tags"
)

type KeepOptions struct {
	Path string
	Text bool
	// TextContent is the text to keep. When nil, text is read from the console.
	TextContent *string
	Name        string
	Tags        []string
	Description *string
	// New refuses to keep over an existing item with the same name.
	New bool
}

type keepResult struct {
	id      string
	name    string
	kind    string
	size    int64
	created bool
}

type payload struct {
	content   io.Reader
	fileName  string
	kind      string
	size      int64
	sourceDir string
	cleanup   func()
}

// Keep creates a document for name, or replaces the content of the document
// store created for it earlier.
func (a *App) Keep(ctx context.Context, o KeepOptions) error {
	r, err := a.keep(ctx, o)
	if err != nil {
		return err
	}
	verb := "Updated"
	if r.created {
		verb = "Created"
	}
	fmt.Fprintf(a.Stdout, "%s %q (%s, %s) in %s\n", verb, r.name, r.kind, humanSize(r.size), a.Location)
	return nil
}

func (a *App) keep(ctx context.Context, o KeepOptions) (keepResult, error) {
	fail := func(err error) (keepResult, error) { return keepResult{}, err }
	name := strings.TrimSpace(o.Name)
	if name == "" {
		return fail(errors.New("--name is required"))
	}
	if strings.ContainsAny(name, "\r\n") {
		return fail(errors.New("--name cannot contain line breaks"))
	}
	if o.Text && o.Path != "" {
		return fail(errors.New("provide either a PATH or --text, not both"))
	}
	if !o.Text && o.Path == "" {
		return fail(errors.New("provide a PATH to keep, or --text to keep text from stdin"))
	}
	userTags, err := tags.NormalizeAll(o.Tags, true)
	if err != nil {
		return fail(err)
	}

	p, err := a.preparePayload(o, name)
	if err != nil {
		return fail(err)
	}
	defer p.cleanup()

	var existing *provider.Item
	if o.New {
		err = a.requireUnusedName(ctx, name)
	} else {
		existing, err = a.findForKeep(ctx, name)
	}
	if err != nil {
		return fail(err)
	}
	result := keepResult{name: name, kind: p.kind, size: p.size, created: existing == nil}
	meta := a.metaTags(p.kind, p.sourceDir)
	doc := provider.Document{Title: name, FileName: p.fileName, Content: p.content, Description: o.Description}
	if existing == nil {
		doc.Tags = tags.Merge(nil, userTags, meta)
		if result.id, err = a.Provider.CreateDocument(ctx, doc); err != nil {
			result.created = result.id != "" || errors.Is(err, provider.ErrCreatedWithoutID)
			return result, err
		}
		return result, nil
	}
	doc.Tags = tags.Merge(existing.Tags, userTags, meta)
	if err := a.Provider.UpdateDocument(ctx, existing.ID, doc); err != nil {
		return fail(err)
	}
	result.id = existing.ID
	return result, nil
}

// requireUnusedName returns an error if any item in the vault is titled name.
func (a *App) requireUnusedName(ctx context.Context, name string) error {
	items, err := a.Provider.ListItems(ctx)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Title == name {
			return fmt.Errorf("%q already exists; choose a different name", name)
		}
	}
	return nil
}

// findForKeep returns the managed document titled name, nil if no item has
// that title, or an error if updating would be unsafe.
func (a *App) findForKeep(ctx context.Context, name string) (*provider.Item, error) {
	items, err := a.Provider.ListItems(ctx)
	if err != nil {
		return nil, err
	}
	var matches []provider.Item
	for _, it := range items {
		if it.Title == name {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		m := matches[0]
		if m.Kind != provider.KindDocument || !tags.Has(m.Tags, tags.Managed) {
			return nil, fmt.Errorf("%q already exists in %s as a %s that store did not create; choose a different --name", name, a.Location, m.Category)
		}
		return &m, nil
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.ID
		}
		return nil, fmt.Errorf("%d items in %s are named %q (%s); rename or remove the duplicates first", len(matches), a.Location, name, strings.Join(ids, ", "))
	}
}

func (a *App) metaTags(kind, sourceDir string) []string {
	out := []string{tags.Managed, tags.Build(tags.TypeRoot, kind), tags.Build(tags.OSRoot, runtime.GOOS)}
	if h := tags.Build(tags.HostRoot, a.Hostname); h != "" {
		out = append(out, h)
	}
	if sourceDir != "" {
		if s := tags.Build(tags.SourceRoot, strings.Split(filepath.ToSlash(sourceDir), "/")...); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (a *App) preparePayload(o KeepOptions, name string) (*payload, error) {
	if o.Text {
		return a.textPayload(name, o.TextContent)
	}
	abs, err := filepath.Abs(restore.ExpandHome(o.Path))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s does not exist (to keep text, use --text)", o.Path)
	}
	if err != nil {
		return nil, err
	}
	switch {
	case info.Mode().IsRegular():
		f, err := os.Open(abs)
		if err != nil {
			return nil, err
		}
		return &payload{
			content:   f,
			fileName:  filepath.Base(abs),
			kind:      tags.TypeFile,
			size:      info.Size(),
			sourceDir: filepath.Dir(abs),
			cleanup:   func() { f.Close() },
		}, nil
	case info.IsDir():
		return folderPayload(abs)
	default:
		return nil, fmt.Errorf("%s is not a regular file or folder", o.Path)
	}
}

func folderPayload(abs string) (*payload, error) {
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp("", "store-keep-*.zip")
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}
	if err := archive.PackDir(root, tmp); err != nil {
		cleanup()
		return nil, err
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err == nil {
		_, err = tmp.Seek(0, io.SeekStart)
	}
	if err != nil {
		cleanup()
		return nil, err
	}
	base := filepath.Base(abs)
	if base == string(filepath.Separator) || base == "." {
		base = "folder"
	}
	return &payload{
		content:   tmp,
		fileName:  base + ".zip",
		kind:      tags.TypeFolder,
		size:      size,
		sourceDir: filepath.Dir(abs),
		cleanup:   cleanup,
	}, nil
}

func (a *App) textPayload(name string, content *string) (*payload, error) {
	var data []byte
	if content != nil {
		data = []byte(*content)
	} else if a.Console.Interactive {
		s, err := a.Console.ReadSecret("Text to keep (input hidden): ")
		if err != nil {
			return nil, err
		}
		data = []byte(s)
	} else {
		var err error
		if data, err = io.ReadAll(a.Console.Reader()); err != nil {
			return nil, err
		}
	}
	if len(data) == 0 {
		return nil, errors.New("no text was provided")
	}
	if !utf8.Valid(data) {
		return nil, errors.New("text must be valid UTF-8; keep binary data as a file instead")
	}
	return &payload{
		content:  bytes.NewReader(data),
		fileName: safeFileName(name) + ".txt",
		kind:     tags.TypeText,
		size:     int64(len(data)),
		cleanup:  func() {},
	}, nil
}
