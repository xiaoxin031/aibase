// Package audit records metadata only. Recording failures never stop a request.
package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/fileio"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaoxin031/aibase/internal/redact"
	"github.com/xiaoxin031/aibase/internal/skills"
)

const MaxSize = 5 * 1024 * 1024

var mu sync.Mutex

type Entry struct {
	TS         string `json:"ts"`
	Client     string `json:"client"`
	Action     string `json:"action"`
	Bytes      int    `json:"bytes"`
	Redactions int    `json:"redactions"`
}

func Record(home, client, action string, bytes, redactions int) {
	mu.Lock()
	defer mu.Unlock()
	if skills.Guard(home) != nil {
		return
	}
	if os.MkdirAll(home, 0700) != nil {
		return
	}
	p := filepath.Join(home, "audit.jsonl")
	if skills.Guard(p) != nil {
		return
	}
	if st, e := os.Lstat(p); e == nil && !st.Mode().IsRegular() {
		return
	}
	lockPath := filepath.Join(home, ".audit.lock")
	if st, err := os.Lstat(lockPath); err == nil && time.Since(st.ModTime()) > 30*time.Second {
		_ = os.Remove(lockPath)
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	lock.Close()
	defer os.Remove(lockPath)

	b, err := json.Marshal(Entry{time.Now().UTC().Format(time.RFC3339Nano), client, action, bytes, redactions})
	if err != nil {
		return
	}
	b = append(b, '\n')
	if st, err := os.Stat(p); err == nil && st.Size()+int64(len(b)) > MaxSize {
		if fileio.Replace(p, p+".1") != nil {
			return
		}
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(b)
}
func Read(home string, n int) (string, error) {
	n = min(max(n, 1), 10000)
	var lines []string
	for _, name := range []string{"audit.jsonl.1", "audit.jsonl"} {
		path := filepath.Join(home, name)
		if err := redact.CheckPath(path); err != nil {
			return "", err
		}
		if e := skills.Guard(path); e != nil {
			return "", e
		}
		if st, e := os.Lstat(path); e == nil && !st.Mode().IsRegular() {
			return "", fmt.Errorf("audit file is not bounded regular data")
		}
		f, err := fileio.OpenRead(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if st, e := f.Stat(); e == nil && st.Size() > MaxSize {
			f.Seek(st.Size()-MaxSize, 0)
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			lines = append(lines, sc.Text())
			if len(lines) > n {
				lines = lines[1:]
			}
		}
		err = sc.Err()
		f.Close()
		if err != nil {
			return "", err
		}
	}
	return strings.Join(lines, "\n"), nil
}
