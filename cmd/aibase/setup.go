package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/brief"
	"github.com/xiaoxin031/aibase/internal/clients"
	"github.com/xiaoxin031/aibase/internal/doctor"
	"github.com/xiaoxin031/aibase/internal/memory"
)

const helpText = `aibase — the local context layer for every AI coding agent

Getting started
  aibase setup [--dry-run]          migrate memory, install detected clients, refresh, doctor
  aibase help [save]                show help or saving instructions
Context
  aibase context [cwd]              bounded session context
  aibase inspect host [--level basic|tools|runtime]
  aibase inspect project [cwd]
  aibase projects [query]
  aibase refresh [--level basic|tools]
Memory
  aibase ls [target] | show <name> | search <query>
  aibase save [file]                apply authorized memory batches
  aibase add <target> <text> | rm <target> <substring>
  aibase log [--agent A] [--session ID] [text]
  aibase note <slug> [--desc D] | import <file|dir>...
  aibase migrate amem
Skills
  aibase skill ls|show|new|check|sync|promote
Integration
  aibase install [client] [--dry-run]
  aibase uninstall [client] [--dry-run]
  aibase mcp | hook | inject
Maintenance
  aibase version
  aibase status [cwd] --json
  aibase stats [--json]             tokens, live sessions and memory at a glance
  aibase config [inject|autosave true|false] --json
  aibase doctor [--json]
  aibase audit [-n N]
  aibase redact scan [path]

Start with: aibase setup
Use --json for structured output.
`

type setupStep struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type setupReport struct {
	Steps  []setupStep    `json:"steps"`
	Doctor []doctor.Check `json:"doctor"`
}

func syncRefresh() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, e := brief.Refresh(ctx, "tools")
		if !errors.Is(e, brief.ErrRefreshBusy) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
func setupCommand(args []string) (string, error) {
	dry := len(args) == 1 && args[0] == "--dry-run"
	if len(args) > 0 && !dry {
		return "", fmt.Errorf("usage: aibase setup [--dry-run]")
	}
	r := setupReport{Steps: []setupStep{}}
	var failures []error
	add := func(name, detail string, e error) {
		status := "success"
		if e != nil {
			status = "failed"
			detail += " " + e.Error()
			failures = append(failures, e)
		}
		r.Steps = append(r.Steps, setupStep{name, status, strings.TrimSpace(detail)})
	}
	source := os.Getenv("AMEM_HOME")
	if source == "" {
		h, e := os.UserHomeDir()
		if e != nil {
			add("migrate", "", e)
		}
		source = filepath.Join(h, ".amem")
	}
	if _, e := os.Stat(source); e == nil {
		if dry {
			r.Steps = append(r.Steps, setupStep{"migrate", "planned", "would migrate " + source})
		} else {
			msg, e := memory.Migrate()
			add("migrate", msg, e)
		}
	} else if !os.IsNotExist(e) {
		add("migrate", "", e)
	} else {
		r.Steps = append(r.Steps, setupStep{"migrate", "skipped", "no amem source"})
	}
	for _, d := range clients.Detect() {
		if !d.Detected {
			r.Steps = append(r.Steps, setupStep{"install." + d.Name, "skipped", "not detected"})
			continue
		}
		a := []string{d.Name}
		if dry {
			a = append(a, "--dry-run")
		}
		msg, e := clients.Run(a, false)
		add("install."+d.Name, msg, e)
		if e == nil {
			if strings.Contains(msg, "Summary: success=0 skipped=") {
				r.Steps[len(r.Steps)-1].Status = "skipped"
			} else if dry {
				r.Steps[len(r.Steps)-1].Status = "planned"
			}
		}
	}
	if dry {
		r.Steps = append(r.Steps, setupStep{"refresh", "planned", "would synchronously refresh tools and skill cache"})
	} else {
		add("refresh", "tools and skill cache", syncRefresh())
	}
	r.Doctor = doctor.Run("")
	out, e := asJSON(r)
	return out, errors.Join(append(failures, e)...)
}
func setupText(s string) string {
	var r setupReport
	if json.Unmarshal([]byte(s), &r) != nil {
		return s
	}
	var b strings.Builder
	for _, v := range r.Steps {
		fmt.Fprintf(&b, "%s %s: %s\n", v.Status, v.Name, v.Detail)
	}
	b.WriteString(doctor.Text(r.Doctor))
	return b.String()
}
