package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var version = "dev"

const usage = `aibase — one small, bounded memory for every AI coding CLI

  aibase context [cwd] [--json]    bounded session brief (also inject)
  aibase inspect host [--level basic|tools|runtime]
  aibase inspect project [cwd]    inspect declarations without executing them
  aibase projects [query]         find local repositories
  aibase skill ls|show|new|check|sync|promote [name] [--draft]
  aibase doctor [cwd]             bounded checks and repair suggestions
  aibase inject                     print the session-start snapshot
  aibase hook                       SessionStart hook: inject the snapshot (Claude Code / Codex)
  aibase hook stop --agent A        Stop hook: ask the agent to auto-save after edits / every N turns
  aibase save [file]                apply JSON batches from file or stdin (see: aibase help save)
  aibase add <target> <text>        add one entry   (target: user|memory|project)
  aibase rm <target> <substring>    remove the entry containing substring
  aibase ls [target]                list entries with usage
  aibase log [--agent A] [--session ID] [text]
                                  append a session summary (same session replaces)
  aibase note <slug> [--desc D]     save a reference note (body from stdin)
  aibase show <slug>                print a note
  aibase search <words> [-n N]      search notes and session logs
  aibase import <file|dir>...       import markdown files as notes
  aibase mcp                        run as an MCP server (stdio)
  aibase uninstall [claude|codex|gemini|cursor|opencode] remove aibase hooks (backs up first)
  aibase migrate amem             copy old memory, skipping existing files
  aibase redact scan [path]       report secret locations, never secret values
  aibase audit [-n N]             show recent audit metadata
  aibase install [claude|codex|gemini|cursor|opencode] [--dry-run]     wire hooks + /save into that CLI, or every one found (backs up first)

All commands accept --json (hook/MCP keep their native protocol).
Install --print-agents-md only prints generic integration instructions.

Data lives in $AIBASE_HOME (default ~/.aibase).`

const SaveGuide = `Saving to aibase — do the steps that apply, then tell the user what was saved.

1. Session log (what happened; searchable, only the latest few are auto-loaded):
   aibase log --agent <your-name> <<'EOF'
   3-8 lines: what was done, files changed, decisions and why, open TODOs.
   EOF

2. Durable memory (auto-loaded every session, size-capped) in ONE batch:
   aibase save <<'EOF'
   [{"target":"project","ops":[
     {"op":"add","text":"..."},
     {"op":"replace","old":"unique substring","text":"..."},
     {"op":"remove","old":"unique substring"}]}]
   EOF
   targets: user    = who the user is, preferences, corrections
            memory  = cross-project facts (machines, accounts, tools, quirks)
            project = this repo: layout, commands, conventions, gotchas
   Keep only what spares the next session re-discovery or the user repeating
   themselves. One fact per entry, terse. No progress logs (step 1), no code.
   Replace stale or overlapping entries rather than adding. If a store would
   exceed its limit nothing is saved and the error lists current entries:
   merge, shorten or remove in the same batch and retry.

3. Long reference material (runbooks, inventories) goes to a note, searchable
   but not auto-loaded:
   aibase note <slug> --desc "one line" <<'EOF'
   ...
   EOF`

func Run(cmd string, args []string) (string, error) {
	p := detectProject(cwd())
	switch cmd {
	case "inject":
		return snapshot(p), nil
	case "hook":
		if len(args) > 0 && args[0] == "stop" {
			agent, _ := flagVal(args[1:], "--agent")
			return stopHook(agent, os.Stdin)
		}
		return hook()
	case "save":
		raw, err := input(args)
		if err != nil {
			return "", err
		}
		batches, err := parseBatches(raw)
		if err != nil {
			return "", err
		}
		return saveBatches(batches, p)
	case "add", "rm":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: aibase %s <target> <text>", cmd)
		}
		o := op{Op: "add", Text: strings.Join(args[1:], " ")}
		if cmd == "rm" {
			o = op{Op: "remove", Old: o.Text}
		}
		return saveBatches([]batch{{Target: args[0], Ops: []op{o}}}, p)
	case "ls":
		return list(args, p)
	case "log":
		agent, args := flagVal(args, "--agent")
		session, args := flagVal(args, "--session")
		date, args := flagVal(args, "--date")
		ts := time.Now()
		if date != "" {
			t, err := time.ParseInLocation("2006-01-02", date, time.Local)
			if err != nil {
				return "", fmt.Errorf("--date wants YYYY-MM-DD: %w", err)
			}
			ts = t
		}
		text, err := textArg(args)
		if err != nil {
			return "", err
		}
		return appendLog(text, agent, session, ts, p)
	case "note":
		desc, args := flagVal(args, "--desc")
		if len(args) != 1 {
			return "", errors.New("usage: aibase note <slug> [--desc D] < body")
		}
		body, err := input(nil)
		if err != nil {
			return "", err
		}
		return writeNote(note{Slug: args[0], Desc: desc, Body: body})
	case "show":
		if len(args) != 1 {
			return "", errors.New("usage: aibase show <slug>")
		}
		return showNote(args[0])
	case "search":
		n, args := flagVal(args, "-n")
		limit, _ := strconv.Atoi(n)
		if limit <= 0 {
			limit = 8
		}
		return search(strings.Join(args, " "), limit)
	case "import":
		return importNotes(args)
	case "help", "-h", "--help":
		if len(args) > 0 && args[0] == "save" {
			return SaveGuide, nil
		}
		return usage, nil
	case "version", "--version":
		return version + " " + runtime.GOOS + "/" + runtime.GOARCH, nil
	}
	return "", fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
}

func parseBatches(raw string) ([]batch, error) {
	raw = strings.TrimSpace(raw)
	var batches []batch
	if strings.HasPrefix(raw, "{") {
		var b batch
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		batches = []batch{b}
	} else if err := json.Unmarshal([]byte(raw), &batches); err != nil {
		return nil, fmt.Errorf("invalid JSON (want [{\"target\":...,\"ops\":[...]}]): %w", err)
	}
	return batches, nil
}

func list(args []string, p *project) (string, error) {
	targets := []string{"user", "memory", "project"}
	if len(args) > 0 {
		targets = args[:1]
	}
	var b strings.Builder
	for _, t := range targets {
		s, err := storeFor(t, p)
		if err != nil {
			if len(args) > 0 {
				return "", err
			}
			continue
		}
		entries, err := s.load()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "%s  %d/%d chars  %s\n%s\n", t, size(entries), s.Limit, s.Path, listing(entries))
	}
	return b.String(), nil
}

func importNotes(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("usage: aibase import <file|dir>...")
	}
	var files []string
	for _, p := range paths {
		if st, err := os.Stat(p); err != nil {
			return "", err
		} else if st.IsDir() {
			md, _ := filepath.Glob(filepath.Join(p, "*.md"))
			for _, f := range md {
				if filepath.Base(f) != "MEMORY.md" {
					files = append(files, f)
				}
			}
		} else {
			files = append(files, p)
		}
	}
	var done []string
	for _, f := range files {
		b, err := readFile(f)
		if err != nil {
			return "", err
		}
		n := parseNote(string(b), strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
		if _, err := writeNote(n); err != nil {
			return "", fmt.Errorf("%s: %w", f, err)
		}
		done = append(done, n.Slug)
	}
	return fmt.Sprintf("imported %d notes: %s", len(done), strings.Join(done, ", ")), nil
}

func flagVal(args []string, name string) (string, []string) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], append(args[:i:i], args[i+2:]...)
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, append(args[:i:i], args[i+1:]...)
		}
	}
	return "", args
}

func textArg(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	return input(nil)
}

// input reads a file argument, or stdin when it is piped.
func input(args []string) (string, error) {
	if len(args) > 0 && args[0] != "-" {
		b, err := readFile(args[0])
		return string(b), err
	}
	if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		return "", errors.New("expected input on stdin")
	}
	b, err := io.ReadAll(os.Stdin)
	return string(b), err
}

// hook emits SessionStart additionalContext, the format shared by Claude Code and Codex.
func hook() (string, error) {
	if !InjectionEnabled() {
		return "", nil
	}
	dir := cwd()
	if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice == 0 {
		var in struct {
			Cwd string `json:"cwd"`
		}
		if b, err := io.ReadAll(os.Stdin); err == nil && json.Unmarshal(b, &in) == nil && in.Cwd != "" {
			dir = in.Cwd
		}
	}
	out, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "SessionStart",
		"additionalContext": snapshot(detectProject(dir)),
	}})
	return string(out), err
}
