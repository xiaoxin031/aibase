//go:build !darwin && !linux

package host

import "os/exec"

// CommandContext terminates the direct process. Descendant job isolation on
// Windows is not implemented and must not be claimed as validated.
func configureProcess(c *exec.Cmd) {}
