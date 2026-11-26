package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type pathEntry struct {
	name string
	dir  bool
	// current is the row that stands for the open folder itself.
	current bool
}

func (e pathEntry) Title() string {
	if e.current {
		return "./ (this folder)"
	}
	// File names may hold control characters that would corrupt the screen.
	name := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, e.name)
	if e.dir {
		name += "/"
	}
	return name
}
func (e pathEntry) Description() string { return "" }
func (e pathEntry) FilterValue() string { return e.name }

// pathPicker browses the filesystem for a file, or for a folder when folders
// is set.
type pathPicker struct {
	folders bool
	dir     string
	entries []pathEntry
	filter  textinput.Model
	list    list.Model
	err     string
}

func newPathPicker(folders bool, dir string) pathPicker {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)
	l := list.New(nil, delegate, 30, 5)
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.SetFilteringEnabled(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)

	filter := textinput.New()
	filter.Prompt = "Filter: "
	filter.Placeholder = "Type to filter"
	filter.Focus()

	p := pathPicker{folders: folders, filter: filter, list: l, dir: dir}
	p.open(dir, "")
	return p
}

// open shows dir with the entry named selected highlighted. If dir cannot be
// read, the picker stays where it is and shows the error.
func (p *pathPicker) open(dir, selected string) {
	entries, err := readPathEntries(dir, p.folders)
	if err != nil {
		p.err = err.Error()
		return
	}
	p.err = ""
	p.dir = dir
	p.entries = entries
	p.filter.Reset()
	p.applyFilter()
	if selected == "" {
		return
	}
	for i, e := range entries {
		if e.name == selected {
			p.list.Select(i)
			return
		}
	}
}

func readPathEntries(dir string, foldersOnly bool) ([]pathEntry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var entries []pathEntry
	for _, d := range dirEntries {
		mode := d.Type()
		if mode&fs.ModeSymlink != 0 {
			info, err := os.Stat(filepath.Join(dir, d.Name()))
			if err != nil {
				continue
			}
			mode = info.Mode()
		}
		if mode.IsDir() || (mode.IsRegular() && !foldersOnly) {
			entries = append(entries, pathEntry{name: d.Name(), dir: mode.IsDir()})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].dir != entries[j].dir {
			return entries[i].dir
		}
		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})
	if foldersOnly {
		entries = append([]pathEntry{{dir: true, current: true}}, entries...)
	}
	return entries, nil
}

func (p *pathPicker) applyFilter() {
	query := strings.ToLower(p.filter.Value())
	var items []list.Item
	for _, e := range p.entries {
		if strings.Contains(strings.ToLower(e.name), query) {
			items = append(items, e)
		}
	}
	p.list.SetItems(items)
	p.list.Select(0)
}

func (p *pathPicker) openParent() {
	if parent := filepath.Dir(p.dir); parent != p.dir {
		p.open(parent, filepath.Base(p.dir))
	}
}

// update handles msg and returns the chosen absolute path, or "" if nothing
// has been chosen yet.
func (p *pathPicker) update(msg tea.Msg) (string, tea.Cmd) {
	var cmd tea.Cmd
	if key, ok := msg.(tea.KeyPressMsg); ok {
		entry, selected := p.list.SelectedItem().(pathEntry)
		path := filepath.Join(p.dir, entry.name)
		switch key.String() {
		case "up", "down", "pgup", "pgdown":
			p.list, cmd = p.list.Update(msg)
			return "", cmd
		case "left":
			p.openParent()
			return "", nil
		case "backspace":
			if p.filter.Value() == "" {
				p.openParent()
				return "", nil
			}
		case "right":
			if selected && entry.dir && !entry.current {
				p.open(path, "")
			}
			return "", nil
		case "enter":
			switch {
			case !selected:
			case entry.dir && !p.folders:
				p.open(path, "")
			default:
				return path, nil
			}
			return "", nil
		}
	}

	before := p.filter.Value()
	p.filter, cmd = p.filter.Update(msg)
	if p.filter.Value() != before {
		p.err = ""
		p.applyFilter()
	}
	return "", cmd
}

func (p *pathPicker) setSize(width, height int) {
	p.filter.SetWidth(max(1, width-ansi.StringWidth(p.filter.Prompt)-1))
	p.list.SetSize(width, max(1, height))
}

func (p pathPicker) hint() string {
	if p.folders {
		return "Enter choose • → open • ← up • Esc back"
	}
	return "Enter open/choose • ← up • Esc back"
}

// shortPath abbreviates the home directory and keeps the end of path visible
// when it is wider than width.
func shortPath(path string, width int) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			path = "~"
		} else if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
			path = filepath.Join("~", rest)
		}
	}
	path = pathEntry{name: path}.Title()
	if w := ansi.StringWidth(path); w > width {
		path = ansi.TruncateLeft(path, w-width+1, "…")
	}
	return path
}
