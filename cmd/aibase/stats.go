package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xiaoxin031/aibase/internal/activity"
	"github.com/xiaoxin031/aibase/internal/audit"
	"github.com/xiaoxin031/aibase/internal/clients"
	"github.com/xiaoxin031/aibase/internal/memory"
)

type statsReport struct {
	Version  string              `json:"version"`
	DataDir  string              `json:"data_dir"`
	Config   memory.Settings     `json:"config"`
	Activity activity.Report     `json:"activity"`
	Today    map[string]int      `json:"today"`
	Memory   []memory.UsageItem  `json:"memory"`
	Notes    int                 `json:"notes"`
	Logs     int                 `json:"logs"`
	Recent   any                 `json:"recent"`
	Clients  []clients.Detection `json:"clients"`
}

// statsCommand is the dashboard feed: activity from client transcripts plus
// memory state, without doctor's slower probes.
func statsCommand(args []string) (string, error) {
	if len(args) > 0 {
		return "", fmt.Errorf("usage: aibase stats [--json]")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cfg, err := memory.ReadSettings()
	if err != nil {
		return "", err
	}
	recent, err := memory.RecentSessions()
	if err != nil {
		return "", err
	}
	now := time.Now()
	r := statsReport{
		Version:  memory.Version,
		DataDir:  memory.Home(),
		Config:   cfg,
		Activity: activity.Collect(home, filepath.Join(memory.Home(), "state", "activity-index.json"), now),
		Today:    todayCounts(now),
		Memory:   memory.Usage(home),
		Recent:   recent,
		Clients:  clients.Detect(),
	}
	r.Notes, r.Logs = memory.ArchiveCounts()
	return asJSON(r)
}

// todayCounts tallies today's audit entries: hook-started sessions, auto-save
// reviews requested, and memory writes.
func todayCounts(now time.Time) map[string]int {
	out := map[string]int{"sessions": 0, "reviews": 0, "saves": 0}
	rows, err := audit.Read(memory.Home(), 10000)
	if err != nil {
		return out
	}
	today := now.Format("2006-01-02")
	for _, row := range strings.Split(rows, "\n") {
		var e audit.Entry
		if json.Unmarshal([]byte(row), &e) != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, e.TS)
		if err != nil || t.Local().Format("2006-01-02") != today {
			continue
		}
		switch {
		case e.Action == "SessionStart" && e.Client != "mcp":
			out["sessions"]++
		case e.Action == "Stop" && e.Bytes > 0:
			out["reviews"]++
		case memoryWrite(e.Action):
			out["saves"]++
		}
	}
	return out
}

func memoryWrite(cmd string) bool {
	switch cmd {
	case "save", "add", "rm", "note", "log", "memory_save", "memory_log", "memory_note":
		return true
	}
	return false
}

func statsText(out string) string {
	var r statsReport
	if json.Unmarshal([]byte(out), &r) != nil {
		return out
	}
	var b strings.Builder
	fmt.Fprintf(&b, "aibase %s\n\nTokens today: %s", r.Version, humanCount(r.Activity.Total))
	names := make([]string, 0, len(r.Activity.Today))
	for c := range r.Activity.Today {
		names = append(names, c)
	}
	sort.Strings(names)
	for i, c := range names {
		sep := " ("
		if i > 0 {
			sep = ", "
		}
		fmt.Fprintf(&b, "%s%s %s", sep, c, humanCount(r.Activity.Today[c].Total()))
	}
	if len(names) > 0 {
		b.WriteString(")")
	}
	b.WriteString("\nLast 7 days:")
	for _, d := range r.Activity.Days {
		fmt.Fprintf(&b, " %s", humanCount(d.Total))
	}
	fmt.Fprintf(&b, "\nActive sessions (%s): %d\n", activity.ActiveFor, len(r.Activity.Active))
	for _, s := range r.Activity.Active {
		fmt.Fprintf(&b, "  %s · %s · %s ago\n", s.Client, s.Project, time.Since(s.Updated).Round(time.Second))
	}
	fmt.Fprintf(&b, "Today: %d sessions, %d auto-save reviews, %d memory writes\n", r.Today["sessions"], r.Today["reviews"], r.Today["saves"])
	for _, u := range r.Memory {
		fmt.Fprintf(&b, "Memory %s: %d/%d\n", u.Target, u.Used, u.Limit)
	}
	fmt.Fprintf(&b, "Archive: %d notes, %d session logs\n", r.Notes, r.Logs)
	return b.String()
}

func humanCount(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}
