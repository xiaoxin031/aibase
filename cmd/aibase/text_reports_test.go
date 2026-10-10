package main

import (
	"strings"
	"testing"
)

func TestCollectionText(t *testing.T) {
	for _, c := range []struct{ cmd, in, want string }{
		{"projects", `{"status":"ok","value":{"projects":[{"name":{"value":"demo","status":"ok"},"path":{"value":"/work/demo","status":"ok"}}]}}`, "demo: /work/demo"},
		{"skill", `[{"name":"demo","description":"A skill"}]`, "demo: A skill"},
		{"skill", `"created fixture"`, "created fixture"},
	} {
		out, ok := collectionText(c.cmd, c.in)
		if humanFacts(c.cmd, c.in) != c.want {
			t.Fatal("human dispatcher did not render collection")
		}
		if !ok || out != c.want || strings.HasPrefix(out, "{") {
			t.Fatal(out, ok)
		}
	}
}
