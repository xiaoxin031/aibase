package project

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

var Manifests = []string{"go.mod", "package.json", "pom.xml", "build.gradle", "build.gradle.kts", "Cargo.toml", "pyproject.toml", "Makefile", "Dockerfile", "compose.yml", "compose.yaml", "docker-compose.yml", "docker-compose.yaml"}
var targetRE = regexp.MustCompile(`^([A-Za-z0-9_.%/ -]+):(?:[^=]|$)`)
var taskRE = regexp.MustCompile(`(?:tasks\.(?:register|create)\s*\(?\s*["']([^"']+)["']|\btask\s+([A-Za-z_][A-Za-z0-9_]*))`)

func parse(name string, b []byte) (any, error) {
	s := string(b)
	switch name {
	case "package.json":
		var p struct {
			Scripts map[string]string `json:"scripts"`
		}
		if e := json.Unmarshal(b, &p); e != nil {
			return nil, e
		}
		for k, v := range p.Scripts {
			p.Scripts[k] = shortDeclaration(v)
		}
		return map[string]any{"scripts": p.Scripts}, nil
	case "go.mod":
		out := map[string]any{}
		for _, l := range strings.Split(s, "\n") {
			l = strings.TrimSpace(l)
			f := strings.Fields(l)
			if len(f) >= 2 && (f[0] == "module" || f[0] == "go" || f[0] == "toolchain") {
				out[f[0]] = f[1]
			}

		}
		return out, nil
	case "pom.xml":
		var p struct {
			Group    string   `xml:"groupId"`
			Artifact string   `xml:"artifactId"`
			Version  string   `xml:"version"`
			Modules  []string `xml:"modules>module"`
			Goals    []string `xml:"build>plugins>plugin>executions>execution>goals>goal"`
		}
		if e := xml.Unmarshal(b, &p); e != nil {
			return nil, e
		}
		return p, nil
	case "Makefile":
		targets := []string{}
		seen := map[string]bool{}
		for _, l := range strings.Split(s, "\n") {
			if strings.HasPrefix(l, "\t") || strings.HasPrefix(strings.TrimSpace(l), "#") {
				continue
			}
			if m := targetRE.FindStringSubmatch(l); m != nil {
				for _, t := range strings.Fields(m[1]) {
					if !seen[t] && !strings.HasPrefix(t, ".") {
						targets = append(targets, t)
						seen[t] = true
					}
				}
			}
		}
		return map[string]any{"targets": targets}, nil
	case "build.gradle", "build.gradle.kts":
		var tasks []string
		for _, m := range taskRE.FindAllStringSubmatch(s, -1) {
			tasks = append(tasks, m[1]+m[2])
		}
		return map[string]any{"tasks": tasks}, nil
	case "Cargo.toml", "pyproject.toml":
		// Select static sections only; never evaluate TOML expressions.
		var sections []string
		for _, l := range strings.Split(s, "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "[") && strings.Contains(l, "]") {
				sections = append(sections, shortDeclaration(l[:strings.Index(l, "]")+1]))
			}
		}
		return map[string]any{"sections": sections, "values": declarationSections(s), "parser": "static declarations; no TOML evaluation"}, nil
	case "Dockerfile":
		var commands []string
		for _, l := range strings.Split(s, "\n") {
			f := strings.Fields(l)
			if len(f) > 0 {
				switch strings.ToUpper(f[0]) {
				case "FROM", "CMD", "ENTRYPOINT", "EXPOSE":
					commands = append(commands, shortDeclaration(l))
				}
			}
		}
		return map[string]any{"instructions": commands}, nil
	default:
		if strings.Contains(name, "compose") {
			return composeDeclarations(s), nil
		}
	}
	return nil, fmt.Errorf("unsupported manifest")
}

// declarationSections recognizes static TOML key/value declarations without
// evaluating expressions. Raw values preserve arrays, inline tables and quoting.
func declarationSections(s string) map[string]map[string]string {
	out := map[string]map[string]string{}
	section := ""
	for _, line := range strings.Split(s, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.HasPrefix(l, "[") {
			section = strings.Trim(l, "[] ")
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		k = strings.Trim(strings.TrimSpace(k), "\"'")
		if k == "" {
			continue
		}
		if section != "package" && section != "project" && section != "project.scripts" && section != "tool.poetry.scripts" {
			continue
		}
		if (section == "package" || section == "project") && k != "name" && k != "version" && k != "edition" && k != "requires-python" {
			continue
		}
		if out[section] == nil {
			out[section] = map[string]string{}
		}
		out[section][k] = shortDeclaration(strings.TrimSpace(v))
	}
	return out
}
func composeDeclarations(s string) map[string]any {
	services := map[string]map[string]string{}
	inServices := false
	service := ""
	for _, line := range strings.Split(s, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inServices = l == "services:"
			continue
		}
		if !inServices {
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		if indent == 2 {
			service = strings.Trim(k, "\"'")
			services[service] = map[string]string{}
			continue
		}
		if service != "" {
			switch k {
			case "image", "build", "command", "entrypoint", "ports":
				services[service][k] = shortDeclaration(strings.TrimSpace(v))
			}
		}
	}
	return map[string]any{"services": services, "parser": "static declarations; no YAML evaluation"}
}

func shortDeclaration(s string) string {
	r := []rune(s)
	if len(r) > 120 {
		return string(r[:119]) + "…"
	}
	return s
}
