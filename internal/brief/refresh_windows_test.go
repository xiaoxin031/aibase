package brief

import (
	"os/exec"
	"testing"
)

func TestDetachedProcessConfiguration(t *testing.T) {
	c := exec.Command("aibase", "refresh")
	detach(c)
	if c.SysProcAttr == nil || c.SysProcAttr.CreationFlags&(0x8|0x200) != (0x8|0x200) || c.Stdin != nil || c.Stdout != nil || c.Stderr != nil {
		t.Fatal("not detached with null stdio")
	}
}
