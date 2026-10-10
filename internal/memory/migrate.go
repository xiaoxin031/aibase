package memory

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaoxin031/aibase/internal/redact"
)

func noSymlinks(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for p := abs; ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link not allowed: %s", p)
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}

// Resolve the deepest existing ancestor, then append the missing suffix.
func resolveAncestors(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p := abs
	var suffix []string
	for {
		_, err := os.Lstat(p)
		if err == nil {
			real, err := filepath.EvalSymlinks(p)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				real = filepath.Join(real, suffix[i])
			}
			return real, nil
		}
		if !os.IsNotExist(err) || filepath.Dir(p) == p {
			return "", err
		}
		suffix = append(suffix, filepath.Base(p))
		p = filepath.Dir(p)
	}
}
func nested(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func Migrate() (string, error) {
	source := os.Getenv("AMEM_HOME")
	if source == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		source = filepath.Join(h, ".amem")
	}
	source, err := resolveAncestors(source)
	if err != nil {
		return "", err
	}
	dest, err := resolveAncestors(home())
	if err != nil {
		return "", err
	}
	// Check overlap after resolving existing root aliases.
	if nested(source, dest) || nested(dest, source) {
		return "", errors.New("source and destination must not overlap")
	}
	if err = noSymlinks(source); err != nil {
		return "", err
	}
	if err = noSymlinks(dest); err != nil {
		return "", err
	}
	st, err := os.Stat(source)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", errors.New("amem source must be a directory")
	}
	// SameFile catches case aliases on case-insensitive filesystems.
	if ancestorIs(dest, st) {
		return "", errors.New("source and destination must not overlap")
	}
	if targetInfo, err := os.Stat(dest); err == nil && ancestorIs(source, targetInfo) {
		return "", errors.New("source and destination must not overlap")
	}
	sourceRoot, err := os.OpenRoot(source)
	if err != nil {
		return "", err
	}
	defer sourceRoot.Close()
	if err = os.MkdirAll(dest, 0700); err != nil {
		return "", err
	}
	destRoot, err := os.OpenRoot(dest)
	if err != nil {
		return "", err
	}
	defer destRoot.Close()
	var report strings.Builder
	err = fs.WalkDir(sourceRoot.FS(), ".", func(rel string, d fs.DirEntry, e error) error {
		path := filepath.Join(source, rel)
		if e != nil {
			return e
		}
		to := filepath.Join(dest, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link not allowed: %s", path)
		}
		if redact.Forbidden(path) {
			fmt.Fprintf(&report, "skipped %s (sensitive path)\n", rel)
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if e = noSymlinks(to); e != nil {
			return e
		}
		if d.IsDir() {
			if existing, err := destRoot.Lstat(rel); err == nil && !existing.IsDir() {
				fmt.Fprintf(&report, "skipped %s (exists)\n", rel)
				return fs.SkipDir
			}
			return destRoot.MkdirAll(rel, 0700)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular source: %s", path)
		}
		out, e := destRoot.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if errors.Is(e, os.ErrExist) {
			fmt.Fprintf(&report, "skipped %s (exists)\n", rel)
			return nil
		}
		if e != nil {
			return e
		}
		in, e := sourceRoot.Open(rel)
		if e != nil {
			out.Close()
			destRoot.Remove(rel)
			return e
		}
		_, e = io.Copy(out, in)
		in.Close()
		closeErr := out.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			destRoot.Remove(rel)
			return e
		}
		fmt.Fprintf(&report, "copied %s\n", rel)
		return nil
	})
	return report.String(), err
}

func ancestorIs(path string, info os.FileInfo) bool {
	for p := path; ; p = filepath.Dir(p) {
		if candidate, err := os.Stat(p); err == nil && os.SameFile(info, candidate) {
			return true
		}
		if filepath.Dir(p) == p {
			return false
		}
	}
}
