package skills

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/redact"
)

// Guard rejects redirected skill/config paths, while allowing macOS system aliases.
func Guard(path string) error {
	abs, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	for p := abs; ; p = filepath.Dir(p) {
		if redact.Forbidden(p) {
			return fmt.Errorf("sensitive path excluded: %s", p)
		}
		st, e := os.Lstat(p)
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			if runtime.GOOS == "darwin" && (p == "/tmp" || p == "/var" || p == "/etc") {
				continue
			}
			return fmt.Errorf("symlink path refused: %s", p)
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nil
}

type treeFile struct {
	rel  string
	b    []byte
	mode fs.FileMode
}

func tree(src string) ([]treeFile, error) {
	if e := Guard(src); e != nil {
		return nil, e
	}
	files := []treeFile{}
	entries, total := 0, 0
	e := filepath.WalkDir(src, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		entries++
		if entries > 512 {
			return fmt.Errorf("skill tree exceeds 512 entries")
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		if strings.Count(rel, string(os.PathSeparator)) > 8 {
			return fmt.Errorf("skill tree exceeds depth 8")
		}
		if d.Type()&os.ModeSymlink != 0 || redact.Forbidden(p) {
			return fmt.Errorf("unsafe skill tree entry: %s", p)
		}
		if d.IsDir() {
			return nil
		}
		b, e := Read(p)
		if e != nil {
			return e
		}
		total += len(b)
		if total > 4<<20 {
			return fmt.Errorf("skill tree exceeds 4 MiB")
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		files = append(files, treeFile{rel, b, st.Mode().Perm()})
		return nil
	})
	return files, e
}
func treeHash(src string) (string, error) {
	files, e := tree(src)
	if e != nil {
		return "", e
	}
	h := sha256.New()
	for _, f := range files {
		fmt.Fprintf(h, "%d:%s:%d:", len(f.rel), f.rel, len(f.b))
		h.Write(f.b)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
func copyTree(src, dst string, promote ...bool) error {
	files, e := tree(src)
	if e != nil {
		return e
	}
	if e = Guard(dst); e != nil {
		return e
	}
	if e = os.Mkdir(dst, 0700); e != nil {
		return e
	}
	// Destination is newly and exclusively created. Failure cleanup owns only it.
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dst)
		}
	}()
	for _, f := range files {
		if len(promote) > 0 && promote[0] && f.rel == "SKILL.md" {
			f.b = []byte(strings.Replace(string(f.b), "UNVERIFIED DRAFT. Review and run aibase skill promote before use.", "Validated by aibase skill promote. Installation does not authorize scripts.", 1))
		}
		p := filepath.Join(dst, f.rel)
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		mode := fs.FileMode(0600)
		if f.mode&0111 != 0 {
			mode = 0700
		}
		if e = os.WriteFile(p, f.b, mode); e != nil {
			return e
		}
	}
	success = true
	return nil
}

func OpenDir(path string) (*os.File, error) {
	if e := Guard(path); e != nil {
		return nil, e
	}
	st, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", path)
	}
	return os.Open(path)
}

func replaceOwnedCopy(src, dst string) error {
	if e := Guard(dst); e != nil {
		return e
	}
	parent := filepath.Dir(dst)
	staging, e := os.MkdirTemp(parent, ".aibase-copy-*")
	if e != nil {
		return e
	}
	if e = os.Remove(staging); e != nil {
		return e
	}
	if e = copyTree(src, staging); e != nil {
		return e
	}
	defer os.RemoveAll(staging)
	backups := filepath.Join(filepath.Dir(parent), "aibase-skill-backups")
	if e = Guard(backups); e != nil {
		return e
	}
	if e = os.MkdirAll(backups, 0700); e != nil {
		return e
	}
	backup := filepath.Join(backups, filepath.Base(dst)+"-"+time.Now().Format("20060102-150405.000000000"))
	if e = os.Rename(dst, backup); e != nil {
		return e
	}
	if e = os.Rename(staging, dst); e != nil {
		restoreErr := os.Rename(backup, dst)
		if restoreErr != nil {
			return fmt.Errorf("copy replace failed: %v; restore failed: %v; backup at %s", e, restoreErr, backup)
		}
		return e
	}
	return nil
}
