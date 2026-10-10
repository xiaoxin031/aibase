package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

func validateArguments(name string, raw json.RawMessage) error {
	var spec map[string]any
	for _, t := range mcpTools {
		if t["name"] == name {
			spec, _ = t["inputSchema"].(map[string]any)
			break
		}
	}
	if spec == nil {
		return nil
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("arguments exceed 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if e := d.Decode(&value); e != nil {
		return e
	}
	return validateValue(value, spec, "arguments")
}
func validateValue(v any, s map[string]any, path string) error {
	fail := func() error { return fmt.Errorf("%s must be %s", path, s["type"]) }
	switch s["type"] {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return fail()
		}
		props, _ := s["properties"].(map[string]any)
		required, _ := s["required"].([]string)
		for _, k := range required {
			if _, ok := m[k]; !ok {
				return fmt.Errorf("%s.%s required", path, k)
			}
		}
		for k, value := range m {
			p, known := props[k]
			if !known {
				if s["additionalProperties"] == false {
					return fmt.Errorf("unknown argument %s", k)
				}
				continue
			}
			if e := validateValue(value, p.(map[string]any), path+"."+k); e != nil {
				return e
			}
		}
	case "string":
		value, ok := v.(string)
		if !ok {
			return fail()
		}
		if enum, ok := s["enum"].([]string); ok {
			found := false
			for _, e := range enum {
				found = found || value == e
			}
			if !found {
				return fmt.Errorf("unsupported %s", path)
			}
		}
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return fail()
		}
		if _, e := strconv.ParseInt(string(n), 10, 64); e != nil {
			return fail()
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			return fail()
		}
		item, _ := s["items"].(map[string]any)
		if item != nil {
			for _, v := range a {
				if e := validateValue(v, item, path+"[]"); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
