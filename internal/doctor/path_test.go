package doctor

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistentPathAdvice(t *testing.T) {
	h := t.TempDir()
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("ZDOTDIR", "/custom/zsh")
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	for _, c := range []struct{ os, shell, want string }{{"darwin", "/bin/zsh", "/custom/zsh/.zshrc"}, {"linux", "/bin/bash", ".bashrc"}, {"darwin", "/bin/bash", ".bash_profile"}, {"linux", "/bin/fish", "/custom/config/fish/config.fish"}, {"linux", "/bin/sh", ".profile"}, {"windows", "", "SetEnvironmentVariable"}} {
		got := pathAdvice(c.os, c.shell, "/a path/bin")
		if !strings.Contains(filepath.ToSlash(got), c.want) {
			t.Fatal(got)
		}
		if c.os == "windows" && (!strings.Contains(got, "-notcontains") || !strings.Contains(got, "'User'")) {
			t.Fatal(got)
		}
	}
}
