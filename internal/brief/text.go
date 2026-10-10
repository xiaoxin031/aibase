package brief

import (
	"fmt"
	"sort"
	"strings"
)

func summaryText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.Join(strings.Fields(x), " ")
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			if s := summaryText(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "; ")
	case map[string]any:
		if status, ok := x["status"].(string); ok {
			if status == "missing" {
				return ""
			}
			if value, exists := x["value"]; exists {
				if status != "ok" {
					return status
				}
				return summaryText(value)
			}
		}
		if name, ok := x["name"].(string); ok {
			if description, ok := x["description"]; ok {
				return name + ": " + summaryText(description)
			}
			if detail, ok := x["detail"]; ok {
				s := name + ": " + summaryText(detail)
				if fix, ok := x["fix"].(string); ok && fix != "" {
					s += " → " + fix
				}
				return s
			}
		}
		if skills, ok := x["skills"]; ok {
			s := summaryText(skills)
			if s == "" {
				s = "none"
			}
			if x["truncated"] == true {
				s += "; … aibase skill ls"
			}
			return s
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			if s := summaryText(x[k]); s != "" {
				parts = append(parts, k+": "+s)
			}
		}
		return strings.Join(parts, "; ")
	default:
		return fmt.Sprint(x)
	}
}
