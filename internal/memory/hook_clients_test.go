package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStopProtocols(t *testing.T) {
	h := t.TempDir()
	t.Setenv("AIBASE_HOME", h)
	os.WriteFile(filepath.Join(h, "config.json"), []byte(`{"review_every":1}`), 0600)
	cases := []struct{ agent, input, key string }{{"cursor", `{"conversation_id":"c1","status":"completed","loop_count":0}`, "followup_message"}, {"cursor", `{"conversation_id":"c1","status":"completed","loop_count":1}`, ""}, {"cursor", `{"conversation_id":"c1","status":"error"}`, ""}, {"gemini", `{"session_id":"g1","prompt_response":"done"}`, "reason"}, {"gemini", `{"session_id":"g1","prompt_response":"done","stop_hook_active":true}`, ""}}
	for _, c := range cases {
		out, e := stopHook(c.agent, strings.NewReader(c.input))
		if e != nil {
			t.Fatal(e)
		}
		if c.key == "" {
			if out != "" {
				t.Fatal(out)
			}
			continue
		}
		var v map[string]string
		if json.Unmarshal([]byte(out), &v) != nil || v[c.key] == "" {
			t.Fatal(out)
		}
		if c.agent == "gemini" && v["decision"] != "deny" {
			t.Fatal(out)
		}
	}
}
