package memory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) *project {
	t.Helper()
	t.Setenv("AIBASE_HOME", t.TempDir())
	return &project{ID: "demo-1234", Root: filepath.Join(t.TempDir(), "demo")}
}

func TestParseEntries(t *testing.T) {
	got := parseEntries("§\na\n§\nb\nline2\r\n§\n\n§\n")
	if len(got) != 2 || got[0] != "a" || got[1] != "b\nline2" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyOps(t *testing.T) {
	out, err := applyOps([]string{"alpha one", "beta two"}, []op{
		{Op: "add", Text: "gamma"},
		{Op: "add", Text: "gamma"},
		{Op: "replace", Old: "beta", Text: "beta 2"},
		{Op: "remove", Old: "alpha"},
	})
	if err != nil || strings.Join(out, "|") != "beta 2|gamma" {
		t.Fatalf("got %q, %v", out, err)
	}
	for _, bad := range [][]op{
		{{Op: "remove", Old: "a"}},
		{{Op: "remove", Old: "zzz"}},
		{{Op: "replace", Old: "alpha", Text: ""}},
		{{Op: "add", Text: "x\n§\ny"}},
		{{Op: "nope"}},
	} {
		if _, err := applyOps([]string{"alpha", "beta"}, bad); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
}

func TestSaveLimitIsAtomic(t *testing.T) {
	p := setup(t)
	os.WriteFile(filepath.Join(home(), "config.json"), []byte(`{"limits":{"user":10}}`), 0o600)
	if _, err := saveBatches([]batch{{"memory", []op{{Op: "add", Text: "kept"}}}}, p); err != nil {
		t.Fatal(err)
	}
	_, err := saveBatches([]batch{
		{"memory", []op{{Op: "add", Text: "not written"}}},
		{"user", []op{{Op: "add", Text: "way more than ten chars"}}},
	}, p)
	if err == nil || !strings.Contains(err.Error(), "nothing was saved") {
		t.Fatalf("want limit error, got %v", err)
	}
	s, _ := storeFor("memory", p)
	if got, _ := s.load(); len(got) != 1 {
		t.Fatalf("memory changed despite rejected batch: %q", got)
	}
	// Already over the limit (hand edit): shrinking is allowed, growing is not.
	u, _ := storeFor("user", p)
	os.WriteFile(u.Path, []byte("aaaaaaaaaaaa\n§\nbbbbbbbbbbbb\n"), 0o600)
	if _, err := saveBatches([]batch{{"user", []op{{Op: "remove", Old: "aaa"}}}}, p); err != nil {
		t.Fatalf("shrink rejected: %v", err)
	}
	if _, err := saveBatches([]batch{{"project", nil}}, nil); err == nil {
		t.Fatal("project target without project should fail")
	}
}

func TestSearchAndSnapshot(t *testing.T) {
	p := setup(t)
	writeNote(note{Slug: "vast-ops", Desc: "抢卡运维要点", Body: "RunPod 失败时切换 Vast"})
	appendLog("修好了抢卡脚本的超时", "claude", "", time.Now(), p)
	appendLog("other project work", "codex", "", time.Now(), &project{ID: "x", Root: "/x"})
	saveBatches([]batch{{"user", []op{{Op: "add", Text: "说中文"}}}, {"project", []op{{Op: "add", Text: "build: make"}}}}, p)

	out, _ := search("抢卡", 8)
	if !strings.Contains(out, "2 matches") || !strings.Contains(out, "[note] vast-ops") {
		t.Fatalf("search: %s", out)
	}
	if out, _ := search("姓名 runpod vast", 8); !strings.Contains(out, "1 matches") {
		t.Fatalf("any-term search: %s", out)
	}
	if out, _ := search("抢卡 runpod", 8); !strings.HasPrefix(strings.SplitN(out, "\n\n", 2)[1], "[note]") {
		t.Fatalf("more matched terms should rank first: %s", out)
	}
	snap := snapshot(p)
	for _, want := range []string{"说中文", "build: make", "修好了抢卡", "1 notes, 2 session logs"} {
		if !strings.Contains(snap, want) {
			t.Errorf("snapshot missing %q:\n%s", want, snap)
		}
	}
	if strings.Contains(snap, "other project work") {
		t.Error("snapshot leaked another project's log")
	}
}

func TestParseNoteFrontmatter(t *testing.T) {
	n := parseNote("---\nname: foo_bar\ndescription: \"desc: x\"\nmetadata:\n  type: user\n---\n\nbody\n", "file")
	if n.Slug != "foo_bar" || n.Desc != "desc: x" || n.Body != "body" {
		t.Fatalf("%+v", n)
	}
	if n := parseNote("plain", "file"); n.Slug != "file" || n.Body != "plain" {
		t.Fatalf("%+v", n)
	}
}

func TestStopHook(t *testing.T) {
	setup(t)
	os.WriteFile(filepath.Join(home(), "config.json"), []byte(`{"review_every":3}`), 0o600)
	tr := filepath.Join(t.TempDir(), "t.jsonl")
	turn := func(transcript string, active bool) bool {
		f, _ := os.OpenFile(tr, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		f.WriteString(transcript + "\n")
		f.Close()
		in, _ := json.Marshal(hookInput{SessionID: "s1", TranscriptPath: tr, StopHookActive: active})
		out, err := stopHook("claude", bytes.NewReader(in))
		if err != nil {
			t.Fatal(err)
		}
		return strings.Contains(out, `"decision":"block"`)
	}
	if turn(`{"text":"hi"}`, false) || turn(`{"text":"chat"}`, false) {
		t.Fatal("no review expected before N turns without edits")
	}
	if !turn(`{"text":"chat"}`, false) {
		t.Fatal("review expected every 3 turns")
	}
	if turn(`{"text":"saved"}`, true) {
		t.Fatal("must not block while the review turn stops")
	}
	if !turn(`{"type":"tool_use","name":"Edit"}`, false) {
		t.Fatal("review expected right after an edit")
	}
	turn(`{}`, true)
	if turn(`{"text":"plain"}`, false) {
		t.Fatal("old edits must not retrigger")
	}
}

func TestSessionLogReplaces(t *testing.T) {
	p := setup(t)
	appendLog("first", "claude", "s1", time.Now(), p)
	appendLog("other", "claude", "s2", time.Now(), p)
	appendLog("second", "claude", "s1", time.Now(), p)
	logs, _ := readLogs()
	if len(logs) != 2 || logs[1].Text != "second" || logs[0].Text != "other" {
		t.Fatalf("%+v", logs)
	}
}
