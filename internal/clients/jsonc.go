package clients

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/xiaoxin031/aibase/internal/skills"
)

type jsonNode struct {
	start, end int
	keyStart   int
	members    map[string]*jsonNode
	value      any
}
type jsonParser struct {
	text string
	pos  int
}

func (p *jsonParser) space() error {
	for p.pos < len(p.text) {
		if strings.ContainsRune(" \r\n\t", rune(p.text[p.pos])) {
			p.pos++
			continue
		}
		if strings.HasPrefix(p.text[p.pos:], "//") {
			for p.pos < len(p.text) && p.text[p.pos] != '\n' {
				p.pos++
			}
			continue
		}
		if strings.HasPrefix(p.text[p.pos:], "/*") {
			i := strings.Index(p.text[p.pos+2:], "*/")
			if i < 0 {
				return fmt.Errorf("unterminated comment")
			}
			p.pos += i + 4
			continue
		}
		break
	}
	return nil
}
func (p *jsonParser) node(depth int) (*jsonNode, error) {
	if depth > 128 {
		return nil, fmt.Errorf("JSONC nesting too deep")
	}
	if e := p.space(); e != nil {
		return nil, e
	}
	if p.pos >= len(p.text) {
		return nil, fmt.Errorf("unexpected end")
	}
	n := &jsonNode{start: p.pos}
	ch := p.text[p.pos]
	if ch == '{' || ch == '[' {
		p.pos++
		close := byte('}')
		var array []any
		if ch == '{' {
			n.members = map[string]*jsonNode{}
		} else {
			close = ']'
			array = []any{}
		}
		for {
			if e := p.space(); e != nil {
				return nil, e
			}
			if p.pos >= len(p.text) {
				return nil, fmt.Errorf("unclosed container")
			}
			if p.text[p.pos] == close {
				p.pos++
				break
			}
			key := ""
			keyStart := p.pos
			if ch == '{' {
				k, e := p.node(depth + 1)
				if e != nil {
					return nil, e
				}
				var ok bool
				key, ok = k.value.(string)
				if !ok {
					return nil, fmt.Errorf("object key must be string")
				}
				if _, ok := n.members[key]; ok {
					return nil, fmt.Errorf("duplicate key %q", key)
				}
				if e = p.space(); e != nil {
					return nil, e
				}
				if p.pos >= len(p.text) || p.text[p.pos] != ':' {
					return nil, fmt.Errorf("missing colon")
				}
				p.pos++
			}
			v, e := p.node(depth + 1)
			if e != nil {
				return nil, e
			}
			if ch == '{' {
				v.keyStart = keyStart
				n.members[key] = v
			} else {
				array = append(array, v.value)
			}
			if e = p.space(); e != nil {
				return nil, e
			}
			if p.pos >= len(p.text) {
				return nil, fmt.Errorf("unclosed container")
			}
			if p.text[p.pos] == close {
				p.pos++
				break
			}
			if p.text[p.pos] != ',' {
				return nil, fmt.Errorf("missing comma")
			}
			p.pos++
		}
		if ch == '{' {
			m := map[string]any{}
			for k, v := range n.members {
				m[k] = v.value
			}
			n.value = m
		} else {
			n.value = array
		}
	} else {
		if ch == '"' {
			p.pos++
			closed := false
			for p.pos < len(p.text) {
				c := p.text[p.pos]
				p.pos++
				if c == '\\' {
					p.pos++
				} else if c == '"' {
					closed = true
					break
				}
			}
			if !closed || p.pos > len(p.text) {
				return nil, fmt.Errorf("unclosed string")
			}
		} else {
			for p.pos < len(p.text) && !strings.ContainsRune(" \r\n\t,}]/", rune(p.text[p.pos])) {
				p.pos++
			}
		}
		if p.pos == n.start || json.Unmarshal([]byte(p.text[n.start:p.pos]), &n.value) != nil {
			return nil, fmt.Errorf("invalid JSONC value")
		}
	}
	n.end = p.pos
	return n, nil
}
func parseJSONC(s string) (*jsonNode, error) {
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("JSONC must be UTF-8")
	}
	p := jsonParser{text: s}
	n, e := p.node(0)
	if e != nil {
		return nil, e
	}
	if e = p.space(); e != nil {
		return nil, e
	}
	if p.pos != len(s) || n.members == nil {
		return nil, fmt.Errorf("config must be one object")
	}
	return n, nil
}
func opencodePath() (string, error) {
	dir := clientDir("opencode")
	a, b := filepath.Join(dir, "opencode.json"), filepath.Join(dir, "opencode.jsonc")
	_, ea := os.Lstat(a)
	_, eb := os.Lstat(b)
	if ea != nil && !os.IsNotExist(ea) {
		return "", ea
	}
	if eb != nil && !os.IsNotExist(eb) {
		return "", eb
	}
	if ea == nil && eb == nil {
		return "", fmt.Errorf("both opencode.json and opencode.jsonc exist; choose one global config manually, then add mcp.aibase with command [aibase, mcp]")
	}
	if eb == nil {
		return b, nil
	}
	return a, nil
}
func insertMember(s string, n *jsonNode, key string, value any) string {
	k, _ := json.Marshal(key)
	v, _ := json.Marshal(value)
	add := string(k) + ": " + string(v)
	if len(n.members) > 0 {
		add += ","
	}
	return s[:n.start+1] + add + s[n.start+1:]
}
func editOpencode(s, exe string) (string, error) {
	n, e := parseJSONC(s)
	if e != nil {
		return "", e
	}
	want := map[string]any{"type": "local", "command": []any{exe, "mcp"}, "enabled": true}
	m := n.members["mcp"]
	if m == nil {
		return insertMember(s, n, "mcp", map[string]any{"aibase": want}), nil
	}
	if m.members == nil {
		return "", fmt.Errorf("mcp must be an object")
	}
	a := m.members["aibase"]
	if a == nil {
		return insertMember(s, m, "aibase", want), nil
	}
	if reflect.DeepEqual(a.value, want) {
		return s, nil
	}
	old, ok := a.value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("conflicting mcp.aibase; edit manually")
	}
	cmd, _ := old["command"].([]any)
	if len(cmd) != 2 || !isAibaseExe(cmd[0]) || cmd[1] != "mcp" {
		return "", fmt.Errorf("conflicting mcp.aibase; edit manually")
	}
	// Replacing only existing scalar/array values preserves comments elsewhere.
	for _, key := range []string{"type", "command", "enabled"} {
		n, e = parseJSONC(s)
		if e != nil {
			return "", e
		}
		a = n.members["mcp"].members["aibase"]
		v := a.members[key]
		if v == nil {
			s = insertMember(s, a, key, want[key])
			continue
		}
		if reflect.DeepEqual(v.value, want[key]) {
			continue
		}
		raw := s[v.start:v.end]
		q := jsonParser{text: raw}
		hasComment := false
		for q.pos < len(raw) {
			if strings.HasPrefix(raw[q.pos:], "//") || strings.HasPrefix(raw[q.pos:], "/*") {
				hasComment = true
				break
			}
			if raw[q.pos] == '"' {
				_, e := q.node(0)
				if e != nil {
					return "", e
				}
			} else {
				q.pos++
			}
		}
		if hasComment {
			return "", fmt.Errorf("comments inside mcp.aibase.%s cannot be preserved safely; update this value manually", key)
		}
		b, _ := json.Marshal(want[key])
		s = s[:v.start] + string(b) + s[v.end:]
	}
	return s, nil
}
func (c manager) installOpencode(exe string) (string, error) {
	path, e := opencodePath()
	if e != nil {
		return "skipped opencode: " + e.Error(), nil
	}
	b, e := skills.Read(path)
	if os.IsNotExist(e) {
		b = []byte("{}\n")
	} else if e != nil {
		return "", e
	}
	body, e := editOpencode(string(b), exe)
	if e != nil {
		return "", fmt.Errorf("%s preserved: %w; manually add mcp.aibase in the standard global config", path, e)
	}
	return c.writeWithBackup(path, body)
}

func containsJSONComment(s string) bool {
	p := jsonParser{text: s}
	for p.pos < len(s) {
		if strings.HasPrefix(s[p.pos:], "//") || strings.HasPrefix(s[p.pos:], "/*") {
			return true
		}
		if s[p.pos] == '"' {
			if _, e := p.node(0); e != nil {
				return true
			}
		} else {
			p.pos++
		}
	}
	return false
}
func restoreOpencode(before *string, after, current string) (string, error) {
	a, e := parseJSONC(after)
	if e != nil {
		return "", e
	}
	n, e := parseJSONC(current)
	if e != nil {
		return "", e
	}
	var prior *jsonNode
	if before != nil {
		b, e := parseJSONC(*before)
		if e != nil {
			return "", e
		}
		if m := b.members["mcp"]; m != nil {
			prior = m.members["aibase"]
		}
	}
	am := a.members["mcp"]
	nm := n.members["mcp"]
	if am == nil || nm == nil || nm.members["aibase"] == nil {
		return current, nil
	}
	av, nv := am.members["aibase"], nm.members["aibase"]
	if av == nil || !reflect.DeepEqual(av.value, nv.value) {
		return "", fmt.Errorf("modified mcp.aibase preserved; restore manually from backup")
	}
	if containsJSONComment(current[nv.keyStart:nv.end]) {
		return "", fmt.Errorf("comments in modified mcp.aibase preserved; restore manually from backup")
	}
	if prior != nil {
		return current[:nv.start] + (*before)[prior.start:prior.end] + current[nv.end:], nil
	}
	start, end := nv.keyStart, nv.end
	p := jsonParser{text: current, pos: end}
	if e = p.space(); e != nil {
		return "", e
	}
	if p.pos < len(current) && current[p.pos] == ',' {
		current = current[:p.pos] + current[p.pos+1:]
	} else {
		// The comma before the removed member belongs to the preceding value.
		previousEnd := 0
		for _, v := range nm.members {
			if v.end <= start && v.end > previousEnd {
				previousEnd = v.end
			}
		}
		if previousEnd > 0 {
			q := jsonParser{text: current, pos: previousEnd}
			if e = q.space(); e != nil {
				return "", e
			}
			if q.pos < start && current[q.pos] == ',' {
				current = current[:q.pos] + current[q.pos+1:]
				start--
				end--
			}
		}
	}
	return current[:start] + current[end:], nil
}

func (c manager) installOpencodeGuide() (string, error) {
	path := filepath.Join(clientDir("opencode"), "AGENTS.md")
	b, e := skills.Read(path)
	if e != nil && !os.IsNotExist(e) {
		return "", e
	}
	body := string(b)
	start, end := "<!-- aibase:start -->", "<!-- aibase:end -->"
	a, z := strings.Index(body, start), strings.Index(body, end)
	if (a < 0) != (z < 0) || z >= 0 && z < a {
		return "", fmt.Errorf("conflicting managed AGENTS.md markers")
	}
	if a >= 0 {
		if body[a:z+len(end)] != strings.TrimSpace(AgentsGuide) {
			return "", fmt.Errorf("modified managed AGENTS.md block; preserved")
		}
		return c.writeWithBackup(path, body)
	}
	if body != "" {
		body += "\n\n"
	}
	return c.writeWithBackup(path, body+AgentsGuide)
}
