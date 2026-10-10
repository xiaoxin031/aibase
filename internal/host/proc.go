package host

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

type Listener struct {
	Address     string    `json:"address"`
	Port        uint64    `json:"port"`
	Inode       string    `json:"inode"`
	Processes   []Process `json:"processes"`
	Attribution string    `json:"attribution"`
}
type Process struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

func procAddress(s string) (string, uint64, error) {
	a, p, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, fmt.Errorf("invalid address")
	}
	b, e := hex.DecodeString(a)
	if e != nil || (len(b) != 4 && len(b) != 16) {
		return "", 0, fmt.Errorf("invalid address")
	}
	for j := 0; j < len(b); j += 4 {
		b[j], b[j+3] = b[j+3], b[j]
		b[j+1], b[j+2] = b[j+2], b[j+1]
	}
	port, e := strconv.ParseUint(p, 16, 16)
	return net.IP(b).String(), port, e
}
func (i *Inspector) linuxListeners(ctx context.Context) (any, error) {
	var out []Listener
	var readErr error
	success := false
	for _, p := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, e := i.FS.ReadFile(p)
		if e != nil {
			readErr = e
			continue
		}
		success = true
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 10 || f[3] != "0A" {
				continue
			}
			a, port, e := procAddress(f[1])
			if e != nil {
				continue
			}
			out = append(out, Listener{Address: a, Port: port, Inode: f[9], Attribution: "missing"})
			if len(out) >= 20000 {
				break
			}
		}
		if len(out) >= 20000 {
			break
		}
	}
	if !success {
		return nil, readErr
	}
	byInode := map[string][]int{}
	for j, l := range out {
		byInode[l.Inode] = append(byInode[l.Inode], j)
	}
	ds, e := i.FS.ReadDir("/proc")
	if e != nil {
		for j := range out {
			out[j].Attribution = string(Status(e))
		}
		return out, nil
	}
	fdBudget := 20000
	denied := false
	defer func() {
		if denied {
			for j := range out {
				if out[j].Attribution == "missing" {
					out[j].Attribution = "denied"
				}
			}
		}
	}()
	for _, d := range ds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pid, e := strconv.Atoi(d.Name())
		if e != nil {
			continue
		}
		base := filepath.Join("/proc", d.Name())
		fds, e := i.FS.ReadDir(filepath.Join(base, "fd"))
		if e != nil {
			if Status(e) == "denied" {
				denied = true
			}
			continue
		}
		name, _ := i.FS.ReadFile(filepath.Join(base, "comm"))
		if exe, e := i.FS.Readlink(filepath.Join(base, "exe")); e == nil {
			name = []byte(filepath.Base(strings.TrimSuffix(exe, " (deleted)")))
		}
		for _, fd := range fds {
			if fdBudget <= 0 {
				return out, nil
			}
			fdBudget--
			target, e := i.FS.Readlink(filepath.Join(base, "fd", fd.Name()))
			if e != nil {
				continue
			}
			inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
			for _, j := range byInode[inode] {
				if len(out[j].Processes) < 8 {
					out[j].Processes = append(out[j].Processes, Process{PID: pid, Name: trim(name)})
					out[j].Attribution = "ok"
				}
			}
		}
	}
	return out, nil
}
func (i *Inspector) normalizedLinuxListeners(ctx context.Context) (any, error) {
	v, e := i.linuxListeners(ctx)
	if e != nil {
		return nil, e
	}
	rows := []RuntimeListener{}
	for _, l := range v.([]Listener) {
		if len(l.Processes) == 0 {
			rows = append(rows, RuntimeListener{l.Port, "tcp", l.Address, "unknown (" + l.Attribution + ")", 0})
		}
		for _, p := range l.Processes {
			rows = append(rows, RuntimeListener{l.Port, "tcp", l.Address, p.Name, p.PID})
		}
	}
	return listenersSorted(rows), nil
}
