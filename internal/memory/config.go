package memory

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/skills"
	"io"
	"os"
	"path/filepath"
	"time"
)

type Settings struct {
	Inject      bool `json:"inject"`
	Autosave    bool `json:"autosave"`
	ReviewEvery int  `json:"review_every"`
}

func ReadSettings() (Settings, error) {
	c := Settings{Inject: true, Autosave: true, ReviewEvery: 8}
	var raw struct {
		Inject      *bool `json:"inject"`
		ReviewEvery *int  `json:"review_every"`
	}
	b, e := skills.Read(filepath.Join(home(), "config.json"))
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &raw); e != nil {
		return c, e
	}
	if raw.Inject != nil {
		c.Inject = *raw.Inject
	}
	if raw.ReviewEvery != nil {
		c.ReviewEvery = *raw.ReviewEvery
	}
	c.Autosave = c.ReviewEvery >= 0
	return c, nil
}
func InjectionEnabled() bool { c, e := ReadSettings(); return e == nil && c.Inject }

// SetSetting preserves unknown fields and backs up an existing configuration.
func SetSetting(key string, enabled bool) (Settings, error) {
	if key != "inject" && key != "autosave" {
		return Settings{}, fmt.Errorf("setting must be inject or autosave")
	}
	unlock, e := lock()
	if e != nil {
		return Settings{}, e
	}
	defer unlock()
	p := filepath.Join(home(), "config.json")
	b, e := skills.Read(p)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return Settings{}, e
	}
	if e == nil {
		if _, validationErr := ReadSettings(); validationErr != nil {
			return Settings{}, validationErr
		}
	}
	raw := map[string]json.RawMessage{}
	if len(b) > 0 {
		if e = json.Unmarshal(b, &raw); e != nil {
			return Settings{}, e
		}
		if raw == nil {
			return Settings{}, fmt.Errorf("config must be an object")
		}
	}
	if key == "inject" {
		raw[key], _ = json.Marshal(enabled)
	} else {
		n := -1
		if enabled {
			n = 8
		}
		raw["review_every"], _ = json.Marshal(n)
	}
	next, e := json.MarshalIndent(raw, "", "  ")
	if e != nil {
		return Settings{}, e
	}
	if len(b) > 0 {
		if e = writeAtomic(p+".bak-"+time.Now().UTC().Format("20060102T150405.000000000Z"), string(b)); e != nil {
			return Settings{}, e
		}
	}
	if e = writeAtomic(p, string(next)+"\n"); e != nil {
		return Settings{}, e
	}
	return ReadSettings()
}

// RecentSessions reads only a bounded tail, newest first, without modifying state.
func RecentSessions() ([]logEntry, error) {
	out := []logEntry{}
	f, e := openRegular(historyPath())
	if errors.Is(e, os.ErrNotExist) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return out, e
	}
	start := max(int64(0), st.Size()-(1<<20))
	b := make([]byte, st.Size()-start)
	_, e = f.ReadAt(b, start)
	if e != nil && e != io.EOF {
		return out, e
	}
	seen := map[string]bool{}
	lines := bytes.Split(b, []byte("\n"))
	if start > 0 {
		lines = lines[1:]
	}
	for j := len(lines) - 1; j >= 0 && len(out) < 5; j-- {
		var row logEntry
		if json.Unmarshal(lines[j], &row) != nil || row.Text == "" {
			continue
		}
		if row.Session != "" {
			if seen[row.Session] {
				continue
			}
			seen[row.Session] = true
		}
		out = append(out, row)
	}
	return out, nil
}
