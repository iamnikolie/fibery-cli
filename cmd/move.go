package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

// moveKind says how one source field is carried to the target.
type moveKind string

const (
	mvPrimitive moveKind = "value"
	mvEnum      moveKind = "single-select"
	mvUser      moveKind = "user"
	mvUsers     moveKind = "users"
	mvDoc       moveKind = "rich text"
	mvFiles     moveKind = "files"
	mvRelText   moveKind = "relation → text"
)

type moveField struct {
	Src, Dst string
	Kind     moveKind
	SrcType  string // fibery/type of the source field
	DstType  string
	SrcMany  bool
	TitleOf  string // mvRelText: title field of the related database
}

type movePlan struct {
	Src, Dst             string
	SrcTitle, DstTitle   string
	SrcTypeID, DstTypeID string
	Fields               []moveField
	Dropped              []string // "Field (reason)"
	Workflow             bool     // both sides have workflow/state
	Comments             bool     // both sides have comments
}

func typeMap(schema map[string]any, db string) map[string]any {
	for _, t := range mustSlice(schema["fibery/types"]) {
		if tm, ok := t.(map[string]any); ok && asStr(tm["fibery/name"]) == db {
			return tm
		}
	}
	return nil
}

func fieldMeta(fm map[string]any) map[string]any {
	m, _ := fm["fibery/meta"].(map[string]any)
	return m
}

// moveSkipFields are carried by the command itself or have no meaning on a copy.
var moveSkipFields = map[string]bool{
	"fibery/id": true, "fibery/public-id": true, "fibery/creation-date": true,
	"fibery/modification-date": true, "fibery/rank": true, "fibery/created-by": true,
	"workflow/state": true, "comments/comments": true,
	"Collaboration~Documents/References": true, "documents/documents": true,
}

// buildMovePlan matches src fields to dst fields by their name inside the space
// ("Space A/Priority" → "Space B/Priority") and checks the kinds agree. textMap
// sends a relation field's titles to a text field instead
// ("Old/Module" → "New/Legacy module"). Unmatched fields are listed as dropped.
func buildMovePlan(schema map[string]any, src, dst string, textMap map[string]string) (movePlan, error) {
	p := movePlan{Src: src, Dst: dst}
	st, dt := typeMap(schema, src), typeMap(schema, dst)
	if st == nil {
		return p, fmt.Errorf("database %q not found — run 'fibery schema sync'", src)
	}
	if dt == nil {
		return p, fmt.Errorf("database %q not found — run 'fibery schema sync'", dst)
	}
	if src == dst {
		return p, fmt.Errorf("source and target are the same database")
	}
	p.SrcTypeID, p.DstTypeID = asStr(st["fibery/id"]), asStr(dt["fibery/id"])
	p.SrcTitle, p.DstTitle = findTitleField(schema, src), findTitleField(schema, dst)
	p.Workflow = findSchemaField(schema, src, "workflow/state") != nil && findSchemaField(schema, dst, "workflow/state") != nil
	p.Comments = findSchemaField(schema, src, "comments/comments") != nil && findSchemaField(schema, dst, "comments/comments") != nil
	if findSchemaField(schema, src, "workflow/state") != nil && !p.Workflow {
		p.Dropped = append(p.Dropped, "workflow/state (target has no workflow)")
	}

	for f := range textMap {
		if findSchemaField(schema, src, f) == nil {
			return p, fmt.Errorf("--text %s: %s has no such field", f, src)
		}
	}
	for _, f := range mustSlice(st["fibery/fields"]) {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name, typ := asStr(fm["fibery/name"]), asStr(fm["fibery/type"])
		if moveSkipFields[name] || name == p.SrcTitle {
			continue
		}
		meta := fieldMeta(fm)
		if meta["fibery/formula"] != nil || meta["fibery/lookup"] != nil || meta["fibery/readonly?"] == true {
			continue
		}
		srcMany := meta["fibery/collection?"] == true
		if to, ok := textMap[name]; ok {
			dfm := findSchemaField(schema, dst, to)
			if dfm == nil || asStr(dfm["fibery/type"]) != "fibery/text" {
				return p, fmt.Errorf("--text %s=%s: %s has no text field %q", name, to, dst, to)
			}
			p.Fields = append(p.Fields, moveField{Src: name, Dst: to, Kind: mvRelText, SrcType: typ, DstType: "fibery/text",
				SrcMany: srcMany, TitleOf: findTitleField(schema, typ)})
			continue
		}
		if !strings.Contains(name, "/") || strings.HasPrefix(name, "fibery/") {
			continue
		}
		dstName := spaceOf(dst) + "/" + localName(name)
		dfm := findSchemaField(schema, dst, dstName)
		if dfm == nil {
			p.Dropped = append(p.Dropped, name+" (no "+dstName+")")
			continue
		}
		dtyp := asStr(dfm["fibery/type"])
		dstMany := fieldMeta(dfm)["fibery/collection?"] == true
		mf := moveField{Src: name, Dst: dstName, SrcType: typ, DstType: dtyp, SrcMany: srcMany}
		switch {
		case typ == "Collaboration~Documents/Document" && dtyp == typ:
			mf.Kind = mvDoc
		case typ == "fibery/file" && dtyp == typ:
			mf.Kind = mvFiles
		case typ == "fibery/user" && dtyp == typ && dstMany:
			mf.Kind = mvUsers
		case typ == "fibery/user" && dtyp == typ && !srcMany:
			mf.Kind = mvUser
		case isEnumLikeType(typ) && isEnumLikeType(dtyp) && !srcMany && !dstMany:
			mf.Kind = mvEnum
		case primitiveFieldTypes[typ] && typ == dtyp:
			mf.Kind = mvPrimitive
		default:
			p.Dropped = append(p.Dropped, fmt.Sprintf("%s (%s → %s: kinds differ)", name, typ, dtyp))
			continue
		}
		p.Fields = append(p.Fields, mf)
	}
	sort.Slice(p.Fields, func(i, j int) bool { return p.Fields[i].Src < p.Fields[j].Src })
	sort.Strings(p.Dropped)
	return p, nil
}

// moveSelect is the q/select that reads everything the plan carries.
func moveSelect(p movePlan) map[string]any {
	sel := map[string]any{
		"id":      []any{"fibery/id"},
		"pid":     []any{"fibery/public-id"},
		"created": []any{"fibery/creation-date"},
		"by":      []any{"fibery/created-by", "fibery/id"},
		"rank":    []any{"fibery/rank"},
	}
	if p.SrcTitle != "" {
		sel["title"] = []any{p.SrcTitle}
	}
	if p.Workflow {
		sel["state"] = []any{"workflow/state", "enum/name"}
	}
	if p.Comments {
		sel["comments"] = map[string]any{
			"q/from": "comments/comments",
			"q/select": map[string]any{
				"id": []any{"fibery/id"}, "secret": []any{"comment/document-secret"},
				"created": []any{"fibery/creation-date"}, "author": []any{"comment/author", "fibery/id"},
				"by": []any{"fibery/created-by", "fibery/id"}, "parent": []any{"comments/parent", "fibery/id"},
			},
			"q/order-by": []any{[]any{[]any{"fibery/creation-date"}, "q/asc"}},
			"q/limit":    "q/no-limit",
		}
	}
	for i, f := range p.Fields {
		k := "f" + strconv.Itoa(i)
		switch f.Kind {
		case mvPrimitive:
			sel[k] = []any{f.Src}
		case mvEnum:
			sel[k] = []any{f.Src, "enum/name"}
		case mvUser:
			sel[k] = []any{f.Src, "fibery/id"}
		case mvUsers:
			if f.SrcMany {
				sel[k] = map[string]any{"q/from": f.Src, "q/select": map[string]any{"id": []any{"fibery/id"}}, "q/limit": "q/no-limit"}
			} else {
				sel[k] = []any{f.Src, "fibery/id"}
			}
		case mvDoc:
			sel[k] = []any{f.Src, "Collaboration~Documents/secret"}
		case mvFiles:
			sel[k] = map[string]any{"q/from": f.Src, "q/select": map[string]any{
				"secret": []any{"fibery/secret"}, "name": []any{"fibery/name"}, "ct": []any{"fibery/content-type"},
			}, "q/limit": "q/no-limit"}
		case mvRelText:
			title := f.TitleOf
			if title == "" {
				title = "fibery/public-id"
			}
			if f.SrcMany {
				sel[k] = map[string]any{"q/from": f.Src, "q/select": map[string]any{"t": []any{title}}, "q/limit": "q/no-limit"}
			} else {
				sel[k] = []any{f.Src, title}
			}
		}
	}
	return sel
}

var reFileSecret = regexp.MustCompile(`/api/files/([A-Za-z0-9-]{16,})`)

// docFileSecrets lists the distinct file secrets a document's JSON points at.
func docFileSecrets(raw []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range reFileSecret.FindAllSubmatch(raw, -1) {
		s := string(m[1])
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// remapMentions points entity mentions ({"type":"entity","attrs":{id,typeId}})
// of moved entities at their copies. It reports whether anything changed.
func remapMentions(v any, srcTypeID, dstTypeID string, ids map[string]string) bool {
	changed := false
	switch n := v.(type) {
	case map[string]any:
		if n["type"] == "entity" {
			if a, ok := n["attrs"].(map[string]any); ok && asStr(a["typeId"]) == srcTypeID {
				if nid, ok := ids[asStr(a["id"])]; ok {
					a["id"], a["typeId"] = nid, dstTypeID
					changed = true
				}
			}
		}
		for _, c := range n {
			if remapMentions(c, srcTypeID, dstTypeID, ids) {
				changed = true
			}
		}
	case []any:
		for _, c := range n {
			if remapMentions(c, srcTypeID, dstTypeID, ids) {
				changed = true
			}
		}
	}
	return changed
}

// legacyIDValue is what the legacy-id field holds on a copy: "Space/DB #42".
func legacyIDValue(src, publicID string) string { return src + " #" + publicID }

// parsePairs turns "A=B" flags into a map.
func parsePairs(flag string, raw []string) (map[string]string, error) {
	out := map[string]string{}
	for _, r := range raw {
		k, v, ok := strings.Cut(r, "=")
		if !ok || strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("--%s %q: expected A=B", flag, r)
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out, nil
}

// targetState picks the target state for a source state name: --state-map
// first, then the same name ignoring case.
func targetState(name string, stateMap map[string]string, states map[string]string) (string, bool) {
	if m, ok := stateMap[name]; ok {
		name = m
	}
	for n, id := range states {
		if strings.EqualFold(n, name) {
			return id, true
		}
	}
	return "", false
}

var (
	moveTo, moveLegacy, moveOut string
	moveSet, moveStateMap       []string
	moveText, moveFilter        []string
	moveLimit                   int
	moveYes, moveDryRun         bool
)

var moveCmd = &cobra.Command{
	Use:   "move <source-database> --to <target-database>",
	Short: "Copy entities to another database with documents, files, comments, authors and dates",
	Long: `Copy every entity of a database (or those matching --filter) into another
database. Fields are matched by their name inside the space ("Old/Priority" →
"New/Priority"): values, single-selects (by option name), users, rich text
(Fibery JSON, so formatting and inline comments survive), attachments and
images pasted into documents (downloaded and uploaded again, so the copy does not
depend on the source). The workflow state goes by name (--state-map renames).
Creation date, author and order are kept; comments are copied with their author,
date and replies. Mentions between moved entities are pointed at the copies.

The source is not changed. --legacy-id names a text field on the target that
gets "Source/DB #<public id>"; entities already copied (that value exists) are
skipped, so a run can be repeated after a failure. --set gives a value to every
copy; --text sends a relation's titles to a text field. Fields with no
counterpart are listed as dropped in the plan.

Prints the plan; nothing changes without --yes. --out writes the old → new id
map as JSON.

Example:
  fibery move "Old/Tasks" --to "New/Work" --legacy-id "Legacy ID" \
    --set Type=Task --set Project=Old --set Archive=true \
    --state-map Done=Released --text "Old/Module=New/Legacy module" --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runMove,
}

func runMove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	schema, err := cache.LoadSchema(account)
	if err != nil {
		return fmt.Errorf("no schema — run: fibery schema sync")
	}
	src, dst := args[0], moveTo
	if dst == "" {
		return fmt.Errorf("--to is required")
	}
	if moveLegacy == "" {
		return fmt.Errorf("--legacy-id is required (a text field on %s that records where each copy came from)", dst)
	}
	legacy := resolveFieldName(schema, dst, qualifyField(dst, moveLegacy))
	if findFieldType(schema, dst, legacy) != "fibery/text" {
		return fmt.Errorf("--legacy-id %q is not a text field of %s", legacy, dst)
	}
	textMap, err := parsePairs("text", moveText)
	if err != nil {
		return err
	}
	stateMap, err := parsePairs("state-map", moveStateMap)
	if err != nil {
		return err
	}
	p, err := buildMovePlan(schema, src, dst, textMap)
	if err != nil {
		return err
	}
	for _, f := range p.Fields {
		if f.Dst == legacy {
			return fmt.Errorf("%s is filled from %s and also used as --legacy-id", legacy, f.Src)
		}
	}

	// Constants.
	consts := map[string]any{}
	var constNames []string
	for _, s := range moveSet {
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			return fmt.Errorf("--set %q: expected Field=value", s)
		}
		field := resolveFieldName(schema, dst, qualifyField(dst, strings.TrimSpace(k)))
		if err := validateField(schema, dst, field); err != nil {
			return err
		}
		val, err := constValue(ctx, schema, dst, field, strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("--set %s: %w", s, err)
		}
		consts[field] = val
		constNames = append(constNames, field+"="+strings.TrimSpace(v))
	}

	// Source rows.
	query := map[string]any{"q/from": src, "q/select": moveSelect(p), "q/order-by": []any{[]any{[]any{"fibery/rank"}, "q/asc"}}}
	var params map[string]any
	if len(moveFilter) > 0 {
		where, ps, err := buildFilterClause(schema, src, moveFilter)
		if err != nil {
			return err
		}
		query["q/where"], params = where, ps
	}
	raw, err := cli.QueryAll(ctx, query, params, 100)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return err
	}
	if moveLimit > 0 && len(rows) > moveLimit {
		rows = rows[:moveLimit]
	}

	// Already copied.
	doneRaw, err := cli.QueryAll(ctx, map[string]any{"q/from": dst, "q/select": map[string]any{"id": []any{"fibery/id"}, "l": []any{legacy}}}, nil, 500)
	if err != nil {
		return fmt.Errorf("read %s: %w", dst, err)
	}
	var doneRows []map[string]any
	_ = json.Unmarshal(doneRaw, &doneRows)
	done := map[string]string{}
	for _, r := range doneRows {
		if l := asStr(r["l"]); l != "" {
			done[l] = asStr(r["id"])
		}
	}

	// Target states.
	states := map[string]string{}
	if p.Workflow {
		ids, names, err := stateNames(ctx, dst)
		if err != nil {
			return err
		}
		for i := range ids {
			states[names[i]] = ids[i]
		}
	}

	// Plan.
	stateCount := map[string]int{}
	var todo []map[string]any
	nComments, nFiles := 0, 0
	for _, r := range rows {
		if _, ok := done[legacyIDValue(src, asStr(r["pid"]))]; ok {
			continue
		}
		todo = append(todo, r)
		stateCount[asStr(r["state"])]++
		nComments += len(mustSlice(r["comments"]))
		for i, f := range p.Fields {
			if f.Kind == mvFiles {
				nFiles += len(mustSlice(r["f"+strconv.Itoa(i)]))
			}
		}
	}
	fmt.Printf("## Move %s → %s\n\n", src, dst)
	fmt.Printf("Entities: %d to copy, %d already copied (by %s)\n", len(todo), len(rows)-len(todo), legacy)
	fmt.Printf("Comments: %d · attachments: %d\n\n", nComments, nFiles)
	fmt.Printf("### Fields\n\n- %s → %s (title)\n", p.SrcTitle, p.DstTitle)
	for _, f := range p.Fields {
		fmt.Printf("- %s → %s (%s)\n", f.Src, f.Dst, f.Kind)
	}
	fmt.Printf("- %s ← \"%s #<id>\"\n", legacy, src)
	for _, c := range constNames {
		fmt.Printf("- %s (every copy)\n", c)
	}
	if len(p.Dropped) > 0 {
		fmt.Printf("\n### Dropped (%d)\n\n", len(p.Dropped))
		for _, d := range p.Dropped {
			fmt.Println("- " + d)
		}
	}
	var missing []string
	if p.Workflow {
		fmt.Printf("\n### States\n\n")
		var names []string
		for n := range stateCount {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			_, ok := targetState(n, stateMap, states)
			to := n
			if m, ok := stateMap[n]; ok {
				to = m
			}
			mark := ""
			if !ok {
				mark = " — MISSING on target"
				missing = append(missing, n)
			}
			fmt.Printf("- %s (%d) → %s%s\n", n, stateCount[n], to, mark)
		}
	}
	fmt.Println()
	if len(missing) > 0 {
		return fmt.Errorf("states %s have no target state — add --state-map Old=New", strings.Join(missing, ", "))
	}
	if moveDryRun {
		fmt.Println("Dry run — nothing changed.")
		return nil
	}
	if !moveYes {
		return fmt.Errorf("re-run with --yes to copy the entities above")
	}

	m := &mover{ctx: ctx, schema: schema, p: p, legacy: legacy, consts: consts, states: states, stateMap: stateMap, ids: map[string]string{}}
	// Ids of entities copied in earlier runs, so mentions of them resolve too.
	for _, r := range rows {
		if nid, ok := done[legacyIDValue(src, asStr(r["pid"]))]; ok {
			m.ids[asStr(r["id"])] = nid
		}
	}
	var mapOut []map[string]string
	for i, r := range todo {
		nid, err := m.copyOne(r)
		if err != nil {
			return fmt.Errorf("#%s %q (%d of %d): %w — fix and re-run; copied entities are skipped", asStr(r["pid"]), asStr(r["title"]), i+1, len(todo), err)
		}
		mapOut = append(mapOut, map[string]string{"old": asStr(r["id"]), "old_public_id": asStr(r["pid"]), "new": nid})
		fmt.Printf("  %d/%d #%s → %s\n", i+1, len(todo), asStr(r["pid"]), nid)
	}
	n, err := m.fixMentions()
	if err != nil {
		return fmt.Errorf("copied, but mentions not remapped: %w", err)
	}
	fmt.Printf("Copied %d entities, %d comments, %d files, %d images; remapped mentions in %d documents.\n",
		len(todo), m.nComments, m.nFiles, m.nImages, n)
	if moveOut != "" {
		b, _ := json.MarshalIndent(map[string]any{"source": src, "target": dst, "entities": mapOut}, "", "  ")
		if err := os.WriteFile(moveOut, b, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", moveOut, err)
		}
		fmt.Println("Id map written to " + moveOut)
	}
	return nil
}

// constValue converts a --set value for the field's type.
func constValue(ctx context.Context, schema map[string]any, db, field, v string) (any, error) {
	switch findFieldType(schema, db, field) {
	case "fibery/bool":
		return strconv.ParseBool(v)
	case "fibery/int":
		return strconv.Atoi(v)
	case "fibery/decimal":
		return strconv.ParseFloat(v, 64)
	}
	return resolveFieldValueOpts(ctx, schema, db, field, v, resolveOpts{})
}

// stateNames lists db's workflow states (ids and names, same order).
func stateNames(ctx context.Context, db string) (ids, names []string, err error) {
	res, err := cli.One(ctx, client.Command{Command: "fibery.entity/query", Args: map[string]any{"query": map[string]any{
		"q/from": "workflow/state_" + db, "q/select": map[string]any{"id": []any{"fibery/id"}, "n": []any{"enum/name"}}, "q/limit": "q/no-limit",
	}}})
	if err != nil {
		return nil, nil, err
	}
	var rows []struct{ ID, N string }
	if err := json.Unmarshal(res, &rows); err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		ids, names = append(ids, r.ID), append(names, r.N)
	}
	return ids, names, nil
}

type mover struct {
	ctx                        context.Context
	schema                     map[string]any
	p                          movePlan
	legacy                     string
	consts                     map[string]any
	states                     map[string]string
	stateMap                   map[string]string
	enums                      map[string]any    // enumType + "\x00" + name → ref
	ids                        map[string]string // source id → copy id
	docs                       []string          // target doc secrets that mention source entities
	nComments, nFiles, nImages int
}

func ref2(id string) any { return map[string]any{"fibery/id": id} }

func (m *mover) enumRef(typ, name string) (any, error) {
	if m.enums == nil {
		m.enums = map[string]any{}
	}
	k := typ + "\x00" + name
	if v, ok := m.enums[k]; ok {
		return v, nil
	}
	v, err := resolveEnumByName(m.ctx, typ, name)
	if err != nil {
		return nil, err
	}
	m.enums[k] = v
	return v, nil
}

func (m *mover) copyOne(r map[string]any) (string, error) {
	ctx, p := m.ctx, m.p
	nid := client.NewUUID()
	e := map[string]any{"fibery/id": nid}
	if p.DstTitle != "" {
		e[p.DstTitle] = r["title"]
	}
	if v := asStr(r["created"]); v != "" {
		e["fibery/creation-date"] = v
	}
	if v := asStr(r["by"]); v != "" {
		e["fibery/created-by"] = ref2(v)
	}
	if r["rank"] != nil {
		e["fibery/rank"] = r["rank"]
	}
	e[m.legacy] = legacyIDValue(p.Src, asStr(r["pid"]))
	for k, v := range m.consts {
		e[k] = v
	}
	if p.Workflow && asStr(r["state"]) != "" {
		id, _ := targetState(asStr(r["state"]), m.stateMap, m.states)
		e["workflow/state"] = ref2(id)
	}
	type later struct {
		f moveField
		v any
	}
	var after []later
	for i, f := range p.Fields {
		v := r["f"+strconv.Itoa(i)]
		if v == nil {
			continue
		}
		switch f.Kind {
		case mvPrimitive:
			e[f.Dst] = v
		case mvEnum:
			ev, err := m.enumRef(f.DstType, asStr(v))
			if err != nil {
				return "", fmt.Errorf("%s: %w", f.Dst, err)
			}
			e[f.Dst] = ev
		case mvUser:
			e[f.Dst] = ref2(asStr(v))
		case mvRelText:
			if f.SrcMany {
				var ts []string
				for _, x := range mustSlice(v) {
					if xm, ok := x.(map[string]any); ok && asStr(xm["t"]) != "" {
						ts = append(ts, asStr(xm["t"]))
					}
				}
				if len(ts) > 0 {
					e[f.Dst] = strings.Join(ts, ", ")
				}
			} else if s := asStr(v); s != "" {
				e[f.Dst] = s
			}
		default:
			after = append(after, later{f, v})
		}
	}
	if _, err := cli.One(ctx, client.Command{Command: "fibery.entity/create", Args: map[string]any{"type": p.Dst, "entity": e}}); err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	m.ids[asStr(r["id"])] = nid

	for _, a := range after {
		switch a.f.Kind {
		case mvUsers:
			var items []any
			if a.f.SrcMany {
				for _, x := range mustSlice(a.v) {
					if xm, ok := x.(map[string]any); ok {
						items = append(items, ref2(asStr(xm["id"])))
					}
				}
			} else if s := asStr(a.v); s != "" {
				items = append(items, ref2(s))
			}
			if len(items) > 0 {
				if err := addCollectionItems(ctx, p.Dst, nid, a.f.Dst, items); err != nil {
					return nid, fmt.Errorf("%s: %w", a.f.Dst, err)
				}
			}
		case mvDoc:
			srcSecret := asStr(a.v)
			if srcSecret == "" {
				continue
			}
			dstSecret, err := resolveDocSecretByID(ctx, p.Dst, nid, a.f.Dst)
			if err != nil {
				return nid, err
			}
			if err := m.copyDoc(srcSecret, dstSecret); err != nil {
				return nid, fmt.Errorf("%s: %w", a.f.Dst, err)
			}
		case mvFiles:
			var items []any
			for _, x := range mustSlice(a.v) {
				xm, ok := x.(map[string]any)
				if !ok {
					continue
				}
				data, ct, err := cli.DownloadFile(ctx, asStr(xm["secret"]))
				if err != nil {
					return nid, fmt.Errorf("download %q: %w", asStr(xm["name"]), err)
				}
				if s := asStr(xm["ct"]); s != "" {
					ct = s
				}
				fi, err := cli.UploadFile(ctx, asStr(xm["name"]), ct, data)
				if err != nil {
					return nid, err
				}
				items = append(items, ref2(fi.ID))
			}
			if len(items) > 0 {
				if err := addCollectionItems(ctx, p.Dst, nid, a.f.Dst, items); err != nil {
					return nid, fmt.Errorf("%s: %w", a.f.Dst, err)
				}
				m.nFiles += len(items)
			}
		}
	}

	// Comments, oldest first so a reply's parent already exists.
	cmap := map[string]string{}
	for _, c := range mustSlice(r["comments"]) {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		cid, secret := client.NewUUID(), client.NewUUID()
		ce := map[string]any{"fibery/id": cid, "comment/document-secret": secret}
		if v := asStr(cm["created"]); v != "" {
			ce["fibery/creation-date"] = v
		}
		if v := asStr(cm["author"]); v != "" {
			ce["comment/author"] = ref2(v)
		}
		if v := asStr(cm["by"]); v != "" {
			ce["fibery/created-by"] = ref2(v)
		}
		if par := cmap[asStr(cm["parent"])]; par != "" {
			ce["comments/parent"] = ref2(par)
		}
		if _, err := cli.One(ctx, client.Command{Command: "fibery.command/batch", Args: map[string]any{"commands": []any{
			map[string]any{"command": "fibery.entity/create", "args": map[string]any{"type": "comments/comment", "entity": ce}},
			map[string]any{"command": "fibery.entity/add-collection-items", "args": map[string]any{
				"type": p.Dst, "entity": ref2(nid), "field": "comments/comments", "items": []any{ref2(cid)},
			}},
		}}}); err != nil {
			return nid, fmt.Errorf("comment: %w", err)
		}
		cmap[asStr(cm["id"])] = cid
		if err := cli.CreateDocumentMarkdown(ctx, secret, ""); err != nil {
			return nid, fmt.Errorf("comment body: %w", err)
		}
		if s := asStr(cm["secret"]); s != "" {
			if err := m.copyDoc(s, secret); err != nil {
				return nid, fmt.Errorf("comment body: %w", err)
			}
		}
		m.nComments++
	}
	return nid, nil
}

// copyDoc copies a document as JSON. Images it points at are uploaded again and
// relinked, so the copy does not depend on files owned by the source.
func (m *mover) copyDoc(srcSecret, dstSecret string) error {
	d, err := cli.GetDocumentJSON(m.ctx, srcSecret)
	if err != nil {
		return err
	}
	b, err := json.Marshal(d.Content)
	if err != nil {
		return err
	}
	for _, s := range docFileSecrets(b) {
		data, ct, err := cli.DownloadFile(m.ctx, s)
		if err != nil {
			return fmt.Errorf("image %s: %w", s, err)
		}
		fi, err := cli.UploadFile(m.ctx, "image", ct, data)
		if err != nil {
			return err
		}
		b = []byte(strings.ReplaceAll(string(b), s, fi.Secret))
		m.nImages++
	}
	var c client.DocContent
	if err := json.Unmarshal(b, &c); err != nil {
		return err
	}
	if c.Comments == nil {
		c.Comments = []any{}
	}
	if strings.Contains(string(b), m.p.SrcTypeID) {
		m.docs = append(m.docs, dstSecret)
	}
	return cli.SetDocumentJSON(m.ctx, dstSecret, c)
}

// fixMentions rewrites, in the copied documents, mentions of source entities
// that now have copies.
func (m *mover) fixMentions() (int, error) {
	n := 0
	for _, s := range m.docs {
		d, err := cli.GetDocumentJSON(m.ctx, s)
		if err != nil {
			return n, err
		}
		var doc any
		if err := json.Unmarshal(d.Content.Doc, &doc); err != nil {
			return n, err
		}
		if !remapMentions(doc, m.p.SrcTypeID, m.p.DstTypeID, m.ids) {
			continue
		}
		b, _ := json.Marshal(doc)
		d.Content.Doc = b
		if err := cli.SetDocumentJSON(m.ctx, s, d.Content); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func init() {
	f := moveCmd.Flags()
	f.StringVar(&moveTo, "to", "", "target database (required)")
	f.StringVar(&moveLegacy, "legacy-id", "", "text field on the target for \"Source/DB #<id>\" (required; makes re-runs skip copied entities)")
	f.StringArrayVar(&moveSet, "set", nil, "Field=value for every copy (repeatable)")
	f.StringArrayVar(&moveStateMap, "state-map", nil, "Old=New workflow state (repeatable)")
	f.StringArrayVar(&moveText, "text", nil, "SrcRelation=DstTextField: relation titles into a text field (repeatable)")
	f.StringArrayVar(&moveFilter, "filter", nil, "only source entities matching field=value (repeatable, as in list)")
	f.IntVar(&moveLimit, "limit", 0, "copy at most N entities")
	f.StringVar(&moveOut, "out", "", "write the old → new id map to this JSON file")
	f.BoolVar(&moveYes, "yes", false, "copy (required)")
	f.BoolVar(&moveDryRun, "dry-run", false, "print the plan and stop")
	rootCmd.AddCommand(moveCmd)
}
