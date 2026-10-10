package main

import (
	"encoding/json"
	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/project"
	"github.com/xiaoxin031/aibase/internal/skills"
	"strings"
)

func collectionText(cmd, out string) (string, bool) {
	switch cmd {
	case "projects":
		var f cache.Fact
		if json.Unmarshal([]byte(out), &f) == nil {
			return project.ProjectsText(f), true
		}
	case "skill":
		var all []skills.Skill
		if json.Unmarshal([]byte(out), &all) == nil {
			return skills.ListText(all), true
		}
		var s skills.Skill
		if json.Unmarshal([]byte(out), &s) == nil && s.Name != "" {
			return skills.Text(s), true
		}
		var messages []string
		if json.Unmarshal([]byte(out), &messages) == nil {
			return strings.Join(messages, "\n"), true
		}
		var text string
		if json.Unmarshal([]byte(out), &text) == nil {
			return text, true
		}
	}
	return "", false
}
