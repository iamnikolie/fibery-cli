package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// List renders a JSON array as a Markdown table.
func List(w io.Writer, data json.RawMessage) error {
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("render.List: %w", err)
	}
	if len(items) == 0 {
		fmt.Fprintln(w, "_No results._")
		return nil
	}

	keySet := map[string]bool{}
	for _, item := range items {
		for k := range item {
			keySet[k] = true
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sep := make([]string, len(keys))
	for i := range sep {
		sep[i] = "---"
	}
	fmt.Fprintf(w, "| %s |\n", strings.Join(keys, " | "))
	fmt.Fprintf(w, "| %s |\n", strings.Join(sep, " | "))

	for _, item := range items {
		cells := make([]string, len(keys))
		for i, k := range keys {
			cells[i] = cell(item[k])
		}
		fmt.Fprintf(w, "| %s |\n", strings.Join(cells, " | "))
	}
	return nil
}

// KV renders a JSON object as Markdown key-value pairs.
func KV(w io.Writer, data json.RawMessage) error {
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("render.KV: %w", err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "**%s:** %v\n", k, m[k])
	}
	return nil
}

// CSV renders a JSON array as comma-separated values with a header row.
func CSV(w io.Writer, data json.RawMessage) error {
	return separatedValues(w, data, ",")
}

// TSV renders a JSON array as tab-separated values with a header row.
func TSV(w io.Writer, data json.RawMessage) error {
	return separatedValues(w, data, "\t")
}

func separatedValues(w io.Writer, data json.RawMessage, sep string) error {
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	if len(items) == 0 {
		return nil
	}
	keySet := map[string]bool{}
	for _, item := range items {
		for k := range item {
			keySet[k] = true
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintln(w, strings.Join(keys, sep))
	for _, item := range items {
		cells := make([]string, len(keys))
		for i, k := range keys {
			s := cell(item[k])
			if sep == "," && strings.ContainsAny(s, ",\"\n") {
				s = `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
			}
			cells[i] = s
		}
		fmt.Fprintln(w, strings.Join(cells, sep))
	}
	return nil
}

// cell formats one value for a table or CSV row. A sub-query result (a collection
// read with --fields) becomes "<Public ID> <Name>" items joined by "; ".
func cell(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			m, ok := item.(map[string]any)
			if !ok {
				parts = append(parts, fmt.Sprintf("%v", item))
				continue
			}
			pid, name := m["Public ID"], m["Name"]
			switch {
			case pid != nil && name != nil:
				parts = append(parts, fmt.Sprintf("%v %v", pid, name))
			case pid != nil:
				parts = append(parts, fmt.Sprintf("%v", pid))
			case name != nil:
				parts = append(parts, fmt.Sprintf("%v", name))
			default:
				parts = append(parts, fmt.Sprintf("%v", m))
			}
		}
		return strings.Join(parts, "; ")
	default:
		return fmt.Sprintf("%v", v)
	}
}
