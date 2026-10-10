package brief

import (
	"fmt"
	"slices"
	"strings"

	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/memory"
	"github.com/xiaoxin031/aibase/internal/project"
)

func ProjectKey(root string) string { return "brief:project:" + root }

// ProjectSummary renders the cheap, high-value parts of a project report in one
// line. Fact values are JSON-normalized: numbers are float64, lists []any.
func ProjectSummary(r project.Report) string {
	var parts []string
	if f := r["git.branch"]; f.Status == cache.OK && f.Value != nil && f.Value != "" {
		s := "branch " + fmt.Sprint(f.Value)
		if d := num(r["git.dirty_count"].Value); d > 0 {
			s += fmt.Sprintf(", %d uncommitted", d)
		}
		if ab, ok := r["git.ahead_behind"].Value.(map[string]any); ok && num(ab["ahead"])+num(ab["behind"]) > 0 {
			s += fmt.Sprintf(", ahead %d behind %d", num(ab["ahead"]), num(ab["behind"]))
		}
		parts = append(parts, s)
	}
	if c := strs(r["git.commits"].Value); len(c) > 0 {
		parts = append(parts, "last commit: "+memory.Truncate(c[0], 80))
	}
	if m, ok := r["Makefile"].Value.(map[string]any); ok {
		if t := strs(m["targets"]); len(t) > 0 {
			parts = append(parts, "make: "+strings.Join(t[:min(len(t), 12)], " "))
		}
	}
	if m, ok := r["package.json"].Value.(map[string]any); ok {
		if s, ok := m["scripts"].(map[string]any); ok && len(s) > 0 {
			names := make([]string, 0, len(s))
			for k := range s {
				names = append(names, k)
			}
			slices.Sort(names)
			parts = append(parts, "npm: "+strings.Join(names[:min(len(names), 12)], " "))
		}
	}
	return strings.Join(parts, "; ")
}

func num(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	}
	return 0
}

func strs(v any) []string {
	var out []string
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}
