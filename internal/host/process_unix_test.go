//go:build darwin || linux

package host

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTimeoutKillsProcessGroup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pid")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, e := (ExecRunner{}).Run(ctx, "", "sh", "-c", `echo $$ > "$1"; sleep 30 & wait`, "sh", p)
	if e == nil {
		t.Fatal("no timeout")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	for n := 0; n < 20; n++ {
		if syscall.Kill(-pid, 0) == syscall.ESRCH {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("process group survived cancellation")
}
