//go:build darwin || linux

package skills

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestFIFORejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fifo")
	if e := syscall.Mkfifo(p, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Read(p); e == nil {
		t.Fatal("read FIFO")
	}
	if f, e := OpenDir(p); e == nil {
		f.Close()
		t.Fatal("opened FIFO")
	}
}
