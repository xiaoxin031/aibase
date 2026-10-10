package brief

import (
	"strings"
	"testing"
)

func TestCompactCachedSummaries(t *testing.T) {
	cases := []struct {
		value        any
		want, absent string
	}{
		{map[string]any{"git.branch": map[string]any{"value": "main", "status": "ok"}, "readme": map[string]any{"value": nil, "status": "missing"}}, "git.branch: main", "readme"},
		{map[string]any{"skills": []any{map[string]any{"name": "example", "description": "A useful\nskill"}}, "total": 1, "truncated": false}, "example: A useful skill", "description"},
		{[]any{map[string]any{"name": "path", "detail": "not found", "fix": "aibase setup", "status": "fail"}}, "path: not found → aibase setup", "\""},
	}
	for _, tc := range cases {
		got := summaryText(tc.value)
		if !strings.Contains(got, tc.want) || strings.Contains(got, tc.absent) {
			t.Fatal(got)
		}
	}
}
