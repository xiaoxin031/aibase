package host

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/xiaoxin031/aibase/internal/cache"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNormalizedHardware(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"memory_total", "16000000000", "16"}, {"memory_total", "15625000 kB", "16"},
		{"disk", "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/data 1000000 400000 600000 40% /System/Volumes/Data", `"free_gb":0.6144`},
		{"disk", `{"DeviceID":"C:","FreeSpace":600000000,"Size":1000000000}`, `"used_gb":0.4`},
		{"gpu", `{"SPDisplaysDataType":[{"sppci_model":"Apple M1 Max"}]}`, `["Apple M1 Max"]`},
		{"gpu", `[{"Name":"GPU one"},{"Name":"GPU two"}]`, `["GPU one","GPU two"]`},
	} {
		v, e := normalize(tc.name, tc.in)
		b, _ := json.Marshal(v)
		if e != nil || !strings.Contains(string(b), tc.want) {
			t.Fatalf("%s: %s %v", tc.name, b, e)
		}
	}
	for _, name := range []string{"disk", "gpu", "memory_total"} {
		if _, e := normalize(name, "bad"); e == nil {
			t.Fatal(name)
		}
	}
}
func TestCompactVersions(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{{"codex", "WARNING: helper aliases unavailable\ncodex-cli 1.2.3", "1.2.3"}, {"gradle", "\n------------------------------------------------------------\nGradle 8.14.3\n------------------------------------------------------------", "8.14.3"}, {"ffmpeg", "ffmpeg version 7.1.1 Copyright\nconfiguration: huge", "7.1.1"}, {"kubectl", "Client Version: v1.32.2\nKustomize Version: v5.5.0", "1.32.2"}, {"java", "java version \"21.0.2\"\nRuntime Environment", "21.0.2"}, {"go", "go version go1.24.2 darwin/arm64", "1.24.2"}} {
		if got := toolVersion(tc.name, tc.in); got != tc.want {
			t.Fatal(got)
		}
	}
}
func TestSensitivePlatformAndText(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		s := strings.Join(sensitivePaths(platform), " ")
		if platform != "windows" && strings.Contains(s, "AppData") {
			t.Fatal(s)
		}
		if platform != "darwin" && strings.Contains(s, "Library/") {
			t.Fatal(s)
		}
	}
	r := Report{"os": cache.New("test", "", time.Hour, cache.OK), "sensitive.exists": cache.New(true, "", 0, cache.OK), "sensitive.absent": cache.New(false, "", 0, cache.Missing), "tool.java": cache.New(map[string]any{"reason": "stub, no JDK"}, "", 0, cache.Missing)}
	s := Text(r)
	if !strings.Contains(s, "exists: exists") || strings.Contains(s, "absent") || strings.Contains(s, "stub, no JDK") {
		t.Fatal(s)
	}
}

type stubRunner struct{ calls []string }

func (r *stubRunner) LookPath(n string) (string, error) { return "/usr/bin/" + n, nil }
func (r *stubRunner) Run(ctx context.Context, dir, n string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, n)
	return nil, fmt.Errorf("not installed")
}
func TestDarwinStubs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS stubs")
	}
	i, _ := fixture(t)
	r := &stubRunner{}
	i.Runner = r
	if s := i.stub(context.Background(), "java", "/usr/bin/java"); s != "stub, no JDK" {
		t.Fatal(s)
	}
	if s := i.stub(context.Background(), "python3", "/usr/bin/python3"); !strings.Contains(s, "stub") {
		t.Fatal(s)
	}
	if s := i.stub(context.Background(), "java", "/opt/bin/java"); s != "" {
		t.Fatal(s)
	}
	for _, n := range r.calls {
		if n == "/usr/bin/java" || n == "/usr/bin/python3" {
			t.Fatal("executed stub")
		}
	}
}

func TestSummaryContainsOnlyAvailableFacts(t *testing.T) {
	r := Report{}
	for k, v := range map[string]any{"os": "linux", "os_version": "Example 1", "architecture": "arm64", "cpu_model": "Example CPU", "cpu_cores": 8, "memory_total": 16.0, "disk": map[string]any{"free_gb": 12.5}, "gpu": []string{"GPU X"}, "shell": "/bin/sh", "ssh": true, "container": true, "wsl": true, "root": true, "tool.go": map[string]any{"version": "1.24.0"}, "tool.docker": map[string]any{"version": "28.0.0"}, "tool.ffmpeg": map[string]any{"version": "7.1"}, "tool.codex": map[string]any{"version": "1.0"}} {
		r[k] = cache.New(v, "fixture", time.Hour, cache.OK)
	}
	r["tool.java"] = cache.New(nil, "fixture", time.Hour, cache.Missing)
	s := Summary(r)
	for _, want := range []string{"Example 1/arm64", "Example CPU (8 cores)", "RAM 16.0GB", "disk free 12.5GB", "GPU X", "shell /bin/sh", "ssh", "container", "wsl", "root", "go 1.24.0", "docker 28.0.0", "ffmpeg 7.1", "codex 1.0"} {
		if !strings.Contains(s, want) {
			t.Fatal(want, s)
		}
	}
	if strings.Contains(s, "java") || strings.Contains(s, "\n") {
		t.Fatal(s)
	}
}
