package memory

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/skills"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type logEntry struct {
	TS      string `json:"ts"`
	Session string `json:"session,omitempty"`
	Project string `json:"project,omitempty"`
	Path    string `json:"path,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Text    string `json:"text"`
}

func historyPath() string { return filepath.Join(home(), "history.jsonl") }
func notesDir() string    { return filepath.Join(home(), "notes") }

// appendLog adds a session summary; a later summary for the same session replaces it.
func appendLog(text, agent, session string, ts time.Time, p *project) (string, error) {
	if text = strings.TrimSpace(text); text == "" {
		return "", errors.New("empty log summary")
	}
	e := logEntry{TS: ts.Format(time.RFC3339), Session: session, Agent: agent, Text: text}
	if p != nil {
		e.Project, e.Path = p.ID, p.Root
	}
	line, _ := json.Marshal(e)
	unlock, err := lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if session != "" {
		if replaced, err := replaceSessionLog(session, line); err != nil || replaced {
			return "updated session summary" + projectSuffix(p), err
		}
	}
	if e := skills.Guard(historyPath()); e != nil {
		return "", e
	}
	if st, e := os.Lstat(historyPath()); e == nil && !st.Mode().IsRegular() {
		return "", fmt.Errorf("history is not regular data")
	}
	f, err := os.OpenFile(historyPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return "", err
	}
	return "logged session summary" + projectSuffix(p), nil
}

func replaceSessionLog(session string, line []byte) (bool, error) {
	b, err := readFile(historyPath())
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	kept := lines[:0]
	found := false
	for _, l := range lines {
		var e logEntry
		if json.Unmarshal([]byte(l), &e) == nil && e.Session == session {
			found = true
			continue
		}
		kept = append(kept, l)
	}
	if !found {
		return false, nil
	}
	return true, writeAtomic(historyPath(), strings.Join(append(kept, string(line)), "\n")+"\n")
}

func projectSuffix(p *project) string {
	if p == nil {
		return " (global)"
	}
	return " for " + p.Root
}

func readLogs() ([]logEntry, error) {
	var out []logEntry
	err := visitLogs(func(e logEntry) { out = append(out, e) })
	return out, err
}
func visitLogs(visit func(logEntry)) error {
	f, err := openRead(historyPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 32<<10), 1<<24)
	for sc.Scan() {
		var e logEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Text != "" {
			visit(e)
		}
	}
	return sc.Err()
}

type note struct {
	Slug, Desc, Body string
}

var slugRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}._-]*$`)

func notePath(slug string) (string, error) {
	if !slugRe.MatchString(slug) {
		return "", fmt.Errorf("invalid slug %q (letters, digits, . _ -)", slug)
	}
	return filepath.Join(notesDir(), slug+".md"), nil
}

func writeNote(n note) (string, error) {
	path, err := notePath(n.Slug)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(n.Body) == "" {
		return "", errors.New("empty note body")
	}
	desc := strings.Join(strings.Fields(n.Desc), " ")
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", n.Slug, desc, strings.TrimSpace(n.Body))
	unlock, err := lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := writeAtomic(path, content); err != nil {
		return "", err
	}
	return "saved note " + n.Slug, nil
}

// parseNote accepts plain markdown or markdown with name/description frontmatter.
func parseNote(raw, fallbackSlug string) note {
	n := note{Slug: fallbackSlug, Body: raw}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if rest, ok := strings.CutPrefix(raw, "---\n"); ok {
		if fm, body, ok := strings.Cut(rest, "\n---"); ok {
			n.Body = strings.TrimSpace(strings.TrimPrefix(body, "\n"))
			for _, line := range strings.Split(fm, "\n") {
				k, v, _ := strings.Cut(line, ":")
				v = strings.Trim(strings.TrimSpace(v), `"'`)
				switch strings.TrimSpace(k) {
				case "name":
					if slugRe.MatchString(v) {
						n.Slug = v
					}
				case "description":
					n.Desc = v
				}
			}
		}
	}
	return n
}

func readNotes() ([]note, error) {
	files, err := filepath.Glob(filepath.Join(notesDir(), "*.md"))
	if err != nil {
		return nil, err
	}
	var out []note
	for _, f := range files {
		b, err := readFile(f)
		if err != nil {
			continue
		}
		out = append(out, parseNote(string(b), strings.TrimSuffix(filepath.Base(f), ".md")))
	}
	return out, nil
}

func showNote(slug string) (string, error) {
	path, err := notePath(slug)
	if err != nil {
		return "", err
	}
	b, err := readFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("no note %q; try `aibase search`", slug)
	}
	return string(b), err
}

type hit struct {
	score int
	ts    string
	head  string
	text  string
	terms []string
}

// search does case-insensitive substring matching; any term may match, and hits
// matching more distinct terms rank first. Substrings work for CJK without spaces.
func search(query string, limit int) (string, error) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return "", errors.New("empty query")
	}
	notes, err := readNotes()
	if err != nil {
		return "", err
	}
	limit = max(1, limit)
	var hits []hit
	total := 0
	add := func(h hit) {
		total++
		at := sort.Search(len(hits), func(i int) bool {
			return hits[i].score < h.score || (hits[i].score == h.score && hits[i].ts < h.ts)
		})
		if at >= limit {
			return
		}
		if len(hits) < limit {
			hits = append(hits, hit{})
		}
		copy(hits[at+1:], hits[at:len(hits)-1])
		hits[at] = h
	}
	for _, n := range notes {
		meta := strings.ToLower(n.Slug + " " + n.Desc)
		if s := score(meta+" "+strings.ToLower(n.Body), terms); s > 0 {
			s += 5 * score(meta, terms)
			add(hit{s, "", "[note] " + n.Slug + " — " + n.Desc, n.Body, terms})
		}
	}
	err = visitLogs(func(e logEntry) {
		if s := score(strings.ToLower(e.Project+" "+e.Text), terms); s > 0 {
			head := "[log " + e.TS[:min(10, len(e.TS))]
			if e.Path != "" {
				head += " " + e.Path
			}
			if e.Agent != "" {
				head += " · " + e.Agent
			}
			add(hit{s, e.TS, head + "]", e.Text, terms})
		}
	})
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "no matches for " + query, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d matches", total)
	if total > limit {
		fmt.Fprintf(&b, ", showing %d", limit)
	}
	b.WriteString(" (full note: aibase show <slug>)\n")
	for _, h := range hits {
		fmt.Fprintf(&b, "\n%s\n%s\n", h.head, snippet(h.text, h.terms, 400))
	}
	return b.String(), nil
}

func score(hay string, terms []string) int {
	matched, total := 0, 0
	for _, t := range terms {
		if c := strings.Count(hay, t); c > 0 {
			matched++
			total += min(c, 20)
		}
	}
	if matched == 0 {
		return 0
	}
	return matched*1000 + total
}

func snippet(text string, terms []string, width int) string {
	r := []rune(text)
	if len(r) <= width {
		return text
	}
	lower := []rune(strings.ToLower(text))
	at := 0
	for _, t := range terms {
		if i := strings.Index(string(lower), t); i >= 0 {
			at = len([]rune(string(lower)[:i]))
			break
		}
	}
	start := min(max(0, at-width/3), len(r))
	end := min(len(r), start+width)
	s := string(r[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(r) {
		s += "…"
	}
	return s
}
