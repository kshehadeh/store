package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeServer struct {
	*httptest.Server
	latestHits atomic.Int32
}

func tarball(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{{"README.md", []byte("readme")}, {name, content}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write(f.body)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// serve publishes version with one archive for goos/goarch. A non-empty
// badSum replaces the archive's checksum.
func serve(t *testing.T, version string, binary []byte, badSum string) *fakeServer {
	t.Helper()
	asset := AssetName(version, "linux", "arm64")
	archive := tarball(t, "store", binary)
	sum := sha256.Sum256(archive)
	sums := fmt.Sprintf("%s  %s\n0000  other.tar.gz\n", hex.EncodeToString(sum[:]), asset)
	if badSum != "" {
		sums = fmt.Sprintf("%s  %s\n", badSum, asset)
	}
	fs := &fakeServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fs.latestHits.Add(1)
		base := "http://" + r.Host
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[{"name":%q,"browser_download_url":"%s/dl/archive"},{"name":"checksums.txt","browser_download_url":"%s/dl/sums"}]}`,
			version, asset, base, base)
	})
	mux.HandleFunc("/dl/archive", func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) })
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(sums)) })
	fs.Server = httptest.NewServer(mux)
	t.Cleanup(fs.Close)
	return fs
}

func TestVersions(t *testing.T) {
	for _, v := range []string{"v0.3.0", "v10.0.1"} {
		if !IsRelease(v) {
			t.Errorf("IsRelease(%q) = false", v)
		}
	}
	for _, v := range []string{"dev", "0.3.0", "v0.3", "v0.3.0-1-gabc", "v0.3.0-dirty", "v01.2.3", "v1.2.x"} {
		if IsRelease(v) {
			t.Errorf("IsRelease(%q) = true", v)
		}
	}
	for _, tc := range []struct {
		latest, current string
		want            bool
	}{
		{"v0.3.1", "v0.3.0", true}, {"v0.10.0", "v0.9.9", true}, {"v1.0.0", "v0.99.0", true},
		{"v0.3.0", "v0.3.0", false}, {"v0.2.9", "v0.3.0", false}, {"v0.4.0", "dev", false},
	} {
		if got := Newer(tc.latest, tc.current); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v", tc.latest, tc.current, got)
		}
	}
}

func TestIsHomebrew(t *testing.T) {
	if !IsHomebrew("/opt/homebrew/Caskroom/store/0.3.0/store") {
		t.Error("cask path not detected")
	}
	if IsHomebrew("/Users/me/.local/bin/store") {
		t.Error("plain install detected as Homebrew")
	}
}

func TestInstallReplacesBinaryAfterVerifyingChecksum(t *testing.T) {
	srv := serve(t, "v0.4.0", []byte("new binary"), "")
	c := Client{API: srv.URL}
	rel, err := c.Latest(context.Background())
	if err != nil || rel.Version != "v0.4.0" {
		t.Fatalf("Latest = %+v, %v", rel, err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "store")
	os.WriteFile(exe, []byte("old binary"), 0o700)
	if err := c.Install(context.Background(), rel, exe, "linux", "arm64"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "new binary" {
		t.Fatalf("binary = %q", got)
	}
	if info, _ := os.Stat(exe); info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", info.Mode())
	}
	assertOnlyFile(t, dir, "store")
}

func TestInstallRejectsBadChecksumAndMissingPlatform(t *testing.T) {
	srv := serve(t, "v0.4.0", []byte("new binary"), strings.Repeat("ab", 32))
	c := Client{API: srv.URL}
	rel, err := c.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "store")
	os.WriteFile(exe, []byte("old binary"), 0o755)
	if err := c.Install(context.Background(), rel, exe, "linux", "arm64"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("bad checksum error = %v", err)
	}
	if err := c.Install(context.Background(), rel, exe, "windows", "amd64"); err == nil || !strings.Contains(err.Error(), "no download for windows/amd64") {
		t.Fatalf("missing platform error = %v", err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "old binary" {
		t.Fatalf("binary changed after a failed update: %q", got)
	}
	assertOnlyFile(t, dir, "store")
}

func assertOnlyFile(t *testing.T, dir, name string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != name {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("directory contains %v; want only %s", names, name)
	}
}

func TestNoticeChecksAtMostDaily(t *testing.T) {
	srv := serve(t, "v0.4.0", []byte("x"), "")
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	n := Notifier{
		Client:    Client{API: srv.URL},
		StatePath: filepath.Join(t.TempDir(), "cache", "update-check.json"),
		Current:   "v0.3.0",
		Now:       func() time.Time { return now },
	}
	msg := n.Notice(context.Background())
	if !strings.Contains(msg, "v0.4.0") || !strings.Contains(msg, "store update") {
		t.Fatalf("notice = %q", msg)
	}
	now = now.Add(23 * time.Hour)
	if msg := n.Notice(context.Background()); msg == "" {
		t.Fatal("cached notice was not shown")
	}
	if hits := srv.latestHits.Load(); hits != 1 {
		t.Fatalf("GitHub contacted %d times within a day; want 1", hits)
	}
	now = now.Add(2 * time.Hour)
	n.Notice(context.Background())
	if hits := srv.latestHits.Load(); hits != 2 {
		t.Fatalf("stale cache did not trigger a check; hits = %d", hits)
	}
	n.Current = "v0.4.0"
	if msg := n.Notice(context.Background()); msg != "" {
		t.Fatalf("up-to-date notice = %q", msg)
	}
}

func TestNoticeRecordsFailedChecks(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	var hits atomic.Int32
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	})
	n := Notifier{Client: Client{API: srv.URL}, StatePath: filepath.Join(t.TempDir(), "state.json"), Current: "v0.3.0"}
	for range 3 {
		if msg := n.Notice(context.Background()); msg != "" {
			t.Fatalf("notice after failure = %q", msg)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("failed check retried %d times; want 1", hits.Load())
	}
}
