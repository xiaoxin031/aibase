// Package testutil contains portable filesystem test fixtures.
package testutil

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
)

func Symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if os.IsPermission(err) || runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		t.Fatal(err)
	}
}
