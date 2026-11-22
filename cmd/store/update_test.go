package main_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// releaseServer publishes version as the latest release. Its archive holds a
// shell script standing in for the new store binary.
func releaseServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\necho \"store version %s\"\n", version)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "store", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg})
	tw.Write([]byte(script))
	tw.Close()
	gz.Close()
	archive := buf.Bytes()
	asset := fmt.Sprintf("store_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(archive)

	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":"%s/archive"},{"name":"checksums.txt","browser_download_url":"%s/sums"}]}`,
			version, asset, base, base)
	})
	mux.HandleFunc("/archive", func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// releaseBinary builds store as release version into path.
func releaseBinary(t *testing.T, path, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", path,
		"-ldflags", "-X github.com/kshehadeh/store/internal/cli.Version="+version,
		"github.com/kshehadeh/store/cmd/store")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", version, err, out)
	}
}

func runBinary(t *testing.T, bin string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return string(out), code
}

func TestUpdateReplacesReleaseBinary(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin", "store")
	releaseBinary(t, bin, "v0.1.0")
	srv := releaseServer(t, "v9.9.9")
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + root, "STORE_UPDATE_API=" + srv.URL}

	out, code := runBinary(t, bin, env, "update", "--check")
	if code != 0 || !strings.Contains(out, "store v9.9.9 is available (you have v0.1.0)") {
		t.Fatalf("update --check = %d\n%s", code, out)
	}
	if out, _ := runBinary(t, bin, env, "--version"); !strings.Contains(out, "v0.1.0") {
		t.Fatalf("--check changed the binary: %s", out)
	}

	out, code = runBinary(t, bin, env, "update")
	if code != 0 || !strings.Contains(out, "Updated store from v0.1.0 to v9.9.9.") {
		t.Fatalf("update = %d\n%s", code, out)
	}
	if out, _ := runBinary(t, bin, env, "--version"); strings.TrimSpace(out) != "store version v9.9.9" {
		t.Fatalf("binary after update reports %q", out)
	}
}

func TestUpdateWhenCurrentAndForDevBuilds(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin", "store")
	releaseBinary(t, bin, "v9.9.9")
	srv := releaseServer(t, "v9.9.9")
	env := []string{"PATH=/usr/bin:/bin", "HOME=" + root, "STORE_UPDATE_API=" + srv.URL}

	if out, code := runBinary(t, bin, env, "update"); code != 0 || !strings.Contains(out, "store v9.9.9 is up to date.") {
		t.Fatalf("up-to-date update = %d\n%s", code, out)
	}
	if out, code := runBinary(t, storeBin, env, "update"); code == 0 || !strings.Contains(out, "is not a release") {
		t.Fatalf("dev build update = %d\n%s", code, out)
	}
}

func TestUpdateDelegatesHomebrewInstalls(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "homebrew", "Caskroom", "store", "0.1.0", "store")
	releaseBinary(t, bin, "v0.1.0")
	srv := releaseServer(t, "v9.9.9")

	brewDir := filepath.Join(root, "brewbin")
	brewLog := filepath.Join(root, "brew.log")
	if err := os.MkdirAll(brewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	brew := fmt.Sprintf("#!/bin/sh\necho \"$@\" > %q\n", brewLog)
	if err := os.WriteFile(filepath.Join(brewDir, "brew"), []byte(brew), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "homebrew", "bin", "store")
	os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + brewDir + ":/usr/bin:/bin", "HOME=" + root, "STORE_UPDATE_API=" + srv.URL}

	out, code := runBinary(t, link, env, "update")
	if code != 0 || !strings.Contains(out, "installed with Homebrew") {
		t.Fatalf("Homebrew update = %d\n%s", code, out)
	}
	got, _ := os.ReadFile(brewLog)
	if strings.TrimSpace(string(got)) != "upgrade --cask kshehadeh/tap/store" {
		t.Fatalf("brew args = %q", got)
	}
	if out, _ := runBinary(t, link, env, "--version"); !strings.Contains(out, "v0.1.0") {
		t.Fatalf("store replaced a Homebrew-managed binary itself: %s", out)
	}
}
