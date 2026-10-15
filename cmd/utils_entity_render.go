package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// docsHiddenHint returns the one-line notice shown in place of suppressed
// rich-text document bodies, naming the hidden fields and how to include them.
// Empty when there are no document fields.
func docsHiddenHint(docKeys []string) string {
	if len(docKeys) == 0 {
		return ""
	}
	labels := make([]string, len(docKeys))
	for i, k := range docKeys {
		labels[i] = strings.TrimPrefix(k, "_doc_")
	}
	return fmt.Sprintf("_Document fields hidden (%s) — re-run with --docs to include the bodies._", strings.Join(labels, ", "))
}

// printEntityLLMFull renders an entity with all fields in LLM-ready Markdown.
// docKeys are keys in the entity whose values are document secrets. When showDocs
// is true their bodies are fetched and appended; otherwise a one-line hint names
// the hidden fields (bodies are the largest token sink, so they are off by default).
func printEntityLLMFull(ctx context.Context, raw json.RawMessage, db string, docKeys []string, showDocs bool) error {
	var entity map[string]any
	if err := json.Unmarshal(raw, &entity); err != nil {
		return err
	}

	// Header: entity name
	if name := asStr(entity["Name"]); name != "" {
		fmt.Fprintf(os.Stdout, "# %s\n\n", strings.TrimSpace(name))
	}
	fmt.Fprintf(os.Stdout, "**Database:** %s\n", db)
	if pid := asStr(entity["Public ID"]); pid != "" && cfg != nil {
		fmt.Fprintf(os.Stdout, "**URL:** %s\n", buildEntityURL(cfg.BaseURL(), db, pid, asStr(entity["Name"])))
	}

	// Field display order
	priorityOrder := []string{"Public ID", "State", "Priority", "Created", "Created By"}
	docKeySet := map[string]bool{}
	for _, k := range docKeys {
		docKeySet[k] = true
	}

	// "fibery/id" is in the JSON output (so scripts can grab the UUID) but hidden
	// from the human-friendly print — the UUID alone isn't useful to read.
	skip := map[string]bool{"Name": true, "fibery/id": true}

	printField := func(k string) {
		if skip[k] || docKeySet[k] {
			return
		}
		v := entity[k]
		if v == nil {
			return
		}
		// Handle complex values (sub-query results)
		switch val := v.(type) {
		case []any:
			if len(val) == 0 {
				return
			}
			var names []string
			for _, item := range val {
				if m, ok := item.(map[string]any); ok {
					if n := asStr(m["Name"]); n != "" {
						names = append(names, n)
					}
				}
			}
			if len(names) > 0 {
				fmt.Fprintf(os.Stdout, "**%s:** %s\n", k, strings.Join(names, ", "))
			}
		case map[string]any:
			// shouldn't happen for resolved fields
		default:
			s := fmt.Sprintf("%v", v)
			if strings.TrimSpace(s) == "" || s == "false" {
				return
			}
			fmt.Fprintf(os.Stdout, "**%s:** %s\n", k, s)
		}
		skip[k] = true
	}

	// Priority fields first
	for _, k := range priorityOrder {
		printField(k)
	}

	// Remaining fields alphabetically
	var rest []string
	for k := range entity {
		if !skip[k] && !docKeySet[k] {
			rest = append(rest, k)
		}
	}
	for i := 0; i < len(rest)-1; i++ {
		for j := i + 1; j < len(rest); j++ {
			if rest[i] > rest[j] {
				rest[i], rest[j] = rest[j], rest[i]
			}
		}
	}
	for _, k := range rest {
		printField(k)
	}

	// Document sections. The rich-text bodies are the single largest token sink,
	// so by default they are suppressed and replaced with a one-line hint naming
	// the hidden fields. --docs (or an explicit --fields naming a doc) opts in.
	if !showDocs {
		if hint := docsHiddenHint(docKeys); hint != "" {
			fmt.Fprintf(os.Stdout, "\n%s\n", hint)
		}
		return nil
	}
	for _, key := range docKeys {
		secret := asStr(entity[key])
		if secret == "" {
			continue
		}
		content, err := cli.GetDocument(ctx, secret)
		if err != nil || strings.TrimSpace(content) == "" {
			continue
		}
		// Label: strip "_doc_" prefix from key
		label := strings.TrimPrefix(key, "_doc_")
		fmt.Fprintf(os.Stdout, "\n---\n**%s:**\n\n", label)
		fmt.Fprint(os.Stdout, content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Fprintln(os.Stdout)
		}
	}

	return nil
}

// printEntityLLM renders an entity in clean LLM-ready Markdown.
// Internal fields (_secret, ID UUID) are hidden. Document content is appended if non-empty.
func printEntityLLM(ctx context.Context, raw json.RawMessage, db, secret string) error {
	var entity map[string]any
	if err := json.Unmarshal(raw, &entity); err != nil {
		return err
	}

	// Internal / noisy fields to skip in output
	skip := map[string]bool{"_secret": true}

	// Print header: Name first if present, then other fields
	if name := asStr(entity["Name"]); name != "" {
		fmt.Fprintf(os.Stdout, "# %s\n\n", name)
		skip["Name"] = true
	}
	if db != "" {
		fmt.Fprintf(os.Stdout, "**Database:** %s\n", db)
	}

	// Ordered output: PublicID, State, Created, then rest alphabetically
	ordered := []string{"PublicID", "State", "Created"}
	seen := map[string]bool{}
	var remaining []string
	for _, k := range ordered {
		if _, ok := entity[k]; ok && !skip[k] {
			seen[k] = true
		}
	}
	for k := range entity {
		if !skip[k] && !seen[k] {
			remaining = append(remaining, k)
		}
	}
	// stable sort remaining
	for i := 0; i < len(remaining)-1; i++ {
		for j := i + 1; j < len(remaining); j++ {
			if remaining[i] > remaining[j] {
				remaining[i], remaining[j] = remaining[j], remaining[i]
			}
		}
	}
	sortedKeys := append(ordered, remaining...)

	for _, k := range sortedKeys {
		v := entity[k]
		if v == nil || asStr(v) == "" {
			continue
		}
		fmt.Fprintf(os.Stdout, "**%s:** %v\n", k, v)
	}

	// Document content
	if secret != "" {
		content, err := cli.GetDocument(ctx, secret)
		if err == nil && strings.TrimSpace(content) != "" {
			fmt.Fprint(os.Stdout, "\n---\n\n")
			fmt.Fprint(os.Stdout, content)
			if !strings.HasSuffix(content, "\n") {
				fmt.Fprintln(os.Stdout)
			}
		}
	}

	return nil
}
