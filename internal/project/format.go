package project

import (
	"encoding/json"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/cache"
	"sort"
	"strings"
)

func Text(r Report) string {
	keys := make([]string, 0, len(r))
	for k, f := range r {
		if f.Status != cache.Missing {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		f := r[k]
		if f.Status != cache.OK {
			fmt.Fprintf(&b, "%s: %s\n", k, f.Status)
			continue
		}
		if s, ok := f.Value.(string); ok {
			fmt.Fprintf(&b, "%s: %s\n", k, s)
		} else {
			fmt.Fprintf(&b, "%s: %s\n", k, compact(f.Value))
		}
	}
	return strings.TrimSpace(b.String())
}

func compact(v any) string {
	switch x := v.(type) {
	case nil:
		return "none"
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, compact(item))
		}
		if len(parts) == 0 {
			return "none"
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+compact(x[k]))
		}
		if len(parts) == 0 {
			return "none"
		}
		return strings.Join(parts, "; ")
	default:
		return fmt.Sprint(x)
	}
}

func ProjectsText(f cache.Fact) string {
	b, _ := json.Marshal(f.Value)
	var index indexValue
	if json.Unmarshal(b, &index) != nil {
		return "Project index: " + string(f.Status)
	}
	lines := make([]string, 0, len(index.Projects)+1)
	for _, e := range index.Projects {
		lines = append(lines, fmt.Sprint(e.Name.Value)+": "+fmt.Sprint(e.Path.Value))
	}
	if len(lines) == 0 {
		lines = append(lines, "No matching projects.")
	}
	if index.Truncated {
		lines = append(lines, "Partial index; time budget reached. Run again to rescan.")
	}
	return strings.Join(lines, "\n")
}
