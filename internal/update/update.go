// Package update finds published store releases and installs them over the
// running binary.
package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultAPI is the GitHub API URL of store's repository.
const DefaultAPI = "https://api.github.com/repos/kshehadeh/store"

// BrewCask is the Homebrew cask that installs store.
const BrewCask = "kshehadeh/tap/store"

const (
	maxMetadataBytes = 1 << 20
	maxArchiveBytes  = 200 << 20
)

// Release is a published store version and its downloadable assets.
type Release struct {
	Version string
	Assets  map[string]string
}

// Client reads release information. API is the repository API URL.
type Client struct {
	API  string
	HTTP *http.Client
}

// NewClient uses STORE_UPDATE_API when set, which lets tests serve releases
// locally.
func NewClient() Client {
	api := os.Getenv("STORE_UPDATE_API")
	if api == "" {
		api = DefaultAPI
	}
	return Client{API: strings.TrimRight(api, "/"), HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

// Latest returns the newest published, non-prerelease version.
func (c Client) Latest(ctx context.Context) (Release, error) {
	body, err := c.get(ctx, c.API+"/releases/latest", maxMetadataBytes)
	if err != nil {
		return Release{}, fmt.Errorf("checking for the latest release: %w", err)
	}
	var out struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Release{}, fmt.Errorf("unexpected release information: %w", err)
	}
	if !IsRelease(out.TagName) {
		return Release{}, fmt.Errorf("latest release has an unexpected version %q", out.TagName)
	}
	rel := Release{Version: out.TagName, Assets: map[string]string{}}
	for _, a := range out.Assets {
		rel.Assets[a.Name] = a.URL
	}
	return rel, nil
}

func (c Client) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "store-cli")
	req.Header.Set("Accept", "application/vnd.github+json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", url)
	}
	return body, nil
}

// IsRelease reports whether v is a plain release version such as v1.2.3.
// Development builds and git-describe versions are not.
func IsRelease(v string) bool {
	_, ok := parse(v)
	return ok
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if !strings.HasPrefix(v, "v") || len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || strconv.Itoa(n) != p {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// Newer reports whether release version latest is newer than current.
func Newer(latest, current string) bool {
	l, ok1 := parse(latest)
	c, ok2 := parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// IsHomebrew reports whether exe, a resolved executable path, belongs to a
// Homebrew cask installation.
func IsHomebrew(exe string) bool {
	return strings.Contains(filepath.ToSlash(exe), "/Caskroom/")
}

// AssetName is the release archive for a platform.
func AssetName(version, goos, goarch string) string {
	return fmt.Sprintf("store_%s_%s_%s.tar.gz", strings.TrimPrefix(version, "v"), goos, goarch)
}

// Install downloads rel's archive for goos/goarch, verifies it against the
// release checksums, and atomically replaces exe with the archived binary.
func (c Client) Install(ctx context.Context, rel Release, exe, goos, goarch string) error {
	name := AssetName(rel.Version, goos, goarch)
	archiveURL, ok := rel.Assets[name]
	if !ok {
		return fmt.Errorf("release %s has no download for %s/%s", rel.Version, goos, goarch)
	}
	sumsURL, ok := rel.Assets["checksums.txt"]
	if !ok {
		return fmt.Errorf("release %s has no checksums.txt", rel.Version)
	}
	sums, err := c.get(ctx, sumsURL, maxMetadataBytes)
	if err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}
	want, err := checksumFor(sums, name)
	if err != nil {
		return err
	}
	archive, err := c.get(ctx, archiveURL, maxArchiveBytes)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", name, err)
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s does not match its published checksum; nothing was changed", name)
	}
	binary, err := extractBinary(archive)
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}
	return replace(exe, binary)
}

func checksumFor(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("archive does not contain the store binary")
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Clean(h.Name) == "store" {
			return io.ReadAll(io.LimitReader(tr, maxArchiveBytes))
		}
	}
}

// replace writes the new binary beside exe and renames it into place, so an
// interrupted update leaves the old binary intact.
func replace(exe string, binary []byte) error {
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".store-update-*")
	if err != nil {
		return replaceError(exe, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(binary); err != nil {
		tmp.Close()
		return replaceError(exe, err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return replaceError(exe, err)
	}
	if err := tmp.Close(); err != nil {
		return replaceError(exe, err)
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return replaceError(exe, err)
	}
	return nil
}

func replaceError(exe string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("cannot replace %s: permission denied; rerun with permission to write that directory, or reinstall store", exe)
	}
	return fmt.Errorf("cannot replace %s: %w", exe, err)
}
