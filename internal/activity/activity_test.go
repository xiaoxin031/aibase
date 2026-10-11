package activity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func write(t *testing.T, path, s string, appendTo bool) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0700)
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendTo {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(path, flag, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(s)
	f.Close()
}

func TestCollect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	now := time.Now()
	ts := now.UTC().Format(time.RFC3339Nano)
	old := now.AddDate(0, 0, -2).UTC().Format(time.RFC3339Nano)
	claude := filepath.Join(home, ".claude", "projects", "-p", "s1.jsonl")
	msg := `{"type":"assistant","timestamp":"` + ts + `","cwd":"/work/app","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100,"cache_creation_input_tokens":20}}}` + "\n"
	write(t, claude, `{"type":"user","cwd":"/work/app"}`+"\n"+msg+msg, false)
	write(t, filepath.Join(home, ".claude", "projects", "-p", "s1", "subagents", "a.jsonl"), `{"type":"assistant","timestamp":"`+old+`","message":{"id":"m2","usage":{"input_tokens":1,"output_tokens":1}}}`+"\n", false)
	codex := filepath.Join(home, ".codex", "sessions", "2026", "01", "01", "r.jsonl")
	count := func(in, cached, out int) string {
		return `{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":` + strconv.Itoa(in) + `,"cached_input_tokens":` + strconv.Itoa(cached) + `,"output_tokens":` + strconv.Itoa(out) + `}}}}` + "\n"
	}
	cwd, _ := json.Marshal(home)
	write(t, codex, `{"type":"session_meta","payload":{"cwd":`+string(cwd)+`}}`+"\n"+count(100, 60, 10)+count(100, 60, 10)+count(300, 200, 30)+`{"partial`, false)
	idx := filepath.Join(home, "idx.json")

	r := Collect(home, idx, now)
	if got := r.Today["claude"]; got != (Tokens{Input: 10, Output: 5, CacheRead: 100, CacheWrite: 20}) {
		t.Fatalf("claude today: %+v", got)
	}
	if got := r.Today["codex"]; got != (Tokens{Input: 100, Output: 30, CacheRead: 200}) {
		t.Fatalf("codex today: %+v", got)
	}
	if r.Total != 135+330 || len(r.Days) != Days || r.Days[Days-1].Total != r.Total || r.Days[Days-3].Clients["claude"] != 2 {
		t.Fatalf("days: %+v total %d", r.Days, r.Total)
	}
	if len(r.Active) != 2 || r.Active[0].Project == "" {
		t.Fatalf("active: %+v", r.Active)
	}
	projects := map[string]string{}
	for _, s := range r.Active {
		projects[s.Client] = s.Project
	}
	if projects["claude"] != "app" || projects["codex"] != "~" {
		t.Fatalf("projects: %+v", projects)
	}

	write(t, codex, `"}`+"\n"+count(400, 250, 40), true)
	write(t, claude, msg, true)
	r = Collect(home, idx, now)
	if got := r.Today["codex"]; got.Total() != 440 {
		t.Fatalf("incremental codex: %+v", got)
	}
	if got := r.Today["claude"]; got.Total() != 135 {
		t.Fatalf("a repeated message id must count once: %+v", got)
	}
	old2 := now.Add(-time.Hour)
	os.Chtimes(claude, old2, old2)
	os.Chtimes(codex, old2, old2)
	if r = Collect(home, idx, now); len(r.Active) != 0 {
		t.Fatalf("idle sessions are not active: %+v", r.Active)
	}
}
