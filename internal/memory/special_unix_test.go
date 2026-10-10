//go:build darwin || linux

package memory

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestSpecialMemoryRead(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fifo")
	if e := syscall.Mkfifo(p, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := readFile(p); e == nil {
		t.Fatal("FIFO accepted")
	}
}
