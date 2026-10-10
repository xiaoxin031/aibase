// Package cache stores independently replaceable, redacted facts. TTL is in
// seconds on the wire; At is the observation time, never the read time.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/xiaoxin031/aibase/internal/fileio"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/xiaoxin031/aibase/internal/redact"
)

type Status string

const (
	OK          Status = "ok"
	Missing     Status = "missing"
	Denied      Status = "denied"
	Timeout     Status = "timeout"
	Unsupported Status = "unsupported"
	Stale       Status = "stale"
)

type Fact struct {
	Value  any           `json:"value"`
	Source string        `json:"source"`
	At     time.Time     `json:"at"`
	TTL    time.Duration `json:"ttl"`
	Status Status        `json:"status"`
}

// TTLUnit distinguishes new seconds-based facts from legacy nanosecond caches.
func (f Fact) MarshalJSON() ([]byte, error) {
	type wire Fact
	return json.Marshal(struct {
		wire
		TTL  float64 `json:"ttl"`
		Unit string  `json:"ttl_unit"`
	}{wire(f), f.TTL.Seconds(), "s"})
}
func (f *Fact) UnmarshalJSON(b []byte) error {
	type wire Fact
	var v struct {
		Value  any       `json:"value"`
		Source string    `json:"source"`
		At     time.Time `json:"at"`
		TTL    float64   `json:"ttl"`
		Unit   string    `json:"ttl_unit"`
		Status Status    `json:"status"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if v.Unit != "" && v.Unit != "s" {
		return errors.New("unknown TTL unit")
	}
	n := v.TTL
	if v.Unit == "s" {
		n *= float64(time.Second)
	}
	if n < 0 || n >= float64(1<<63) {
		return errors.New("invalid TTL")
	}
	*f = Fact{v.Value, v.Source, v.At, time.Duration(n), v.Status}
	return nil
}

var secretKey = regexp.MustCompile(`(?i)secret|token|password|passwd|api_key|credential|private`)
var colonSecret = regexp.MustCompile(`(?im)(["']?[a-z_][a-z0-9_]*(?:["']?)[ \t]*:[ \t]*)([^\r\n]+)`)

func cleanText(s string) string {
	s = colonSecret.ReplaceAllStringFunc(s, func(v string) string {
		loc := colonSecret.FindStringSubmatchIndex(v)
		if loc == nil || !secretKey.MatchString(v[loc[2]:loc[3]]) {
			return v
		}
		return v[:loc[4]] + "[REDACTED]"
	})
	s, _ = redact.Text(s)
	return s
}

// Clean also filters map keys: project script names and paths are untrusted.
func Clean(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var x any
	if json.Unmarshal(b, &x) != nil {
		return nil
	}
	var walk func(any) any
	walk = func(x any) any {
		switch t := x.(type) {
		case string:
			return cleanText(t)
		case []any:
			for i := range t {
				t[i] = walk(t[i])
			}
			return t
		case map[string]any:
			m := map[string]any{}
			for k, v := range t {
				s, _ := redact.Text(k)
				if _, ok := v.(string); ok && secretKey.MatchString(k) {
					m[s] = "[REDACTED]"
				} else {
					m[s] = walk(v)
				}
			}
			return m
		}
		return x
	}
	return walk(x)
}
func New(value any, source string, ttl time.Duration, status Status) Fact {
	s, _ := redact.Text(source)
	return Fact{Clean(value), s, time.Now().UTC(), ttl, status}
}
func (f Fact) Fresh(now time.Time) bool {
	return valid(f.Status) && !f.At.IsZero() && f.TTL > 0 && !f.At.After(now) && now.Before(f.At.Add(f.TTL)) && f.Status != Stale
}
func valid(s Status) bool {
	return s == OK || s == Missing || s == Denied || s == Timeout || s == Unsupported || s == Stale
}
func Home() string {
	if h := os.Getenv("AIBASE_HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".aibase")
}

type Store struct{ Dir string }

func NewStore(home string) *Store {
	if home == "" {
		home = Home()
	}
	return &Store{filepath.Join(home, "cache")}
}
func (s *Store) path(key string) string {
	h := sha256.Sum256([]byte(key))
	return filepath.Join(s.Dir, hex.EncodeToString(h[:])+".json")
}

// Get treats malformed/oversized entries as misses. It never deletes on read:
// doing so could unlink a concurrent writer's valid replacement.
func (s *Store) Get(key string) (Fact, bool) { return s.GetBounded(key, 4<<20) }

// GetBounded lets latency-sensitive callers reject large cached facts before reading.
func (s *Store) GetBounded(key string, limit int64) (Fact, bool) {
	if limit <= 0 || limit > 4<<20 {
		limit = 4 << 20
	}
	var f Fact
	p := s.path(key)
	if redact.CheckPath(p) != nil {
		return f, false
	}
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > limit {
		return f, false
	}
	r, err := fileio.OpenRead(p)
	if err != nil {
		return f, false
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || int64(len(b)) > limit || json.Unmarshal(b, &f) != nil || !valid(f.Status) || f.At.IsZero() || f.TTL < 0 {
		return Fact{}, false
	}
	f.Value = Clean(f.Value)
	f.Source, _ = redact.Text(f.Source)
	if !f.Fresh(time.Now()) {
		f.Status = Stale
	}
	return f, true
}

// Put is last-writer-wins per key. Unique temporary files plus OS replacement
// avoid stale lock recovery races and work across processes; no read-modify-write.
func (s *Store) Put(key string, f Fact) error {
	if !valid(f.Status) || f.At.IsZero() || f.TTL < 0 {
		return errors.New("invalid fact")
	}
	f.Value = Clean(f.Value)
	f.Source, _ = redact.Text(f.Source)
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(b) > 4<<20 {
		return errors.New("fact too large")
	}
	if err = redact.CheckPath(s.Dir); err != nil {
		return err
	}
	if err = os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, ".fact-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return fileio.Replace(tmp.Name(), s.path(key))
}
