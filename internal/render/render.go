package render

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// JSON pretty-prints raw JSON to w.
func JSON(w io.Writer, data json.RawMessage) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("render.JSON: %w", err)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

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
			v := item[k]
			if v == nil {
				cells[i] = ""
			} else {
				cells[i] = fmt.Sprintf("%v", v)
			}
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
			v := item[k]
			s := ""
			if v != nil {
				s = fmt.Sprintf("%v", v)
			}
			if sep == "," && strings.ContainsAny(s, ",\"\n") {
				s = `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
			}
			cells[i] = s
		}
		fmt.Fprintln(w, strings.Join(cells, sep))
	}
	return nil
}
