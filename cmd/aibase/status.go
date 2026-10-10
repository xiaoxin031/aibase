package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/xiaoxin031/aibase/internal/audit"
	"github.com/xiaoxin031/aibase/internal/clients"
	"github.com/xiaoxin031/aibase/internal/doctor"
	"github.com/xiaoxin031/aibase/internal/memory"
)

func configCommand(args []string) (string, error) {
	if len(args) == 0 {
		v, e := memory.ReadSettings()
		if e != nil {
			return "", e
		}
		return asJSON(v)
	}
	if len(args) != 2 {
		return "", fmt.Errorf("usage: aibase config [inject|autosave true|false] --json")
	}
	enabled, e := strconv.ParseBool(args[1])
	if e != nil {
		return "", fmt.Errorf("value must be true or false")
	}
	v, e := memory.SetSetting(args[0], enabled)
	if e != nil {
		return "", e
	}
	return asJSON(v)
}
func statusCommand(args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("usage: aibase status [cwd] --json")
	}
	dir, _ := os.Getwd()
	if len(args) == 1 {
		dir = args[0]
	}
	cfg, e := memory.ReadSettings()
	if e != nil {
		return "", e
	}
	sessions, e := memory.RecentSessions()
	if e != nil {
		return "", e
	}
	entries, e := audit.Read(memory.Home(), 10000)
	if e != nil {
		return "", e
	}
	var latest *audit.Entry
	rows := strings.Split(entries, "\n")
	for j := len(rows) - 1; j >= 0; j-- {
		var v audit.Entry
		if json.Unmarshal([]byte(rows[j]), &v) == nil && v.Action == "SessionStart" && v.Bytes > 0 {
			latest = &v
			break
		}
	}
	checks := doctor.Quick(dir)
	counts := map[string]int{"ok": 0, "warn": 0, "fail": 0}
	for _, c := range checks {
		counts[c.Status]++
	}
	return asJSON(map[string]any{"version": memory.Version, "platform": runtime.GOOS + "/" + runtime.GOARCH, "data_dir": memory.Home(), "config": cfg, "clients": clients.Detect(), "memory": memory.Usage(dir), "sessions": sessions, "last_injection": latest, "doctor": map[string]any{"scope": "quick", "counts": counts, "checks": checks}, "sessions_scope": "latest 5 in last 1 MiB of history", "injection_scope": "retained audit log"})
}
