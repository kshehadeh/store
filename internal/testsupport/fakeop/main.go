// Command fakeop emulates the subset of the 1Password CLI that store uses,
// backed by a JSON file named by FAKEOP_STATE. It rejects flags store is not
// expected to send, so argument mistakes fail tests. Every invocation's
// arguments are appended to FAKEOP_LOG.
//
// Test-only commands: __seed (item JSON on stdin), __tag ID TAG, __dump.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

type account struct {
	URL         string `json:"url"`
	Email       string `json:"email"`
	UserUUID    string `json:"user_uuid"`
	AccountUUID string `json:"account_uuid"`
}

type vault struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type section struct {
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

type field struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Purpose string   `json:"purpose,omitempty"`
	Label   string   `json:"label"`
	Value   string   `json:"value,omitempty"`
	Section *section `json:"section,omitempty"`
}

type item struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Category  string   `json:"category"`
	Vault     string   `json:"vault"`
	Tags      []string `json:"tags,omitempty"`
	Version   int      `json:"version"`
	UpdatedAt string   `json:"updated_at"`
	Fields    []field  `json:"fields,omitempty"`
	FileName  string   `json:"file_name,omitempty"`
	Content   []byte   `json:"content,omitempty"`
}

type state struct {
	Accounts []account `json:"accounts"`
	Vaults   []vault   `json:"vaults"`
	Items    []item    `json:"items"`
	Next     int       `json:"next"`
}

type vaultRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type listOut struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Version   int      `json:"version"`
	Vault     vaultRef `json:"vault"`
	Category  string   `json:"category"`
	Tags      []string `json:"tags,omitempty"`
	UpdatedAt string   `json:"updated_at"`
}

type fileOut struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int    `json:"size"`
	ContentPath string `json:"content_path"`
}

type getOut struct {
	listOut
	Fields []field   `json:"fields,omitempty"`
	Files  []fileOut `json:"files,omitempty"`
}

var boolFlags = map[string]bool{"--reveal": true, "--force": true, "--version": true}

var allowed = map[string][]string{
	"whoami":          {},
	"account list":    {},
	"vault list":      {},
	"vault get":       {},
	"item list":       {"--vault", "--tags"},
	"item get":        {"--vault", "--reveal"},
	"item delete":     {"--vault"},
	"item edit":       {"--vault", "--tags", "--title"},
	"document create": {"--vault", "--title", "--file-name", "--tags"},
	"document edit":   {"--vault", "--title", "--file-name", "--tags"},
	"document get":    {"--vault", "--out-file", "--file-mode", "--force"},
	"__seed":          {},
	"__tag":           {},
	"__dump":          {},
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] 2026/01/02 03:04:05 %s\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if p := os.Getenv("FAKEOP_LOG"); p != "" {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			b, _ := json.Marshal(args)
			f.Write(append(b, '\n'))
			f.Close()
		}
	}
	pos, flags, err := parse(args)
	if err != nil {
		return err
	}
	if _, ok := flags["--version"]; ok && len(pos) == 0 {
		fmt.Println("2.42.0")
		return nil
	}
	if len(pos) == 0 {
		return errors.New("no command")
	}
	cmd := pos[0]
	if len(pos) >= 2 && (cmd == "item" || cmd == "document" || cmd == "vault" || cmd == "account") {
		cmd += " " + pos[1]
	}
	ok, known := allowed[cmd]
	if !known {
		return fmt.Errorf("unknown command %q", cmd)
	}
	for f := range flags {
		if f == "--account" || f == "--format" || contains(ok, f) {
			continue
		}
		return fmt.Errorf("unknown flag: %s for %s", f, cmd)
	}
	if v, ok := flags["--format"]; ok && v != "json" {
		return fmt.Errorf("unsupported format %q", v)
	}

	st, err := load()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(cmd, "__") {
		if os.Getenv("FAKEOP_SIGNED_OUT") == "1" && cmd != "account list" {
			return errors.New("account is not signed in")
		}
		if a, ok := flags["--account"]; ok && !st.hasAccount(a) {
			return fmt.Errorf("no account found for filter %q", a)
		}
	}

	switch cmd {
	case "whoami":
		return emit(map[string]string{"email": st.Accounts[0].Email, "url": st.Accounts[0].URL, "user_uuid": st.Accounts[0].UserUUID, "account_uuid": st.Accounts[0].AccountUUID})
	case "account list":
		return emit(st.Accounts)
	case "vault list":
		return emit(st.Vaults)
	case "vault get":
		v, err := st.vault(arg(pos, 2))
		if err != nil {
			return err
		}
		return emit(v)
	case "item list":
		v, err := st.vault(flags["--vault"])
		if err != nil {
			return err
		}
		var filters []string
		if t := flags["--tags"]; t != "" {
			filters = strings.Split(t, ",")
		}
		out := []listOut{}
		for _, it := range st.Items {
			if it.Vault == v.ID && matchesAny(it.Tags, filters) {
				out = append(out, st.listOut(it))
			}
		}
		return emit(out)
	case "item get":
		it, err := st.find(arg(pos, 2), flags["--vault"])
		if err != nil {
			return err
		}
		g := getOut{listOut: st.listOut(*it)}
		for _, f := range it.Fields {
			if f.Type == "CONCEALED" && flags["--reveal"] == "" {
				f.Value = ""
			}
			g.Fields = append(g.Fields, f)
		}
		if it.Category == "DOCUMENT" {
			g.Files = []fileOut{{ID: "file-" + it.ID, Name: it.FileName, Size: len(it.Content), ContentPath: "/v1/files/" + it.ID}}
		}
		return emit(g)
	case "item delete":
		it, err := st.find(arg(pos, 2), flags["--vault"])
		if err != nil {
			return err
		}
		for i := range st.Items {
			if &st.Items[i] == it {
				st.Items = append(st.Items[:i], st.Items[i+1:]...)
				return st.save()
			}
		}
		return errors.New("item not found")
	case "item edit":
		it, err := st.find(arg(pos, 2), flags["--vault"])
		if err != nil {
			return err
		}
		assignment := arg(pos, 3)
		if assignment != "" {
			fieldName, value, ok := strings.Cut(assignment, "=")
			if !ok || fieldName != "Description[text]" {
				return fmt.Errorf("unsupported item edit assignment %q", assignment)
			}
			found := false
			for i := range it.Fields {
				if it.Fields[i].Label == "Description" {
					it.Fields[i].Type = "STRING"
					it.Fields[i].Value = value
					found = true
				}
			}
			if !found {
				it.Fields = append(it.Fields, field{
					ID: "description", Type: "STRING", Label: "Description", Value: value,
				})
			}
		}
		if title, ok := flags["--title"]; ok {
			it.Title = title
		}
		if itemTags, ok := flags["--tags"]; ok {
			it.Tags = splitTags(itemTags)
		}
		it.Version++
		it.UpdatedAt = now()
		return st.save()
	case "document create":
		if arg(pos, 2) != "-" {
			return errors.New("expected '-' to read the document from stdin")
		}
		v, err := st.vault(flags["--vault"])
		if err != nil {
			return err
		}
		data, err := readPipedStdin()
		if err != nil {
			return err
		}
		st.Next++
		it := item{
			ID:        fmt.Sprintf("doc%04d", st.Next),
			Title:     flags["--title"],
			Category:  "DOCUMENT",
			Vault:     v.ID,
			Tags:      splitTags(flags["--tags"]),
			Version:   1,
			UpdatedAt: now(),
			FileName:  flags["--file-name"],
			Content:   data,
		}
		st.Items = append(st.Items, it)
		if err := st.save(); err != nil {
			return err
		}
		return emit(map[string]string{"uuid": it.ID, "createdAt": it.UpdatedAt, "updatedAt": it.UpdatedAt, "vaultUuid": v.ID})
	case "document edit":
		it, err := st.find(arg(pos, 2), flags["--vault"])
		if err != nil {
			return err
		}
		if it.Category != "DOCUMENT" {
			return errors.New("item is not a document")
		}
		if arg(pos, 3) != "-" {
			return errors.New("expected '-' to read the document from stdin")
		}
		data, err := readPipedStdin()
		if err != nil {
			return err
		}
		it.Content = data
		if v, ok := flags["--file-name"]; ok {
			it.FileName = v
		}
		if v, ok := flags["--title"]; ok {
			it.Title = v
		}
		if v, ok := flags["--tags"]; ok {
			it.Tags = splitTags(v)
		}
		it.Version++
		it.UpdatedAt = now()
		return st.save()
	case "document get":
		it, err := st.find(arg(pos, 2), flags["--vault"])
		if err != nil {
			return err
		}
		if it.Category != "DOCUMENT" {
			return errors.New("item is not a document")
		}
		out := flags["--out-file"]
		if out == "" {
			_, err := os.Stdout.Write(it.Content)
			return err
		}
		if _, err := os.Stat(out); err == nil && flags["--force"] == "" {
			return errors.New("file already exists; use --force")
		}
		mode := os.FileMode(0o600)
		if m := flags["--file-mode"]; m != "" {
			n, err := strconv.ParseUint(m, 8, 32)
			if err != nil {
				return err
			}
			mode = os.FileMode(n)
		}
		return os.WriteFile(out, it.Content, mode)
	case "__seed":
		var it item
		if err := json.NewDecoder(os.Stdin).Decode(&it); err != nil {
			return err
		}
		v, err := st.vault(it.Vault)
		if err != nil {
			return err
		}
		it.Vault = v.ID
		if it.ID == "" {
			st.Next++
			it.ID = fmt.Sprintf("seed%04d", st.Next)
		}
		it.Version = 1
		it.UpdatedAt = now()
		st.Items = append(st.Items, it)
		if err := st.save(); err != nil {
			return err
		}
		fmt.Print(it.ID)
		return nil
	case "__tag":
		it, err := st.find(arg(pos, 1), "")
		if err != nil {
			return err
		}
		it.Tags = append(it.Tags, arg(pos, 2))
		return st.save()
	case "__dump":
		return emit(st)
	}
	return fmt.Errorf("unhandled command %q", cmd)
}

func parse(args []string) ([]string, map[string]string, error) {
	var pos []string
	flags := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-" || !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
			continue
		}
		if k, v, ok := strings.Cut(a, "="); ok {
			flags[k] = v
			continue
		}
		if boolFlags[a] {
			flags[a] = "true"
			continue
		}
		if i+1 >= len(args) {
			return nil, nil, fmt.Errorf("flag needs an argument: %s", a)
		}
		flags[a] = args[i+1]
		i++
	}
	return pos, flags, nil
}

func arg(pos []string, i int) string {
	if i < len(pos) {
		return pos[i]
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func splitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func matchesAny(tags, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		for _, t := range tags {
			if t == f || strings.HasPrefix(t, f+"/") {
				return true
			}
		}
	}
	return false
}

// readPipedStdin mirrors the real op, which rejects stdin that is a regular
// file (for example "< file") and only accepts piped data.
func readPipedStdin() ([]byte, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode().IsRegular() {
		return nil, errors.New("expected data on stdin but none found")
	}
	return io.ReadAll(os.Stdin)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func emit(v any) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(v)
}

func load() (*state, error) {
	data, err := os.ReadFile(os.Getenv("FAKEOP_STATE"))
	if err != nil {
		return nil, err
	}
	var st state
	return &st, json.Unmarshal(data, &st)
}

func (st *state) save() error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(os.Getenv("FAKEOP_STATE"), data, 0o600)
}

func (st *state) hasAccount(a string) bool {
	for _, x := range st.Accounts {
		if a == x.AccountUUID || a == x.URL || a == x.Email || a == x.UserUUID {
			return true
		}
	}
	return false
}

func (st *state) vault(ref string) (vault, error) {
	for _, v := range st.Vaults {
		if v.ID == ref || v.Name == ref {
			return v, nil
		}
	}
	return vault{}, fmt.Errorf("%q isn't a vault in this account", ref)
}

func (st *state) listOut(it item) listOut {
	v, _ := st.vault(it.Vault)
	return listOut{ID: it.ID, Title: it.Title, Version: it.Version, Vault: vaultRef(v), Category: it.Category, Tags: it.Tags, UpdatedAt: it.UpdatedAt}
}

func (st *state) find(ref, vaultRef string) (*item, error) {
	var vid string
	if vaultRef != "" {
		v, err := st.vault(vaultRef)
		if err != nil {
			return nil, err
		}
		vid = v.ID
	}
	var matches []*item
	for i := range st.Items {
		it := &st.Items[i]
		if vid != "" && it.Vault != vid {
			continue
		}
		if it.ID == ref || it.Title == ref {
			matches = append(matches, it)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("%q isn't an item in the specified vault", ref)
	case 1:
		return matches[0], nil
	default:
		return nil, errors.New("more than one item matches")
	}
}
