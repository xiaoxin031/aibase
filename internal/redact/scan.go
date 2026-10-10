package redact

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Forbidden is deliberately conservative, including explicitly supplied roots.
func Forbidden(path string) bool {
	p := strings.ToLower(strings.ReplaceAll(filepath.ToSlash(path), `\`, "/"))
	for _, part := range strings.Split(p, "/") {
		switch part {
		case ".ssh", ".aws", ".azure", ".config/gcloud", "gcloud", "keychains", "keychain", "credentials", "credential manager", "chrome", "google-chrome", "edge", "chromium", "firefox", "safari", "microsoft edge", "brave-browser":
			return true
		}
		if part == ".env" || strings.HasPrefix(part, ".env.") {
			return true
		}
	}
	return false
}
func Scan(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if Forbidden(resolved) {
		return fmt.Sprintf("%s:0:exists (excluded)\n", root), nil
	}
	var out strings.Builder
	entries, total, findings := 0, 0, 0
	truncated := false
	var walk func(string, int) error
	walk = func(dir string, depth int) error {
		st, e := os.Lstat(dir)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if Forbidden(dir) {
			fmt.Fprintf(&out, "%s:0:exists (excluded)\n", dir)
			return nil
		}
		entries++
		if entries > 20000 || depth > 8 || total >= 16<<20 || findings >= 1000 {
			truncated = true
			return nil
		}
		if st.IsDir() {
			f, e := os.Open(dir)
			if e != nil {
				return e
			}
			defer f.Close()
			for {
				ds, e := f.ReadDir(32)
				for _, d := range ds {
					if e := walk(filepath.Join(dir, d.Name()), depth+1); e != nil {
						return e
					}
					if truncated {
						return nil
					}
				}
				if e == io.EOF {
					return nil
				}
				if e != nil {
					return e
				}
			}
		}
		if !st.Mode().IsRegular() {
			return nil
		}
		if st.Size() > 1<<20 {
			fmt.Fprintf(&out, "%s:0:not scanned (file exceeds 1 MiB)\n", dir)
			return nil
		}
		f, e := os.Open(dir)
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
		f.Close()
		if e != nil {
			return e
		}
		total += len(b)
		if len(b) > 1<<20 {
			truncated = true
			return nil
		}
		text := string(b)
		for _, m := range Matches(text) {
			findings++
			if findings > 1000 {
				truncated = true
				break
			}
			fmt.Fprintf(&out, "%s:%d:%s\n", dir, 1+strings.Count(text[:m.Start], "\n"), m.Type)
		}
		return nil
	}
	err = walk(root, 0)
	if truncated {
		out.WriteString("scan truncated: 20000 entries / depth 8 / 16 MiB / 1000 findings\n")
	}

	return out.String(), err
}

// CheckPath guards explicit file input as well as scan roots and link targets.
func CheckPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if Forbidden(abs) {
		return fmt.Errorf("sensitive path excluded: %s", path)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil && Forbidden(resolved) {
		return fmt.Errorf("sensitive path excluded: %s", path)
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
