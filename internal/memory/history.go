package memory

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// reverseLines holds at most one record and one block, not the whole archive.
func reverseLines(f *os.File, visit func([]byte) bool) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	pos := st.Size()
	var pending []byte
	for pos > 0 {
		n := min(pos, int64(32*1024))
		pos -= n
		b := make([]byte, int(n))
		if _, err = f.ReadAt(b, pos); err != nil && err != io.EOF {
			return err
		}
		b = append(b, pending...)
		end := len(b)
		for i := len(b) - 1; i >= 0; i-- {
			if b[i] == '\n' {
				if i+1 < end && !visit(b[i+1:end]) {
					return nil
				}
				end = i
			}
		}
		pending = bytes.Clone(b[:end])
	}
	if len(pending) > 0 {
		visit(pending)
	}
	return nil
}

type historyCount struct {
	Version  int   `json:"version"`
	Size     int64 `json:"size"`
	Modified int64 `json:"modified"`
	Count    int   `json:"count"`
}

func recentHistory(p *project) ([]logEntry, int) {
	return recentHistoryWithDecoder(p, json.Unmarshal)
}

func recentHistoryWithDecoder(p *project, decode func([]byte, any) error) ([]logEntry, int) {
	f, err := openRead(historyPath())
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, 0
	}
	cachePath := filepath.Join(home(), "state", "history-count.json")
	var cached historyCount
	b, _ := readFile(cachePath)
	valid := json.Unmarshal(b, &cached) == nil && cached.Version == 1 && cached.Size == st.Size() && cached.Modified == st.ModTime().UnixNano()
	count := cached.Count
	if !valid {
		count, err = countHistory(f)
		if err != nil {
			return nil, 0
		}
		cached = historyCount{Version: 1, Size: st.Size(), Modified: st.ModTime().UnixNano(), Count: count}
		b, _ = json.Marshal(cached)
		_ = writeAtomic(cachePath, string(b))
	}
	return readRecentHistory(f, p, decode), count
}

// Count nonblank physical lines without decoding JSON or allocating per record.
func countHistory(r io.Reader) (int, error) {
	buf := make([]byte, 32*1024)
	count, nonblank := 0, false
	for {
		n, err := r.Read(buf)
		for _, c := range buf[:n] {
			if c == '\n' {
				if nonblank {
					count++
				}
				nonblank = false
			} else if c != ' ' && c != '\t' && c != '\r' {
				nonblank = true
			}
		}
		if err != nil {
			if err == io.EOF {
				if nonblank {
					count++
				}
				return count, nil
			}
			return count, err
		}
	}
}

func readRecentHistory(f *os.File, p *project, decode func([]byte, any) error) []logEntry {
	var recent []logEntry
	if p == nil {
		return recent
	}
	_ = reverseLines(f, func(line []byte) bool {
		var e logEntry
		if decode(line, &e) == nil && e.Text != "" && e.Project == p.ID {
			recent = append(recent, e)
		}
		return len(recent) < recentLogs
	})
	return recent
}
