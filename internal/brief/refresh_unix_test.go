//go:build !windows

package brief

import (
	"os/exec"
	"testing"
)

func TestDetachedProcessConfiguration(t *testing.T) {
	c := exec.Command("aibase", "refresh")
	detach(c)
	if c.SysProcAttr == nil || !c.SysProcAttr.Setsid || c.Stdin != nil || c.Stdout != nil || c.Stderr != nil {
		t.Fatal("not detached with null stdio")
	}
}
