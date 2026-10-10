package memory

import (
	"encoding/json"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/skills"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Claude Code edit tools, and Codex apply_patch payloads.
var editRe = regexp.MustCompile(`"name":\s*"(Edit|Write|MultiEdit|NotebookEdit|write_file|replace|edit_file)"|\*\*\* Begin Patch`)

const reviewPrompt = `[aibase auto-save] Before stopping, review the conversation since the last auto-save. If it produced anything worth keeping across sessions (user preferences or corrections, decisions and why, gotchas, project conventions, environment facts), record it now; otherwise skip the memory update.
- Durable facts: ONE batch, terse, one fact per entry; replace or merge stale/overlapping entries instead of piling up:
  aibase save <<'EOF'
  [{"target":"project|memory|user","ops":[{"op":"add","text":"..."},{"op":"replace","old":"substring","text":"..."}]}]
  EOF
- Always refresh this session's running summary (3-6 lines covering the whole session: what was done, files changed, decisions, open TODOs; it replaces the previous one):
  aibase log --session %s --agent %s <<'EOF'
  ...
  EOF
If this multi-step workflow succeeded and is reusable, record it with ` + "`aibase skill new --draft <name>`" + `; drafts are unverified and do not authorize script execution.
Write in the user's language. Do not repeat your previous answer; finish with one short line naming what was remembered.`

type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	StopHookActive bool   `json:"stop_hook_active"`
	ConversationID string `json:"conversation_id"`
	LoopCount      int    `json:"loop_count"`
	Status         string `json:"status"`
	PromptResponse string `json:"prompt_response"`
}

type reviewState struct {
	Offset int64 `json:"offset"`
	Turns  int   `json:"turns"`
	Edited bool  `json:"edited"`
}

func reviewEvery() int {
	var cfg struct {
		ReviewEvery *int `json:"review_every"`
	}
	if b, err := skills.Read(filepath.Join(home(), "config.json")); err == nil && json.Unmarshal(b, &cfg) == nil && cfg.ReviewEvery != nil {
		return *cfg.ReviewEvery
	}
	return 8
}

// stopHook decides after each turn whether the agent should save memory:
// right after a turn that edited files, or every N turns otherwise.
// The review runs inside the live session, so it reuses its prompt cache.
func stopHook(agent string, r io.Reader) (string, error) {
	var in hookInput
	if b, err := io.ReadAll(io.LimitReader(r, (1<<20)+1)); err != nil || len(b) > 1<<20 || json.Unmarshal(b, &in) != nil {
		return "", nil
	}
	if agent == "cursor" || agent == "cursor-agent" {
		in.SessionID = in.ConversationID
		if in.Status != "completed" || in.LoopCount > 0 {
			return "", nil
		}
	}
	if agent == "gemini" && (in.StopHookActive || in.PromptResponse == "") {
		return "", nil
	}
	if in.SessionID == "" {
		return "", nil
	}
	every := reviewEvery()
	if every < 0 {
		return "", nil
	}
	dir := filepath.Join(home(), "state")
	path := filepath.Join(dir, sanitize(in.SessionID)+".json")
	var st reviewState
	if b, err := skills.Read(path); err == nil {
		json.Unmarshal(b, &st)
	} else {
		pruneState(dir)
	}

	var size int64
	if in.TranscriptPath != "" {
		if f, err := openRegular(in.TranscriptPath); err == nil {
			if fi, err := f.Stat(); err == nil {
				size = fi.Size()
				if size > st.Offset {
					f.Seek(st.Offset, io.SeekStart)
					if b, err := io.ReadAll(io.LimitReader(f, 256<<10)); err == nil && editRe.Match(b) {
						st.Edited = true
					}
				}
			}
			f.Close()
		}
	}
	st.Offset = max(st.Offset, size)

	out := ""
	switch {
	case in.StopHookActive:
		st.Turns, st.Edited = 0, false
	default:
		st.Turns++
		if st.Edited || (every > 0 && st.Turns >= every) {
			st.Turns, st.Edited = 0, false
			prompt := fmt.Sprintf(reviewPrompt, sanitize(in.SessionID), sanitize(agent))
			response := map[string]string{"decision": "block", "reason": prompt}
			if agent == "gemini" {
				response["decision"] = "deny"
			}
			if agent == "cursor" || agent == "cursor-agent" {
				response = map[string]string{"followup_message": prompt}
			}
			b, _ := json.Marshal(response)
			out = string(b)
		}
	}
	b, _ := json.Marshal(st)
	if err := writeAtomic(path, string(b)); err != nil {
		return "", err
	}
	return out, nil
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return '_'
	}, s)
}

func pruneState(dir string) {
	f, e := skills.OpenDir(dir)
	if e != nil {
		return
	}
	defer f.Close()
	ds, _ := f.ReadDir(128)
	for _, d := range ds {
		if !strings.HasSuffix(d.Name(), ".json") || !d.Type().IsRegular() {
			continue
		}
		if st, e := d.Info(); e == nil && time.Since(st.ModTime()) > 14*24*time.Hour {
			_ = os.Remove(filepath.Join(dir, d.Name()))
		}
	}
}
