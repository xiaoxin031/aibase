package project

import (
	"context"
	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/host"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func remoteURL(s string) string {
	lines := strings.Split(s, "\n")
	for j, l := range lines {
		fields := strings.Fields(l)
		for k, v := range fields {
			if u, e := url.Parse(v); e == nil && u.Scheme != "" {
				u.User = nil
				u.RawQuery = ""
				u.Fragment = ""
				fields[k] = u.String()
			} else if at := strings.LastIndex(v, "@"); at >= 0 {
				fields[k] = v[at+1:]
			}
		}
		lines[j] = strings.Join(fields, " ")
	}
	return strings.Join(lines, "\n")
}
func (i *Inspector) gitRun(ctx context.Context, root string, args ...string) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return i.Runner.Run(ctx, root, "git", append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.untrackedCache=false"}, args...)...)
}
func (i *Inspector) git(ctx context.Context, root string, out Report) {
	keys := []string{"branch", "remotes", "ahead_behind", "dirty_count", "commits"}
	st, e := os.Lstat(filepath.Join(root, ".git"))
	if e != nil {
		for _, k := range keys {
			out["git."+k] = fact(nil, "git", e)
		}
		return
	}
	if st.Mode()&os.ModeSymlink != 0 {
		for _, k := range keys {
			out["git."+k] = cache.New(nil, "git symlink excluded", 0, cache.Denied)
		}
		return
	}
	// Successful porcelain headers distinguish an unborn branch or absent
	// upstream from unrelated git failures without parsing localized fatal text.
	state, stateErr := i.gitRun(ctx, root, "status", "--porcelain=v2", "--branch", "--untracked-files=no")
	noCommits, noUpstream := false, false
	if stateErr == nil && len(state) < host.MaxOutput {
		headers := string(state)
		noCommits = strings.Contains(headers, "# branch.oid (initial)\n")
		noUpstream = strings.Contains(headers, "# branch.oid ") && !strings.Contains(headers, "# branch.upstream ")
	}
	cmds := [][]string{{"branch", "--show-current"}, {"remote", "-v"}, {"rev-list", "--left-right", "--count", "HEAD...@{upstream}"}, {"status", "--porcelain=v1", "-z", "--untracked-files=normal"}, {"log", "-5", "--format=%s"}}
	for j, args := range cmds {
		if keys[j] == "commits" && noCommits {
			out["git.commits"] = fact("no commits yet", "git status --porcelain=v2 --branch", nil)
			continue
		}
		if keys[j] == "ahead_behind" && noUpstream {
			out["git.ahead_behind"] = fact("no upstream", "git status --porcelain=v2 --branch", nil)
			continue
		}
		b, e := i.gitRun(ctx, root, args...)
		var v any = strings.TrimSpace(string(b))
		if e == nil {
			switch keys[j] {
			case "remotes":
				remote := remoteURL(v.(string))
				v = []string{}
				if remote != "" {
					v = strings.Split(remote, "\n")
				}
			case "commits":
				if v == "" {
					v = []string{}
				} else {
					v = strings.Split(v.(string), "\n")
				}
			case "ahead_behind":
				f := strings.Fields(string(b))
				if len(f) == 2 {
					a, _ := strconv.Atoi(f[0])
					z, _ := strconv.Atoi(f[1])
					v = map[string]int{"ahead": a, "behind": z}
				}
			case "dirty_count":
				n := 0
				parts := strings.Split(string(b), "\x00")
				for k := 0; k < len(parts); k++ {
					if len(parts[k]) < 3 {
						continue
					}
					n++
					if strings.ContainsAny(parts[k][:2], "RC") {
						k++
					}
				}
				v = n
			}
		}
		if len(b) >= host.MaxOutput {
			out["git."+keys[j]] = cache.New(nil, "git output truncated", 0, cache.Unsupported)
		} else {
			out["git."+keys[j]] = fact(v, "git "+strings.Join(args, " "), e)
		}
	}
}
