package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/cache"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRuntimeNormalization(t *testing.T) {
	v, e := runtimeNormalize("listeners", "COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME\nserver 42 me 3u IPv4 0 0t0 TCP 127.0.0.1:8080 (LISTEN)\nserver 42 me 4u IPv6 0 0t0 TCP [::1]:8080 (LISTEN)\nother 50 me 5u IPv4 0 0t0 TCP *:80 (LISTEN)")
	if e != nil {
		t.Fatal(e)
	}
	rows := v.([]RuntimeListener)
	if len(rows) != 2 || rows[0].Port != 80 || rows[1].Bind != "127.0.0.1" {
		t.Fatal(rows)
	}
	b, _ := json.Marshal(v)
	var decoded any
	json.Unmarshal(b, &decoded)
	if got := strings.Join(runtimeText("listeners", decoded), "\n"); !strings.Contains(got, "8080 tcp 127.0.0.1 server(42)") {
		t.Fatal(got)
	}
	v, _ = runtimeNormalize("services", "PID Status Label\n- 0 idle\n3 0 com.apple.foo\n4 0 org.example.server")
	if strings.Join(v.([]string), ",") != "org.example.server" {
		t.Fatal(v)
	}
	v, _ = runtimeNormalize("services", "example.service loaded active running Example server\nstopped.service loaded inactive dead Example")
	if strings.Join(v.([]string), ",") != "example.service" {
		t.Fatal(v)
	}
	v, _ = runtimeNormalize("resources", "PID %CPU %MEM COMM\n3 2.0 1.0 /some path/server\n4 90 5 /bin/busy")
	rs := v.([]Resource)
	if len(rs) != 2 || rs[0].PID != 4 || rs[1].Name != "server" {
		t.Fatal(rs)
	}
	rows = make([]RuntimeListener, 55)
	if got := strings.Join(runtimeText("listeners", rows), "\n"); !strings.Contains(got, "5 more (55 total)") || len(strings.Split(got, "\n")) != 52 {
		t.Fatal(got)
	}
}

type dockerRunner struct{ err error }

func (r dockerRunner) LookPath(string) (string, error) { return "docker", nil }
func (r dockerRunner) Run(context.Context, string, string, ...string) ([]byte, error) {
	return []byte("demo\timage:v1\tUp 2 hours\t127.0.0.1:8080->80/tcp\n"), r.err
}
func TestDockerStates(t *testing.T) {
	i, _ := fixture(t)
	i.Runner = dockerRunner{}
	v, e := i.containers().run(context.Background())
	if e != nil || v.([]Container)[0].Name != "demo" {
		t.Fatal(v, e)
	}
	i.Runner = dockerRunner{errors.New("daemon offline")}
	v, e = i.containers().run(context.Background())
	if e != nil || v.(cache.Fact).Status != cache.Denied {
		t.Fatal(v, e)
	}
	i.Runner = dockerRunner{&exec.Error{Name: "docker", Err: exec.ErrNotFound}}
	_, e = i.containers().run(context.Background())
	if Status(e) != cache.Missing {
		t.Fatal(e)
	}
}

func TestLsofFieldOutputAndWindowsRows(t *testing.T) {
	v, e := runtimeNormalize("listeners", "p12\ncExample App\nf3\nPTCP\nn127.0.0.1:8080\nf4\nPTCP\nn[::1]:8080\n")
	if e != nil || len(v.([]RuntimeListener)) != 1 || v.([]RuntimeListener)[0].Process != "Example App" {
		t.Fatal(v, e)
	}
	v, e = runtimeNormalize("listeners", `[{"port":8080,"proto":"tcp","bind":"::","process":"server.exe","pid":12},{"port":8080,"proto":"tcp","bind":"0.0.0.0","process":"server.exe","pid":12}]`)
	if e != nil || len(v.([]RuntimeListener)) != 1 {
		t.Fatal(v, e)
	}
	v, e = runtimeNormalize("resources", `{"pid":12,"cpu":50.5,"mem":1.5,"name":"C:\\Program Files\\app.exe"}`)
	if e != nil || v.([]Resource)[0].Name != "app.exe" {
		t.Fatal(v, e)
	}
}

type outputRunner struct{ output string }

func (r outputRunner) LookPath(n string) (string, error) { return n, nil }
func (r outputRunner) Run(context.Context, string, string, ...string) ([]byte, error) {
	return []byte(r.output), nil
}
func TestSortedResourcesOverLimit(t *testing.T) {
	i, _ := fixture(t)
	s := "PID %CPU %MEM COMM\n"
	for n := 0; n < 2000; n++ {
		s += fmt.Sprintf("%d %.1f 1.0 %s\n", n+1, 100.0-float64(n)/20, strings.Repeat("x", 40))
	}
	i.Runner = outputRunner{s[:MaxOutput]}
	v, e := i.command("resources", time.Minute, "ps", "-Ac", "-o", "pid,pcpu,pmem,comm", "-r").run(context.Background())
	if e != nil || len(v.([]Resource)) != 10 || v.([]Resource)[0].PID != 1 {
		t.Fatal(v, e)
	}
	v, e = i.command("resources", time.Minute, "ps", "-Ao", "pid,pcpu,pmem,comm").run(context.Background())
	if e != nil || v.(cache.Fact).Status != cache.Unsupported {
		t.Fatal(v, e)
	}
}
func TestLsofCRLF(t *testing.T) {
	v, e := runtimeNormalize("listeners", "p12\r\ncserver\r\nn127.0.0.1:8080\r\n")
	if e != nil || len(v.([]RuntimeListener)) != 1 || v.([]RuntimeListener)[0].PID != 12 {
		t.Fatal(v, e)
	}
}
