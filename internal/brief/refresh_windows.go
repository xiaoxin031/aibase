package brief

import (
	"errors"
	"os/exec"
	"syscall"
)

func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
}
func lockFile(path string) (func(), error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		if errors.Is(e, syscall.Errno(32)) || errors.Is(e, syscall.Errno(33)) {
			return nil, ErrRefreshBusy
		}
		return nil, e
	}
	return func() { _ = syscall.CloseHandle(h) }, nil
}
