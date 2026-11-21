package archive

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZip(t *testing.T, entries func(w *zip.Writer)) string {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	entries(w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPackExtractRoundTrip(t *testing.T) {
	src := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "sub", "deep"), 0o755))
	must(os.MkdirAll(filepath.Join(src, "empty"), 0o755))
	must(os.WriteFile(filepath.Join(src, ".hidden"), []byte("h"), 0o644))
	must(os.WriteFile(filepath.Join(src, "sub", "deep", "f.txt"), []byte("deep"), 0o644))
	must(os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\n"), 0o755))

	zp := filepath.Join(t.TempDir(), "out.zip")
	f, err := os.Create(zp)
	must(err)
	must(PackDir(src, f))
	must(f.Close())

	dest := filepath.Join(t.TempDir(), "restored")
	must(Extract(zp, dest, DefaultLimits))
	for p, want := range map[string]string{".hidden": "h", "sub/deep/f.txt": "deep", "run.sh": "#!/bin/sh\n"} {
		got, err := os.ReadFile(filepath.Join(dest, p))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", p, got, err, want)
		}
	}
	if info, err := os.Stat(filepath.Join(dest, "empty")); err != nil || !info.IsDir() {
		t.Errorf("empty dir not restored: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "run.sh"))
	must(err)
	if info.Mode().Perm()&0o100 == 0 || info.Mode().Perm()&0o077 != 0 {
		t.Errorf("run.sh mode = %v; want owner-executable and private", info.Mode().Perm())
	}
}

func TestPackRejectsSymlinks(t *testing.T) {
	src := t.TempDir()
	if err := os.Symlink("/etc/hosts", filepath.Join(src, "link")); err != nil {
		t.Skip(err)
	}
	err := PackDir(src, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("PackDir error = %v; want symlink rejection", err)
	}
}

func TestExtractRejectsUnsafeEntries(t *testing.T) {
	names := []string{"../escape.txt", "/abs.txt", "a/../../b", `a\b`, "C:/x", "./x"}
	for _, name := range names {
		zp := writeZip(t, func(w *zip.Writer) {
			f, _ := w.Create(name)
			f.Write([]byte("x"))
		})
		parent := t.TempDir()
		dest := filepath.Join(parent, "out")
		if err := Extract(zp, dest, DefaultLimits); err == nil {
			t.Errorf("Extract accepted %q", name)
		}
		if _, err := os.Stat(filepath.Join(parent, "escape.txt")); err == nil {
			t.Errorf("%q escaped the destination", name)
		}
	}
}

func TestExtractRejectsSymlinkEntries(t *testing.T) {
	zp := writeZip(t, func(w *zip.Writer) {
		h := &zip.FileHeader{Name: "link"}
		h.SetMode(os.ModeSymlink | 0o777)
		f, _ := w.CreateHeader(h)
		f.Write([]byte("/etc/passwd"))
	})
	if err := Extract(zp, filepath.Join(t.TempDir(), "out"), DefaultLimits); err == nil {
		t.Fatal("Extract accepted a symlink entry")
	}
}

func TestExtractEnforcesLimits(t *testing.T) {
	zp := writeZip(t, func(w *zip.Writer) {
		for _, n := range []string{"a", "b", "c"} {
			f, _ := w.Create(n)
			f.Write(bytes.Repeat([]byte("x"), 1000))
		}
	})
	if err := Extract(zp, filepath.Join(t.TempDir(), "o1"), Limits{MaxFiles: 2, MaxBytes: 1 << 20}); err == nil {
		t.Error("file count limit not enforced")
	}
	if err := Extract(zp, filepath.Join(t.TempDir(), "o2"), Limits{MaxFiles: 10, MaxBytes: 2500}); err == nil {
		t.Error("size limit not enforced")
	}
}
