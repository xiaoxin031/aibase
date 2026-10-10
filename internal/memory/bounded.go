package memory

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/xiaoxin031/aibase/internal/fileio"
	"github.com/xiaoxin031/aibase/internal/redact"
	"github.com/xiaoxin031/aibase/internal/skills"
)

// BriefSnapshot never counts the archive or enumerates notes. Only a bounded
// history tail is inspected; omitted older records remain available via search.
func BriefSnapshot(dir string) string {
	p := detectProject(dir)
	var out strings.Builder
	for _, u := range Usage(dir) {
		if u.Error != "" {
			msg, _ := redact.Text(u.Error)
			fmt.Fprintf(&out, "%s: unavailable (%s)\n", u.Target, msg)
			continue
		}
		b, e := skills.Read(u.Path)
		if e == nil && len(b) > 0 {
			fmt.Fprintf(&out, "%s [%d/%d]\n%s\n", strings.ToUpper(u.Target), u.Used, u.Limit, string(b))
		}
	}
	if f, e := openRegular(historyPath()); e == nil {
		defer f.Close()
		if st, e := f.Stat(); e == nil && st.Mode().IsRegular() {
			start := max(int64(0), st.Size()-(64<<10))
			b := make([]byte, st.Size()-start)
			_, e = f.ReadAt(b, start)
			if e == nil || e == io.EOF {
				lines := strings.Split(string(b), "\n")
				n := 0
				for i := len(lines) - 1; i >= 0 && n < 3; i-- {
					if i == 0 && start > 0 {
						break
					}
					var entry logEntry
					if json.Unmarshal([]byte(lines[i]), &entry) != nil || entry.Text == "" {
						continue
					}
					if p != nil && entry.Project != p.ID {
						continue
					}
					fmt.Fprintf(&out, "Recent: %s %s\n", entry.TS, entry.Text)
					n++
				}
			}
		}
	}
	out.WriteString("Older history and notes: aibase search <words>; aibase show <slug>.\n")
	return out.String()
}

type UsageItem struct {
	Target string `json:"target"`
	Path   string `json:"path"`
	Used   int    `json:"used"`
	Limit  int    `json:"limit"`
	Error  string `json:"error,omitempty"`
}

func Usage(dir string) []UsageItem {
	p := detectProject(dir)
	l := map[string]int{"user": 1200, "memory": 2500, "project": 2500}
	var cfg struct {
		Limits map[string]int `json:"limits"`
	}
	if b, e := skills.Read(filepath.Join(home(), "config.json")); e == nil && json.Unmarshal(b, &cfg) == nil {
		for k, v := range cfg.Limits {
			if v > 0 {
				l[k] = v
			}
		}
	}
	out := []UsageItem{}
	for _, t := range []string{"user", "memory", "project"} {
		path := filepath.Join(home(), strings.ToUpper(t)+".md")
		if t == "project" {
			if p == nil {
				continue
			}
			path = filepath.Join(home(), "projects", p.ID, "MEMORY.md")
		}
		u := UsageItem{Target: t, Path: path, Limit: l[t]}
		b, e := skills.Read(path)
		if e != nil && !os.IsNotExist(e) {
			u.Error = e.Error()
		}
		u.Used = utf8.RuneCountInString(strings.Join(parseEntries(string(b)), entrySep))
		out = append(out, u)
	}
	return out
}

func openRegular(path string) (*os.File, error) {
	if e := skills.Guard(path); e != nil {
		return nil, e
	}
	st, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	return fileio.OpenRead(path)
}
