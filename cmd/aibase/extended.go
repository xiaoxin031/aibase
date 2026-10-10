package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/host"
	"github.com/xiaoxin031/aibase/internal/project"
	"io"
	"os"
	"strings"

	"github.com/xiaoxin031/aibase/internal/brief"
	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/doctor"
	"github.com/xiaoxin031/aibase/internal/facts"
	"github.com/xiaoxin031/aibase/internal/memory"

	"github.com/xiaoxin031/aibase/internal/skills"
)

func asJSON(v any) (string, error) { b, e := json.Marshal(v); return string(b), e }
func factCommand(args []string) (string, error) {
	name := ""
	a := map[string]string{}
	switch args[0] {
	case "context", "doctor":
		name = args[0]
		if len(args) > 2 {
			return "", fmt.Errorf("usage: aibase %s [cwd]", name)
		}
		if len(args) == 2 {
			a["cwd"] = args[1]
		}
	case "projects":
		name = "projects_find"
		a["query"] = strings.Join(args[1:], " ")
	case "inspect":
		if len(args) < 2 {
			return "", fmt.Errorf("inspect host|project required")
		}
		switch args[1] {
		case "host":
			name = "host_inspect"
			rest := args[2:]
			if len(rest) > 0 && rest[0] == "--level" {
				if len(rest) != 2 {
					return "", fmt.Errorf("--level requires basic, tools or runtime")
				}
				rest = rest[1:]
			}
			if len(rest) > 1 {
				return "", fmt.Errorf("usage: inspect host [--level basic|tools|runtime]")
			}
			if len(rest) == 1 {
				a["level"] = strings.TrimPrefix(rest[0], "--level=")
				if a["level"] == "" {
					return "", fmt.Errorf("--level must not be empty")
				}
			}
		case "project":
			name = "project_inspect"
			if len(args) > 3 {
				return "", fmt.Errorf("usage: inspect project [cwd]")
			}
			if len(args) == 3 {
				a["cwd"] = args[2]
			}
		default:
			return "", fmt.Errorf("unknown inspect target")
		}
	}
	b, _ := json.Marshal(a)
	v, e := facts.Call(name, b)
	if e != nil {
		return "", e
	}
	return asJSON(v)
}
func skillCommand(args []string) (string, error) {
	draft := false
	rest := []string{}
	for _, a := range args {
		if a == "--draft" {
			draft = true
		} else {
			rest = append(rest, a)
		}
	}
	args = rest
	if len(args) == 0 {
		return "", fmt.Errorf("skill ls|show|new|check|sync|promote required")
	}
	var v any
	var e error
	switch args[0] {
	case "ls", "check":
		if len(args) > 2 {
			return "", fmt.Errorf("too many arguments")
		}
		if len(args) == 2 {
			v, e = skills.Show(args[1], draft)
		} else if draft {
			v, e = skills.List(true)
		} else {
			v, e = brief.RefreshSkills()
		}
	case "show", "new", "promote":
		if len(args) != 2 {
			return "", fmt.Errorf("skill name required")
		}
		switch args[0] {
		case "show":
			v, e = skills.Show(args[1], draft)
		case "new":
			v, e = skills.New(args[1], draft)
		case "promote":
			v, e = skills.Promote(args[1])
		}
	case "sync":
		if len(args) != 1 || draft {
			return "", fmt.Errorf("usage: skill sync")
		}
		v, e = skills.Sync()
	default:
		return "", fmt.Errorf("unknown skill command")
	}
	if e != nil {
		return "", e
	}
	return asJSON(cache.Clean(v))
}
func sessionHook(args []string, counts ...*int) (string, error) {
	if !memory.InjectionEnabled() {
		return "", nil
	}
	agent := ""
	for i, a := range args {
		if a == "--agent" && i+1 < len(args) {
			agent = args[i+1]
		}
	}
	var in struct {
		Cwd   string   `json:"cwd"`
		Roots []string `json:"workspace_roots"`
	}
	if st, e := os.Stdin.Stat(); e == nil && st.Mode()&os.ModeCharDevice == 0 {
		b, e := io.ReadAll(io.LimitReader(os.Stdin, (1<<20)+1))
		if e != nil {
			return "", e
		}
		if len(b) > 1<<20 {
			return "", fmt.Errorf("hook input too large")
		}
		if len(b) > 0 && json.Unmarshal(b, &in) != nil {
			return "", fmt.Errorf("invalid hook JSON")
		}
	}
	if in.Cwd == "" && len(in.Roots) > 0 {
		in.Cwd = in.Roots[0]
	}
	r := brief.Build(in.Cwd)
	if len(counts) > 0 {
		*counts[0] = r.Redactions
	}
	if agent == "cursor" || agent == "cursor-agent" {
		return asJSON(map[string]string{"additional_context": r.Text})
	}
	return asJSON(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "SessionStart", "additionalContext": r.Text}})
}
func humanFacts(cmd, out string) string {
	if text, ok := collectionText(cmd, out); ok {
		return text
	}
	if cmd == "setup" {
		return setupText(out)
	}
	if cmd == "inspect" || cmd == "refresh" {
		var r host.Report
		if json.Unmarshal([]byte(out), &r) == nil {
			if _, ok := r["os"]; ok {
				return host.Text(r)
			}
			if _, ok := r["root"]; ok {
				return project.Text(project.Report(r))
			}
		}
	}

	if cmd == "context" {
		var r brief.Report
		if json.Unmarshal([]byte(out), &r) == nil {
			return r.Text
		}
	}
	if cmd == "doctor" {
		var c []doctor.Check
		if json.Unmarshal([]byte(out), &c) == nil {
			return doctor.Text(c)
		}
	}
	return out
}
func briefText() (string, error) { return brief.Build("").Text, nil }

func refreshCommand(args []string) (string, error) {
	level := "tools"
	if len(args) == 2 && args[0] == "--level" {
		level = args[1]
	} else if len(args) == 1 && strings.HasPrefix(args[0], "--level=") {
		level = strings.TrimPrefix(args[0], "--level=")
	} else if len(args) != 0 {
		return "", fmt.Errorf("usage: aibase refresh [--level basic|tools]")
	}
	r, e := brief.Refresh(context.Background(), level)
	if e != nil {
		return "", e
	}
	return asJSON(r)
}
