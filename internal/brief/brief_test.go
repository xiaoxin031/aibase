package brief

import (
	"context"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/cache"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func isolate(t *testing.T) string {
	h := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE", "AIBASE_HOME"} {
		t.Setenv(k, h)
	}
	return h
}
func TestCutGolden(t *testing.T) {
	got := Cut("机器项目技能中文内容"+strings.Repeat("甲", 30), 30, "aibase ls")
	want := "机器项目技能中文内容\n…Details: aibase ls"
	if got != want {
		t.Fatalf("golden mismatch %q", got)
	}
	for n := 0; n < 90; n++ {
		if utf8.RuneCountInString(Cut(strings.Repeat("中文", 100), n, "aibase ls")) > n {
			t.Fatal(n)
		}
	}
}
func TestBoundedNoCache(t *testing.T) {
	h := isolate(t)
	os.WriteFile(filepath.Join(h, "USER.md"), []byte(strings.Repeat("中文", 1000000)), 0600)
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"brief_limit":1700}`), 0600)
	r := Build(h)
	if utf8.RuneCountInString(r.Text) > 1700 || utf8.RuneCountInString(r.Machine) > 800 || utf8.RuneCountInString(r.Project) > 600 || utf8.RuneCountInString(r.Skills) > 600 || utf8.RuneCountInString(r.Gaps) > 400 {
		t.Fatal("budget")
	}
	if !strings.Contains(r.Skills, "cache missing") {
		t.Fatal("cold brief must return before refreshing")
	}
}
func TestCacheTimeAndMemoryExemption(t *testing.T) {
	h := isolate(t)
	now := time.Now()
	f := cache.New("value", "probe", time.Hour, cache.OK)
	f.At = now.Add(-2 * time.Hour)
	if Label(f, now) != "[cached 2h ago][stale]" {
		t.Fatal(Label(f, now))
	}
	f.At = now.Add(time.Hour)
	if Label(f, now) != "[cached age unknown]" {
		t.Fatal(Label(f, now))
	}
	os.WriteFile(filepath.Join(h, "USER.md"), []byte("TOKEN=historical-secret"), 0600)
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"redact":{"memory":false}}`), 0600)
	cache.NewStore("").Put("skills:index:v1", cache.New("TOKEN=current-secret", "test", time.Hour, cache.OK))
	r := Build(h)
	if !strings.Contains(r.Text, "historical-secret") || strings.Contains(r.Text, "current-secret") {
		t.Fatal(r.Text)
	}
}

func TestRedactionBeforeBudget(t *testing.T) {
	h := isolate(t)
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"brief_limit":500}`), 0600)
	os.WriteFile(filepath.Join(h, "USER.md"), []byte(strings.Repeat("TOKEN=x\n", 2000)), 0600)
	r := Build(h)
	if utf8.RuneCountInString(r.Text) > 500 || strings.Contains(r.Text, "TOKEN=x") {
		t.Fatal(r.Text)
	}
	for _, n := range []int{1, 10, 50, 300} {
		os.WriteFile(filepath.Join(h, "config.json"), []byte(fmt.Sprintf(`{"brief_limit":%d}`, n)), 0600)
		if utf8.RuneCountInString(Build(h).Text) > n {
			t.Fatal(n)
		}
	}
}

func TestEmptyBriefGolden(t *testing.T) {
	h := isolate(t)
	b, e := os.ReadFile("testdata/empty.golden")
	if e != nil {
		t.Fatal(e)
	}
	expected := strings.NewReplacer("{{OS}}", runtime.GOOS, "{{ARCH}}", runtime.GOARCH, "{{CPU}}", strconv.Itoa(runtime.NumCPU()), "{{CWD}}", h).Replace(strings.TrimSuffix(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"))
	r := Build(h)
	if r.Text != expected {
		t.Fatalf("brief golden mismatch\nwant: %q\n got: %q", expected, r.Text)
	}
}

func TestProjectAndDoctorSummaryCache(t *testing.T) {
	h := isolate(t)
	s := cache.NewStore("")
	s.Put("brief:project:"+h, cache.New("branch=review TOKEN=hidden", "inspect project", time.Minute, cache.OK))
	s.Put("brief:doctor:"+h, cache.New([]map[string]string{{"name": "compiler", "detail": "missing compiler", "status": "warn"}}, "doctor", time.Minute, cache.OK))
	r := Build(h)
	if !strings.Contains(r.Project, "branch=review") || strings.Contains(r.Project, "hidden") || !strings.Contains(r.Gaps, "missing compiler") {
		t.Fatal(r)
	}
}

func TestRefreshLock(t *testing.T) {
	isolate(t)
	s := cache.NewStore("")
	unlock, e := refreshLock(s)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := refreshLock(s); e != ErrRefreshBusy {
		t.Fatal(e)
	}
	unlock()
	unlock, e = refreshLock(s)
	if e != nil {
		t.Fatal(e)
	}
	unlock()
}
func TestRefreshCapturesRoot(t *testing.T) {
	h := isolate(t)
	root := filepath.Join(h, "fixture-skills")
	os.MkdirAll(filepath.Join(root, "sample"), 0700)
	os.WriteFile(filepath.Join(root, "sample", "SKILL.md"), []byte("---\nname: sample\ndescription: Fixture\n---\n"), 0600)
	store := cache.NewStore(h)
	if _, e := refresh(context.Background(), store, root, "basic"); e != nil {
		t.Fatal(e)
	}
	cwd, _ := os.Getwd()
	for _, key := range []string{MachineKey, "skills:index:v1", "brief:doctor:" + cwd} {
		if _, ok := store.Get(key); !ok {
			t.Fatal(key)
		}
	}
}
func TestMachineSnapshotHotPath(t *testing.T) {
	h := isolate(t)
	s := cache.NewStore("")
	r := map[string]cache.Fact{"os": cache.New("fixtureOS", "test", time.Hour, cache.OK), "cpu_model": cache.New("fixtureCPU", "test", time.Hour, cache.OK)}
	s.Put(MachineKey, cache.New(r, "test", time.Hour, cache.OK))
	s.Put("skills:index:v1", cache.New([]string{}, "test", time.Hour, cache.OK))
	t.Setenv("PATH", "")
	got := Build(h)
	if !strings.Contains(got.Machine, "fixtureCPU") || strings.Contains(got.Machine, "\n") {
		t.Fatal(got.Machine)
	}
	if _, ok := s.Get("brief:refresh-request:v1:" + h); ok {
		t.Fatal("hot path refreshed")
	}
}

func TestSynchronousRefreshValidationAndLock(t *testing.T) {
	isolate(t)
	if _, e := Refresh(context.Background(), "runtime"); e == nil {
		t.Fatal("runtime refresh accepted")
	}
	unlock, e := refreshLock(cache.NewStore(""))
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	if _, e := Refresh(context.Background(), "tools"); e != ErrRefreshBusy {
		t.Fatal(e)
	}
}

func TestGapLimitAndEmptyOmission(t *testing.T) {
	h := isolate(t)
	s := cache.NewStore("")
	rows := []map[string]string{}
	for _, name := range []string{"gap1", "gap2", "gap3", "gap4"} {
		rows = append(rows, map[string]string{"name": name, "detail": "missing", "status": "warn"})
	}
	s.Put("brief:doctor:"+h, cache.New(rows, "test", time.Hour, cache.OK))
	r := Build(h)
	if !strings.Contains(r.Gaps, "gap3") || strings.Contains(r.Gaps, "gap4") {
		t.Fatal(r.Gaps)
	}
	s.Put("brief:doctor:"+h, cache.New([]any{}, "test", time.Hour, cache.OK))
	r = Build(h)
	if r.Gaps != "" || strings.Contains(r.Text, "Gaps:") {
		t.Fatal(r)
	}
}

func TestExpiredEmptyDoctorIsVisible(t *testing.T) {
	h := isolate(t)
	f := cache.New([]any{}, "test", time.Minute, cache.OK)
	f.At = time.Now().Add(-2 * time.Minute)
	if err := cache.NewStore("").Put("brief:doctor:"+h, f); err != nil {
		t.Fatal(err)
	}
	if got := Build(h).Gaps; !strings.Contains(got, "stale") || !strings.Contains(got, "expired") {
		t.Fatal(got)
	}
}
