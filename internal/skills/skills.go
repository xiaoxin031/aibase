package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/fileio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/xiaoxin031/aibase/internal/cache"
)

const MaxSize = 256 << 10
const MaxSkills = 256

func Root(draft bool) string {
	if draft {
		return filepath.Join(cache.Home(), "skills-draft")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".agents", "skills")
}
func Read(path string) ([]byte, error) {
	if err := Guard(path); err != nil {
		return nil, err
	}
	st, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Size() > MaxSize {
		return nil, fmt.Errorf("not a bounded regular file: %s", path)
	}
	f, e := fileio.OpenRead(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, MaxSize+1))
	if len(b) > MaxSize {
		return nil, fmt.Errorf("file exceeds size limit")
	}
	return b, e
}
func Show(name string, draft bool) (Skill, error) {
	return showAt(Root(draft), name, draft)
}
func showAt(root, name string, draft bool) (Skill, error) {
	var s Skill
	if !safeReadName(name) {
		return s, fmt.Errorf("unsafe skill name")
	}
	p := filepath.Join(root, name, "SKILL.md")
	b, e := Read(p)
	if e != nil {
		return s, e
	}
	s, e = Parse(b)
	s.Path = p
	s.Draft = draft

	if e == nil {
		for _, dep := range strings.Split(strings.Join([]string{s.Metadata["requires"], s.Metadata["requires.bins"], s.Metadata["requires.binaries"]}, ","), ",") {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`).MatchString(dep) {
				e = fmt.Errorf("invalid dependency executable name")
				break
			}
			if _, err := exec.LookPath(dep); err != nil {
				s.Missing = append(s.Missing, dep)
			}
		}
	}
	return s, e
}
func List(draft bool) ([]Skill, error) {
	return listContext(context.Background(), Root(draft), draft)
}

// ListContext bounds refresh work and captures the root before any asynchronous reads.
func ListContext(ctx context.Context, root string) ([]Skill, error) {
	return listContext(ctx, root, false)
}
func listContext(ctx context.Context, root string, draft bool) ([]Skill, error) {
	if e := Guard(root); e != nil {
		return nil, e
	}
	f, e := OpenDir(root)
	if os.IsNotExist(e) {
		return []Skill{}, nil
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	ds, e := f.ReadDir(MaxSkills + 1)
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(ds) > MaxSkills {
		return nil, fmt.Errorf("skill count exceeds %d; narrow the directory", MaxSkills)
	}
	out := []Skill{}
	for _, d := range ds {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !d.IsDir() && d.Type()&os.ModeSymlink == 0 {
			continue
		}
		s, e := showAt(root, d.Name(), draft)
		s.Body = ""
		if e != nil {
			s.Name = d.Name()
			s.Error = e.Error()
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func create(name string, b []byte, draft bool) (string, error) {
	if !ValidName(name) {
		return "", fmt.Errorf("unsafe skill name")
	}
	root := Root(draft)
	if e := Guard(root); e != nil {
		return "", e
	}
	if e := os.MkdirAll(root, 0700); e != nil {
		return "", e
	}
	f, e := OpenDir(root)
	if e != nil {
		return "", e
	}
	defer f.Close()
	ds, e := f.ReadDir(MaxSkills + 1)
	if e == io.EOF {
		e = nil
	}
	if e != nil {
		return "", e
	}
	if len(ds) >= MaxSkills {
		return "", fmt.Errorf("skill count limit")
	}
	for _, d := range ds {
		if strings.EqualFold(d.Name(), name) {
			return "", fmt.Errorf("skill already exists (case insensitive)")
		}
	}
	dir := filepath.Join(root, name)
	if e = os.Mkdir(dir, 0700); e != nil {
		return "", e
	}
	p := filepath.Join(dir, "SKILL.md")
	e = os.WriteFile(p, b, 0600)
	return p, e
}
func New(name string, draft bool) (string, error) {
	state := "Review before use; installation does not authorize scripts."
	if draft {
		state = "UNVERIFIED DRAFT. Review and run aibase skill promote before use."
	}
	return create(name, []byte("---\nname: "+name+"\ndescription: Describe when to use this skill.\n---\n\n"+state+"\n"), draft)
}
func Promote(name string) (string, error) {
	s, e := Show(name, true)
	if e != nil {
		return "", e
	}
	if len(s.Missing) > 0 {
		return "", fmt.Errorf("missing dependencies: %s", strings.Join(s.Missing, ", "))
	}
	root := Root(false)
	if e = Guard(root); e != nil {
		return "", e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return "", e
	}
	f, e := OpenDir(root)
	if e != nil {
		return "", e
	}
	ds, e := f.ReadDir(MaxSkills + 1)
	f.Close()
	if e != nil && e != io.EOF {
		return "", e
	}
	if len(ds) >= MaxSkills {
		return "", fmt.Errorf("skill count limit")
	}
	for _, d := range ds {
		if strings.EqualFold(d.Name(), name) {
			return "", fmt.Errorf("skill already exists")
		}
	}
	dst := filepath.Join(root, name)
	e = copyTree(filepath.Dir(s.Path), dst, true)
	return dst, e
}

// Sync only bridges Claude. Every target is exclusive; a matching ownership
// record is required even for an existing link. Never replace user files.
func Sync() (any, error) { return syncSkills(os.Symlink, runtime.GOOS == "windows") }

func syncSkills(symlink func(string, string) error, copyFallback bool) (any, error) {
	all, e := List(false)
	if e != nil {
		return nil, e
	}
	h, _ := os.UserHomeDir()
	dir := filepath.Join(h, ".claude", "skills")
	record := filepath.Join(cache.Home(), "skill-sync.json")
	owned := map[string]string{}
	if b, err := Read(record); err == nil {
		if json.Unmarshal(b, &owned) != nil || owned == nil {
			return nil, fmt.Errorf("invalid sync ownership record")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if e = Guard(dir); e != nil {
		return nil, e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	done := []string{}
	for _, s := range all {
		if s.Error != "" {
			return nil, errors.New(s.Error)
		}
		src := filepath.Dir(s.Path)
		if _, err := tree(src); err != nil {
			return nil, err
		}
		dst := filepath.Join(dir, filepath.Base(src))
		if _, err := os.Lstat(dst); err == nil {
			if owned[dst] == src {
				if link, err := os.Readlink(dst); err == nil && link == src {
					done = append(done, "unchanged "+s.Name)
					continue
				}
				hash, err := treeHash(dst)
				if err == nil && owned[dst+"#sha256"] == hash {
					sourceHash, err := treeHash(src)
					if err != nil {
						return nil, err
					}
					if sourceHash == hash {
						done = append(done, "unchanged "+s.Name)
						continue
					}
					if err = replaceOwnedCopy(src, dst); err != nil {
						return nil, err
					}
					owned[dst+"#sha256"], err = treeHash(dst)
					if err != nil {
						return nil, err
					}
					if err = saveSyncRecord(record, owned); err != nil {
						return nil, err
					}
					done = append(done, "updated owned copy "+s.Name)
					continue
				}
			}
			return nil, fmt.Errorf("unowned or modified target: %s", dst)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		method := "linked "
		if e = symlink(src, dst); e != nil {
			if !copyFallback {
				return nil, e
			}
			if e = copyTree(src, dst); e != nil {
				return nil, e
			}
			hash, e := treeHash(dst)
			if e != nil {
				return nil, e
			}
			owned[dst+"#sha256"] = hash
			method = "copied "
		}
		owned[dst] = src
		done = append(done, method+s.Name)
		if e = saveSyncRecord(record, owned); e != nil {
			return nil, e
		}
	}
	return done, nil
}

func saveSyncRecord(path string, owned map[string]string) error {
	if e := Guard(path); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	b, e := json.Marshal(owned)
	if e != nil {
		return e
	}
	if len(b) > MaxSize {
		return fmt.Errorf("sync ownership record too large")
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".sync-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return fileio.Replace(f.Name(), path)
}

func safeReadName(name string) bool {
	if runtime.GOOS == "windows" && strings.Contains(name, ":") {
		return false
	}
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00") && !filepath.IsAbs(name)
}
