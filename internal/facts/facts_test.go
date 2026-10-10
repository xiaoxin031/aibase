package facts

import (
	"encoding/json"
	"testing"
)

func TestSchemaRejects(t *testing.T) {
	for _, v := range []struct{ name, raw string }{{"context", `{"cwd":3}`}, {"context", `{"extra":"x"}`}, {"context", `null`}, {"host_inspect", `{"level":"execute"}`}, {"skill_show", `{}`}, {"context", `[]`}} {
		if _, e := Call(v.name, json.RawMessage(v.raw)); e == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
}
