//go:build windows

package host

import (
	"encoding/binary"
	"net/netip"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var getExtendedTcpTable = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

const tcpTableOwnerPIDListener = 3

func (i *Inspector) listeners() probe {
	return native("listeners", "GetExtendedTcpTable", time.Minute, func() (any, error) {
		names := processNames()
		rows := []RuntimeListener{}
		for _, af := range []uint32{syscall.AF_INET, syscall.AF_INET6} {
			b, e := tcpTable(af)
			if e != nil {
				return nil, e
			}
			rowSize, addrLen := 24, 4
			if af == syscall.AF_INET6 {
				rowSize, addrLen = 56, 16
			}
			if len(b) < 4 {
				continue
			}
			n := int(binary.LittleEndian.Uint32(b))
			for k := 0; k < n && 4+(k+1)*rowSize <= len(b); k++ {
				r := b[4+k*rowSize : 4+(k+1)*rowSize]
				var addr netip.Addr
				var portField, pid uint32
				if af == syscall.AF_INET {
					addr = netip.AddrFrom4([4]byte(r[4:8]))
					portField, pid = binary.LittleEndian.Uint32(r[8:12]), binary.LittleEndian.Uint32(r[20:24])
				} else {
					addr = netip.AddrFrom16([16]byte(r[0:addrLen]))
					portField, pid = binary.LittleEndian.Uint32(r[20:24]), binary.LittleEndian.Uint32(r[52:56])
				}
				port := uint64(portField&0xff)<<8 | uint64(portField>>8&0xff)
				name := names[pid]
				if name == "" {
					name = "unknown"
				}
				rows = append(rows, RuntimeListener{port, "tcp", addr.String(), name, int(pid)})
			}
		}
		return listenersSorted(rows), nil
	})
}

func tcpTable(af uint32) ([]byte, error) {
	size := uint32(16 << 10)
	for range 4 {
		b := make([]byte, size)
		r, _, _ := getExtendedTcpTable.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(af), tcpTableOwnerPIDListener, 0)
		if r == 0 {
			return b[:size], nil
		}
		if syscall.Errno(r) != syscall.ERROR_INSUFFICIENT_BUFFER {
			return nil, syscall.Errno(r)
		}
	}
	return nil, syscall.ERROR_INSUFFICIENT_BUFFER
}

func processNames() map[uint32]string {
	out := map[uint32]string{}
	h, e := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if e != nil {
		return out
	}
	defer syscall.CloseHandle(h)
	var p syscall.ProcessEntry32
	p.Size = uint32(unsafe.Sizeof(p))
	for e = syscall.Process32First(h, &p); e == nil; e = syscall.Process32Next(h, &p) {
		out[p.ProcessID] = strings.TrimSuffix(syscall.UTF16ToString(p.ExeFile[:]), ".exe")
	}
	return out
}
