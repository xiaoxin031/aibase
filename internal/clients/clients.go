package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/fileio"
	"github.com/xiaoxin031/aibase/internal/skills"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type manager struct {
	dryRun    bool
	target    string
	restoring bool
}

func Run(args []string, uninstall bool) (out string, resultErr error) {
	defer func() {
		if out == AgentsGuide {
			return
		}
		success, skipped, failed := 0, 0, 0
		for _, line := range strings.Split(out, "\n") {
			switch {
			case strings.HasPrefix(line, "skipped "):
				skipped++
			case strings.HasPrefix(line, "failed"):
				failed++
			case strings.HasPrefix(line, "created "), strings.HasPrefix(line, "updated "), strings.HasPrefix(line, "unchanged "), strings.HasPrefix(line, "would "), strings.HasPrefix(line, "removed "):
				success++
			}
		}
		if resultErr != nil && failed == 0 {
			failed = 1
		}
		out += fmt.Sprintf("\nSummary: success=%d skipped=%d failed=%d", success, skipped, failed)
	}()
	for _, a := range args {
		if a == "--print-agents-md" {
			return AgentsGuide, nil
		}
	}
	c := manager{}
	var targets []string
	for _, a := range args {
		if a == "--dry-run" {
			c.dryRun = true
		} else {
			if a == "cursor-agent" {
				a = "cursor"
			}
			targets = append(targets, a)
		}
	}
	if len(targets) > 1 {
		return "", errors.New("usage: aibase install|uninstall [claude|codex] [--dry-run]")
	}
	if len(targets) == 1 {
		if uninstall {
			return c.uninstallOwned(targets[0])
		}
		return c.installOwned(targets[0])
	}
	if !uninstall {
		return c.installDetected()
	}
	var lines []string
	var failures []error
	for _, d := range Detect() {
		t := d.Name
		if !d.Detected {
			continue
		}
		s, err := c.uninstallOwned(t)
		if err != nil {
			failures = append(failures, err)
			lines = append(lines, "failed "+t+": "+err.Error())
		}
		lines = append(lines, s)
	}
	return strings.Join(lines, "\n"), errors.Join(failures...)
}
func quoteExe(exe string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(exe, `"`, `\"`) + `"`
	}
	if strings.ContainsAny(exe, " \t\r\n\"'\\$`;&|<>()*?![]{}") {
		return "'" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "'"
	}
	return exe
}

// Only a direct executable followed by the hook argument is owned by us.
// Backslashes are kept literal so Windows paths can be recognized on every OS.
func isHook(command, name string) bool {
	s := strings.TrimSpace(command)
	if s == "" {
		return false
	}
	var exe string
	if s[0] == '"' || s[0] == '\'' {
		q := s[0]
		i := strings.IndexByte(s[1:], q)
		if i < 0 {
			return false
		}
		exe = s[1 : 1+i]
		s = strings.TrimSpace(s[i+2:])
	} else {
		i := strings.IndexAny(s, " \t\r\n")
		if i < 0 {
			return false
		}
		exe = s[:i]
		s = strings.TrimSpace(s[i:])
	}
	base := filepath.Base(strings.ReplaceAll(exe, `\`, "/"))
	if strings.Contains(exe, `\`) || strings.HasSuffix(strings.ToLower(base), ".exe") {
		base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	}
	if strings.ContainsAny(s, ";|&<>`$\r\n") {
		return false
	}
	fields := strings.Fields(s)
	return (base == name || name == "aibase" && isAibaseExe(exe)) && len(fields) > 0 && fields[0] == "hook"
}
func addHooks(path, exe, agent string, allow bool) (string, error) {
	return (manager{}).addHooks(path, exe, agent, allow)
}
func exclusiveWrite(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	e := f.Close()
	if err != nil {
		return err
	}
	return e
}
func writeAtomic(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".aibase-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return fileio.Replace(f.Name(), path)
}
func (c manager) uninstall(target string) (string, error) {
	if target != "claude" && target != "codex" {
		return c.extended(target, true)
	}
	filename := ""
	switch target {
	case "claude":
		filename = "settings.json"
	case "codex":
		filename = "hooks.json"
	default:
		return "", fmt.Errorf("unknown target %q (want claude|codex)", target)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	_ = h
	path := filepath.Join(clientDir(target), filename)
	b, err := skills.Read(path)
	if os.IsNotExist(err) {
		return "unchanged " + path, nil
	}
	if err != nil {
		return "", err
	}
	var root map[string]any
	if root, err = decodeRoot(b); err != nil {
		return "", err
	}
	if err := validateRoot(root); err != nil {
		return "", err
	}
	changed := false
	hooks, _ := root["hooks"].(map[string]any)
	for event, v := range hooks {
		groups, ok := v.([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, g := range groups {
			gm, _ := g.(map[string]any)
			hs, ok := gm["hooks"].([]any)
			if !ok {
				kept = append(kept, g)
				continue
			}
			remain := make([]any, 0, len(hs))
			removed := false
			for _, x := range hs {
				hm, _ := x.(map[string]any)
				cmd, _ := hm["command"].(string)
				if isHook(cmd, "aibase") {
					changed = true
					removed = true
					continue
				}
				remain = append(remain, x)
			}
			if !removed {
				kept = append(kept, g)
				continue
			}
			if len(remain) > 0 || len(gm) > 1 {
				gm["hooks"] = remain
				kept = append(kept, g)
			}
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if perms, ok := root["permissions"].(map[string]any); ok {
		if allow, ok := perms["allow"].([]any); ok {
			kept := make([]any, 0, len(allow))
			for _, a := range allow {
				if a == "Bash(aibase:*)" {
					changed = true
				} else {
					kept = append(kept, a)
				}
			}
			perms["allow"] = kept
		}
	}
	if !changed {
		return "unchanged " + path, nil
	}
	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	return c.writeWithBackup(path, string(out)+"\n")
}

func validateRoot(root map[string]any) error {
	if root == nil {
		return errors.New("client config must be a JSON object")
	}
	if v, ok := root["hooks"]; ok {
		hooks, ok := v.(map[string]any)
		if !ok {
			return errors.New("hooks must be an object")
		}
		for _, event := range []string{"SessionStart", "Stop"} {
			if v, exists := hooks[event]; exists {
				if _, ok := v.([]any); !ok {
					return fmt.Errorf("hooks.%s must be an array", event)
				}
			}
		}
	}
	if v, ok := root["permissions"]; ok {
		p, ok := v.(map[string]any)
		if !ok {
			return errors.New("permissions must be an object")
		}
		if v, ok := p["allow"]; ok {
			if _, ok := v.([]any); !ok {
				return errors.New("permissions.allow must be an array")
			}
		}
	}
	return nil
}

func decodeRoot(b []byte) (map[string]any, error) {
	if !json.Valid(b) {
		return nil, errors.New("invalid client JSON")
	}
	var root map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.UseNumber()
	value, err := decodeUnique(decoder)
	if err != nil {
		return nil, err
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("client config must be an object")
	}
	return root, nil
}

func decodeUnique(d *json.Decoder) (any, error) {
	t, e := d.Token()
	if e != nil {
		return nil, e
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return nil, e
			}
			key, ok := k.(string)
			if !ok {
				return nil, errors.New("invalid JSON key")
			}
			if _, exists := m[key]; exists {
				return nil, fmt.Errorf("duplicate JSON key %s", key)
			}
			v, e := decodeUnique(d)
			if e != nil {
				return nil, e
			}
			m[key] = v
		}
		_, e = d.Token()
		return m, e
	case '[':
		a := []any{}
		for d.More() {
			v, e := decodeUnique(d)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		_, e = d.Token()
		return a, e
	}
	return nil, errors.New("invalid JSON delimiter")
}
