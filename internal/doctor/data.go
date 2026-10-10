package doctor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xiaoxin031/aibase/internal/redact"
	"github.com/xiaoxin031/aibase/internal/skills"
)

func dataChecks(root string) []Check {
	out := []Check{}
	entries, total := 0, 0
	cut := false
	add := func(name, detail, fix string) {
		detail, _ = redact.Text(detail)
		fix, _ = redact.Text(fix)
		out = append(out, Check{name, "warn", detail, fix})
	}
	var walk func(string, int)
	walk = func(dir string, depth int) {
		if depth > 4 {
			cut = true
			return
		}
		if e := skills.Guard(dir); e != nil {
			add("data", e.Error(), "Inspect the data directory path")
			return
		}
		f, e := skills.OpenDir(dir)
		if os.IsNotExist(e) {
			return
		}
		if e != nil {
			add("data", e.Error(), "Inspect data permissions")
			return
		}
		defer f.Close()
		for {
			ds, e := f.ReadDir(32)
			for _, d := range ds {
				entries++
				if entries > 256 || total >= 4<<20 {
					cut = true
					return
				}
				p := filepath.Join(dir, d.Name())
				if d.Type()&os.ModeSymlink != 0 {
					add("data", "symlink excluded: "+p, "Inspect the link before removing it")
					continue
				}
				if redact.Forbidden(p) {
					continue
				}
				st, err := d.Info()
				if err != nil {
					continue
				}
				if runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0 {
					mode := "600"
					if st.IsDir() {
						mode = "700"
					}
					add("permissions", "accessible to other users: "+p, "chmod "+mode+" "+shellQuote(p))
				}
				if d.IsDir() {

					walk(p, depth+1)
					if cut {
						return
					}
					continue
				}
				if !(strings.HasSuffix(d.Name(), ".md") || d.Name() == "history.jsonl") {
					continue
				}
				b, err := skills.Read(p)
				if err != nil {
					add("secrets", "not scanned (bounded read): "+p, "aibase redact scan")
					continue
				}
				total += len(b)
				for _, m := range redact.Matches(string(b)) {
					if len(out) >= 128 {
						cut = true
						return
					}
					add("secrets", fmt.Sprintf("%s:%d:%s", p, 1+strings.Count(string(b[:m.Start]), "\n"), m.Type), "aibase redact scan")
				}
			}
			if e == io.EOF {
				return
			}
			if e != nil {
				add("data", e.Error(), "Inspect data permissions")
				return
			}
		}
	}
	walk(root, 0)
	if cut {
		add("data", "check truncated: 256 entries / 4 MiB / depth 4 / 128 findings", "aibase redact scan")
	}
	return out
}
