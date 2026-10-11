package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/xiaoxin031/aibase/internal/audit"
	"github.com/xiaoxin031/aibase/internal/clients"
	"github.com/xiaoxin031/aibase/internal/mcp"
	"github.com/xiaoxin031/aibase/internal/memory"
	"github.com/xiaoxin031/aibase/internal/redact"
)

func run(args []string) (string, error) {
	if len(args) == 0 {
		return helpText, nil
	}
	switch args[0] {
	case "status":
		return statusCommand(args[1:])
	case "stats":
		return statsCommand(args[1:])
	case "config":
		return configCommand(args[1:])
	case "setup":
		return setupCommand(args[1:])
	case "help", "-h", "--help":
		if len(args) == 2 && args[1] == "save" {
			return memory.Run(args[0], args[1:])
		}
		return helpText, nil
	case "refresh":
		return refreshCommand(args[1:])
	case "context", "inspect", "projects", "doctor":
		return factCommand(args)
	case "skill":
		return skillCommand(args[1:])
	case "inject":
		return briefText()
	case "hook":
		if len(args) < 2 || args[1] != "stop" {
			return sessionHook(args[1:])
		}

	case "mcp":
		return "", mcp.Serve(os.Stdin, os.Stdout)
	case "install", "uninstall":
		out, e := clients.Run(args[1:], args[0] == "uninstall")
		if args[0] == "install" && !slices.Contains(args, "--dry-run") && !slices.Contains(args, "--print-agents-md") {
			re := syncRefresh()
			if re != nil {
				out += "\nfailed refresh: " + re.Error()
			} else {
				out += "\nsuccess refresh: tools and skill cache"
			}
			e = errors.Join(e, re)
		}
		return out, e
	case "migrate":
		if len(args) != 2 || args[1] != "amem" {
			return "", fmt.Errorf("usage: aibase migrate amem")
		}
		return memory.Migrate()
	case "redact":
		if len(args) < 2 || args[1] != "scan" || len(args) > 3 {
			return "", fmt.Errorf("usage: aibase redact scan [path]")
		}
		path := memory.Home()
		if len(args) == 3 {
			path = args[2]
		}
		return redact.Scan(path)
	case "audit":
		n := 20
		if len(args) > 1 {
			if len(args) != 3 || args[1] != "-n" {
				return "", fmt.Errorf("usage: aibase audit [-n N]")
			}
			var err error
			n, err = strconv.Atoi(args[2])
			if err != nil || n < 1 {
				return "", fmt.Errorf("N must be positive")
			}
		}
		return audit.Read(memory.Home(), n)
	}
	out, err := memory.Run(args[0], args[1:])
	if err != nil && strings.HasPrefix(err.Error(), "unknown command ") {
		return "", fmt.Errorf("unknown command %q\n\n%s", args[0], helpText)
	}
	return out, err
}
func memoryOutput(cmd string) bool {
	switch cmd {
	case "inject", "hook", "ls", "show", "search":
		return true
	}
	return false
}
func emit(w io.Writer, s string, structured, filter bool) (int, int) {
	if s == "" {
		return 0, 0
	}
	s = strings.TrimRight(s, "\n")
	count := 0
	if filter {
		if structured {
			b, n, err := redact.JSON([]byte(s))
			if err == nil {
				s = string(b)
				count = n
			} else {
				s, count = redact.Text(s)
			}
		} else {
			s, count = redact.Text(s)
		}
	}
	n, _ := io.WriteString(w, s+"\n")
	return n, count
}
func execute(args []string, stdout, stderr io.Writer) int {
	jsonMode := false
	clean := []string{}
	for _, a := range args {
		if a == "--json" {
			jsonMode = true
		} else {
			clean = append(clean, a)
		}
	}
	args = clean
	protocol := len(args) > 0 && (args[0] == "hook" || args[0] == "mcp")
	if protocol {
		jsonMode = false
	}

	var installPlan string
	if len(args) > 0 && (args[0] == "install" || args[0] == "uninstall") {
		dryRun := false
		for _, arg := range args[1:] {
			if arg == "--dry-run" || arg == "--print-agents-md" {
				dryRun = true
			}
		}
		if !dryRun {
			planArgs := append(append([]string{}, args[1:]...), "--dry-run")
			plan, err := clients.Run(planArgs, args[0] == "uninstall")
			installPlan = plan
			if !jsonMode {
				emit(stdout, plan, false, true)
			}
			if err != nil && !jsonMode {
				emit(stderr, "plan: "+err.Error(), false, true)
			}
		}
	}
	var out string
	var err error
	preRedactions := 0
	if len(args) > 0 && args[0] == "hook" && (len(args) < 2 || args[1] != "stop") {
		out, err = sessionHook(args[1:], &preRedactions)
	} else {
		out, err = run(args)
	}
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	filter := !memoryOutput(cmd) || redact.MemoryEnabled(memory.Home())
	if cmd == "hook" && len(args) > 1 && args[1] == "stop" {
		filter = true
	}
	if cmd == "context" || cmd == "inject" {
		filter = false
	}
	if jsonMode {
		if filter {
			if json.Valid([]byte(out)) {
				b, _, _ := redact.JSON([]byte(out))
				out = string(b)
			} else {
				out, _ = redact.Text(out)
			}
		}
		var value any = out
		if json.Valid([]byte(out)) {
			var raw json.RawMessage = []byte(out)
			value = raw
		}
		if installPlan != "" {
			plan, _ := redact.Text(installPlan)
			value = map[string]any{"plan": plan, "result": value}
		}
		if err != nil {
			msg, _ := redact.Text(err.Error())
			value = map[string]any{"error": msg, "result": value}
		}
		b, _ := json.Marshal(value)
		emit(stdout, string(b), true, false)
		if err != nil {
			return 1
		}
		return 0
	}
	out = humanFacts(cmd, out)
	n, count := emit(stdout, out, cmd == "hook" || json.Valid([]byte(out)), filter)
	count += preRedactions
	if err != nil {
		en, ec := emit(stderr, "aibase: "+err.Error(), false, true)
		n += en
		count += ec
	}
	if cmd == "hook" || (memoryWrite(cmd) && err == nil) {
		action := cmd
		client := "cli"
		if cmd == "hook" {
			action, client = "SessionStart", "unknown"
			if len(args) > 1 && args[1] == "stop" {
				action = "Stop"
			}
		}
		for i, a := range args {
			if a == "--agent" && i+1 < len(args) {
				switch args[i+1] {
				case "claude", "codex", "gemini", "cursor", "cursor-agent":
					client = args[i+1]
				}
			}
		}
		audit.Record(memory.Home(), client, action, n, count)
	}
	if err != nil {
		return 1
	}

	return 0
}
func main() { os.Exit(execute(os.Args[1:], os.Stdout, os.Stderr)) }
