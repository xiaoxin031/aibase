package host

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

type RuntimeListener struct {
	Port    uint64 `json:"port"`
	Proto   string `json:"proto"`
	Bind    string `json:"bind"`
	Process string `json:"process"`
	PID     int    `json:"pid"`
}
type Resource struct {
	PID  int     `json:"pid"`
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
	Name string  `json:"name"`
}
type Container struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
	Ports  string `json:"ports"`
}

func executableName(s string) string {
	return path.Base(strings.ReplaceAll(strings.TrimSpace(s), "\\", "/"))
}
func listenersSorted(rows []RuntimeListener) []RuntimeListener {
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		if a.PID != b.PID {
			return a.PID < b.PID
		}
		return a.Bind < b.Bind
	})
	out := []RuntimeListener{}
	seen := map[string]bool{}
	for _, r := range rows {
		r.Process = executableName(r.Process)
		if r.Process == "" || r.Process == "." {
			r.Process = "unknown"
		}
		key := fmt.Sprintf("%d/%s/%d/%s", r.Port, r.Proto, r.PID, r.Process)
		// Without attribution, distinct addresses cannot safely be merged.
		if r.PID == 0 {
			key += "/" + r.Bind
		}
		if !seen[key] {
			out = append(out, r)
			seen[key] = true
		}
	}
	return out
}
func runtimeNormalize(name, s string) (any, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	switch name {
	case "listeners":
		rows := []RuntimeListener{}
		if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
			if strings.HasPrefix(s, "{") {
				s = "[" + s + "]"
			}
			if e := json.Unmarshal([]byte(s), &rows); e != nil {
				return nil, e
			}
		} else if strings.HasPrefix(s, "p") && !strings.Contains(strings.SplitN(s, "\n", 2)[0], " ") {
			pid := 0
			process := "unknown"
			for _, line := range strings.Split(s, "\n") {
				if len(line) < 2 {
					continue
				}
				switch line[0] {
				case 'p':
					pid, _ = strconv.Atoi(line[1:])
					process = "unknown"
				case 'c':
					process = line[1:]
				case 'n':
					endpoint := line[1:]
					idx := strings.LastIndex(endpoint, ":")
					if idx < 0 {
						continue
					}
					port, e := strconv.ParseUint(endpoint[idx+1:], 10, 16)
					if e != nil {
						continue
					}
					rows = append(rows, RuntimeListener{port, "tcp", strings.Trim(endpoint[:idx], "[]"), process, pid})
				}
			}
		} else {
			for _, line := range strings.Split(s, "\n") {
				f := strings.Fields(line)
				if len(f) < 9 {
					continue
				}
				pid, e := strconv.Atoi(f[1])
				if e != nil {
					continue
				}
				endpoint := f[8]
				idx := strings.LastIndex(endpoint, ":")
				if idx < 0 {
					continue
				}
				port, e := strconv.ParseUint(endpoint[idx+1:], 10, 16)
				if e != nil {
					continue
				}
				rows = append(rows, RuntimeListener{port, "tcp", strings.Trim(endpoint[:idx], "[]"), f[0], pid})
			}
		}
		return listenersSorted(rows), nil
	case "services":
		out := []string{}
		for _, line := range strings.Split(s, "\n") {
			f := strings.Fields(line)
			if len(f) == 0 {
				continue
			}
			if len(f) >= 3 && (f[0] == "-" || f[0] == "PID") {
				continue
			}
			if len(f) >= 3 {
				if _, e := strconv.Atoi(f[0]); e == nil {
					if !strings.HasPrefix(f[2], "com.apple.") {
						out = append(out, f[2])
					}
					continue
				}
			}
			if len(f) >= 4 && f[2] == "active" && f[3] == "running" {
				out = append(out, f[0])
				continue
			}
			if len(f) == 1 {
				out = append(out, f[0])
			}
		}
		sort.Strings(out)
		return out, nil
	case "resources":
		out := []Resource{}
		if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") {
			if strings.HasPrefix(s, "{") {
				s = "[" + s + "]"
			}
			if e := json.Unmarshal([]byte(s), &out); e != nil {
				return nil, e
			}
		} else {
			for _, line := range strings.Split(s, "\n") {
				f := strings.Fields(line)
				if len(f) < 4 {
					continue
				}
				pid, e := strconv.Atoi(f[0])
				if e != nil {
					continue
				}
				cpu, e := strconv.ParseFloat(f[1], 64)
				if e != nil {
					continue
				}
				mem, e := strconv.ParseFloat(f[2], 64)
				if e != nil {
					continue
				}
				out = append(out, Resource{pid, cpu, mem, strings.Join(f[3:], " ")})
			}
		}
		for j := range out {
			out[j].Name = executableName(out[j].Name)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].CPU == out[j].CPU {
				return out[i].PID < out[j].PID
			}
			return out[i].CPU > out[j].CPU
		})
		return out[:min(10, len(out))], nil
	}
	return nil, fmt.Errorf("unknown runtime section %s", name)
}
func runtimeText(name string, v any) []string {
	b, _ := json.Marshal(v)
	lines := []string{name + ":"}
	limit := 50
	switch name {
	case "listeners":
		var rows []RuntimeListener
		_ = json.Unmarshal(b, &rows)
		for _, r := range rows {
			lines = append(lines, fmt.Sprintf("%d %s %s %s(%d)", r.Port, r.Proto, r.Bind, r.Process, r.PID))
		}
	case "services":
		limit = 40
		var rows []string
		_ = json.Unmarshal(b, &rows)
		lines = append(lines, rows...)
	case "resources":
		var rows []Resource
		_ = json.Unmarshal(b, &rows)
		for _, r := range rows {
			lines = append(lines, fmt.Sprintf("%s(%d) cpu %.1f%% mem %.1f%%", r.Name, r.PID, r.CPU, r.Mem))
		}
	case "containers":
		var rows []Container
		_ = json.Unmarshal(b, &rows)
		for _, r := range rows {
			lines = append(lines, fmt.Sprintf("%s %s %s %s", r.Name, r.Image, r.Status, r.Ports))
		}
	}
	total := len(lines) - 1
	if total == 0 {
		return []string{name + ": none"}
	}
	if total > limit {
		lines = append(lines[:limit+1], fmt.Sprintf("... %d more (%d total)", total-limit, total))
	}
	return lines
}
