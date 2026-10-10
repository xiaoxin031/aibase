package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"fyne.io/systray"
)

//go:embed icons/tray.png
var pngIcon []byte

//go:embed icons/template.png
var templateIcon []byte

//go:embed icons/tray.ico
var icoIcon []byte

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	var workers sync.WaitGroup
	systray.Run(func() {
		regular := pngIcon
		if runtime.GOOS == "windows" {
			regular = icoIcon
		}
		systray.SetTemplateIcon(templateIcon, regular)
		systray.SetTooltip("aibase")
		m := newMenu()
		actions := make(chan string, 1)
		workers.Add(1)
		go func() {
			defer workers.Done()
			m.work(ctx, actions)
		}()
		// Return from onReady so systray can finish publishing its initial menu.
		go func() {
			enqueue := func(action string) {
				select {
				case actions <- action:
				default:
				}
			}
			enqueue("refresh")
			for {
				select {
				case <-ctx.Done():
					return
				case <-m.quit.ClickedCh:
					cancel()
					ticker.Stop()
					workers.Wait()
					systray.Quit()
					return
				case <-ticker.C:
					enqueue("refresh")
				case <-m.refresh.ClickedCh:
					enqueue("refresh")
				case <-m.pause.ClickedCh:
					enqueue("pause")
				case <-m.autosave.ClickedCh:
					enqueue("autosave")
				case <-m.open.ClickedCh:
					enqueue("open")
				case <-m.doctor.ClickedCh:
					enqueue("doctor")
				}
			}
		}()
	}, func() { cancel(); ticker.Stop(); workers.Wait() })
}

type menu struct {
	headline, injection, health                  *systray.MenuItem
	clients, memory, sessions                    []*systray.MenuItem
	pause, autosave, open, doctor, refresh, quit *systray.MenuItem
}

func info(title string) *systray.MenuItem {
	m := systray.AddMenuItem(title, "")
	m.Disable()
	return m
}
func slots(title string, n int) []*systray.MenuItem {
	parent := systray.AddMenuItem(title, "")
	out := make([]*systray.MenuItem, n)
	for i := range out {
		out[i] = parent.AddSubMenuItem("None", "")
		out[i].Disable()
		if i > 0 {
			out[i].Hide()
		}
	}
	return out
}
func newMenu() *menu {
	m := &menu{headline: info("aibase: loading...")}
	m.clients = slots("Clients (configuration only)", 8)
	m.memory = slots("Memory usage", 3)
	m.sessions = slots("Recent sessions (up to 5)", 5)
	m.injection = info("Last injection: unknown")
	m.health = info("Doctor: unknown")
	systray.AddSeparator()
	m.pause = systray.AddMenuItemCheckbox("Pause injection", "Disable automatic context injection", false)
	m.autosave = systray.AddMenuItemCheckbox("Auto-save", "Enable automatic review prompts", true)
	m.pause.Disable()
	m.autosave.Disable()
	m.open = systray.AddMenuItem("Open data directory", "")
	m.open.Disable()
	m.doctor = systray.AddMenuItem("Run doctor...", "Open a temporary report in your default text editor")
	m.refresh = systray.AddMenuItem("Refresh", "Refresh tray status")
	systray.AddSeparator()
	m.quit = systray.AddMenuItem("Quit", "")
	return m
}
func setRows(items []*systray.MenuItem, rows []string) {
	if len(rows) == 0 {
		rows = []string{"None"}
	}
	for i, m := range items {
		if i < len(rows) {
			m.SetTitle(label(rows[i]))
			m.Show()
		} else {
			m.Hide()
		}
	}
}
func checked(m *systray.MenuItem, value bool) {
	if value {
		m.Check()
	} else {
		m.Uncheck()
	}
}
func (m *menu) render(s status) {
	m.headline.SetTitle(label("aibase " + s.Version))
	var rows []string
	for _, c := range s.Clients {
		state := "not detected"
		if c.Detected {
			state = "detected, not configured"
		}
		if c.Configured {
			state = "configured; activation unverified"
		}
		if c.Error != "" {
			state = "error: " + c.Error
		}
		rows = append(rows, c.Name+": "+state)
	}
	setRows(m.clients, rows)
	rows = nil
	for _, u := range s.Memory {
		line := fmt.Sprintf("%s: %d/%d characters", u.Target, u.Used, u.Limit)
		if u.Error != "" {
			line = u.Target + ": unavailable"
		}
		rows = append(rows, line)
	}
	setRows(m.memory, rows)
	rows = nil
	for _, s := range s.Sessions {
		rows = append(rows, s.TS+" "+s.Agent+": "+s.Text)
	}
	setRows(m.sessions, rows)
	injection := "Last injection: none in retained history"
	if s.LastInjection != nil {
		injection = "Last injection: " + s.LastInjection.TS + " (" + s.LastInjection.Client + ")"
	}
	m.injection.SetTitle(label(injection))
	m.health.SetTitle(fmt.Sprintf("Doctor (quick): %d OK, %d warnings, %d failures", s.Doctor.Counts["ok"], s.Doctor.Counts["warn"], s.Doctor.Counts["fail"]))
	checked(m.pause, !s.Config.Inject)
	checked(m.autosave, s.Config.Autosave)
	m.pause.Enable()
	m.autosave.Enable()
	if s.DataDir != "" {
		m.open.Enable()
	} else {
		m.open.Disable()
	}
}
func (m *menu) work(ctx context.Context, actions <-chan string) {
	var current status
	valid := false
	reportDir := ""
	defer func() {
		if reportDir != "" {
			os.RemoveAll(reportDir)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case action := <-actions:
			exe, err := os.Executable()
			var path string
			if err == nil {
				path, err = findCore(exe)
			}
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				valid = false
				m.fail(err)
				continue
			}
			c := client{path: path, timeout: 20 * time.Second}
			switch action {
			case "pause", "autosave":
				if !valid {
					continue
				}
				key, value := "inject", !current.Config.Inject
				if action == "autosave" {
					key, value = "autosave", !current.Config.Autosave
				}
				var cfg settings
				err = c.call(ctx, &cfg, "config", key, strconv.FormatBool(value))
			case "doctor":
				var result json.RawMessage
				err = c.call(ctx, &result, "doctor")
				if err == nil {
					if reportDir == "" {
						reportDir, err = os.MkdirTemp("", "aibase-doctor-")
					}
					if err == nil {
						file := reportDir + string(os.PathSeparator) + "doctor.txt"
						var pretty []byte
						pretty, err = json.MarshalIndent(result, "", "  ")
						if err == nil {
							err = os.WriteFile(file, append(pretty, '\n'), 0600)
						}
						if err == nil {
							err = timedOpen(ctx, file)
						}
					}
				}
			case "open":
				if valid {
					err = timedOpen(ctx, current.DataDir)
				}
			}
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				valid = false
				m.fail(err)
				continue
			}
			next, err := c.status(ctx)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				valid = false
				m.fail(err)
				continue
			}
			current = next
			valid = true
			m.render(current)
		}
	}
}
func timedOpen(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return openPath(ctx, path)
}
func (m *menu) fail(err error) {
	m.headline.SetTitle(label("Error: " + err.Error()))
	m.health.SetTitle("Status unavailable; previous values may be stale")
	m.pause.Disable()
	m.autosave.Disable()
	m.open.Disable()
}
