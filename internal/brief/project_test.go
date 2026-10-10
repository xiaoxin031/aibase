package brief

import (
	"testing"

	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/project"
)

func TestProjectSummary(t *testing.T) {
	r := project.Report{
		"git.branch":       {Value: "main", Status: cache.OK},
		"git.dirty_count":  {Value: float64(3), Status: cache.OK},
		"git.ahead_behind": {Value: map[string]any{"ahead": float64(1), "behind": float64(0)}, Status: cache.OK},
		"git.commits":      {Value: []any{"add invoice v2 client", "init"}, Status: cache.OK},
		"Makefile":         {Value: map[string]any{"targets": []any{"build", "test"}}, Status: cache.OK},
		"package.json":     {Value: map[string]any{"scripts": map[string]any{"test": "vitest", "dev": "vite"}}, Status: cache.OK},
	}
	want := "branch main, 3 uncommitted, ahead 1 behind 0; last commit: add invoice v2 client; make: build test; npm: dev test"
	if got := ProjectSummary(r); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	if got := ProjectSummary(project.Report{"git.ahead_behind": {Value: "no upstream", Status: cache.OK}}); got != "" {
		t.Fatalf("empty report summary = %q", got)
	}
}
