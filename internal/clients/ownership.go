package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/skills"
)

type ownedFile struct {
	Path   string  `json:"path"`
	Before *string `json:"before"`
	After  string  `json:"after"`
}
type ownership struct {
	Files []ownedFile `json:"files"`
}

func statePath(target string) string {
	return filepath.Join(cache.Home(), "client-state", target+".json")
}
func managedPaths(t string) []string {
	names := map[string][]string{"claude": {"settings.json", "commands/save.md"}, "codex": {"hooks.json", "skills/save/SKILL.md"}, "gemini": {"settings.json"}, "cursor": {"hooks.json", "mcp.json"}, "opencode": {"opencode.json", "opencode.jsonc", "AGENTS.md"}}
	out := []string{}
	for _, n := range names[t] {
		out = append(out, filepath.Join(clientDir(t), filepath.FromSlash(n)))
	}
	return out
}
func loadOwnership(t string) (ownership, error) {
	var o ownership
	b, e := skills.Read(statePath(t))
	if e != nil {
		return o, e
	}
	e = json.Unmarshal(b, &o)
	if e != nil {
		return o, e
	}
	allowed := map[string]bool{}
	for _, p := range managedPaths(t) {
		allowed[p] = true
	}
	for _, f := range o.Files {
		if !allowed[f.Path] {
			return o, fmt.Errorf("ownership path does not match current client home")
		}
	}
	return o, nil
}
func (c manager) installOwned(t string) (string, error) {
	if len(managedPaths(t)) == 0 {
		return "", fmt.Errorf("unknown client %q", t)
	}
	c.target = t
	return c.install(t)
}

func (c manager) ownedWrite(path, content string) (string, error) {
	o, err := loadOwnership(c.target)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := skills.Guard(statePath(c.target)); err != nil {
		return "", err
	}
	b, err := skills.Read(path)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	var before *string
	if err == nil {
		v := string(b)
		before = &v
	}
	index := -1
	for i, f := range o.Files {
		if f.Path == path {
			index = i
			if string(b) == f.After {
				before = f.Before
			} else if os.IsNotExist(err) || f.Before != nil && string(b) == *f.Before {
				// A previously restored or removed file starts a fresh ownership cycle.
			} else if strings.HasSuffix(path, ".json") && c.target != "opencode" {
				prior := map[string]any{}
				var e error
				if f.Before != nil {
					prior, e = decodeRoot([]byte(*f.Before))
					if e != nil {
						return "", e
					}
				}
				after, e := decodeRoot([]byte(f.After))
				if e != nil {
					return "", e
				}
				current, e := decodeRoot(b)
				if e != nil {
					return "", e
				}
				restored, _ := restoreValue(prior, after, current)
				raw, e := json.MarshalIndent(restored, "", "  ")
				if e != nil {
					return "", e
				}
				v := string(raw) + "\n"
				before = &v
			} else if c.target == "opencode" && filepath.Base(path) != "AGENTS.md" {
				restored, e := restoreOpencode(f.Before, f.After, string(b))
				if e != nil {
					return "", e
				}
				before = &restored
			} else if filepath.Base(path) == "AGENTS.md" {
				v := strings.Replace(string(b), AgentsGuide, "", 1)
				before = &v
			} else {
				before = f.Before
			}
			break
		}
	}
	f := ownedFile{Path: path, Before: before, After: content}
	if index < 0 {
		o.Files = append(o.Files, f)
	} else {
		o.Files[index] = f
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	if len(raw) > skills.MaxSize {
		return "", fmt.Errorf("ownership record exceeds size limit")
	}
	plain := c
	plain.target = ""
	msg, err := plain.writeWithBackup(path, content)
	if err != nil || strings.HasPrefix(msg, "skipped ") {
		return msg, err
	}
	if err = writeAtomic(statePath(c.target), string(raw)); err != nil {
		var rollback error
		if b == nil {
			rollback = os.Remove(path)
		} else {
			rollback = writeAtomic(path, string(b))
		}
		return msg, errors.Join(fmt.Errorf("ownership ledger: %w", err), rollback)
	}
	return msg, nil
}

// restoreValue restores only unchanged owned leaves and array entries, retaining
// unrelated user changes made since installation.
func restoreValue(before, after, current any) (any, bool) {
	if reflect.DeepEqual(before, after) {
		return current, true
	}
	if reflect.DeepEqual(current, after) {
		return before, before != nil
	}
	am, aok := after.(map[string]any)
	cm, cok := current.(map[string]any)
	bm, _ := before.(map[string]any)
	if aok && cok {
		for k, av := range am {
			cv, exists := cm[k]
			if !exists {
				continue
			}
			bv, existed := bm[k]
			v, keep := restoreValue(bv, av, cv)
			if existed && reflect.DeepEqual(cv, av) {
				v, keep = bv, true
			}
			if keep {
				cm[k] = v
			} else {
				delete(cm, k)
			}
		}
		return cm, true
	}
	aa, aok := after.([]any)
	ca, cok := current.([]any)
	ba, _ := before.([]any)
	if aok && cok {
		used := make([]bool, len(ba))
		out := []any{}
		for _, cv := range ca {
			matched := -1
			for j, av := range aa {
				if reflect.DeepEqual(cv, av) {
					matched = j
					break
				}
			}
			if matched < 0 {
				for j, av := range aa {
					if sharesCommand(av, cv) {
						matched = j
						break
					}
				}
				if matched >= 0 {
					var bv any
					if matched < len(ba) {
						bv = ba[matched]
						used[matched] = true
					}
					v, keep := restoreValue(bv, aa[matched], cv)
					if keep {
						out = append(out, v)
					}
					continue
				}
				out = append(out, cv)
				continue
			}
			av := aa[matched]
			found := -1
			for j, bv := range ba {
				if reflect.DeepEqual(av, bv) {
					found = j
					break
				}
			}
			if found >= 0 {
				used[found] = true
				out = append(out, cv)
				continue
			}
			if matched < len(ba) && !used[matched] {
				used[matched] = true
				out = append(out, ba[matched])
			}
		}
		return out, true
	}
	return current, true
}
func (c manager) uninstallOwned(t string) (string, error) {
	c.restoring = true
	o, e := loadOwnership(t)
	if os.IsNotExist(e) {
		if t != "claude" && t != "codex" {
			return "unchanged " + clientDir(t) + " (no ownership record)", nil
		}
		return c.uninstall(t)
	}
	if e != nil {
		return "", e
	}
	type change struct {
		path, body string
		remove     bool
	}
	var changes []change
	var failures []error
	prepare := func(f ownedFile) error {
		b, e := skills.Read(f.Path)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		current := string(b)
		if f.Before != nil && current == *f.Before {
			return nil
		}
		if current == f.After {
			if f.Before == nil {
				changes = append(changes, change{path: f.Path, remove: true})
			} else {
				changes = append(changes, change{path: f.Path, body: *f.Before})
			}
			return nil
		}
		if t == "opencode" && filepath.Base(f.Path) != "AGENTS.md" {
			body, err := restoreOpencode(f.Before, f.After, current)
			if err != nil {
				return err
			}
			changes = append(changes, change{path: f.Path, body: body})
			return nil
		}
		if filepath.Ext(f.Path) == ".json" {
			before := map[string]any{}
			if f.Before != nil {
				before, e = decodeRoot([]byte(*f.Before))
				if e != nil {
					return e
				}
			}
			after, e := decodeRoot([]byte(f.After))
			if e != nil {
				return e
			}
			now, e := decodeRoot(b)
			if e != nil {
				return fmt.Errorf("modified JSON/JSONC is unsupported; preserved")
			}
			v, _ := restoreValue(before, after, now)
			out, _ := json.MarshalIndent(v, "", "  ")
			changes = append(changes, change{path: f.Path, body: string(out) + "\n"})
		} else if filepath.Base(f.Path) == "AGENTS.md" && strings.Contains(current, AgentsGuide) {
			changes = append(changes, change{path: f.Path, body: strings.Replace(current, AgentsGuide, "", 1)})
		}
		return nil
	}
	for _, f := range o.Files {
		if err := prepare(f); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", f.Path, err))
		}
	}
	out := []string{}
	apply := func(ch change) error {
		if ch.remove {
			if c.dryRun {
				out = append(out, "would back up and remove "+ch.path)
				return nil
			}
			b, e := skills.Read(ch.path)
			if e != nil {
				return e
			}
			if e = backupOnly(ch.path, b); e != nil {
				return e
			}
			if e = os.Remove(ch.path); e != nil {
				return e
			}
			out = append(out, "removed "+ch.path)
		} else {
			s, e := c.writeWithBackup(ch.path, ch.body)
			if e != nil {
				return e
			}
			out = append(out, s)
		}
		return nil
	}
	for _, ch := range changes {
		if err := apply(ch); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", ch.path, err))
		}
	}
	for _, err := range failures {
		out = append(out, "failed: "+err.Error())
	}
	// Keep the ledger to allow a later retry for user-modified owned entries.
	return strings.Join(out, "\n"), errors.Join(failures...)
}

func backupOnly(path string, b []byte) error {
	return exclusiveWrite(path+".bak-aibase-"+time.Now().Format("20060102-150405.000000000"), b)
}

func sharesCommand(a, b any) bool {
	commands := map[string]bool{}
	var collect func(any, bool)
	found := false
	collect = func(v any, first bool) {
		switch x := v.(type) {
		case map[string]any:
			if c, ok := x["command"].(string); ok {
				if first {
					commands[c] = true
				} else if commands[c] {
					found = true
				}
			}
			for _, v := range x {
				collect(v, first)
			}
		case []any:
			for _, v := range x {
				collect(v, first)
			}
		}
	}
	collect(a, true)
	collect(b, false)
	return found
}
