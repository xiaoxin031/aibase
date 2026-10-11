package clients

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/memory"
	"github.com/xiaoxin031/aibase/internal/skills"
)

func (c manager) install(target string) (string, error) {
	if target != "claude" && target != "codex" {
		return c.extended(target, false)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	var steps []string
	var failures []error
	add := func(msg string, err error) {
		if msg != "" {
			steps = append(steps, msg)
		}
		if err != nil {
			failures = append(failures, err)
			steps = append(steps, "failed: "+err.Error())
		}
	}
	switch target {
	case "claude":
		dir := clientDir("claude")
		msg, err := c.addHooks(filepath.Join(dir, "settings.json"), exe, "claude", true)
		add(msg, err)
		cmd := "---\ndescription: Save this session to aibase memory\n---\n\n" + guideFor("claude")
		msg, err = c.writeWithBackup(filepath.Join(dir, "commands", "save.md"), cmd)
		add(msg+" (/save)", err)
	case "codex":
		dir := clientDir("codex")
		hooks := filepath.Join(dir, "hooks.json")
		msg, err := c.addHooks(hooks, exe, "codex", false)
		add(msg, err)
		if err == nil {
			add(c.trustCodexHooks(hooks), nil)
		}
		skill := "---\nname: save\ndescription: Save this session to aibase memory. Use when the user says save, /save, or asks to remember something.\n---\n\n" + guideFor("codex")
		msg, err = c.writeWithBackup(filepath.Join(dir, "skills", "save", "SKILL.md"), skill)
		add(msg+" ($save)", err)
	default:
		return "", fmt.Errorf("unknown target %q (want claude|codex; other tools: run `aibase mcp` as an MCP server)", target)
	}
	return strings.Join(steps, "\n"), errors.Join(failures...)
}

func (c manager) installDetected() (string, error) {
	var out []string
	var failures []error
	for _, d := range Detect() {
		t := d.Name
		if !d.Detected {
			continue
		}
		msg, err := c.installOwned(t)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", t, err))
			if !strings.Contains(msg, "failed:") {
				out = append(out, "failed "+t+": "+err.Error())
			}
		}
		out = append(out, msg)
	}
	if len(out) == 0 {
		return "", errors.New("no supported client detected; use install <client> or install --print-agents-md")
	}
	return strings.Join(out, "\n"), errors.Join(failures...)
}

func guideFor(agent string) string {
	return strings.Replace(memory.SaveGuide, "<your-name>", agent, 1) + "\n\nWrite memory in the user's language.\n"
}

// addHooks merges aibase's SessionStart and Stop command hooks into a Claude/Codex
// hooks file, updating earlier aibase hooks in place. allowBash also lets Claude
// run aibase without permission prompts.
func (c manager) addHooks(path, exe, agent string, allowBash bool) (string, error) {
	root := map[string]any{}
	if b, err := skills.Read(path); err == nil {
		var decodeErr error
		root, decodeErr = decodeRoot(b)
		if decodeErr != nil {
			return "", fmt.Errorf("%s is not valid JSON: %w", path, decodeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := validateRoot(root); err != nil {
		return "", err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for event, command := range map[string]string{
		"SessionStart": quoteExe(exe) + " hook --agent " + agent,
		"Stop":         quoteExe(exe) + " hook stop --agent " + agent,
	} {
		groups, _ := hooks[event].([]any)
		if !setAibaseCommand(groups, command) {
			groups = append(groups, map[string]any{
				"hooks": []any{map[string]any{"type": "command", "command": command}},
			})
		}
		hooks[event] = groups
	}
	root["hooks"] = hooks
	if allowBash {
		perms, _ := root["permissions"].(map[string]any)
		if perms == nil {
			perms = map[string]any{}
		}
		allow, _ := perms["allow"].([]any)
		if !slices.Contains(allow, any("Bash(aibase:*)")) {
			perms["allow"] = append(allow, "Bash(aibase:*)")
		}
		root["permissions"] = perms
	}
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", err
	}
	msg, err := c.writeWithBackup(path, string(b)+"\n")
	if c.dryRun && err == nil && !strings.HasPrefix(msg, "unchanged ") {
		msg += "\n  SessionStart: " + quoteExe(exe) + " hook --agent " + agent
		msg += "\n  Stop: " + quoteExe(exe) + " hook stop --agent " + agent
		if allowBash {
			msg += "\n  permission: Bash(aibase:*)"
		}
	}
	return msg, err
}

func setAibaseCommand(groups []any, command string) bool {
	found := false
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		hs, _ := gm["hooks"].([]any)
		for _, x := range hs {
			if hm, _ := x.(map[string]any); hm != nil {
				if c, _ := hm["command"].(string); isHook(c, "amem") || isHook(c, "aibase") {
					hm["command"] = command
					found = true
				}
			}
		}
	}
	return found
}

func (c manager) writeWithBackup(path, content string) (string, error) {
	if !c.dryRun && c.target != "" {
		return c.ownedWrite(path, content)
	}
	if err := skills.Guard(path); err != nil {
		return "", err
	}
	old, err := skills.Read(path)
	if filepath.Ext(path) == ".md" && err == nil && string(old) != content && filepath.Base(path) != "AGENTS.md" && !c.restoring && !legacySave(string(old)) {
		return "skipped " + path + " (third-party save file preserved)", nil
	}
	if c.dryRun {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if string(old) == content && err == nil {
			return "unchanged " + path, nil
		}
		return "would back up and write " + path, nil
	}
	switch {
	case err == nil && string(old) == content:
		return "unchanged " + path, nil
	case err == nil:
		bak := path + ".bak-aibase-" + time.Now().Format("20060102-150405.000000000")
		if err := exclusiveWrite(bak, old); err != nil {
			return "", err
		}
		if err := writeAtomic(path, content); err != nil {
			return "", err
		}
		return "updated " + path + " (backup " + filepath.Base(bak) + ")", nil
	case errors.Is(err, os.ErrNotExist):
		return "created " + path, writeAtomic(path, content)
	}
	return "", err
}

func legacySave(s string) bool {
	if strings.Contains(s, "Saving to amem") {
		return true
	}
	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return false
	}
	parsed, e := skills.Parse([]byte("---\nname: save\n" + normalized[4:]))
	return e == nil && strings.Contains(strings.ToLower(parsed.Description), "amem")
}
