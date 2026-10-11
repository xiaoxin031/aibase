package memory

import (
	"bytes"
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

// Tool calls in Claude (tool_use) and Codex (function_call, custom_tool_call)
// transcripts. Codex runs edits through shell tools, so sustained tool use
// stands in for edits it cannot name.
var toolRe = regexp.MustCompile(`"type":\s*"(tool_use|function_call|custom_tool_call)"`)

// Phrases with which users state preferences, decisions or things to keep.
var cueRe = regexp.MustCompile(`(?i)记住|记一下|记下|别忘|以后|今后|下次|一律|始终|总是|永远|默认|偏好|约定|规范|决定|不要再|别再|\b(remember|from now on|going forward|always|never|prefer|don't forget)\b`)

const (
	reviewTools = 4
	scanLimit   = 256 << 10
)

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
	Prompt         string `json:"prompt"`
}

type reviewState struct {
	Offset int64 `json:"offset"`
	Turns  int   `json:"turns"`
	Edited bool  `json:"edited"`
	Tools  int   `json:"tools"`
	Cue    bool  `json:"cue"`
}

// userTexts returns what the user typed in a Claude or Codex transcript
// segment, skipping tool results and client-injected context.
func userTexts(seg []byte) []string {
	var out []string
	for _, line := range bytes.Split(seg, []byte("\n")) {
		if !bytes.Contains(line, []byte(`"user"`)) {
			continue
		}
		var v struct {
			Type    string `json:"type"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Payload struct {
				Type    string          `json:"type"`
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &v) != nil {
			continue
		}
		raw := v.Message.Content
		if v.Message.Role != "user" {
			if v.Payload.Type != "message" || v.Payload.Role != "user" {
				continue
			}
			raw = v.Payload.Content
		}
		var texts []string
		var str string
		var parts []struct{ Type, Text string }
		if json.Unmarshal(raw, &str) == nil {
			texts = append(texts, str)
		} else if json.Unmarshal(raw, &parts) == nil {
			for _, p := range parts {
				if p.Type == "text" || p.Type == "input_text" {
					texts = append(texts, p.Text)
				}
			}
		}
		for _, t := range texts {
			t = strings.TrimSpace(t)
			if t != "" && !strings.HasPrefix(t, "<") && !strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "[aibase") {
				out = append(out, t)
			}
		}
	}
	return out
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

// stopHook decides after each turn whether the agent should save memory: after
// edits, sustained tool use or a user remark worth keeping, else every N turns.
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
					if b, err := io.ReadAll(io.LimitReader(f, scanLimit)); err == nil {
						st.Edited = st.Edited || editRe.Match(b)
						st.Tools += len(toolRe.FindAll(b, -1))
						if size-st.Offset > scanLimit {
							st.Tools = max(st.Tools, reviewTools)
						}
						for _, t := range userTexts(b) {
							st.Cue = st.Cue || cueRe.MatchString(t)
						}
					}
				}
			}
			f.Close()
		}
	}
	st.Offset = max(st.Offset, size)
	st.Cue = st.Cue || cueRe.MatchString(in.Prompt)

	out := ""
	switch {
	case in.StopHookActive:
		st = reviewState{Offset: st.Offset}
	default:
		st.Turns++
		if st.Edited || st.Cue || st.Tools >= reviewTools || (every > 0 && st.Turns >= every) {
			st = reviewState{Offset: st.Offset}
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
