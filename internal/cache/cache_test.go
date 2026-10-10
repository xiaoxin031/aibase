package cache

import (
	"encoding/json"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFactExpiryCorruptionRedaction(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("AIBASE_HOME", h)
	s := NewStore("")
	f := New(map[string]any{"TOKEN=hidden": "https://u:secret@example.com", "sk-abcdefghijklmnop": "TOKEN=private"}, "TOKEN=source", time.Hour, OK)
	if e := s.Put("x", f); e != nil {
		t.Fatal(e)
	}
	got, ok := s.Get("x")
	if !ok || got.Status != OK {
		t.Fatal(got)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "private") || strings.Contains(string(b), "hidden") {
		t.Fatal(string(b))
	}
	f.At = time.Now().Add(-2 * time.Hour)
	_ = s.Put("x", f)
	got, _ = s.Get("x")
	if got.Status != Stale {
		t.Fatal(got)
	}
	for _, b := range []string{"{bad", `{"status":"ok"}`, `{"status":"invalid","at":"2026-01-01T00:00:00Z"}`} {
		os.WriteFile(s.path("x"), []byte(b), 0600)
		if _, ok := s.Get("x"); ok {
			t.Fatal("corrupt accepted")
		}
		if e := s.Put("x", New("rebuilt", "test", time.Hour, OK)); e != nil {
			t.Fatal(e)
		}
	}
	st, _ := os.Stat(s.path("x"))
	if runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0 {
		t.Fatal(st.Mode())
	}
}
func TestConcurrentProcesses(t *testing.T) {
	if h := os.Getenv("CACHE_HELPER"); h != "" {
		s := NewStore(h)
		for n := 0; n < 50; n++ {
			if e := s.Put("shared", New(strings.Repeat("x", 1024), "helper", time.Hour, OK)); e != nil {
				os.Exit(2)
			}
		}
		return
	}
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("AIBASE_HOME", h)
	s := NewStore(h)
	_ = s.Put("shared", New("start", "test", time.Hour, OK))
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := exec.Command(os.Args[0], "-test.run=^TestConcurrentProcesses$")
			c.Env = append(os.Environ(), "CACHE_HELPER="+h)
			if b, e := c.CombinedOutput(); e != nil {
				errs <- fmt.Errorf("%s: %w", b, e)
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	for {
		select {
		case <-done:
			close(errs)
			for e := range errs {
				t.Error(e)
			}
			return
		default:
			if _, ok := s.Get("shared"); !ok {
				t.Fatal("partial/missing concurrent entry")
			}
			time.Sleep(time.Millisecond)
		}
	}
}
func TestSymlinkAndOversizeIgnored(t *testing.T) {
	s := NewStore(t.TempDir())
	os.MkdirAll(s.Dir, 0700)
	target := filepath.Join(t.TempDir(), "target")
	os.WriteFile(target, []byte(`{}`), 0600)
	t.Run("symlink", func(t *testing.T) {
		testutil.Symlink(t, target, s.path("x"))
		if _, ok := s.Get("x"); ok {
			t.Fatal("symlink accepted")
		}
	})
	os.WriteFile(s.path("large"), make([]byte, (4<<20)+1), 0600)
	if _, ok := s.Get("large"); ok {
		t.Fatal("oversized accepted")
	}
}

func TestStructuredSecretValues(t *testing.T) {
	v := Clean(map[string]any{"password": "plain secret", "declaration": "services:\n  app:\n    password: yaml secret\n    command: echo hello\n"})
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "plain secret") || strings.Contains(string(b), "yaml secret") || !strings.Contains(string(b), "echo hello") {
		t.Fatal(string(b))
	}
}

func TestBoundedRead(t *testing.T) {
	s := NewStore(t.TempDir())
	f := New(strings.Repeat("x", 10000), "test", time.Hour, OK)
	if e := s.Put("large", f); e != nil {
		t.Fatal(e)
	}
	if _, ok := s.GetBounded("large", 1024); ok {
		t.Fatal("oversized fact read")
	}
	if _, ok := s.Get("large"); !ok {
		t.Fatal("normal read failed")
	}
}

func TestUnknownStatusNeverFresh(t *testing.T) {
	f := New("x", "test", time.Hour, Status("invented"))
	if f.Fresh(time.Now()) {
		t.Fatal("unknown status accepted as fresh")
	}
}

func TestTTLSecondsAndLegacy(t *testing.T) {
	f := New("x", "test", time.Minute, OK)
	b, e := json.Marshal(f)
	if e != nil || !strings.Contains(string(b), `"ttl":60`) || !strings.Contains(string(b), `"ttl_unit":"s"`) {
		t.Fatal(string(b), e)
	}
	var got Fact
	if e = json.Unmarshal(b, &got); e != nil || got.TTL != time.Minute {
		t.Fatal(got, e)
	}
	legacy := fmt.Sprintf(`{"value":"x","source":"old","at":%q,"ttl":60000000000,"status":"ok"}`, time.Now().UTC().Format(time.RFC3339Nano))
	if e = json.Unmarshal([]byte(legacy), &got); e != nil || got.TTL != time.Minute {
		t.Fatal(got, e)
	}
	for _, bad := range []string{`{"ttl":1e100,"ttl_unit":"s"}`, `{"ttl":-1}`, `{"ttl":1,"ttl_unit":"hours"}`} {
		if json.Unmarshal([]byte(bad), &got) == nil {
			t.Fatal(bad)
		}
	}
}
