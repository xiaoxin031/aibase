package memory

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
