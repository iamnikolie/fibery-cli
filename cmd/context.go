package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/iamnikolie/fibery-cli/internal/profile"
)

var (
	contextDB          string
	contextParentsFull bool
	contextNoComments  bool
	contextNoDocs      bool
)

var contextCmd = &cobra.Command{
	Use:   "context <id>",
	Short: "One-shot curated context bundle for an entity and its graph",
	Long: `Assemble a profile-driven context bundle: the root entity (curated fields +
document bodies + all comments) plus its neighbors (parent epic/story, children,
blockers, MRs, bugs) as a headline table.

A profile for the entity's database must exist — create one with
'fibery context init "<db>"'. Use --parents-full to expand parent nodes to full
documents and comments.

Examples:
  fibery context DT-5683 --db "Development/Dev Task"
  fibery context https://acme.fibery.io/Development/Dev_Task/x-5683
  fibery context DT-5683 --db "Development/Dev Task" --parents-full`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		ref := args[0]

		db := contextDB
		publicHint := ref
		if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
			rdb, pid, _, err := resolveURL(ref)
			if err != nil {
				return err
			}
			if rdb == "" {
				return fmt.Errorf("context: could not infer database from URL — pass --db")
			}
			db, publicHint = rdb, pid
		}
		if db == "" {
			return fmt.Errorf("--db is required (e.g. --db \"Development/Dev Task\")")
		}

		profiles, err := profile.Load(account)
		if err != nil {
			return err
		}
		prof, ok := profiles.Get(db)
		if !ok {
			return fmt.Errorf("context: no profile for %q — run 'fibery context init %q' first", db, db)
		}

		schema, err := cache.LoadSchema(account)
		if err != nil {
			return fmt.Errorf("context: %w (run 'fibery schema sync')", err)
		}

		entityID, err := resolveEntityRef(ctx, db, publicHint)
		if err != nil {
			return err
		}

		withComments := !contextNoComments && prof.Comments == "full"
		edgeCmd, aliasMeta := buildEdgesQuery(schema, db, entityID, prof)

		if wantJSON() {
			return emitContextJSON(ctx, schema, profiles, db, entityID, prof, withComments, edgeCmd, aliasMeta)
		}

		// Markdown: root full (curated fields + doc bodies + comments).
		if err := renderEntityFull(ctx, schema, db, entityID, prof, withComments, !contextNoDocs); err != nil {
			return err
		}

		// Edges: one nested query → headline neighbors.
		edgeRes, err := cli.One(ctx, edgeCmd)
		if err != nil {
			return err
		}
		var edgeRows []map[string]any
		if err := json.Unmarshal(edgeRes, &edgeRows); err != nil || len(edgeRows) == 0 {
			return nil // root already printed; no edges resolvable
		}
		renderEdges(os.Stdout, edgeRows[0], aliasMeta)

		if contextParentsFull {
			return renderParentsFull(ctx, schema, profiles, edgeRows[0], aliasMeta)
		}
		return nil
	},
}

// selectFieldName extracts the underlying Fibery field name from one q/select
// entry (array selector → element 0; sub-query map → q/from).
func selectFieldName(sel any) string {
	switch v := sel.(type) {
	case []any:
		if len(v) > 0 {
			return asStr(v[0])
		}
	case map[string]any:
		return asStr(v["q/from"])
	}
	return ""
}

// filterSelectToProfile narrows a buildFullSelect result to the profile's
// keep+docs sets, always preserving identity keys. Returns the narrowed select
// and the surviving document keys.
func filterSelectToProfile(full map[string]any, docKeys []string, prof profile.Profile) (map[string]any, []string) {
	keep := map[string]bool{}
	for _, f := range prof.Keep {
		keep[f] = true
	}
	for _, f := range prof.Docs {
		keep[f] = true
	}
	docKeySet := map[string]bool{}
	for _, k := range docKeys {
		docKeySet[k] = true
	}
	sel := map[string]any{}
	var keptDocs []string
	for alias, selector := range full {
		if alias == "fibery/id" || alias == "Public ID" {
			sel[alias] = selector
			continue
		}
		if keep[selectFieldName(selector)] {
			sel[alias] = selector
			if docKeySet[alias] {
				keptDocs = append(keptDocs, alias)
			}
		}
	}
	return sel, keptDocs
}

// fetchEntityFiltered queries one entity with a profile-narrowed select and
// returns the raw entity JSON plus the surviving document keys.
func fetchEntityFiltered(ctx context.Context, schema map[string]any, db, entityID string, prof profile.Profile) (json.RawMessage, []string, error) {
	full, docKeys := buildFullSelect(schema, db)
	sel, keptDocs := filterSelectToProfile(full, docKeys, prof)
	res, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   db,
				"q/select": sel,
				"q/where":  []any{"=", []any{"fibery/id"}, "$eid"},
				"q/limit":  1,
			},
			"params": map[string]any{"$eid": entityID},
		},
	})
	if err != nil {
		return nil, nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(res, &items); err != nil || len(items) == 0 {
		return nil, nil, fmt.Errorf("context: entity %s not found in %s", entityID, db)
	}
	return items[0], keptDocs, nil
}

// renderEntityFull fetches one entity (profile-narrowed) and prints it via the
// shared LLM-friendly renderer, optionally appending all comments. When showDocs
// is false the document bodies are suppressed (a one-line hint names them).
func renderEntityFull(ctx context.Context, schema map[string]any, db, entityID string, prof profile.Profile, withComments, showDocs bool) error {
	raw, keptDocs, err := fetchEntityFiltered(ctx, schema, db, entityID, prof)
	if err != nil {
		return err
	}
	if err := printEntityLLMFull(ctx, raw, db, keptDocs, showDocs); err != nil {
		return err
	}
	if withComments {
		rows, err := collectCommentRows(ctx, db, entityID, 0, "")
		if err != nil {
			return err
		}
		if len(rows) > 0 {
			fmt.Fprintf(os.Stdout, "\n## Comments (%d)\n\n", len(rows))
			renderComments(ctx, os.Stdout, rows)
		}
	}
	return nil
}

// edgeAlias is the rendering metadata for one selected edge field.
type edgeAlias struct {
	Role     string // parent | children | blockers | code | quality | service
	Label    string // stripped field name for the section header
	Multi    bool   // true → sub-query (collection), false → nested scalars
	TargetDB string // the relation field's target database
}

// edgeFieldsByRole returns the profile's edge fields grouped with their role.
func edgeFieldsByRole(prof profile.Profile) [][2]any {
	// Ordered so output is stable: parents first, then the rest.
	return [][2]any{
		{"parent", prof.Edges.Parent},
		{"children", prof.Edges.Children},
		{"blockers", prof.Edges.Blockers},
		{"code", prof.Edges.Code},
		{"quality", prof.Edges.Quality},
		{"service", prof.Edges.Service},
	}
}

// buildEdgesQuery assembles one nested FQL command fetching headline fields for
// every edge in the profile. Parent (to-one) edges use nested-scalar selects;
// all other roles use a collection sub-query. Returns the command and a map from
// result alias → edgeAlias for rendering.
func buildEdgesQuery(schema map[string]any, db, entityID string, prof profile.Profile) (client.Command, map[string]edgeAlias) {
	sel := map[string]any{"fibery/id": []any{"fibery/id"}}
	meta := map[string]edgeAlias{}
	idx := 0

	for _, rf := range edgeFieldsByRole(prof) {
		role := rf[0].(string)
		fields, _ := rf[1].([]string)
		for _, fieldName := range fields {
			target := findFieldType(schema, db, fieldName) // target db = relation field type
			if target == "" {
				continue
			}
			title := findTitleField(schema, target)
			if title == "" {
				continue
			}
			idx++
			alias := fmt.Sprintf("e%d", idx)
			label := strings.TrimSpace(fieldName)
			if i := strings.Index(label, "/"); i >= 0 {
				label = label[i+1:]
			}
			multi := isCollectionField(schema, db, fieldName)

			if role == "parent" && !multi {
				// to-one: nested scalar selects (Name/PID/State).
				sel[alias+"_Name"] = []any{fieldName, title}
				sel[alias+"_PID"] = []any{fieldName, "fibery/public-id"}
				if sf := findStateField(schema, target); sf != "" {
					sel[alias+"_State"] = []any{fieldName, sf, "enum/name"}
				}
				meta[alias] = edgeAlias{Role: role, Label: label, Multi: false, TargetDB: target}
				continue
			}

			subSel := map[string]any{
				"Name": []any{title},
				"PID":  []any{"fibery/public-id"},
			}
			if sf := findStateField(schema, target); sf != "" {
				subSel["State"] = []any{sf, "enum/name"}
			}
			if pf := findPriorityField(schema, target); pf != "" {
				subSel["Priority"] = []any{pf, "enum/name"}
			}
			sel[alias] = map[string]any{
				"q/from":   fieldName,
				"q/select": subSel,
				"q/limit":  200,
			}
			meta[alias] = edgeAlias{Role: role, Label: label, Multi: true, TargetDB: target}
		}
	}

	cmd := client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   db,
				"q/select": sel,
				"q/where":  []any{"=", []any{"fibery/id"}, "$eid"},
				"q/limit":  1,
			},
			"params": map[string]any{"$eid": entityID},
		},
	}
	return cmd, meta
}

// isEnumType reports whether a Fibery type is an enum. It catches both the
// "Space/Type_Space/Database" name pattern (isEnumLikeType) and enum types whose
// name doesn't encode that (e.g. "GitLab/Merge Request State Enum") by checking
// the type definition's fibery/enum? meta flag.
func isEnumType(schema map[string]any, typeName string) bool {
	if isEnumLikeType(typeName) {
		return true
	}
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != typeName {
			continue
		}
		meta, _ := tm["fibery/meta"].(map[string]any)
		return meta["fibery/enum?"] == true
	}
	return false
}

// findStateField returns the field name carrying an entity's state: workflow/state
// when present, else the first enum field whose stripped name is exactly "state"
// (e.g. GitLab/State on a merge request → opened/merged/closed). "" if none.
func findStateField(schema map[string]any, db string) string {
	if schemaHasField(schema, db, "workflow/state") {
		return "workflow/state"
	}
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			name := asStr(fm["fibery/name"])
			if strippedName(name) == "state" && isEnumType(schema, asStr(fm["fibery/type"])) {
				return name
			}
		}
	}
	return ""
}

// findPriorityField returns the first enum-like field whose stripped name
// contains "priority" for the db, or "".
func findPriorityField(schema map[string]any, db string) string {
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			name := asStr(fm["fibery/name"])
			if isEnumLikeType(asStr(fm["fibery/type"])) && strings.Contains(strippedName(name), "priority") {
				return name
			}
		}
	}
	return ""
}

// sortedAliases returns meta keys ordered for stable output: parents first
// (insertion order via the e<N> numbering), then the rest by alias.
func sortedAliases(meta map[string]edgeAlias) []string {
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		mi, mj := meta[keys[i]], meta[keys[j]]
		if mi.Multi != mj.Multi {
			return !mi.Multi // to-one (parents) before to-many
		}
		return keys[i] < keys[j]
	})
	return keys
}

// renderEdges writes the "## Graph" section: parent headlines first, then a
// table per non-empty to-many role. Empty to-many edges are collapsed into a
// single trailing "_empty: …_" line so a task with no blockers/MRs/bugs doesn't
// produce a wall of "_none_" sections.
func renderEdges(w io.Writer, row map[string]any, meta map[string]edgeAlias) {
	fmt.Fprintf(w, "\n## Graph\n\n")
	var emptyLabels []string
	wrote := false
	for _, alias := range sortedAliases(meta) {
		m := meta[alias]
		if !m.Multi {
			name := asStr(row[alias+"_Name"])
			if name == "" {
				continue
			}
			fmt.Fprintf(w, "↑ %s  %s  %s  [%s]\n", m.Label, asStr(row[alias+"_PID"]), name, asStr(row[alias+"_State"]))
			wrote = true
			continue
		}
		items, _ := row[alias].([]any)
		if len(items) == 0 {
			emptyLabels = append(emptyLabels, m.Label)
			continue
		}
		renderEdgeTable(w, m.Label, items)
		wrote = true
	}
	if len(emptyLabels) > 0 {
		fmt.Fprintf(w, "\n_empty: %s_\n", strings.Join(emptyLabels, ", "))
		wrote = true
	}
	if !wrote {
		fmt.Fprintln(w, "_no edges_")
	}
}

// renderEdgeTable writes one to-many edge as a markdown table. The optional
// state/priority columns are emitted only when at least one row has a value, so
// entity types without them (e.g. merge requests have no priority) don't carry a
// dead column.
func renderEdgeTable(w io.Writer, label string, items []any) {
	hasState, hasPriority := false, false
	for _, it := range items {
		im, _ := it.(map[string]any)
		if asStr(im["State"]) != "" {
			hasState = true
		}
		if asStr(im["Priority"]) != "" {
			hasPriority = true
		}
	}
	header, sep := "| id | name", "| --- | ---"
	if hasState {
		header, sep = header+" | state", sep+" | ---"
	}
	if hasPriority {
		header, sep = header+" | priority", sep+" | ---"
	}
	fmt.Fprintf(w, "\n### %s (%d)\n\n", label, len(items))
	fmt.Fprintln(w, header+" |")
	fmt.Fprintln(w, sep+" |")
	for _, it := range items {
		im, _ := it.(map[string]any)
		line := fmt.Sprintf("| %s | %s", asStr(im["PID"]), asStr(im["Name"]))
		if hasState {
			line += " | " + asStr(im["State"])
		}
		if hasPriority {
			line += " | " + asStr(im["Priority"])
		}
		fmt.Fprintln(w, line+" |")
	}
}

// renderParentsFull expands each parent edge to a full node (curated fields +
// docs + comments), using the parent db's profile when present, else a
// heuristic keep-all profile.
func renderParentsFull(ctx context.Context, schema map[string]any, profiles *profile.Profiles, row map[string]any, meta map[string]edgeAlias) error {
	seen := map[string]bool{}
	for _, alias := range sortedAliases(meta) {
		m := meta[alias]
		if m.Multi || m.Role != "parent" {
			continue
		}
		pid := asStr(row[alias+"_PID"])
		if pid == "" || m.TargetDB == "" {
			continue
		}
		// Multiple parent edges can resolve to the same entity (e.g. "Dev epic
		// Story" and "Story From Epic"). Expand each parent only once.
		if key := m.TargetDB + "#" + pid; seen[key] {
			continue
		} else {
			seen[key] = true
		}
		uuid, err := resolveEntityRef(ctx, m.TargetDB, pid)
		if err != nil {
			continue
		}
		prof, ok := profiles.Get(m.TargetDB)
		if !ok {
			prof = keepAllProfile(schema, m.TargetDB)
		}
		fmt.Fprintf(os.Stdout, "\n---\n\n# Parent: %s %s\n\n", m.Label, pid)
		if err := renderEntityFull(ctx, schema, m.TargetDB, uuid, prof, !contextNoComments, !contextNoDocs); err != nil {
			return err
		}
	}
	return nil
}

// keepAllProfile builds a profile for a db without a saved one, by reusing the
// heuristic classifier (all non-noise fields + allowed docs + comments).
func keepAllProfile(schema map[string]any, db string) profile.Profile {
	prof, _ := classifyProfile(schema, db)
	prof.Comments = "full"
	return prof
}

// --- JSON output ---

func wantJSON() bool {
	return outputFormat == "json" || (outputFormat == "" && jsonOutput)
}

// contextBundle is the structured --json form of a context bundle.
type contextBundle struct {
	Root     json.RawMessage              `json:"root"`
	Comments []commentRow                 `json:"comments,omitempty"`
	Edges    map[string][]json.RawMessage `json:"edges"`
	Parents  []json.RawMessage            `json:"parents,omitempty"`
}

// emitContextJSON assembles and prints the structured bundle.
func emitContextJSON(ctx context.Context, schema map[string]any, profiles *profile.Profiles, db, entityID string, prof profile.Profile, withComments bool, edgeCmd client.Command, meta map[string]edgeAlias) error {
	root, _, err := fetchEntityFiltered(ctx, schema, db, entityID, prof)
	if err != nil {
		return err
	}
	bundle := contextBundle{Root: root, Edges: map[string][]json.RawMessage{}}

	if withComments {
		rows, err := collectCommentRows(ctx, db, entityID, 0, "")
		if err != nil {
			return err
		}
		bundle.Comments = rows
	}

	edgeRes, err := cli.One(ctx, edgeCmd)
	if err != nil {
		return err
	}
	var edgeRows []map[string]any
	seenParent := map[string]bool{}
	if err := json.Unmarshal(edgeRes, &edgeRows); err == nil && len(edgeRows) > 0 {
		row := edgeRows[0]
		for _, alias := range sortedAliases(meta) {
			m := meta[alias]
			if m.Multi {
				items, _ := row[alias].([]any)
				for _, it := range items {
					if b, mErr := json.Marshal(it); mErr == nil {
						bundle.Edges[m.Role] = append(bundle.Edges[m.Role], b)
					}
				}
				continue
			}
			name := asStr(row[alias+"_Name"])
			if name == "" {
				continue
			}
			obj := map[string]any{
				"label": m.Label,
				"pid":   asStr(row[alias+"_PID"]),
				"name":  name,
				"state": asStr(row[alias+"_State"]),
				"db":    m.TargetDB,
			}
			if b, mErr := json.Marshal(obj); mErr == nil {
				bundle.Edges[m.Role] = append(bundle.Edges[m.Role], b)
			}
			if contextParentsFull {
				if key := m.TargetDB + "#" + asStr(row[alias+"_PID"]); seenParent[key] {
					continue
				} else {
					seenParent[key] = true
				}
				uuid, rErr := resolveEntityRef(ctx, m.TargetDB, asStr(row[alias+"_PID"]))
				if rErr != nil {
					continue
				}
				pprof, ok := profiles.Get(m.TargetDB)
				if !ok {
					pprof = keepAllProfile(schema, m.TargetDB)
				}
				praw, _, pErr := fetchEntityFiltered(ctx, schema, m.TargetDB, uuid, pprof)
				if pErr == nil {
					bundle.Parents = append(bundle.Parents, praw)
				}
			}
		}
	}

	out, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(out))
	return nil
}

func init() {
	contextCmd.Flags().StringVar(&contextDB, "db", "", "database name (e.g. \"Development/Dev Task\")")
	contextCmd.Flags().BoolVar(&contextParentsFull, "parents-full", false, "expand parent nodes (epic/story) to full documents and comments")
	contextCmd.Flags().BoolVar(&contextNoComments, "no-comments", false, "suppress comments on root and parents")
	contextCmd.Flags().BoolVar(&contextNoDocs, "no-docs", false, "suppress document bodies (skim mode: header + graph + comments only)")
	contextCmd.AddCommand(contextInitCmd)
	rootCmd.AddCommand(contextCmd)
}
