package memory

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func CallTool(name string, raw json.RawMessage) (string, error) {
	var a struct {
		Cwd, Target, Summary, Agent, Query, Slug, Description, Body string
		Ops                                                         []op
		Limit                                                       int
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return "", fmt.Errorf("bad arguments: %w", err)
		}
	}
	dir := a.Cwd
	if strings.TrimSpace(dir) == "" {
		dir = cwd()
	}
	p := detectProject(dir)
	switch name {
	case "memory_load":
		return snapshot(p), nil
	case "memory_save":
		return saveBatches([]batch{{Target: a.Target, Ops: a.Ops}}, p)
	case "memory_log":
		return appendLog(a.Summary, a.Agent, "", time.Now(), p)
	case "memory_search":
		if a.Limit <= 0 {
			a.Limit = 8
		}
		return search(a.Query, a.Limit)
	case "memory_show":
		return showNote(a.Slug)
	case "memory_note":
		return writeNote(note{Slug: a.Slug, Desc: a.Description, Body: a.Body})
	}
	return "", fmt.Errorf("unknown tool %q", name)
}
