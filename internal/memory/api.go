package memory

import "path/filepath"

var Version = version

func Snapshot() string { return snapshot(detectProject(cwd())) }
func Home() string     { return home() }

func SnapshotAt(dir string) string { return snapshot(detectProject(dir)) }

func Truncate(s string, n int) string { return truncate(s, n) }

// ProjectRoot is the git root containing dir (or dir itself); "" for the home directory.
func ProjectRoot(dir string) string {
	if p := detectProject(dir); p != nil {
		return p.Root
	}
	return ""
}

// ArchiveCounts reports how many notes and session logs are stored.
func ArchiveCounts() (notes, logs int) {
	n, _ := filepath.Glob(filepath.Join(notesDir(), "*.md"))
	if f, err := openRead(historyPath()); err == nil {
		logs, _ = countHistory(f)
		f.Close()
	}
	return len(n), logs
}
