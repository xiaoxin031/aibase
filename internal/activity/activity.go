// Package activity derives token usage and live sessions from the transcripts
// AI clients already write locally. Files are read incrementally: an index
// remembers how far each append-only transcript was parsed.
package activity

import (
	"bytes"
	"encoding/json"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	Days       = 7
	ActiveFor  = 15 * time.Minute
	chunkLimit = 64 << 20
	indexVer   = 1
)

type Tokens struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

func (t Tokens) Total() int64 { return t.Input + t.Output + t.CacheRead + t.CacheWrite }

func (t *Tokens) add(o Tokens) {
	t.Input += o.Input
	t.Output += o.Output
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
}

type Session struct {
	Client  string    `json:"client"`
	Project string    `json:"project"`
	Updated time.Time `json:"updated"`
}

type Day struct {
	Date    string           `json:"date"`
	Total   int64            `json:"total"`
	Clients map[string]int64 `json:"clients"`
}

type Report struct {
	Today  map[string]Tokens `json:"today"`
	Total  int64             `json:"today_total"`
	Days   []Day             `json:"days"`
	Active []Session         `json:"active"`
}

type fileState struct {
	Client string   `json:"client"`
	Offset int64    `json:"offset"`
	Cwd    string   `json:"cwd,omitempty"`
	Sub    bool     `json:"sub,omitempty"`
	IDs    []uint32 `json:"ids,omitempty"`
	seen   map[uint32]bool
	Last   Tokens            `json:"last"`
	Days   map[string]Tokens `json:"days,omitempty"`
}

type index struct {
	Version int                   `json:"version"`
	Files   map[string]*fileState `json:"files"`
}

type root struct {
	client, dir string
	match       func(rel string) bool
}

func roots(home string) []root {
	jsonl := func(rel string) bool { return strings.HasSuffix(rel, ".jsonl") }
	return []root{
		{"claude", filepath.Join(home, ".claude", "projects"), jsonl},
		{"codex", codexSessions(home), jsonl},
		{"gemini", filepath.Join(home, ".gemini", "tmp"), func(rel string) bool {
			return strings.Contains(filepath.ToSlash(rel), "/chats/") && strings.HasSuffix(rel, ".json")
		}},
	}
}

func codexSessions(home string) string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return filepath.Join(h, "sessions")
	}
	return filepath.Join(home, ".codex", "sessions")
}

// Collect updates the index at indexPath and reports the last Days days.
func Collect(home, indexPath string, now time.Time) Report {
	var idx index
	if b, err := os.ReadFile(indexPath); err != nil || json.Unmarshal(b, &idx) != nil || idx.Version != indexVer || idx.Files == nil {
		idx = index{Version: indexVer, Files: map[string]*fileState{}}
	}
	cutoff := now.AddDate(0, 0, -Days)
	seen := map[string]bool{}
	var active []Session
	for _, r := range roots(home) {
		filepath.WalkDir(r.dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			rel, _ := filepath.Rel(r.dir, path)
			if !r.match(rel) {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.ModTime().Before(cutoff) {
				return nil
			}
			seen[path] = true
			st := idx.Files[path]
			if st == nil || st.Client != r.client || info.Size() < st.Offset {
				st = &fileState{Client: r.client}
				idx.Files[path] = st
			}
			if r.client == "gemini" {
				scanGemini(path, info.Size(), st)
			} else if info.Size() > st.Offset {
				scanLines(path, st)
			}
			if r.client == "claude" && strings.Contains(filepath.ToSlash(rel), "/subagents/") {
				st.Sub = true
			}
			if !st.Sub && now.Sub(info.ModTime()) < ActiveFor {
				active = append(active, Session{Client: r.client, Project: project(st.Cwd, home), Updated: info.ModTime()})
			}
			return nil
		})
	}
	for p := range idx.Files {
		if !seen[p] {
			delete(idx.Files, p)
		}
	}
	if b, err := json.Marshal(idx); err == nil {
		os.MkdirAll(filepath.Dir(indexPath), 0700)
		tmp := indexPath + ".tmp"
		if os.WriteFile(tmp, b, 0600) == nil {
			os.Rename(tmp, indexPath)
		}
	}
	return report(idx, active, now)
}

func report(idx index, active []Session, now time.Time) Report {
	r := Report{Today: map[string]Tokens{}, Active: active}
	byDay := map[string]map[string]int64{}
	today := now.Format("2006-01-02")
	for _, st := range idx.Files {
		for day, t := range st.Days {
			if byDay[day] == nil {
				byDay[day] = map[string]int64{}
			}
			byDay[day][st.Client] += t.Total()
			if day == today {
				cur := r.Today[st.Client]
				cur.add(t)
				r.Today[st.Client] = cur
				r.Total += t.Total()
			}
		}
	}
	for i := Days - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		d := Day{Date: day, Clients: map[string]int64{}}
		for c, n := range byDay[day] {
			d.Clients[c] = n
			d.Total += n
		}
		r.Days = append(r.Days, d)
	}
	sort.Slice(r.Active, func(i, j int) bool { return r.Active[i].Updated.After(r.Active[j].Updated) })
	return r
}

func project(cwd, home string) string {
	if cwd == "" {
		return ""
	}
	if filepath.Clean(cwd) == filepath.Clean(home) {
		return "~"
	}
	return filepath.Base(cwd)
}

func day(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ""
	}
	return t.Local().Format("2006-01-02")
}

// scanLines parses complete lines appended since the last run.
func scanLines(path string, st *fileState) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err = f.Seek(st.Offset, io.SeekStart); err != nil {
		return
	}
	b, err := io.ReadAll(io.LimitReader(f, chunkLimit))
	if err != nil {
		return
	}
	end := bytes.LastIndexByte(b, '\n')
	if end < 0 {
		return
	}
	for _, line := range bytes.Split(b[:end], []byte("\n")) {
		if st.Client == "claude" {
			claudeLine(line, st)
		} else {
			codexLine(line, st)
		}
	}
	st.Offset += int64(end + 1)
}

func (st *fileState) record(ts string, t Tokens) {
	d := day(ts)
	if d == "" || t.Total() <= 0 {
		return
	}
	if st.Days == nil {
		st.Days = map[string]Tokens{}
	}
	cur := st.Days[d]
	cur.add(t)
	st.Days[d] = cur
}

func claudeLine(line []byte, st *fileState) {
	if st.Cwd == "" && bytes.Contains(line, []byte(`"cwd"`)) {
		var v struct{ Cwd string }
		if json.Unmarshal(line, &v) == nil {
			st.Cwd = v.Cwd
		}
	}
	if !bytes.Contains(line, []byte(`"usage"`)) || !bytes.Contains(line, []byte(`"assistant"`)) {
		return
	}
	var v struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Message   struct {
			ID    string `json:"id"`
			Usage struct {
				Input      int64 `json:"input_tokens"`
				Output     int64 `json:"output_tokens"`
				CacheRead  int64 `json:"cache_read_input_tokens"`
				CacheWrite int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &v) != nil || v.Type != "assistant" {
		return
	}
	// A response is logged once per content block, and compaction or resume
	// can copy it again far later in the file; count each message id once.
	if id := v.Message.ID; id != "" {
		if st.seen == nil {
			st.seen = make(map[uint32]bool, len(st.IDs))
			for _, h := range st.IDs {
				st.seen[h] = true
			}
		}
		h := fnv.New32a()
		h.Write([]byte(id))
		sum := h.Sum32()
		if st.seen[sum] {
			return
		}
		st.seen[sum] = true
		st.IDs = append(st.IDs, sum)
	}
	u := v.Message.Usage
	st.record(v.Timestamp, Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite})
}

func codexLine(line []byte, st *fileState) {
	if st.Cwd == "" && bytes.Contains(line, []byte(`"session_meta"`)) {
		var v struct {
			Payload struct{ Cwd string } `json:"payload"`
		}
		if json.Unmarshal(line, &v) == nil {
			st.Cwd = v.Payload.Cwd
		}
		return
	}
	if !bytes.Contains(line, []byte(`"token_count"`)) {
		return
	}
	var v struct {
		Timestamp string `json:"timestamp"`
		Payload   struct {
			Type string `json:"type"`
			Info *struct {
				Total struct {
					Input  int64 `json:"input_tokens"`
					Cached int64 `json:"cached_input_tokens"`
					Output int64 `json:"output_tokens"`
				} `json:"total_token_usage"`
			} `json:"info"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &v) != nil || v.Payload.Type != "token_count" || v.Payload.Info == nil {
		return
	}
	// Codex reports running totals; OpenAI input tokens include cached ones.
	t := v.Payload.Info.Total
	cur := Tokens{Input: t.Input - t.Cached, Output: t.Output, CacheRead: t.Cached}
	if cur.Total() < st.Last.Total() {
		st.Last = Tokens{}
	}
	delta := Tokens{Input: cur.Input - st.Last.Input, Output: cur.Output - st.Last.Output, CacheRead: cur.CacheRead - st.Last.CacheRead}
	st.Last = cur
	if delta.Input >= 0 && delta.Output >= 0 && delta.CacheRead >= 0 {
		st.record(v.Timestamp, delta)
	}
}

// Gemini rewrites each chat file whole, so it is re-parsed when it changes.
func scanGemini(path string, size int64, st *fileState) {
	if size == st.Offset {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) > chunkLimit {
		return
	}
	var v struct {
		Messages []struct {
			Timestamp string `json:"timestamp"`
			Tokens    *struct {
				Input    int64 `json:"input"`
				Output   int64 `json:"output"`
				Cached   int64 `json:"cached"`
				Thoughts int64 `json:"thoughts"`
			} `json:"tokens"`
		} `json:"messages"`
	}
	if json.Unmarshal(b, &v) != nil {
		return
	}
	st.Days = nil
	for _, m := range v.Messages {
		if m.Tokens != nil {
			st.record(m.Timestamp, Tokens{Input: m.Tokens.Input - m.Tokens.Cached, Output: m.Tokens.Output + m.Tokens.Thoughts, CacheRead: m.Tokens.Cached})
		}
	}
	st.Offset = size
}
