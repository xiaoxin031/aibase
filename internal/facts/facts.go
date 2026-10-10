// Package facts is the read-only CLI/MCP dispatch boundary.
package facts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaoxin031/aibase/internal/brief"
	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/doctor"
	"github.com/xiaoxin031/aibase/internal/host"
	"github.com/xiaoxin031/aibase/internal/project"
	"github.com/xiaoxin031/aibase/internal/skills"
)

var Inputs = map[string][]string{"context": {"cwd"}, "host_inspect": {"level"}, "project_inspect": {"cwd"}, "projects_find": {"query"}, "skills_list": {}, "skill_show": {"name"}, "doctor": {"cwd"}}

func Call(name string, raw json.RawMessage) (any, error) {
	if len(raw) > 64<<10 {
		return nil, fmt.Errorf("fact arguments exceed 64 KiB")
	}
	allowed, ok := Inputs[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %s", name)
	}
	args := map[string]string{}
	var fields map[string]json.RawMessage
	var argsValue string
	if len(raw) > 0 {
		if string(raw) == "null" {
			return nil, fmt.Errorf("arguments must be an object")
		}
		if e := json.Unmarshal(raw, &fields); e != nil {
			return nil, e
		}
		for k, v := range fields {
			if len(v) == 0 || v[0] != '"' {
				return nil, fmt.Errorf("argument %s must be a string", k)
			}
			if e := json.Unmarshal(v, &argsValue); e != nil {
				return nil, e
			}
			args[k] = argsValue
		}
	}
	for k := range args {
		found := false
		for _, a := range allowed {
			found = found || a == k
		}
		if !found {
			return nil, fmt.Errorf("unknown argument %s", k)
		}
	}
	dir := args["cwd"]
	if dir == "" {
		dir, _ = os.Getwd()
	}
	var v any
	var e error
	switch name {
	case "context":
		return brief.Build(dir), nil
	case "host_inspect":
		level := args["level"]
		if level == "" {
			level = "basic"
		}
		v, e = host.New().InspectLevel(context.Background(), level)
	case "project_inspect":
		report := project.New().Inspect(context.Background(), dir)
		v = report
		if root, _ := report["root"].Value.(string); root != "" {
			_ = cache.NewStore("").Put(brief.ProjectKey(root), cache.New(brief.ProjectSummary(report), "inspect project", 10*time.Minute, cache.OK))
		}
	case "projects_find":
		v = project.New().Projects(context.Background(), args["query"])
	case "skills_list":
		v, e = brief.RefreshSkills()
	case "skill_show":
		if args["name"] == "" {
			return nil, fmt.Errorf("name required")
		}
		v, e = skills.Show(args["name"], false)
	case "doctor":
		checks := doctor.Run(dir)
		v = checks
		issues := []doctor.Check{}
		for _, c := range checks {
			if c.Status != "ok" && len(issues) < 32 {
				issues = append(issues, c)
			}
		}
		abs, _ := filepath.Abs(dir)
		_ = cache.NewStore("").Put("brief:doctor:"+abs, cache.New(issues, "doctor", time.Minute, cache.OK))
	}
	return cache.Clean(v), e
}
