package restore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	got, err := Resolve(dir, ".env")
	if err != nil || got != filepath.Join(dir, ".env") {
		t.Fatalf("Resolve(dir) = %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(filepath.Join(dir, ".env"), "x"); !errors.Is(err, ErrExists) {
		t.Fatalf("Resolve(existing) error = %v; want ErrExists", err)
	}
	if _, err := Resolve(dir, ".env"); !errors.Is(err, ErrExists) {
		t.Fatalf("Resolve(dir with existing child) error = %v; want ErrExists", err)
	}
	if _, err := Resolve(filepath.Join(dir, "missing", "f"), "f"); err == nil {
		t.Fatal("Resolve accepted a missing parent directory")
	}
}

func TestPublishNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "out")
	s, err := NewStage(final)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Cleanup()
	p := s.Path("payload")
	os.WriteFile(p, []byte("new"), 0o600)
	os.WriteFile(final, []byte("old"), 0o600)
	if err := s.PublishFile(p); !errors.Is(err, ErrExists) {
		t.Fatalf("PublishFile error = %v; want ErrExists", err)
	}
	if b, _ := os.ReadFile(final); string(b) != "old" {
		t.Fatalf("existing file changed to %q", b)
	}
	s.Cleanup()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("staging directory left behind: %v", entries)
	}
}
