//go:build !windows

package fileio

import "os"

func Replace(from, to string) error { return os.Rename(from, to) }

func OpenRead(path string) (*os.File, error) { return os.Open(path) }
