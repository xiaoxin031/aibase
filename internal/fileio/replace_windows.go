//go:build windows

package fileio

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                                 = syscall.NewLazyDLL("kernel32.dll")
	moveFileEx                               = kernel32.NewProc("MoveFileExW")
	setFileInformationByHandle               = kernel32.NewProc("SetFileInformationByHandle")
	errSharing                 syscall.Errno = 32
)

const (
	fileRenameInfoEx       = 22
	renameReplaceIfExists  = 0x1
	renamePosixSemantics   = 0x2
	moveReplaceExisting    = 0x1
	moveWriteThrough       = 0x8
	deleteAccess           = 0x00010000
	replaceRetryDeadline   = 2 * time.Second
	replaceRetryInitialGap = 5 * time.Millisecond
)

type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  syscall.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

// Replace atomically renames from over to. POSIX rename semantics (Windows 10
// 1607+, NTFS) let it replace a target that readers still hold open; otherwise
// it falls back to MoveFileEx, retrying while another handle blocks the target.
func Replace(from, to string) error {
	if e := posixRename(from, to); e == nil {
		return nil
	}
	gap := replaceRetryInitialGap
	deadline := time.Now().Add(replaceRetryDeadline)
	for {
		e := moveFile(from, to)
		if e == nil || !transient(e) || time.Now().After(deadline) {
			return e
		}
		time.Sleep(gap)
		gap = min(gap*2, 100*time.Millisecond)
	}
}

func posixRename(from, to string) error {
	abs, e := filepath.Abs(to)
	if e != nil {
		return e
	}
	name, e := syscall.UTF16FromString(abs)
	if e != nil {
		return e
	}
	p, e := syscall.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	h, e := syscall.CreateFile(p, deleteAccess|syscall.SYNCHRONIZE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return e
	}
	defer syscall.CloseHandle(h)
	n := len(name) - 1
	size := int(unsafe.Offsetof(fileRenameInfo{}.FileName)) + (n+1)*2
	buf := make([]uint64, (size+7)/8)
	info := (*fileRenameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = renameReplaceIfExists | renamePosixSemantics
	info.FileNameLength = uint32(n * 2)
	copy(unsafe.Slice(&info.FileName[0], n+1), name)
	r, _, e := setFileInformationByHandle.Call(uintptr(h), fileRenameInfoEx, uintptr(unsafe.Pointer(info)), uintptr(size))
	if r == 0 {
		return e
	}
	return nil
}

func moveFile(from, to string) error {
	f, e := syscall.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	t, e := syscall.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	r, _, e := moveFileEx.Call(uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(t)), moveReplaceExisting|moveWriteThrough)
	if r == 0 {
		return e
	}
	return nil
}

func transient(e error) bool {
	return errors.Is(e, syscall.ERROR_ACCESS_DENIED) || errors.Is(e, errSharing)
}

// Readers must allow delete sharing so another process can replace the entry
// while this reader retains a handle to the complete previous generation.
func OpenRead(path string) (*os.File, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), path), nil
}
