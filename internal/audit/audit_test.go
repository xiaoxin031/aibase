package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordRotateRead(t *testing.T) {
	h := t.TempDir()
	Record(h, "claude", "SessionStart", 123, 2)
	b, err := os.ReadFile(filepath.Join(h, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var e Entry
	if json.Unmarshal(b, &e) != nil || e.Bytes != 123 || e.Redactions != 2 || e.Client != "claude" || e.TS == "" {
		t.Fatalf("%s", b)
	}
	os.WriteFile(filepath.Join(h, "audit.jsonl"), []byte(strings.Repeat("{}\n", MaxSize/3+1)), 0600)
	Record(h, "mcp", "memory_load", 4, 1)
	st, err := os.Stat(filepath.Join(h, "audit.jsonl.1"))
	if err != nil || st.Size() != int64((MaxSize/3+1)*3) {
		t.Fatalf("rotation: %v", err)
	}
	Record(h, "codex", "Stop", 2, 0)
	out, err := Read(h, 1)
	if err != nil || !strings.Contains(out, `"Stop"`) || strings.Contains(out, "memory_load") {
		t.Fatalf("%s %v", out, err)
	}
	Record(filepath.Join(h, "audit.jsonl", "impossible"), "mcp", "ping", 0, 0)
}

func TestBusyAuditDoesNotBlock(t *testing.T) {
	h := t.TempDir()
	os.WriteFile(filepath.Join(h, ".audit.lock"), nil, 0600)
	Record(h, "mcp", "ping", 12, 0)
	if _, err := os.Stat(filepath.Join(h, "audit.jsonl")); !os.IsNotExist(err) {
		t.Fatal("wrote while lock held")
	}
}

func TestStaleAuditLockRecovery(t *testing.T) {
	h := t.TempDir()
	lock := filepath.Join(h, ".audit.lock")
	if err := os.WriteFile(lock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	Record(h, "claude", "SessionStart", 42, 0)
	out, err := Read(h, 1)
	if err != nil || !strings.Contains(out, `"bytes":42`) {
		t.Fatalf("%s %v", out, err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("lock retained: %v", err)
	}
}
