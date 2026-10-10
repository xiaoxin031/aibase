//go:build darwin || linux

package audit

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSpecialAuditFiles(t *testing.T) {
	h := t.TempDir()
	p := filepath.Join(h, "audit.jsonl")
	if e := syscall.Mkfifo(p, 0600); e != nil {
		t.Fatal(e)
	}
	Record(h, "fixture", "Stop", 1, 0)
	if _, e := Read(h, 20); e == nil {
		t.Fatal("FIFO accepted")
	}
	os.Remove(p)
	dst := filepath.Join(t.TempDir(), "outside")
	os.WriteFile(dst, []byte("sentinel"), 0600)
	os.Symlink(dst, p)
	Record(h, "fixture", "Stop", 1, 0)
	b, _ := os.ReadFile(dst)
	if string(b) != "sentinel" {
		t.Fatal("followed symlink")
	}
}
