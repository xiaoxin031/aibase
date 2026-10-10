package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const recentLogs = 3

// snapshot is the bounded context injected at session start. Its size is capped
// by the store limits plus a few recent logs, no matter how much history grows.
func snapshot(p *project) string {
	var b strings.Builder
	b.WriteString("<aibase>\nPersistent memory shared by all your AI tools (snapshot at session start).\n")
	for _, t := range []string{"user", "memory", "project"} {
		s, err := storeFor(t, p)
		if err != nil {
			continue
		}
		entries, err := s.load()
		if err != nil || len(entries) == 0 {
			continue
		}
		title := map[string]string{"user": "USER — who the user is", "memory": "MEMORY — global notes", "project": "PROJECT — " + projectRoot(p)}[t]
		fmt.Fprintf(&b, "\n## %s [%d/%d]\n", title, size(entries), s.Limit)
		for _, e := range entries {
			fmt.Fprintf(&b, "- %s\n", strings.ReplaceAll(e, "\n", "\n  "))
		}
	}
	recent, logCount := recentHistory(p)
	if len(recent) > 0 {
		b.WriteString("\n## Recent sessions here\n")
		for _, e := range recent {
			fmt.Fprintf(&b, "- %s: %s\n", e.TS[:min(10, len(e.TS))], truncate(strings.Join(strings.Fields(e.Text), " "), 300))
		}
	}
	notes, _ := filepath.Glob(filepath.Join(notesDir(), "*.md"))
	fmt.Fprintf(&b, "\nArchive (not loaded): %d notes, %d session logs. Before re-exploring or asking the user, run `aibase search <words>`; read a note with `aibase show <slug>`.\n", len(notes), logCount)
	b.WriteString("To remember something durable, or when the user asks to save: `aibase help save`.\n</aibase>\n")
	return b.String()
}

func projectRoot(p *project) string {
	if p == nil {
		return ""
	}
	return p.Root
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func cwd() string {
	d, _ := os.Getwd()
	return d
}
