package fileio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceWhileReading(t *testing.T) {
	dir := t.TempDir()
	dst, src := filepath.Join(dir, "target"), filepath.Join(dir, "staged")
	if err := os.WriteFile(dst, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := OpenRead(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = Replace(src, dst); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "new" {
		t.Fatal(string(b), err)
	}
	b = make([]byte, 3)
	if _, err = f.Read(b); err != nil || string(b) != "old" {
		t.Fatal(string(b), err)
	}
	if _, err = os.Stat(src); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
