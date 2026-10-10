//go:build !windows

package main

import "os/exec"

func prepareCommand(cmd *exec.Cmd) {}
