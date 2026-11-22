// Package onepassword implements the store provider on top of the official
// 1Password CLI (op). Authentication is handled entirely by op, through the
// 1Password app integration, `op signin`, or a service account token.
package onepassword

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/kshehadeh/store/internal/provider"
)

const (
	Name       = "1password"
	installURL = "https://developer.1password.com/docs/cli/get-started/"
)

// Runner executes op. Payloads are passed on stdin, never as arguments.
type Runner interface {
	Run(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error)
}

// Definition registers 1Password with store.
func Definition() provider.Definition {
	return provider.Definition{
		Name:        Name,
		DisplayName: "1Password",
		Setup: func(ctx context.Context, ui provider.UI, preset map[string]string) (map[string]string, error) {
			return Setup(ctx, NewExecRunner(), ui, preset)
		},
		Open: func(settings map[string]string) (provider.Provider, error) {
			return New(NewExecRunner(), settings)
		},
		Describe: func(s map[string]string) string {
			d := fmt.Sprintf("1Password vault %q", s["vault_name"])
			if l := s["account_label"]; l != "" {
				d += " (" + l + ")"
			}
			return d
		},
	}
}

// ExecRunner runs the op binary. STORE_OP_BIN overrides its location.
type ExecRunner struct{ Bin string }

func NewExecRunner() ExecRunner {
	bin := os.Getenv("STORE_OP_BIN")
	if bin == "" {
		bin = "op"
	}
	return ExecRunner{Bin: bin}
}

// Error is a failed op command. It carries op's message but never the
// command's arguments.
type Error struct {
	Command string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("op %s: %s", e.Command, e.Message) }

func (r ExecRunner) Run(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	if stdin != nil {
		// op rejects stdin that is a regular file, and exec hands an *os.File
		// to the child directly. Hiding the concrete type makes exec use a pipe.
		cmd.Stdin = struct{ io.Reader }{stdin}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		name := commandName(args)
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, &Error{Command: name, Message: fmt.Sprintf("%s was not found; install the 1Password CLI from %s", r.Bin, installURL)}
		}
		msg := cleanMessage(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, &Error{Command: name, Message: msg}
	}
	return stdout.Bytes(), nil
}

func commandName(args []string) string {
	var parts []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") || len(parts) == 2 {
			break
		}
		parts = append(parts, a)
	}
	if len(parts) == 0 && len(args) > 0 {
		return args[0]
	}
	return strings.Join(parts, " ")
}

var errorPrefix = regexp.MustCompile(`^\[ERROR\]\s+\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}\s+`)

func cleanMessage(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(errorPrefix.ReplaceAllString(strings.TrimSpace(l), ""))
		if l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "; ")
}

func withAccount(account string, args []string) []string {
	if account == "" {
		return args
	}
	return append(append([]string{}, args...), "--account", account)
}

// Adapter is a provider.Provider for one 1Password vault.
type Adapter struct {
	run     Runner
	account string
	vault   string
}

func New(run Runner, settings map[string]string) (*Adapter, error) {
	v := settings["vault_id"]
	if v == "" {
		return nil, errors.New("no 1Password vault is configured; run `store setup`")
	}
	return &Adapter{run: run, account: settings["account"], vault: v}, nil
}

func (a *Adapter) op(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	return a.run.Run(ctx, stdin, withAccount(a.account, args)...)
}

type opSection struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type opField struct {
	ID      string     `json:"id"`
	Type    string     `json:"type"`
	Purpose string     `json:"purpose"`
	Label   string     `json:"label"`
	Value   string     `json:"value"`
	Section *opSection `json:"section"`
}

type opFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type opItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Category  string    `json:"category"`
	Tags      []string  `json:"tags"`
	UpdatedAt string    `json:"updated_at"`
	Fields    []opField `json:"fields"`
	Files     []opFile  `json:"files"`
}

func (o opItem) toItem() provider.Item {
	updated, _ := time.Parse(time.RFC3339, o.UpdatedAt)
	return provider.Item{
		ID:       o.ID,
		Title:    o.Title,
		Category: categoryLabel(o.Category),
		Kind:     kindOf(o.Category),
		Tags:     o.Tags,
		Updated:  updated,
	}
}

func kindOf(category string) provider.Kind {
	switch category {
	case "DOCUMENT":
		return provider.KindDocument
	case "SECURE_NOTE":
		return provider.KindNote
	default:
		return provider.KindFields
	}
}

var categoryLabels = map[string]string{
	"API_CREDENTIAL": "API Credential",
	"SSH_KEY":        "SSH Key",
}

func categoryLabel(c string) string {
	if l, ok := categoryLabels[c]; ok {
		return l
	}
	words := strings.Split(strings.ToLower(c), "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func (a *Adapter) Status(ctx context.Context) []provider.Check {
	var checks []provider.Check
	ver, err := a.run.Run(ctx, nil, "--version")
	if err != nil {
		return append(checks, provider.Check{Name: "1Password CLI", Detail: err.Error(), Fix: "install the 1Password CLI: " + installURL})
	}
	checks = append(checks, provider.Check{Name: "1Password CLI", OK: true, Detail: "op " + strings.TrimSpace(string(ver))})

	out, err := a.op(ctx, nil, "vault", "get", a.vault, "--format", "json")
	if err != nil {
		fix := "sign in: turn on \"Integrate with 1Password CLI\" in the 1Password app (Settings > Developer), or run `eval $(op signin)`. If the vault was removed or renamed, run `store setup`."
		return append(checks, provider.Check{Name: "Vault access", Detail: err.Error(), Fix: fix})
	}
	var v struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_ = json.Unmarshal(out, &v)
	checks = append(checks, provider.Check{Name: "Vault access", OK: true, Detail: fmt.Sprintf("can read vault %q (%s)", v.Name, a.vault)})

	// Vault access already proves sign-in; whoami only adds the identity.
	if out, err := a.op(ctx, nil, "whoami", "--format", "json"); err == nil {
		var w struct {
			Email string `json:"email"`
			URL   string `json:"url"`
		}
		if json.Unmarshal(out, &w) == nil && w.Email != "" {
			checks = append(checks, provider.Check{Name: "Signed in", OK: true, Detail: fmt.Sprintf("%s on %s", w.Email, w.URL)})
		}
	}
	return checks
}

func (a *Adapter) ListItems(ctx context.Context) ([]provider.Item, error) {
	out, err := a.op(ctx, nil, "item", "list", "--vault", a.vault, "--format", "json")
	if err != nil {
		return nil, err
	}
	var raw []opItem
	if len(bytes.TrimSpace(out)) > 0 {
		if err := json.Unmarshal(out, &raw); err != nil {
			return nil, fmt.Errorf("unexpected output from op item list: %w", err)
		}
	}
	items := make([]provider.Item, 0, len(raw))
	for _, r := range raw {
		items = append(items, r.toItem())
	}
	return items, nil
}

func (a *Adapter) getItem(ctx context.Context, id string, reveal bool) (opItem, error) {
	args := []string{"item", "get", id, "--vault", a.vault, "--format", "json"}
	if reveal {
		args = append(args, "--reveal")
	}
	out, err := a.op(ctx, nil, args...)
	if err != nil {
		return opItem{}, err
	}
	var it opItem
	if err := json.Unmarshal(out, &it); err != nil {
		return opItem{}, fmt.Errorf("unexpected output from op item get: %w", err)
	}
	return it, nil
}

func (a *Adapter) GetItem(ctx context.Context, id string) (provider.Detail, error) {
	it, err := a.getItem(ctx, id, false)
	if err != nil {
		return provider.Detail{}, err
	}
	d := provider.Detail{Item: it.toItem()}
	if len(it.Files) > 0 {
		d.FileName = it.Files[0].Name
	}
	for _, f := range it.Fields {
		if f.ID == "" {
			continue
		}
		if f.Label == "Description" && f.Type == "STRING" {
			d.Description = f.Value
		}
		field := provider.Field{ID: f.ID, Label: f.Label, Type: strings.ToLower(f.Type)}
		if f.Section != nil {
			field.Section = f.Section.Label
		}
		d.Fields = append(d.Fields, field)
	}
	return d, nil
}

func (a *Adapter) CreateDocument(ctx context.Context, doc provider.Document) (string, error) {
	out, err := a.op(ctx, doc.Content, "document", "create", "-",
		"--vault", a.vault,
		"--title", doc.Title,
		"--file-name", doc.FileName,
		"--tags", strings.Join(doc.Tags, ","),
		"--format", "json")
	if err != nil {
		return "", err
	}
	var res struct {
		UUID string `json:"uuid"`
		ID   string `json:"id"`
	}
	// The document already exists at this point, so unexpected output must
	// not turn a successful create into a reported failure.
	_ = json.Unmarshal(out, &res)
	if res.UUID != "" {
		id := res.UUID
		if doc.Description != nil {
			if err := a.setDescription(ctx, id, *doc.Description); err != nil {
				return id, fmt.Errorf("document %q was created, but setting its description failed: %w", doc.Title, err)
			}
		}
		return id, nil
	}
	if doc.Description != nil {
		if res.ID == "" {
			return "", errors.New("document was created, but 1Password returned no item ID to set its description")
		}
		if err := a.setDescription(ctx, res.ID, *doc.Description); err != nil {
			return res.ID, fmt.Errorf("document %q was created, but setting its description failed: %w", doc.Title, err)
		}
	}
	return res.ID, nil
}

func (a *Adapter) UpdateDocument(ctx context.Context, id string, doc provider.Document) error {
	_, err := a.op(ctx, doc.Content, "document", "edit", id, "-",
		"--vault", a.vault,
		"--title", doc.Title,
		"--file-name", doc.FileName,
		"--tags", strings.Join(doc.Tags, ","))
	if err != nil {
		return err
	}
	if doc.Description != nil {
		if err := a.setDescription(ctx, id, *doc.Description); err != nil {
			return fmt.Errorf("document content was updated, but setting its description failed: %w", err)
		}
	}
	return nil
}

func (a *Adapter) setDescription(ctx context.Context, id, description string) error {
	_, err := a.op(ctx, nil, "item", "edit", id,
		"Description[text]="+description,
		"--vault", a.vault)
	return err
}

func (a *Adapter) RenameItem(ctx context.Context, id, title string) error {
	_, err := a.op(ctx, nil, "item", "edit", id,
		"--vault", a.vault,
		"--title", title)
	return err
}

func (a *Adapter) UpdateItemMetadata(ctx context.Context, id string, description *string, itemTags []string) error {
	args := []string{"item", "edit", id}
	if description != nil {
		args = append(args, "Description[text]="+*description)
	}
	args = append(args, "--vault", a.vault, "--tags", strings.Join(itemTags, ","))
	_, err := a.op(ctx, nil, args...)
	return err
}

func (a *Adapter) DownloadDocument(ctx context.Context, id, path string) error {
	_, err := a.op(ctx, nil, "document", "get", id,
		"--vault", a.vault,
		"--out-file", path,
		"--file-mode", "0600",
		"--force")
	return err
}

func (a *Adapter) ReadNote(ctx context.Context, id string) (string, error) {
	it, err := a.getItem(ctx, id, true)
	if err != nil {
		return "", err
	}
	for _, f := range it.Fields {
		if f.Purpose == "NOTES" || f.ID == "notesPlain" {
			return f.Value, nil
		}
	}
	return "", nil
}

func (a *Adapter) ReadField(ctx context.Context, id, fieldID string) (string, error) {
	it, err := a.getItem(ctx, id, true)
	if err != nil {
		return "", err
	}
	for _, f := range it.Fields {
		if f.ID == fieldID {
			return f.Value, nil
		}
	}
	return "", fmt.Errorf("field %q not found on item %s", fieldID, id)
}

type opAccount struct {
	URL         string `json:"url"`
	Email       string `json:"email"`
	UserUUID    string `json:"user_uuid"`
	AccountUUID string `json:"account_uuid"`
}

type opVault struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Setup chooses an account and vault, verifies read access, and returns the
// identifiers to save. preset may contain "account" and "vault" (ID or name).
func Setup(ctx context.Context, run Runner, ui provider.UI, preset map[string]string) (map[string]string, error) {
	if _, err := run.Run(ctx, nil, "--version"); err != nil {
		return nil, fmt.Errorf("the 1Password CLI is not available: %w", err)
	}
	account, label, err := chooseAccount(ctx, run, ui, preset["account"])
	if err != nil {
		return nil, err
	}
	vault, err := chooseVault(ctx, run, ui, account, preset["vault"])
	if err != nil {
		return nil, err
	}
	if _, err := run.Run(ctx, nil, withAccount(account, []string{"item", "list", "--vault", vault.ID, "--format", "json"})...); err != nil {
		return nil, fmt.Errorf("cannot read items in vault %q: %w", vault.Name, err)
	}
	return map[string]string{
		"account":       account,
		"account_label": label,
		"vault_id":      vault.ID,
		"vault_name":    vault.Name,
	}, nil
}

func chooseAccount(ctx context.Context, run Runner, ui provider.UI, preset string) (string, string, error) {
	if preset != "" {
		return preset, preset, nil
	}
	out, err := run.Run(ctx, nil, "account", "list", "--format", "json")
	if err != nil {
		return "", "", fmt.Errorf("cannot list 1Password accounts: %w", err)
	}
	var accts []opAccount
	if len(bytes.TrimSpace(out)) > 0 {
		if err := json.Unmarshal(out, &accts); err != nil {
			return "", "", fmt.Errorf("unexpected output from op account list: %w", err)
		}
	}
	if len(accts) == 0 {
		if os.Getenv("OP_SERVICE_ACCOUNT_TOKEN") != "" {
			ui.Info("Using the 1Password service account from OP_SERVICE_ACCOUNT_TOKEN.")
			return "", "service account", nil
		}
		return "", "", errors.New("no 1Password accounts are available to the CLI on this computer; turn on \"Integrate with 1Password CLI\" in the 1Password app (Settings > Developer) or run `op account add`")
	}
	labels := make([]string, len(accts))
	for i, a := range accts {
		labels[i] = fmt.Sprintf("%s on %s", a.Email, a.URL)
	}
	idx := 0
	if len(accts) == 1 {
		ui.Info("Using 1Password account %s", labels[0])
	} else if idx, err = ui.Choose("Select a 1Password account:", labels); err != nil {
		return "", "", err
	}
	id := accts[idx].AccountUUID
	if id == "" {
		id = accts[idx].URL
	}
	return id, labels[idx], nil
}

func chooseVault(ctx context.Context, run Runner, ui provider.UI, account, preset string) (opVault, error) {
	out, err := run.Run(ctx, nil, withAccount(account, []string{"vault", "list", "--format", "json"})...)
	if err != nil {
		return opVault{}, fmt.Errorf("cannot list vaults (are you signed in?): %w", err)
	}
	var vaults []opVault
	if len(bytes.TrimSpace(out)) > 0 {
		if err := json.Unmarshal(out, &vaults); err != nil {
			return opVault{}, fmt.Errorf("unexpected output from op vault list: %w", err)
		}
	}
	if len(vaults) == 0 {
		return opVault{}, errors.New("this 1Password account has no vaults you can read")
	}
	names := make([]string, len(vaults))
	for i, v := range vaults {
		names[i] = v.Name
	}
	if preset != "" {
		var matches []opVault
		for _, v := range vaults {
			if v.ID == preset || v.Name == preset {
				matches = append(matches, v)
			}
		}
		switch len(matches) {
		case 1:
			return matches[0], nil
		case 0:
			return opVault{}, fmt.Errorf("vault %q not found; available vaults: %s", preset, strings.Join(names, ", "))
		default:
			return opVault{}, fmt.Errorf("several vaults are named %q; pass the vault ID instead", preset)
		}
	}
	if len(vaults) == 1 {
		ui.Info("Using vault %q", vaults[0].Name)
		return vaults[0], nil
	}
	idx, err := ui.Choose("Select the vault to share items through (choose the same vault on every computer):", names)
	if err != nil {
		return opVault{}, err
	}
	return vaults[idx], nil
}
