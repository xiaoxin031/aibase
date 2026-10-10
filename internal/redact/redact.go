// Package redact filters secrets at output boundaries without changing JSON keys.
package redact

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type rule struct {
	name  string
	re    *regexp.Regexp
	group int
}

var rules = []rule{
	{"pem", regexp.MustCompile(`(?s)-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----.*?-----END (?:[A-Z0-9]+ )*PRIVATE KEY-----`), 0},
	{"url-password", regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^\s/@:]+:([^\s/]+)@`), 1},
	{"assignment", regexp.MustCompile(`(?i)\b[A-Z_][A-Z0-9_]*(?:[ \t]*)=[ \t]*(?:"[^"\r\n]*"|'[^'\r\n]*'|[^\s;,]+)`), -1},
	{"bearer", regexp.MustCompile(`(?i)\bBearer[ \t]+([A-Za-z0-9._~+/=-]+)`), 1},
	{"openai", regexp.MustCompile(`\bsk-(?:ant-)?[A-Za-z0-9_-]{8,}`), 0},
	{"aws", regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z0-9]{16}\b`), 0},
	{"github", regexp.MustCompile(`\b(?:ghp_|github_pat_)[A-Za-z0-9_]{8,}`), 0},
	{"gitlab", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{8,}`), 0},
	{"slack", regexp.MustCompile(`\bxox[abp]-[A-Za-z0-9-]{8,}`), 0},
	{"google", regexp.MustCompile(`\bAIza[A-Za-z0-9_-]{20,}`), 0},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`), 0},
}
var sensitiveKey = regexp.MustCompile(`(?i)secret|token|password|passwd|api_key|credential|private`)

type Match struct {
	Start, End int
	Type       string
}

func Matches(s string) []Match {
	var all []Match
	for _, r := range rules {
		for _, loc := range r.re.FindAllStringSubmatchIndex(s, -1) {
			a, b := loc[0], loc[1]
			if r.group > 0 {
				a, b = loc[2*r.group], loc[2*r.group+1]
			}
			if r.group < 0 {
				eq := strings.IndexByte(s[a:b], '=')
				if !sensitiveKey.MatchString(s[a : a+eq]) {
					continue
				}
				a += eq + 1
				for a < b && (s[a] == ' ' || s[a] == '\t') {
					a++
				}
			}
			if a < b && s[a:b] != "[REDACTED]" {
				all = append(all, Match{a, b, r.name})
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Start == all[j].Start {
			return all[i].End > all[j].End
		}
		return all[i].Start < all[j].Start
	})
	var out []Match
	for _, m := range all {
		if len(out) > 0 && m.Start < out[len(out)-1].End {
			out[len(out)-1].End = max(out[len(out)-1].End, m.End)
			continue
		}
		out = append(out, m)
	}
	return out
}
func Text(s string) (string, int) {
	matches := Matches(s)
	if len(matches) == 0 {
		return s, 0
	}
	var b strings.Builder
	pos := 0
	for _, m := range matches {
		b.WriteString(s[pos:m.Start])
		b.WriteString("[REDACTED]")
		pos = m.End
	}
	b.WriteString(s[pos:])
	return b.String(), len(matches)
}

// JSON redacts string values, preserving keys, numeric precision and structure.
func JSON(data []byte) ([]byte, int, error) { return JSONFilter(data, nil) }

// JSONFilter can exempt specific memory fields; other values remain filtered.
func JSONFilter(data []byte, exempt func([]string) bool) ([]byte, int, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, 0, err
	}
	n := 0
	var walk func(any, []string) any
	walk = func(x any, path []string) any {
		switch t := x.(type) {
		case string:
			if exempt != nil && exempt(path) {
				return t
			}
			s, c := Text(t)
			n += c
			return s
		case []any:
			for i := range t {
				t[i] = walk(t[i], append(path, strconv.Itoa(i)))
			}
		case map[string]any:
			for k, v := range t {
				t[k] = walk(v, append(path, k))
			}
		}
		return x
	}
	out, err := json.Marshal(walk(v, nil))
	return out, n, err
}
func MemoryEnabled(home string) bool {
	var c struct {
		Redact struct {
			Memory *bool `json:"memory"`
		} `json:"redact"`
	}
	path := filepath.Join(home, "config.json")
	if CheckPath(path) != nil {
		return true
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 64<<10 {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	return err != nil || len(b) > 64<<10 || json.Unmarshal(b, &c) != nil || c.Redact.Memory == nil || *c.Redact.Memory
}
