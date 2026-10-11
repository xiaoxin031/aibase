package clients

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/xiaoxin031/aibase/internal/memory"
)

// Codex refuses to run user hooks until hooks.state records the hash it computes
// over its own normalized config. Asking codex app-server (the backend of
// /hooks) for that hash avoids reimplementing it.
var codexBin = "codex"

type codexHook struct {
	Key         string `json:"key"`
	Command     string `json:"command"`
	SourcePath  string `json:"sourcePath"`
	CurrentHash string `json:"currentHash"`
	TrustStatus string `json:"trustStatus"`
	Enabled     bool   `json:"enabled"`
}

type codexRPC struct {
	in  io.Writer
	out *bufio.Reader
	id  int
}

func (r *codexRPC) call(method string, params any) (json.RawMessage, error) {
	r.id++
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err = r.in.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	for {
		line, err := r.out.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("codex %s: %w", method, err)
		}
		var m struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(line, &m) != nil || m.Method != "" || m.ID == nil || *m.ID != r.id {
			continue
		}
		if m.Error != nil {
			return nil, fmt.Errorf("codex %s: %s", method, m.Error.Message)
		}
		return m.Result, nil
	}
}

func (r *codexRPC) notify(method string) error {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	_, err := r.in.Write(append(b, '\n'))
	return err
}

func withCodex(timeout time.Duration, fn func(*codexRPC) error) error {
	exe, err := exec.LookPath(codexBin)
	if err != nil {
		return errors.New("codex not on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "app-server")
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	defer func() {
		in.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()
	r := &codexRPC{in: in, out: bufio.NewReader(out)}
	if _, err = r.call("initialize", map[string]any{"clientInfo": map[string]string{"name": "aibase", "version": memory.Version}}); err != nil {
		return err
	}
	if err = r.notify("initialized"); err != nil {
		return err
	}
	return fn(r)
}

func aibaseCodexHooks(r *codexRPC, hooksPath string) ([]codexHook, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	res, err := r.call("hooks/list", map[string]any{"cwds": []string{home}})
	if err != nil {
		return nil, err
	}
	var v struct {
		Data []struct {
			Hooks []codexHook `json:"hooks"`
		} `json:"data"`
	}
	if err = json.Unmarshal(res, &v); err != nil {
		return nil, err
	}
	want, err := os.Stat(hooksPath)
	if err != nil {
		return nil, err
	}
	var hooks []codexHook
	for _, d := range v.Data {
		for _, h := range d.Hooks {
			if st, e := os.Stat(h.SourcePath); e == nil && os.SameFile(st, want) && isHook(h.Command, "aibase") {
				hooks = append(hooks, h)
			}
		}
		if len(hooks) > 0 {
			break
		}
	}
	return hooks, nil
}

// trustCodexHooks marks aibase's hooks in hooksPath trusted and enabled, the
// same edit /hooks makes. Failure is reported, not fatal: /hooks still works.
func (c manager) trustCodexHooks(hooksPath string) string {
	const manual = " — in Codex run /hooks once and trust + enable both aibase hooks"
	if c.dryRun {
		if _, err := exec.LookPath(codexBin); err != nil {
			return "skipped Codex hook trust (codex not on PATH)" + manual
		}
		return "would trust aibase hooks via codex app-server"
	}
	var msg string
	err := withCodex(15*time.Second, func(r *codexRPC) error {
		hooks, err := aibaseCodexHooks(r, hooksPath)
		if err != nil {
			return err
		}
		if len(hooks) == 0 {
			return errors.New("Codex does not list the aibase hooks")
		}
		edits := map[string]any{}
		for _, h := range hooks {
			if h.TrustStatus != "trusted" || !h.Enabled {
				edits[h.Key] = map[string]any{"trusted_hash": h.CurrentHash, "enabled": true}
			}
		}
		if len(edits) == 0 {
			msg = "unchanged Codex hook trust"
			return nil
		}
		cfg := filepath.Join(clientDir("codex"), "config.toml")
		if b, err := os.ReadFile(cfg); err == nil {
			if err = backupOnly(cfg, b); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err = r.call("config/batchWrite", map[string]any{
			"edits":            []any{map[string]any{"keyPath": "hooks.state", "value": edits, "mergeStrategy": "upsert"}},
			"reloadUserConfig": true,
		}); err != nil {
			return err
		}
		if hooks, err = aibaseCodexHooks(r, hooksPath); err != nil {
			return err
		}
		for _, h := range hooks {
			if h.TrustStatus != "trusted" || !h.Enabled {
				return fmt.Errorf("Codex still reports %s as %s", h.Key, h.TrustStatus)
			}
		}
		msg = fmt.Sprintf("updated Codex hook trust (%d hooks, via codex app-server)", len(edits))
		return nil
	})
	if err != nil {
		return "skipped Codex hook trust (" + err.Error() + ")" + manual
	}
	return msg
}

// CodexHookTrust reports "trusted", "untrusted" or "unknown" for aibase's
// Codex hooks, as Codex itself sees them.
func CodexHookTrust() string {
	status := "unknown"
	withCodex(5*time.Second, func(r *codexRPC) error {
		hooks, err := aibaseCodexHooks(r, filepath.Join(clientDir("codex"), "hooks.json"))
		if err != nil || len(hooks) == 0 {
			return err
		}
		status = "trusted"
		for _, h := range hooks {
			if h.TrustStatus != "trusted" || !h.Enabled {
				status = "untrusted"
			}
		}
		return nil
	})
	return status
}
