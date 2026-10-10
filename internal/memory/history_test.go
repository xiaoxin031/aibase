package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecentHistory(t *testing.T) {
	p := setup(t)
	var lines []string
	for i := range 20 {
		project := "other"
		if i%3 == 0 {
			project = p.ID
		}
		b, _ := json.Marshal(logEntry{Text: fmt.Sprint(i), Project: project})
		lines = append(lines, string(b))
	}
	lines = append(lines, `invalid`, `{"text":""}`, `{"text":"bad","ts":123}`, `{"text":"latest","project":"demo-1234"}`)
	os.WriteFile(historyPath(), []byte(strings.Join(lines, "\r\n")), 0600)
	for range 2 {
		recent, n := recentHistory(p)
		if n != 24 || len(recent) != 3 || recent[0].Text != "latest" || recent[1].Text != "18" || recent[2].Text != "15" {
			t.Fatalf("%+v count=%d", recent, n)
		}
	}
	os.WriteFile(historyPath(), []byte(`{"text":"replacement","project":"demo-1234"}`), 0600)
	recent, n := recentHistory(p)
	if n != 1 || recent[0].Text != "replacement" {
		t.Fatalf("stale cache: %+v %d", recent, n)
	}
	recent, n = recentHistory(nil)
	if n != 1 || len(recent) != 0 {
		t.Fatal("global snapshot must exclude logs")
	}
}
func TestReverseBlockBoundary(t *testing.T) {
	p := setup(t)
	long := strings.Repeat("中", 40000)
	a, _ := json.Marshal(logEntry{Text: long, Project: p.ID})
	os.WriteFile(historyPath(), append(a, []byte("\n{\"text\":\"last\",\"project\":\"demo-1234\"}\n")...), 0600)
	recent, n := recentHistory(p)
	if n != 2 || len(recent) != 2 || recent[1].Text != long {
		t.Fatalf("block boundary %d %d", n, len(recent))
	}
}
func benchmarkHistory(b *testing.B) *project {
	b.Helper()
	b.Setenv("AIBASE_HOME", b.TempDir())
	p := &project{ID: "bench", Root: "/bench"}
	var out strings.Builder
	for i := range 10000 {
		line, _ := json.Marshal(logEntry{TS: "2026-10-11T00:00:00Z", Project: p.ID, Text: fmt.Sprintf("session %d 中文 deployment troubleshooting", i)})
		out.Write(line)
		out.WriteByte('\n')
	}
	if err := os.WriteFile(historyPath(), []byte(out.String()), 0600); err != nil {
		b.Fatal(err)
	}
	return p
}
func BenchmarkSnapshot10K(b *testing.B) {
	p := benchmarkHistory(b)
	snapshot(p)
	b.ResetTimer()
	for b.Loop() {
		snapshot(p)
	}
}
func BenchmarkSnapshot10KColdCount(b *testing.B) {
	p := benchmarkHistory(b)
	b.ResetTimer()
	for b.Loop() {
		os.Remove(filepath.Join(home(), "state", "history-count.json"))
		snapshot(p)
	}
}
func BenchmarkSearch10K(b *testing.B) {
	benchmarkHistory(b)
	b.ResetTimer()
	for b.Loop() {
		if _, err := search("中文 deployment", 8); err != nil {
			b.Fatal(err)
		}
	}
}

func TestColdHistoryOnlyDecodesNeededTail(t *testing.T) {
	p := setup(t)
	var body strings.Builder
	for range 10000 {
		body.WriteString("old invalid JSON that must never be decoded\n")
	}
	for range 3 {
		b, _ := json.Marshal(logEntry{Project: p.ID, Text: "recent"})
		body.Write(b)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(historyPath(), []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		calls := 0
		recent, count := recentHistoryWithDecoder(p, func(b []byte, v any) error {
			calls++
			if bytes.Contains(b, []byte("old invalid")) {
				t.Fatal("decoded old record")
			}
			return json.Unmarshal(b, v)
		})
		if count != 10003 || len(recent) != 3 || calls != 3 {
			t.Fatalf("count=%d recent=%d decoded=%d", count, len(recent), calls)
		}
	}
}

func TestStreamingCount(t *testing.T) {
	for _, body := range []string{"", " \t\r\n\n", "{}", "{}\n", "{}\r\n \t\n{}", strings.Repeat("x", 70000) + "\n{}\n"} {
		want := 0
		for _, line := range strings.Split(body, "\n") {
			if strings.TrimSpace(line) != "" {
				want++
			}
		}
		got, err := countHistory(strings.NewReader(body))
		if err != nil || got != want {
			t.Fatalf("got %d want %d: %v", got, want, err)
		}
	}
}

func TestOldHistoryCountCacheInvalidated(t *testing.T) {
	p := setup(t)
	os.WriteFile(historyPath(), []byte("invalid\n"), 0600)
	st, err := os.Stat(historyPath())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(historyCount{Size: st.Size(), Modified: st.ModTime().UnixNano(), Count: 0})
	if err := writeAtomic(filepath.Join(home(), "state", "history-count.json"), string(b)); err != nil {
		t.Fatal(err)
	}
	_, count := recentHistory(p)
	if count != 1 {
		t.Fatalf("stale pre-streaming count: %d", count)
	}
}

func TestStreamingCountMatchesGeneratedLogs(t *testing.T) {
	p := setup(t)
	for i := range 12 {
		if _, err := appendLog(fmt.Sprintf("session %d\n中文 with newline", i), "claude", fmt.Sprint(i), time.Now(), p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := appendLog("replacement", "claude", "1", time.Now(), p); err != nil {
		t.Fatal(err)
	}
	logs, err := readLogs()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		_, count := recentHistory(p)
		if count != len(logs) || count != 12 {
			t.Fatalf("streaming=%d original=%d", count, len(logs))
		}
	}
}
