// Package archive packs folders into ZIP files and extracts them safely.
package archive

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Limits bound what Extract will write.
type Limits struct {
	MaxFiles int
	MaxBytes int64
}

// DefaultLimits caps extraction at 100,000 entries and 4 GiB of content.
var DefaultLimits = Limits{MaxFiles: 100_000, MaxBytes: 4 << 30}

// PackDir writes the contents of root to w as a ZIP archive, including
// dotfiles and empty directories. Symlinks and special files are rejected so
// the archive restores to exactly what was stored.
func PackDir(root string, w io.Writer) error {
	zw := zip.NewWriter(w)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name = name + "/"
			hdr.Method = zip.Store
			_, err = zw.CreateHeader(hdr)
			return err
		case info.Mode().IsRegular():
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name = name
			hdr.Method = zip.Deflate
			dst, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(dst, f)
			return err
		default:
			return fmt.Errorf("cannot store %s: symlinks and special files are not supported", p)
		}
	})
	if err != nil {
		return err
	}
	return zw.Close()
}

// Extract unpacks zipPath into dest, which must not exist. It rejects
// absolute or escaping paths, links, special files, and archives exceeding
// lim. Files keep their owner permission bits; nothing is readable by group
// or others.
func Extract(zipPath, dest string, lim Limits) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("cannot open folder archive: %w", err)
	}
	defer r.Close()

	if len(r.File) > lim.MaxFiles {
		return fmt.Errorf("archive has %d entries, more than the limit of %d", len(r.File), lim.MaxFiles)
	}
	var declared uint64
	for _, f := range r.File {
		declared += f.UncompressedSize64
		if declared > uint64(lim.MaxBytes) {
			return fmt.Errorf("archive expands beyond the limit of %d bytes", lim.MaxBytes)
		}
	}
	if err := os.Mkdir(dest, 0o700); err != nil {
		return err
	}

	var written int64
	for _, f := range r.File {
		rel, err := safeName(f.Name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		isDir := strings.HasSuffix(f.Name, "/") || mode.IsDir()
		if mode.Type()&^fs.ModeDir != 0 {
			return fmt.Errorf("archive entry %q is a link or special file", f.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if isDir {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		n, err := extractFile(f, target, mode.Perm()&0o700|0o600, lim.MaxBytes-written)
		written += n
		if err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string, perm fs.FileMode, remaining int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, fmt.Errorf("cannot read archive entry %q: %w", f.Name, err)
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, io.LimitReader(rc, remaining+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return n, fmt.Errorf("cannot extract %q: %w", f.Name, err)
	}
	if n > remaining {
		return n, errors.New("archive expands beyond the size limit")
	}
	return n, nil
}

// safeName validates an archive entry name and returns it without a trailing
// slash. Each segment must be a plain name.
func safeName(name string) (string, error) {
	bad := func() (string, error) { return "", fmt.Errorf("archive entry %q has an unsafe path", name) }
	if strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") {
		return bad()
	}
	n := strings.TrimSuffix(name, "/")
	if n == "" {
		return bad()
	}
	for i, seg := range strings.Split(n, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return bad()
		}
		if i == 0 && strings.Contains(seg, ":") {
			return bad()
		}
	}
	return n, nil
}
