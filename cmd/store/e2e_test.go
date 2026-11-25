package main_test

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

var storeBin, opDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "store-e2e-*")
	if err != nil {
		panic(err)
	}
	storeBin = filepath.Join(dir, "store")
	opDir = filepath.Join(dir, "opbin")
	build := func(out, pkg string) {
		cmd := exec.Command("go", "build", "-o", out, pkg)
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			panic(fmt.Sprintf("building %s: %v", pkg, err))
		}
	}
	build(storeBin, "github.com/kshehadeh/store/cmd/store")
	build(filepath.Join(opDir, "op"), "github.com/kshehadeh/store/internal/testsupport/fakeop")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// vaultItem mirrors the fake op's stored item.
type vaultItem struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Category string   `json:"category"`
	Vault    string   `json:"vault"`
	Tags     []string `json:"tags,omitempty"`
	Version  int      `json:"version"`
	Fields   []any    `json:"fields,omitempty"`
	FileName string   `json:"file_name,omitempty"`
	Content  []byte   `json:"content,omitempty"`
}

type world struct {
	t                *testing.T
	root, state, log string
}

type machine struct {
	w                  *world
	config, home, host string
}

type opts struct {
	stdin       string
	interactive bool
	dir         string
	env         []string
}

type result struct {
	stdout, stderr string
	code           int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	root := t.TempDir()
	w := &world{t: t, root: root, state: filepath.Join(root, "vault.json"), log: filepath.Join(root, "op.log")}
	st := map[string]any{
		"accounts": []map[string]string{{"url": "example.1password.com", "email": "me@example.com", "user_uuid": "USER1", "account_uuid": "ACCT1"}},
		"vaults":   []map[string]string{{"id": "vprivate", "name": "Private"}, {"id": "vshared", "name": "Shared"}},
		"items":    []any{},
	}
	data, _ := json.Marshal(st)
	if err := os.WriteFile(w.state, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) env() []string {
	return []string{
		"PATH=" + opDir + ":/usr/bin:/bin",
		"FAKEOP_STATE=" + w.state,
		"FAKEOP_LOG=" + w.log,
		"TMPDIR=" + filepath.Join(w.root, "tmp"),
	}
}

func (w *world) op(stdin string, args ...string) string {
	w.t.Helper()
	cmd := exec.Command(filepath.Join(opDir, "op"), args...)
	cmd.Env = w.env()
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("fake op %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (w *world) seed(it vaultItem) string {
	w.t.Helper()
	if it.Vault == "" {
		it.Vault = "vshared"
	}
	data, _ := json.Marshal(it)
	return w.op(string(data), "__seed")
}

func (w *world) items() []vaultItem {
	w.t.Helper()
	var st struct {
		Items []vaultItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(w.op("", "__dump")), &st); err != nil {
		w.t.Fatal(err)
	}
	return st.Items
}

func (w *world) byTitle(title string) []vaultItem {
	var out []vaultItem
	for _, it := range w.items() {
		if it.Title == title {
			out = append(out, it)
		}
	}
	return out
}

func (w *world) one(title string) vaultItem {
	w.t.Helper()
	m := w.byTitle(title)
	if len(m) != 1 {
		w.t.Fatalf("expected one item titled %q, found %d", title, len(m))
	}
	return m[0]
}

func descriptionOf(it vaultItem) string {
	for _, raw := range it.Fields {
		field, ok := raw.(map[string]any)
		if !ok || field["label"] != "Description" {
			continue
		}
		value, _ := field["value"].(string)
		return value
	}
	return ""
}

func (w *world) machine(name, host string) *machine {
	w.t.Helper()
	home := filepath.Join(w.root, name, "home")
	for _, d := range []string{home, filepath.Join(w.root, "tmp")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			w.t.Fatal(err)
		}
	}
	return &machine{w: w, config: filepath.Join(w.root, name, "config.json"), home: home, host: host}
}

func (m *machine) run(o opts, args ...string) result {
	m.w.t.Helper()
	cmd := exec.Command(storeBin, args...)
	cmd.Dir = m.home
	if o.dir != "" {
		cmd.Dir = o.dir
	}
	cmd.Env = append(m.w.env(), "HOME="+m.home, "STORE_CONFIG="+m.config, "STORE_HOSTNAME="+m.host)
	if o.interactive {
		cmd.Env = append(cmd.Env, "STORE_INTERACTIVE=1")
	}
	cmd.Env = append(cmd.Env, o.env...)
	cmd.Stdin = strings.NewReader(o.stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		m.w.t.Fatal(err)
	}
	return result{stdout.String(), stderr.String(), code}
}

// ok runs a command that must succeed.
func (m *machine) ok(o opts, args ...string) result {
	m.w.t.Helper()
	r := m.run(o, args...)
	if r.code != 0 {
		m.w.t.Fatalf("store %v exited %d\nstdout: %s\nstderr: %s", args, r.code, r.stdout, r.stderr)
	}
	return r
}

// fails runs a command that must fail with stderr containing want.
func (m *machine) fails(want string, o opts, args ...string) result {
	m.w.t.Helper()
	r := m.run(o, args...)
	if r.code == 0 {
		m.w.t.Fatalf("store %v succeeded; want failure containing %q\nstdout: %s", args, want, r.stdout)
	}
	if !strings.Contains(r.stderr+r.stdout, want) {
		m.w.t.Fatalf("store %v output does not contain %q\nstdout: %s\nstderr: %s", args, want, r.stdout, r.stderr)
	}
	return r
}

// twoMachines returns a laptop and a desktop configured for the Shared vault.
func twoMachines(t *testing.T) (*world, *machine, *machine) {
	w := newWorld(t)
	a, b := w.machine("laptop", "laptop"), w.machine("desktop", "desktop")
	a.ok(opts{}, "setup", "--vault", "Shared")
	b.ok(opts{}, "setup", "--vault", "vshared")
	return w, a, b
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t *testing.T, p, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func noStaging(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		matches, _ := filepath.Glob(filepath.Join(d, ".store-staging-*"))
		if len(matches) > 0 {
			t.Errorf("staging left behind: %v", matches)
		}
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}

func TestSetupAndStatus(t *testing.T) {
	w := newWorld(t)
	a := w.machine("laptop", "laptop")

	r := a.run(opts{}, "status")
	if r.code == 0 || !strings.Contains(r.stdout, "store setup") {
		t.Fatalf("status before setup: code %d, %s", r.code, r.stdout)
	}
	a.fails("not set up", opts{}, "list")

	// Interactive: the single account is chosen automatically, then the
	// second vault from the picker.
	r = a.ok(opts{interactive: true, stdin: "2\n"}, "setup")
	if !strings.Contains(r.stderr, "1) Private") || !strings.Contains(r.stdout, `"Shared"`) {
		t.Fatalf("setup output:\n%s\n%s", r.stderr, r.stdout)
	}
	info, err := os.Stat(a.config)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config mode: %v %v", info, err)
	}
	var cfg struct {
		Version     int    `json:"version"`
		Default     string `json:"default"`
		Connections map[string]struct {
			Provider string            `json:"provider"`
			Settings map[string]string `json:"settings"`
		} `json:"connections"`
	}
	json.Unmarshal([]byte(readFile(t, a.config)), &cfg)
	conn, ok := cfg.Connections[cfg.Default]
	if cfg.Version != 2 || !ok || conn.Provider != "1password" || conn.Settings["vault_id"] != "vshared" || conn.Settings["account"] != "ACCT1" {
		t.Fatalf("config = %+v", cfg)
	}

	r = a.ok(opts{}, "status")
	for _, want := range []string{"ok   Configuration", "ok   1Password CLI: op 2.42.0", `ok   Vault access: can read vault "Shared"`, "ok   Signed in: me@example.com", "0 discoverable", "store is ready"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("status missing %q:\n%s", want, r.stdout)
		}
	}

	r = a.run(opts{env: []string{"FAKEOP_SIGNED_OUT=1"}}, "status")
	if r.code == 0 || !strings.Contains(r.stdout, "FAIL Vault access") || !strings.Contains(r.stdout, "Fix:") {
		t.Fatalf("signed-out status: code %d\n%s", r.code, r.stdout)
	}
	r = a.run(opts{env: []string{"PATH=/usr/bin:/bin"}}, "status")
	if r.code == 0 || !strings.Contains(r.stdout, "FAIL 1Password CLI") {
		t.Fatalf("missing op status: code %d\n%s", r.code, r.stdout)
	}

	a.fails(`vault "Nope" not found`, opts{}, "setup", "--vault", "Nope")
	a.fails("not interactive", opts{}, "setup")
	if len(w.items()) != 0 {
		t.Fatal("setup or status wrote to the vault")
	}
}

func TestMultipleConnections(t *testing.T) {
	w := newWorld(t)
	a := w.machine("laptop", "laptop")

	// First connection (Private vault) becomes the default automatically.
	r := a.ok(opts{}, "connection", "add", "--vault", "Private", "--as", "personal")
	if !strings.Contains(r.stdout, `Saved connection "personal"`) {
		t.Fatalf("add output: %s", r.stdout)
	}
	// Second connection (Shared vault) does not take over the default.
	r = a.ok(opts{}, "setup", "--vault", "Shared", "--as", "work")
	if !strings.Contains(r.stdout, `Saved connection "work"`) || !strings.Contains(r.stdout, `default connection is "personal"`) {
		t.Fatalf("second add output: %s", r.stdout)
	}
	a.fails("already exists", opts{}, "connection", "add", "--vault", "Shared", "--as", "work")

	r = a.ok(opts{}, "connection", "list")
	for _, want := range []string{"personal", "work", `vault "Private"`, `vault "Shared"`} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("connection list missing %q:\n%s", want, r.stdout)
		}
	}

	// keep and pull through the default (personal) connection.
	write(t, filepath.Join(a.home, "note.txt"), "personal secret\n", 0o600)
	a.ok(opts{}, "keep", "note.txt", "--name", "note")
	if got := w.one("note").Vault; got != "vprivate" {
		t.Fatalf("default keep went to vault %q", got)
	}
	a.ok(opts{}, "pull", "note", "--to", "pulled.txt")
	if got := readFile(t, filepath.Join(a.home, "pulled.txt")); got != "personal secret\n" {
		t.Fatalf("default pull = %q", got)
	}

	// The same name through --connection work is a different vault.
	r = a.ok(opts{}, "keep", "-c", "work", "note.txt", "--name", "note")
	if !strings.Contains(r.stdout, `work: 1Password vault "Shared"`) {
		t.Fatalf("keep -c output: %s", r.stdout)
	}
	if got := w.byTitle("note"); len(got) != 2 {
		t.Fatalf("expected one note per vault, got %d", len(got))
	}
	a.fails("unknown connection", opts{}, "list", "--connection", "nope")

	// Switch the default and remove the old one.
	r = a.ok(opts{}, "connection", "default", "work")
	if !strings.Contains(r.stdout, `Default connection is now "work"`) {
		t.Fatalf("default output: %s", r.stdout)
	}
	r = a.ok(opts{}, "connection", "remove", "personal")
	if !strings.Contains(r.stdout, `Removed connection "personal"`) {
		t.Fatalf("remove output: %s", r.stdout)
	}
	r = a.ok(opts{}, "status")
	if !strings.Contains(r.stdout, `can read vault "Shared"`) {
		t.Fatalf("status after remove:\n%s", r.stdout)
	}
	a.fails("unknown connection", opts{}, "connection", "default", "personal")
}
func TestKeepAndPullFile(t *testing.T) {
	w, a, b := twoMachines(t)
	envPath := filepath.Join(a.home, ".env")
	write(t, envPath, "SECRET_A=1\n", 0o600)

	r := a.ok(opts{}, "keep", "~/.env", "--name", "root-env", "--tag", "project/api")
	if !strings.Contains(r.stdout, `Created "root-env" (file`) {
		t.Fatalf("keep output: %s", r.stdout)
	}
	it := w.one("root-env")
	if it.Category != "DOCUMENT" || it.Vault != "vshared" || it.FileName != ".env" || string(it.Content) != "SECRET_A=1\n" {
		t.Fatalf("stored item = %+v", it)
	}
	source := "store/meta/source" + filepath.ToSlash(a.home)
	for _, tag := range []string{"store/meta/managed/v1", "store/meta/type/file", "store/meta/host/laptop", "store/meta/os/" + runtime.GOOS, "store/project/api", source} {
		if !slices.Contains(it.Tags, tag) {
			t.Errorf("missing tag %q in %v", tag, it.Tags)
		}
	}

	// Labels added in 1Password survive later keeps.
	w.op("", "__tag", it.ID, "store/team/web")
	w.op("", "__tag", it.ID, "personal")

	write(t, envPath, "SECRET_A=2\n", 0o600)
	r = a.ok(opts{}, "keep", envPath, "--name", "root-env")
	if !strings.Contains(r.stdout, "Updated") {
		t.Fatalf("second keep: %s", r.stdout)
	}
	up := w.one("root-env")
	if up.ID != it.ID || up.Version != 2 || string(up.Content) != "SECRET_A=2\n" {
		t.Fatalf("update = %+v", up)
	}
	for _, tag := range []string{"store/project/api", "store/team/web", "personal", "store/meta/managed/v1"} {
		if !slices.Contains(up.Tags, tag) {
			t.Errorf("update lost tag %q: %v", tag, up.Tags)
		}
	}

	// Pull on the other computer.
	dest := filepath.Join(b.home, ".env")
	r = b.ok(opts{}, "pull", "root-env", "--to", dest)
	if r.stdout != "" {
		t.Errorf("file pull wrote to stdout: %q", r.stdout)
	}
	if got := readFile(t, dest); got != "SECRET_A=2\n" {
		t.Fatalf("pulled %q", got)
	}
	if info, _ := os.Stat(dest); info.Mode().Perm() != 0o600 {
		t.Errorf("pulled file mode %v", info.Mode().Perm())
	}

	// Existing destinations are never overwritten.
	write(t, filepath.Join(b.home, "local.env"), "LOCAL\n", 0o600)
	b.fails("already exists", opts{}, "pull", "root-env", "--to", filepath.Join(b.home, "local.env"))
	if readFile(t, filepath.Join(b.home, "local.env")) != "LOCAL\n" {
		t.Fatal("existing file was modified")
	}
	b.fails("already exists", opts{}, "pull", "root-env", "--to", b.home)

	// An existing directory receives the original file name.
	os.Mkdir(filepath.Join(b.home, "proj"), 0o700)
	b.ok(opts{}, "pull", "root-env", "--to", filepath.Join(b.home, "proj"))
	if readFile(t, filepath.Join(b.home, "proj", ".env")) != "SECRET_A=2\n" {
		t.Fatal("pull into directory failed")
	}

	// Interactive pull asks for the destination and defaults to ./<name>.
	work := filepath.Join(b.home, "work")
	os.Mkdir(work, 0o700)
	r = b.ok(opts{interactive: true, stdin: "\n", dir: work}, "pull", "root-env")
	if !strings.Contains(r.stderr, "Save file to [./.env]") {
		t.Errorf("prompt: %q", r.stderr)
	}
	if readFile(t, filepath.Join(work, ".env")) != "SECRET_A=2\n" {
		t.Fatal("interactive pull failed")
	}
	b.fails("--to PATH is required", opts{}, "pull", "root-env")

	// Keeping from the other computer refreshes the source metadata.
	b.ok(opts{}, "keep", dest, "--name", "root-env")
	up = w.one("root-env")
	if !slices.Contains(up.Tags, "store/meta/host/desktop") || slices.Contains(up.Tags, "store/meta/host/laptop") {
		t.Errorf("host metadata not refreshed: %v", up.Tags)
	}
	if !slices.Contains(up.Tags, "store/team/web") {
		t.Errorf("labels lost on keep from second computer: %v", up.Tags)
	}
	noStaging(t, b.home, work, filepath.Join(b.home, "proj"))

	for _, secret := range []string{"SECRET_A"} {
		if strings.Contains(readFile(t, w.log), secret) {
			t.Errorf("payload %q appeared in op arguments", secret)
		}
	}
}

func TestBareStoreWithoutTerminalPrintsHelp(t *testing.T) {
	_, a, _ := twoMachines(t)
	r := a.ok(opts{})
	if !strings.Contains(r.stdout, "Usage:") || !strings.Contains(r.stdout, "tui") {
		t.Fatalf("bare store without a terminal did not print help:\n%s", r.stdout)
	}
	if r := a.run(opts{}, "bogus"); r.code == 0 || !strings.Contains(r.stderr, `unknown command "bogus"`) {
		t.Fatalf("unknown command = %d\nstderr: %s", r.code, r.stderr)
	}
}

func TestKeepDescription(t *testing.T) {
	w, a, _ := twoMachines(t)
	path := filepath.Join(a.home, "settings.json")
	write(t, path, `{"region":"west"}`, 0o600)

	description := "Local API settings = staging"
	a.ok(opts{}, "keep", path, "--name", "settings", "--description", description)
	it := w.one("settings")
	if got := descriptionOf(it); got != description {
		t.Fatalf("created description = %q; want %q", got, description)
	}

	// Omitting --description on a later keep preserves the stored field.
	write(t, path, `{"region":"east"}`, 0o600)
	a.ok(opts{}, "keep", path, "--name", "settings")
	it = w.one("settings")
	if got := descriptionOf(it); got != description {
		t.Fatalf("description after keep without flag = %q; want %q", got, description)
	}

	updatedDescription := "Production settings"
	a.ok(opts{}, "keep", path, "--name", "settings", "--description", updatedDescription)
	it = w.one("settings")
	if got := descriptionOf(it); got != updatedDescription {
		t.Fatalf("updated description = %q; want %q", got, updatedDescription)
	}
}

func TestBinaryAndFolderRoundTrip(t *testing.T) {
	w, a, b := twoMachines(t)

	img := make([]byte, 300_000)
	rand.Read(img)
	img[0], img[1] = 0, 0
	os.WriteFile(filepath.Join(a.home, "logo.png"), img, 0o644)
	a.ok(opts{}, "keep", filepath.Join(a.home, "logo.png"), "--name", "logo")
	b.ok(opts{}, "pull", "logo", "--to", b.home)
	if got, _ := os.ReadFile(filepath.Join(b.home, "logo.png")); !bytes.Equal(got, img) {
		t.Fatal("binary file changed in round trip")
	}

	proj := filepath.Join(a.home, "proj")
	write(t, filepath.Join(proj, ".hidden"), "h", 0o644)
	write(t, filepath.Join(proj, "sub", "deep", "f.txt"), "deep", 0o644)
	write(t, filepath.Join(proj, "run.sh"), "#!/bin/sh\n", 0o755)
	os.MkdirAll(filepath.Join(proj, "empty"), 0o755)
	a.ok(opts{}, "keep", proj, "--name", "proj", "--tag", "project/api")
	it := w.one("proj")
	if it.FileName != "proj.zip" || !slices.Contains(it.Tags, "store/meta/type/folder") {
		t.Fatalf("folder item = %s %v", it.FileName, it.Tags)
	}

	restored := filepath.Join(b.home, "restored")
	b.ok(opts{}, "pull", "proj", "--to", restored)
	for p, want := range map[string]string{".hidden": "h", "sub/deep/f.txt": "deep", "run.sh": "#!/bin/sh\n"} {
		if got := readFile(t, filepath.Join(restored, p)); got != want {
			t.Errorf("%s = %q", p, got)
		}
	}
	if info, err := os.Stat(filepath.Join(restored, "empty")); err != nil || !info.IsDir() {
		t.Error("empty directory not restored")
	}
	if info, _ := os.Stat(filepath.Join(restored, "run.sh")); info.Mode().Perm()&0o100 == 0 {
		t.Error("executable bit lost")
	}
	// An existing directory is a container, never a merge target.
	b.ok(opts{}, "pull", "proj", "--to", restored)
	if readFile(t, filepath.Join(restored, "proj", ".hidden")) != "h" {
		t.Error("folder not placed inside existing directory")
	}
	b.fails("already exists", opts{}, "pull", "proj", "--to", restored)
	if _, err := os.Stat(filepath.Join(restored, "proj", "proj")); err == nil {
		t.Error("folder merged into existing restore")
	}

	// Default folder name inside an existing directory.
	b.ok(opts{}, "pull", "proj", "--to", filepath.Join(b.home))
	if readFile(t, filepath.Join(b.home, "proj", "sub", "deep", "f.txt")) != "deep" {
		t.Error("folder not restored under its own name")
	}

	zipDest := filepath.Join(b.home, "proj-archive.zip")
	b.ok(opts{}, "pull", "proj", "--as-zip", "--to", zipDest)
	zr, err := zip.OpenReader(zipDest)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	zr.Close()
	for _, want := range []string{".hidden", "empty/", "sub/deep/f.txt"} {
		if !slices.Contains(names, want) {
			t.Errorf("zip missing %q: %v", want, names)
		}
	}
	b.fails("--as-zip applies only to folders", opts{}, "pull", "logo", "--as-zip", "--to", filepath.Join(b.home, "x"))

	bad := filepath.Join(a.home, "bad")
	write(t, filepath.Join(bad, "ok.txt"), "ok", 0o644)
	os.Symlink("/etc/hosts", filepath.Join(bad, "link"))
	a.fails("symlinks", opts{}, "keep", bad, "--name", "bad")
	if len(w.byTitle("bad")) != 0 {
		t.Fatal("rejected folder created an item")
	}
	noStaging(t, b.home)
}

func TestText(t *testing.T) {
	w, a, b := twoMachines(t)

	a.ok(opts{stdin: "us-east-1\n"}, "keep", "--text", "--name", "region", "--tag", "project/api")
	it := w.one("region")
	if it.FileName != "region.txt" || !slices.Contains(it.Tags, "store/meta/type/text") {
		t.Fatalf("text item = %+v", it)
	}
	for _, tag := range it.Tags {
		if strings.HasPrefix(tag, "store/meta/source") {
			t.Errorf("text has a source tag: %s", tag)
		}
	}
	r := b.ok(opts{}, "pull", "region")
	if r.stdout != "us-east-1\n" {
		t.Fatalf("text pull stdout = %q", r.stdout)
	}

	r = a.ok(opts{interactive: true, stdin: "s3cr3t-token\n"}, "keep", "--text", "--name", "token")
	if !strings.Contains(r.stderr, "input hidden") {
		t.Errorf("no hidden prompt: %q", r.stderr)
	}
	if r = b.ok(opts{}, "pull", "token"); r.stdout != "s3cr3t-token" {
		t.Fatalf("token stdout = %q", r.stdout)
	}

	b.ok(opts{}, "pull", "token", "--to", filepath.Join(b.home, "token.txt"))
	if readFile(t, filepath.Join(b.home, "token.txt")) != "s3cr3t-token" {
		t.Fatal("text --to failed")
	}

	a.fails("no text was provided", opts{}, "keep", "--text", "--name", "empty")
	a.fails("provide a PATH", opts{}, "keep", "--name", "x")
	a.fails("not both", opts{stdin: "x"}, "keep", "--text", "a.txt", "--name", "x")
	a.fails("does not exist", opts{}, "keep", "missing.txt", "--name", "x")
	a.fails("--name is required", opts{stdin: "x"}, "keep", "--text")
	a.fails("reserved", opts{stdin: "x"}, "keep", "--text", "--name", "x", "--tag", "meta/type/folder")

	log := readFile(t, w.log)
	for _, secret := range []string{"us-east-1", "s3cr3t-token"} {
		if strings.Contains(log, secret) {
			t.Errorf("payload %q appeared in op arguments", secret)
		}
	}
}

func TestPullWithoutSelector(t *testing.T) {
	w, a, b := twoMachines(t)
	b.fails("no stored items are available", opts{interactive: true, stdin: "1\n"}, "pull")
	b.fails("not interactive", opts{}, "pull")
	a.ok(opts{stdin: "recent text"}, "keep", "--text", "--name", "recent")
	r := b.ok(opts{interactive: true, stdin: "1\n"}, "pull")
	if r.stdout != "recent text" || !strings.Contains(r.stderr, "most recently updated first") || !strings.Contains(r.stderr, "1) recent") {
		t.Fatalf("bare pull: stdout %q stderr %q", r.stdout, r.stderr)
	}
	// Even a single item is not pulled without a selection.
	b.fails("no input received", opts{interactive: true}, "pull")
	b.fails("not interactive", opts{}, "pull")
	b.ok(opts{interactive: true, stdin: "1\n"}, "pull", "--to", filepath.Join(b.home, "recent.txt"))
	if readFile(t, filepath.Join(b.home, "recent.txt")) != "recent text" || len(w.items()) != 1 {
		t.Fatal("bare pull did not preserve the selected text or changed the vault")
	}
}

func TestDiscoveryAndManualItems(t *testing.T) {
	w, a, b := twoMachines(t)
	write(t, filepath.Join(a.home, ".env"), "A=1\n", 0o600)
	a.ok(opts{}, "keep", filepath.Join(a.home, ".env"), "--name", "root-env", "--tag", "project/api")

	loginID := w.seed(vaultItem{Title: "API Login", Category: "LOGIN", Tags: []string{"store/project/api"}, Fields: []any{
		map[string]any{"id": "username", "type": "STRING", "purpose": "USERNAME", "label": "username", "value": "svc"},
		map[string]any{"id": "password", "type": "CONCEALED", "purpose": "PASSWORD", "label": "password", "value": "hunter2"},
		map[string]any{"id": "h1", "type": "STRING", "label": "host", "value": "db.internal", "section": map[string]string{"id": "s1", "label": "Server"}},
	}})
	noteID := w.seed(vaultItem{Title: "API Notes", Category: "SECURE_NOTE", Tags: []string{"store/project/api", "store/notes"}, Fields: []any{
		map[string]any{"id": "notesPlain", "type": "STRING", "purpose": "NOTES", "label": "notesPlain", "value": "line1\nline2"},
	}})
	unrelatedID := w.seed(vaultItem{Title: "Unrelated", Category: "LOGIN", Tags: []string{"work"}})
	w.seed(vaultItem{Title: "Projectile", Category: "DOCUMENT", Tags: []string{"store/projectile"}, FileName: "p.txt", Content: []byte("p")})
	w.seed(vaultItem{Title: "Assets", Category: "DOCUMENT", Tags: []string{"store/meta/type/folder", "store/design"}, FileName: "assets.zip", Content: zipBytes(t, map[string]string{"a/b.txt": "bee"})})
	w.seed(vaultItem{Title: "Handbook", Category: "DOCUMENT", Tags: []string{"store/design"}, FileName: "handbook.zip", Content: zipBytes(t, map[string]string{"x.txt": "x"})})
	w.seed(vaultItem{Title: "Private thing", Vault: "vprivate", Category: "DOCUMENT", Tags: []string{"store/project/api"}, FileName: "x", Content: []byte("x")})

	titles := func(out string) []string {
		var ts []string
		for _, line := range strings.Split(strings.TrimSpace(out), "\n")[1:] {
			ts = append(ts, strings.Split(line, "  ")[0])
		}
		return ts
	}
	if got := titles(b.ok(opts{}, "list").stdout); !slices.Equal(got, []string{"API Login", "API Notes", "Assets", "Handbook", "Projectile", "root-env"}) {
		t.Errorf("list = %v", got)
	}
	for _, f := range []string{"project/api", "project", "store/project"} {
		if got := titles(b.ok(opts{}, "list", "--tag", f).stdout); !slices.Equal(got, []string{"API Login", "API Notes", "root-env"}) {
			t.Errorf("list --tag %s = %v", f, got)
		}
	}
	if got := titles(b.ok(opts{}, "list", "--tag", "project/api", "--tag", "meta/type/file").stdout); !slices.Equal(got, []string{"root-env"}) {
		t.Errorf("tag intersection = %v", got)
	}
	// Known metadata is shown as columns; labels stay in TAGS.
	listing := b.ok(opts{}, "list").stdout
	header := strings.Fields(strings.SplitN(listing, "\n", 2)[0])
	if !slices.Equal(header, []string{"NAME", "TYPE", "HOST", "SOURCE", "TAGS", "UPDATED", "ID"}) {
		t.Errorf("header = %v", header)
	}
	// row returns TYPE, HOST, SOURCE, and TAGS for a single-word title. The
	// last three fields are the update date, time, and ID.
	row := func(title string) []string {
		for _, line := range strings.Split(listing, "\n") {
			if f := strings.Fields(line); len(f) > 6 && f[0] == title {
				return []string{f[1], f[2], f[3], strings.Join(f[4:len(f)-3], " ")}
			}
		}
		t.Fatalf("no row for %q in:\n%s", title, listing)
		return nil
	}
	if got, want := row("root-env"), []string{"file", "laptop", a.home, "project/api"}; !slices.Equal(got, want) {
		t.Errorf("root-env row = %q; want %q", got, want)
	}
	if got, want := row("Assets"), []string{"folder", "-", "-", "design"}; !slices.Equal(got, want) {
		t.Errorf("Assets row = %q; want %q", got, want)
	}
	if strings.Contains(listing, "meta/") {
		t.Errorf("TAGS shows metadata tags:\n%s", listing)
	}
	var withMeta []struct {
		Name   string `json:"name"`
		Host   string `json:"host"`
		Source string `json:"source"`
	}
	json.Unmarshal([]byte(b.ok(opts{}, "list", "--json", "--tag", "meta/type/file").stdout), &withMeta)
	if len(withMeta) != 1 || withMeta[0].Host != "laptop" || withMeta[0].Source != a.home {
		t.Errorf("json metadata = %+v", withMeta)
	}

	r := b.ok(opts{}, "list", "--tag", "nothing")
	if r.stdout != "" || !strings.Contains(r.stderr, "No stored items match") {
		t.Errorf("empty list: %q %q", r.stdout, r.stderr)
	}
	var entries []struct {
		Name string   `json:"name"`
		Type string   `json:"type"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(b.ok(opts{}, "list", "--json", "--tag", "store/design").stdout), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "Assets" || entries[0].Type != "folder" || entries[1].Type != "file" {
		t.Errorf("json list = %+v", entries)
	}
	if strings.Contains(b.ok(opts{}, "list").stdout, "hunter2") {
		t.Fatal("list revealed a secret")
	}

	// Several matches: fail without a terminal, pick with one.
	r = b.fails("3 items match", opts{}, "pull", "--tag", "project/api")
	if !strings.Contains(r.stderr, "--id") || r.stdout != "" {
		t.Errorf("ambiguous pull: %q %q", r.stdout, r.stderr)
	}
	r = b.ok(opts{interactive: true, stdin: "2\n"}, "pull", "--tag", "project/api")
	if r.stdout != "line1\nline2" || !strings.Contains(r.stderr, "2) API Notes") {
		t.Fatalf("picker pull: stdout %q stderr %q", r.stdout, r.stderr)
	}
	if r = b.ok(opts{}, "pull", "--tag", "project/api", "--tag", "meta/type/file", "--to", b.home); readFile(t, filepath.Join(b.home, ".env")) != "A=1\n" {
		t.Fatal("single tag match not pulled")
	}

	// Structured items require an explicit field.
	r = b.fails("--field", opts{}, "pull", "API Login")
	if strings.Contains(r.stdout+r.stderr, "hunter2") || !strings.Contains(r.stderr, "username, password, Server.host") {
		t.Fatalf("login without field: %q %q", r.stdout, r.stderr)
	}
	if r = b.ok(opts{}, "pull", "API Login", "--field", "password"); r.stdout != "hunter2" {
		t.Fatalf("password = %q", r.stdout)
	}
	if r = b.ok(opts{}, "pull", "API Login", "--field", "server.host"); r.stdout != "db.internal" {
		t.Fatalf("section field = %q", r.stdout)
	}
	if r = b.ok(opts{interactive: true, stdin: "1\n"}, "pull", "--id", loginID); r.stdout != "svc" {
		t.Fatalf("picked field = %q", r.stdout)
	}
	b.fails("no field", opts{}, "pull", "API Login", "--field", "pin")
	b.fails("--field applies only", opts{}, "pull", "API Notes", "--field", "x")

	// Manually added documents.
	b.ok(opts{}, "pull", "Assets", "--to", b.home)
	if readFile(t, filepath.Join(b.home, "assets", "a", "b.txt")) != "bee" {
		t.Fatal("manual folder not extracted")
	}
	b.ok(opts{}, "pull", "Handbook", "--to", b.home)
	if info, err := os.Stat(filepath.Join(b.home, "handbook.zip")); err != nil || info.IsDir() {
		t.Fatal("untagged ZIP was not kept as a file")
	}
	if r = b.ok(opts{}, "pull", "--id", noteID); r.stdout != "line1\nline2" {
		t.Fatalf("note by id = %q", r.stdout)
	}
	b.fails("no store/ tag", opts{}, "pull", "--id", unrelatedID)
	b.fails("no stored item matches", opts{}, "pull", "Unrelated")
	b.fails("no stored item matches", opts{}, "pull", "Private thing")
	b.fails("specify a NAME", opts{}, "pull")

	// keep never touches items it did not create.
	before := len(w.items())
	a.fails("store did not create", opts{stdin: "x"}, "keep", "--text", "--name", "API Login")
	a.fails("store did not create", opts{stdin: "x"}, "keep", "--text", "--name", "Unrelated")
	a.fails("store did not create", opts{stdin: "x"}, "keep", "--text", "--name", "Assets")
	managed := []string{"store/meta/managed/v1", "store/meta/type/text"}
	w.seed(vaultItem{Title: "dup", Category: "DOCUMENT", Tags: managed, FileName: "dup.txt", Content: []byte("1")})
	w.seed(vaultItem{Title: "dup", Category: "DOCUMENT", Tags: managed, FileName: "dup.txt", Content: []byte("2")})
	a.fails("2 items", opts{stdin: "x"}, "keep", "--text", "--name", "dup")
	if after := len(w.items()); after != before+2 {
		t.Fatalf("refused keeps changed the vault: %d -> %d items", before, after)
	}
	if w.one("API Login").Version != 1 {
		t.Fatal("unmanaged login was modified")
	}

	// The content type can change under the same name.
	id := w.one("root-env").ID
	a.ok(opts{stdin: "now text"}, "keep", "--text", "--name", "root-env")
	up := w.one("root-env")
	if up.ID != id || !slices.Contains(up.Tags, "store/meta/type/text") || slices.Contains(up.Tags, "store/meta/type/file") || !slices.Contains(up.Tags, "store/project/api") {
		t.Fatalf("type change: %+v", up)
	}
	if r = b.ok(opts{}, "pull", "root-env"); r.stdout != "now text" {
		t.Fatalf("after type change = %q", r.stdout)
	}
}

func TestUnsafeArchiveIsRejected(t *testing.T) {
	w, _, b := twoMachines(t)
	w.seed(vaultItem{Title: "evil", Category: "DOCUMENT", Tags: []string{"store/meta/type/folder"}, FileName: "evil.zip",
		Content: zipBytes(t, map[string]string{"ok.txt": "ok", "../escape.txt": "gotcha"})})
	b.fails("unsafe path", opts{}, "pull", "evil", "--to", filepath.Join(b.home, "evil"))
	for _, p := range []string{filepath.Join(b.home, "evil"), filepath.Join(b.home, "escape.txt"), filepath.Join(filepath.Dir(b.home), "escape.txt")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s was created", p)
		}
	}
	noStaging(t, b.home)
}
