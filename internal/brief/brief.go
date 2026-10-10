// Package brief assembles bounded context without version probes or tree scans.
package brief

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xiaoxin031/aibase/internal/cache"
	"github.com/xiaoxin031/aibase/internal/host"
	"github.com/xiaoxin031/aibase/internal/memory"
	"github.com/xiaoxin031/aibase/internal/redact"
	"github.com/xiaoxin031/aibase/internal/skills"
)

type Report struct {
	Memory     string `json:"memory"`
	Machine    string `json:"machine"`
	Project    string `json:"project"`
	Skills     string `json:"skills"`
	Gaps       string `json:"gaps"`
	Text       string `json:"text"`
	Limit      int    `json:"limit"`
	Redactions int    `json:"-"`
}

func Cut(s string, n int, command string) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	suffix := "\n…Details: " + command
	r := []rune(suffix)
	if len(r) >= n {
		return string(r[:n])
	}
	return string([]rune(s)[:n-len(r)]) + suffix
}
func Label(f cache.Fact, now time.Time) string {
	if f.At.IsZero() || f.At.After(now) {
		return "[cached age unknown]"
	}
	s := fmt.Sprintf("[cached %.0fh ago]", now.Sub(f.At).Hours())
	if !f.Fresh(now) {
		s += "[stale]"
	}
	return s
}
func Limit() int {
	var c struct {
		Limit int `json:"brief_limit"`
	}
	b, e := skills.Read(filepath.Join(cache.Home(), "config.json"))
	if e == nil && json.Unmarshal(b, &c) == nil && c.Limit > 0 {
		return min(c.Limit, 100000)
	}
	return 6000
}
func Build(dir string) Report {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	dir, _ = filepath.Abs(dir)
	store := cache.NewStore("")
	now := time.Now()
	machineFresh := false
	machine := "[live] Machine: " + runtime.GOOS + "/" + runtime.GOARCH + fmt.Sprintf("; CPU %d cores", runtime.NumCPU())
	if f, ok := store.GetBounded(MachineKey, 64<<10); ok {
		var r host.Report
		b, _ := json.Marshal(f.Value)
		if json.Unmarshal(b, &r) == nil && r["os"].Status == cache.OK && r["os"].Value != nil {
			machine = Label(f, now) + " Machine: " + host.Summary(r)
			machineFresh = f.Fresh(now)
		}
	}
	root := memory.ProjectRoot(dir)
	if root == "" {
		root = dir
	}
	project := "[live] Project directory: " + root + "\nManifests: "
	var manifests []string
	for _, n := range []string{"go.mod", "package.json", "pom.xml", "build.gradle", "Cargo.toml", "pyproject.toml", "Makefile", "Dockerfile"} {
		if st, e := os.Lstat(filepath.Join(root, n)); e == nil && st.Mode().IsRegular() {
			manifests = append(manifests, n)
		}
	}
	project += strings.Join(manifests, ", ")
	projectFact, projectOK := store.GetBounded(ProjectKey(root), 64<<10)
	if s, ok := projectFact.Value.(string); projectOK && ok && s != "" {
		project += "\n" + Label(projectFact, now) + " " + s
	}

	skillText := "[cache missing] Skill index unavailable; aibase skill ls"
	gaps := "[live] Gaps: tool dependencies unchecked; aibase doctor"
	if !machineFresh {
		gaps += "; machine cache unavailable or over read budget"
	}
	skillFact, skillsOK := store.GetBounded("skills:index:v1", 64<<10)
	doctorFact, doctorOK := store.GetBounded("brief:doctor:"+dir, 64<<10)
	doctorChecks, doctorValid := doctorFact.Value.([]any)
	if !machineFresh || !projectOK || !projectFact.Fresh(now) || !skillsOK || !skillFact.Fresh(now) || !doctorOK || !doctorValid || !doctorFact.Fresh(now) {
		defer startRefresh(store, dir)
	}
	if f, ok := store.GetBounded("skills:index:v1", 64<<10); ok {
		skillText = Label(f, now) + " Skills (details: aibase skill show <name>): " + summaryText(f.Value)
		if m, ok := f.Value.(map[string]any); ok && num(m["total"]) == 0 {
			skillText = ""
		}
	} else {
		gaps += "; skill index cache missing"
	}
	if doctorOK && doctorValid {
		gaps = ""
		if len(doctorChecks) > 0 {
			gaps = Label(doctorFact, now) + " Gaps: " + summaryText(doctorChecks[:min(3, len(doctorChecks))])
		} else if !doctorFact.Fresh(now) {
			gaps = Label(doctorFact, now) + " Gaps: check expired; aibase doctor"
		}
	}

	machine, _ = redact.Text(machine)
	project, _ = redact.Text(project)
	skillText, _ = redact.Text(skillText)
	gaps, _ = redact.Text(gaps)
	r := Report{Machine: strings.ReplaceAll(Cut(machine, 800, "aibase inspect host"), "\n", " "), Project: Cut(project, 600, "aibase inspect project"), Skills: Cut(skillText, 600, "aibase skill ls"), Gaps: Cut(gaps, 400, "aibase doctor"), Limit: Limit()}
	hist := memory.BriefSnapshot(dir)
	if redact.MemoryEnabled(memory.Home()) {
		hist, r.Redactions = redact.Text(hist)
	}
	r.Memory = "[memory] " + hist
	// Reserve the small facts sections first; all separators count toward the cap.
	var sections []string
	for _, s := range []string{r.Machine, r.Project, r.Skills, r.Gaps} {
		if s != "" {
			sections = append(sections, s)
		}
	}
	facts := strings.Join(sections, "\n\n")
	budget := r.Limit - utf8.RuneCountInString(facts) - 2
	if budget < 0 {
		r.Memory = ""
		r.Text = Cut(facts, r.Limit, "aibase context")
		return r
	}
	r.Memory = Cut(r.Memory, budget, "aibase ls")
	r.Text = Cut(r.Memory+"\n\n"+facts, r.Limit, "aibase context")
	return r
}
func RefreshSkills() ([]skills.Skill, error) {
	s, e := skills.List(false)
	if e != nil {
		return nil, e
	}
	return s, putSkillIndex(cache.NewStore(""), s)
}
func putSkillIndex(store *cache.Store, s []skills.Skill) error {
	index := []map[string]string{}
	for _, v := range s[:min(len(s), 32)] {
		index = append(index, map[string]string{"name": v.Name, "description": memory.Truncate(cache.Clean(v.Description).(string), 100)})
	}
	return store.Put("skills:index:v1", cache.New(map[string]any{"skills": index, "total": len(s), "truncated": len(s) > 32}, "skill ls", 24*time.Hour, cache.OK))
}
