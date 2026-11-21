// Package restore publishes pulled content without overwriting anything.
package restore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var ErrExists = errors.New("destination already exists")

// ExpandHome expands a leading ~ to the user's home directory.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, "~"+string(filepath.Separator)) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// Resolve returns the absolute destination for a restore. If dest is an
// existing directory, the content is placed inside it as defaultName. The
// final path must not exist and its parent directory must.
func Resolve(dest, defaultName string) (string, error) {
	dest = ExpandHome(strings.TrimSpace(dest))
	if dest == "" {
		return "", errors.New("destination path is empty")
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		abs = filepath.Join(abs, defaultName)
	}
	if _, err := os.Lstat(abs); err == nil {
		return "", fmt.Errorf("%w: %s (store never overwrites local files)", ErrExists, abs)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(abs)
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("directory %s does not exist", parent)
	}
	return abs, nil
}

// Stage is a private directory beside the destination. Content is prepared
// there and moved into place only when complete.
type Stage struct {
	dir   string
	final string
}

func NewStage(final string) (*Stage, error) {
	dir, err := os.MkdirTemp(filepath.Dir(final), ".store-staging-*")
	if err != nil {
		return nil, err
	}
	return &Stage{dir: dir, final: final}, nil
}

func (s *Stage) Path(name string) string { return filepath.Join(s.dir, name) }

func (s *Stage) Cleanup() { os.RemoveAll(s.dir) }

// PublishFile moves a staged file to the destination, failing if the
// destination appeared in the meantime.
func (s *Stage) PublishFile(staged string) error {
	err := os.Link(staged, s.final)
	if err == nil {
		return nil
	}
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: %s", ErrExists, s.final)
	}
	// Some filesystems do not support hard links; fall back to a checked rename.
	return s.rename(staged)
}

// PublishDir moves a staged directory to the destination.
func (s *Stage) PublishDir(staged string) error { return s.rename(staged) }

func (s *Stage) rename(staged string) error {
	if _, err := os.Lstat(s.final); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, s.final)
	}
	return os.Rename(staged, s.final)
}
