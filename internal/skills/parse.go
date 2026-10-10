package skills

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var nameRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func ValidName(s string) bool { return len(s) <= 64 && nameRE.MatchString(s) && !reserved(s) }
func reserved(s string) bool {
	s = strings.ToUpper(s)
	return s == "CON" || s == "PRN" || s == "AUX" || s == "NUL" || regexp.MustCompile(`^(COM|LPT)[0-9]$`).MatchString(s)
}

type Skill struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata"`
	Path        string            `json:"path,omitempty"`
	Draft       bool              `json:"unverified"`
	Missing     []string          `json:"missing_dependencies"`
	Error       string            `json:"error,omitempty"`
	Body        string            `json:"body,omitempty"`
}

// scalar reads the string/list subset without evaluating YAML tags or aliases.
func scalar(s string) (string, error) {
	s = strings.TrimSpace(yamlComment(s))
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		var values []string
		start := 1
		var quote byte
		for j := 1; j < len(s); j++ {
			c := s[j]
			if quote != 0 {
				if c == quote && (j == 0 || s[j-1] != '\\') {
					quote = 0
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				continue
			}
			if c == ',' || j == len(s)-1 {
				v, _ := scalar(s[start:j])
				if v != "" {
					values = append(values, v)
				}
				start = j + 1
			}
		}
		return strings.Join(values, ","), nil
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if v, e := strconv.Unquote(s); e == nil {
			return v, nil
		}
		return s[1 : len(s)-1], nil
	}
	return strings.TrimSpace(s), nil
}
func yamlComment(s string) string {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if quote == '"' && c == '\\' {
				i++
				continue
			}
			if c == quote {
				if quote == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if (c == '\'' || c == '"') && (i == 0 || strings.ContainsRune(" [,\t", rune(s[i-1]))) {
			quote = c
		}
		if c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
			return s[:i]
		}
	}
	return s
}

func Parse(b []byte) (Skill, error) {
	s := Skill{Metadata: map[string]string{}, Missing: []string{}}
	if len(b) > MaxSize || !utf8.Valid(b) {
		return s, fmt.Errorf("skill exceeds size limit or is not UTF-8")
	}
	lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
	if len(lines) < 3 || lines[0] != "---" {
		return s, fmt.Errorf("missing YAML frontmatter")
	}
	type level struct {
		indent int
		key    string
	}
	var stack []level
	meta := false
	end := -1
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if line == "---" {
			end = i
			break
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		ind := len(line) - len(strings.TrimLeft(line, " "))
		listItem := strings.HasPrefix(strings.TrimSpace(line), "- ")
		for len(stack) > 0 && (ind < stack[len(stack)-1].indent || (ind == stack[len(stack)-1].indent && !listItem)) {
			stack = stack[:len(stack)-1]
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") && meta && len(stack) > 0 {
			parts := []string{}
			for _, l := range stack {
				parts = append(parts, l.key)
			}
			key := strings.Join(parts, ".")
			value, _ := scalar(strings.TrimSpace(trimmed[2:]))
			if s.Metadata[key] != "" {
				s.Metadata[key] += ","
			}
			s.Metadata[key] += value
			continue
		}
		k, v, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		k, _ = scalar(k)
		v = strings.TrimSpace(v)
		if ind == 0 {
			meta = false
			stack = nil
		}
		if ind == 0 && k == "metadata" {
			meta = true
			continue
		}
		if ind == 0 && k != "name" && k != "description" {
			continue
		}
		if ind > 0 && !meta {
			continue
		}
		parts := []string{}
		for _, l := range stack {
			parts = append(parts, l.key)
		}
		parts = append(parts, k)
		key := strings.Join(parts, ".")
		if ind > 0 && v == "" {
			stack = append(stack, level{ind, k})
			continue
		}
		var value string
		var err error
		if v == "|" || v == "|-" || v == "|+" || v == ">" || v == ">-" || v == ">+" {
			var block []string
			pad := -1
			for i+1 < len(lines) {
				next := lines[i+1]
				n := len(next) - len(strings.TrimLeft(next, " "))
				if strings.TrimSpace(next) != "" && n <= ind {
					break
				}
				i++
				if pad < 0 && strings.TrimSpace(next) != "" {
					pad = n
				}
				if strings.TrimSpace(next) == "" {
					block = append(block, "")
					continue
				}
				if n < pad {
					return s, fmt.Errorf("invalid block indentation")
				}
				block = append(block, next[pad:])
			}
			value = blockString(block, v)
		} else {
			value, err = scalar(v)
		}
		if err != nil {
			return s, fmt.Errorf("%s: %w", key, err)
		}
		if ind > 0 {
			s.Metadata[key] = value
		} else {
			switch k {
			case "name":
				s.Name = value
			case "description":
				s.Description = value

			}
		}
	}
	if end < 0 {
		return s, fmt.Errorf("unterminated frontmatter")
	}
	if strings.TrimSpace(s.Name) == "" || strings.TrimSpace(s.Description) == "" {
		return s, fmt.Errorf("name and description required")
	}
	s.Body = strings.Join(lines[end+1:], "\n")
	return s, nil
}

func blockString(lines []string, style string) string {
	var b strings.Builder
	for i, line := range lines {
		b.WriteString(line)
		if i == len(lines)-1 {
			b.WriteByte('\n')
			continue
		}
		next := lines[i+1]
		if style[0] == '|' || strings.HasPrefix(line, " ") || strings.HasPrefix(next, " ") {
			b.WriteByte('\n')
		} else if line != "" && next != "" {
			b.WriteByte(' ')
		} else if line != "" || next == "" || i == 0 {
			b.WriteByte('\n')
		}
	}
	value := b.String()
	if strings.HasSuffix(style, "+") {
		return value
	}
	value = strings.TrimRight(value, "\n")
	if !strings.HasSuffix(style, "-") && len(lines) > 0 {
		value += "\n"
	}
	return value
}
