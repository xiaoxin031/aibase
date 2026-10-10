package skills

import "strings"

func Text(s Skill) string {
	description := strings.Join(strings.Fields(s.Description), " ")
	if len([]rune(description)) > 160 {
		description = string([]rune(description)[:159]) + "…"
	}
	line := s.Name + ": " + description
	if s.Draft {
		line += " [unverified]"
	}
	if s.Error != "" {
		line += " [error: " + s.Error + "]"
	}
	if len(s.Missing) > 0 {
		line += " [missing: " + strings.Join(s.Missing, ", ") + "]"
	}
	if s.Body != "" {
		line += "\n\n" + strings.TrimSpace(s.Body)
	}
	return line
}

func ListText(all []Skill) string {
	if len(all) == 0 {
		return "No skills found."
	}
	lines := make([]string, 0, len(all))
	for _, s := range all {
		s.Body = ""
		lines = append(lines, Text(s))
	}
	return strings.Join(lines, "\n")
}
