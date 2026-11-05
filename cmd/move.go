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
	"github.com/iamnikolie/fibery-cli/internal/config"
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
func buildMovePlan(ss, ds map[string]any, src, dst string, textMap map[string]string, cross bool) (movePlan, error) {
	p := movePlan{Src: src, Dst: dst}
	st, dt := typeMap(ss, src), typeMap(ds, dst)
	if st == nil {
		return p, fmt.Errorf("database %q not found — run 'fibery schema sync'", src)
	}
	if dt == nil {
		return p, fmt.Errorf("database %q not found — run 'fibery schema sync'", dst)
	}
	if src == dst && !cross {
		return p, fmt.Errorf("source and target are the same database")
	}
	p.SrcTypeID, p.DstTypeID = asStr(st["fibery/id"]), asStr(dt["fibery/id"])
	p.SrcTitle, p.DstTitle = findTitleField(ss, src), findTitleField(ds, dst)
	p.Workflow = findSchemaField(ss, src, "workflow/state") != nil && findSchemaField(ds, dst, "workflow/state") != nil
	p.Comments = findSchemaField(ss, src, "comments/comments") != nil && findSchemaField(ds, dst, "comments/comments") != nil
	if findSchemaField(ss, src, "workflow/state") != nil && !p.Workflow {
		p.Dropped = append(p.Dropped, "workflow/state (target has no workflow)")
	}

	for f := range textMap {
		if findSchemaField(ss, src, f) == nil {
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
			dfm := findSchemaField(ds, dst, to)
			if dfm == nil || asStr(dfm["fibery/type"]) != "fibery/text" {
				return p, fmt.Errorf("--text %s=%s: %s has no text field %q", name, to, dst, to)
			}
			title := findTitleField(ss, typ)
			switch {
			case isEnumLikeType(typ):
				title = "enum/name"
			case typ == "fibery/user":
				title = "user/name"
			}
			p.Fields = append(p.Fields, moveField{Src: name, Dst: to, Kind: mvRelText, SrcType: typ, DstType: "fibery/text",
				SrcMany: srcMany, TitleOf: title})
			continue
		}
		if !strings.Contains(name, "/") || strings.HasPrefix(name, "fibery/") {
			continue
		}
		dstName := spaceOf(dst) + "/" + localName(name)
		dfm := findSchemaField(ds, dst, dstName)
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
	moveToConfig, moveWhere     string
	moveParams                  string
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
copy and wins over a source field of the same name; --text sends a
relation's (or single-select's) values to a text field as "Field: value",
several into one field joined with " · ". Fields with no
counterpart are listed as dropped in the plan.

--to-config copies into the workspace of another account config: users go by
email (a missing one becomes the target token's user, named in the comments
they wrote) and embedded views are dropped. --where/--params select the source
rows with an FQL clause when --filter is not enough.

Prints the plan; nothing changes without --yes. --out writes the old → new id
map as JSON (with both type ids, for 'wiki copy --id-map').

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
	tgt, err := loadMoveTarget(schema)
	if err != nil {
		return err
	}
	ds := tgt.schema
	legacy := resolveFieldName(ds, dst, qualifyField(dst, moveLegacy))
	if findFieldType(ds, dst, legacy) != "fibery/text" {
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
	p, err := buildMovePlan(schema, ds, src, dst, textMap, tgt.cross)
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
		field := resolveFieldName(ds, dst, qualifyField(dst, strings.TrimSpace(k)))
		if err := validateField(ds, dst, field); err != nil {
			return err
		}
		var val any
		if err := onClient(tgt.cli, func() (err error) {
			val, err = constValue(ctx, ds, dst, field, strings.TrimSpace(v))
			return err
		}); err != nil {
			return fmt.Errorf("--set %s: %w", s, err)
		}
		consts[field] = val
		constNames = append(constNames, field+"="+strings.TrimSpace(v))
	}

	// A --set field wins over a source field mapped onto it.
	kept := p.Fields[:0]
	for _, f := range p.Fields {
		if _, ok := consts[f.Dst]; ok {
			p.Dropped = append(p.Dropped, f.Src+" (overridden by --set "+f.Dst+")")
			continue
		}
		kept = append(kept, f)
	}
	p.Fields = kept
	sort.Strings(p.Dropped)

	// Source rows.
	query := map[string]any{"q/from": src, "q/select": moveSelect(p), "q/order-by": []any{[]any{[]any{"fibery/rank"}, "q/asc"}}}
	var params map[string]any
	switch {
	case len(moveFilter) > 0 && moveWhere != "":
		return fmt.Errorf("use either --where or --filter, not both")
	case len(moveFilter) > 0:
		where, ps, err := buildFilterClause(schema, src, moveFilter)
		if err != nil {
			return err
		}
		query["q/where"], params = where, ps
	case moveWhere != "":
		clause, err := parseWhereClause(moveWhere)
		if err != nil {
			return err
		}
		query["q/where"] = clause
		if moveParams != "" {
			if err := json.Unmarshal([]byte(moveParams), &params); err != nil {
				return fmt.Errorf("invalid --params JSON: %w", err)
			}
			params = normalizeFQLParams(params)
		}
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
	label := src
	if tgt.cross {
		label = cfg.Workspace + ":" + src
	}
	doneRaw, err := tgt.cli.QueryAll(ctx, map[string]any{"q/from": dst, "q/select": map[string]any{"id": []any{"fibery/id"}, "l": []any{legacy}}}, nil, 500)
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
		ids, names, err := stateNames(ctx, tgt.cli, dst)
		if err != nil {
			return err
		}
		for i := range ids {
			states[names[i]] = ids[i]
		}
	}

	// Users across workspaces.
	var users *userMap
	if tgt.cross {
		if users, err = loadUserMap(ctx, cli, tgt.cli); err != nil {
			return err
		}
	}

	// Plan.
	stateCount := map[string]int{}
	var todo []map[string]any
	nComments, nFiles := 0, 0
	unmapped := map[string]int{}
	for _, r := range rows {
		if _, ok := done[legacyIDValue(label, asStr(r["pid"]))]; ok {
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
		if users != nil {
			users.countUnmapped(r, unmapped)
		}
	}
	to := dst
	if tgt.cross {
		to = moveToConfig + ":" + dst
	}
	fmt.Printf("## Move %s → %s\n\n", label, to)
	fmt.Printf("Entities: %d to copy, %d already copied (by %s)\n", len(todo), len(rows)-len(todo), legacy)
	fmt.Printf("Comments: %d · attachments: %d\n\n", nComments, nFiles)
	fmt.Printf("### Fields\n\n- %s → %s (title)\n", p.SrcTitle, p.DstTitle)
	for _, f := range p.Fields {
		fmt.Printf("- %s → %s (%s)\n", f.Src, f.Dst, f.Kind)
	}
	fmt.Printf("- %s ← \"%s #<id>\"\n", legacy, label)
	for _, c := range constNames {
		fmt.Printf("- %s (every copy)\n", c)
	}
	if len(p.Dropped) > 0 {
		fmt.Printf("\n### Dropped (%d)\n\n", len(p.Dropped))
		for _, d := range p.Dropped {
			fmt.Println("- " + d)
		}
	}
	if len(unmapped) > 0 {
		fmt.Printf("\n### Users missing on the target (→ %s, name kept in comment text)\n\n", users.fallbackEmail)
		var names []string
		for n := range unmapped {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Printf("- %s (%d)\n", n, unmapped[n])
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

	m := &mover{ctx: ctx, src: cli, dst: tgt.cli, users: users, label: label, p: p, legacy: legacy, consts: consts, states: states, stateMap: stateMap, ids: map[string]string{}}
	// Ids of entities copied in earlier runs, so mentions of them resolve too.
	for _, r := range rows {
		if nid, ok := done[legacyIDValue(label, asStr(r["pid"]))]; ok {
			m.ids[asStr(r["id"])] = nid
		}
	}
	var mapOut []map[string]string
	for i, r := range todo {
		nid, err := m.copyOne(r)
		if err != nil && nid != "" {
			// A half-made copy would be skipped by the next run (its legacy id
			// exists), losing what was not copied yet — remove it instead.
			if _, derr := m.dst.One(ctx, client.Command{Command: "fibery.entity/delete", Args: map[string]any{
				"type": dst, "entity": map[string]any{"fibery/id": nid},
			}}); derr != nil {
				err = fmt.Errorf("%w; the partial copy %s could not be removed (%v) — delete it before re-running", err, nid, derr)
			}
			delete(m.ids, asStr(r["id"]))
		}
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
	if m.nViews > 0 {
		fmt.Printf("Dropped %d embedded views (they belong to the source workspace).\n", m.nViews)
	}
	if moveOut != "" {
		b, _ := json.MarshalIndent(map[string]any{"source": label, "target": to, "source_type_id": p.SrcTypeID, "target_type_id": p.DstTypeID, "entities": mapOut}, "", "  ")
		if err := os.WriteFile(moveOut, b, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", moveOut, err)
		}
		fmt.Println("Id map written to " + moveOut)
	}
	return nil
}

// moveTarget is where copies go: this workspace, or the one --to-config names.
type moveTarget struct {
	cli    *client.Client
	schema map[string]any
	cross  bool
}

func loadMoveTarget(srcSchema map[string]any) (*moveTarget, error) {
	if moveToConfig == "" || moveToConfig == account {
		return &moveTarget{cli: cli, schema: srcSchema}, nil
	}
	tc, err := otherClient(moveToConfig)
	if err != nil {
		return nil, err
	}
	ts, err := cache.LoadSchema(moveToConfig)
	if err != nil {
		return nil, fmt.Errorf("no schema for --to-config %s — run: fibery --config %s schema sync", moveToConfig, moveToConfig)
	}
	return &moveTarget{cli: tc, schema: ts, cross: true}, nil
}

// otherClient builds a client for another account config. It refuses a config
// that points at this same workspace: ids would then collide with the source.
func otherClient(name string) (*client.Client, error) {
	tc, err := config.Load(name)
	if err != nil {
		return nil, err
	}
	if err := tc.Validate(); err != nil {
		return nil, fmt.Errorf("--to-config %s: %w", name, err)
	}
	if tc.BaseURL() == cfg.BaseURL() {
		return nil, fmt.Errorf("--to-config %s is the same workspace (%s) — drop --to-config", name, tc.BaseURL())
	}
	c := client.New(tc.APIToken, tc.BaseURL())
	c.Verbose = verbose
	return c, nil
}

// onClient runs f with the package client switched to c: the shared lookup
// helpers (enums, users, document secrets) all talk to cli.
func onClient(c *client.Client, f func() error) error {
	prev := cli
	cli = c
	defer func() { cli = prev }()
	return f()
}

// userMap carries users from one workspace to another by email. A source user
// with no account on the target becomes the target token's user.
type userMap struct {
	ids           map[string]string // source id → target id
	names         map[string]string // source id → name, for users with no target id
	fallback      string
	fallbackEmail string
}

func loadUserMap(ctx context.Context, src, dst *client.Client) (*userMap, error) {
	read := func(c *client.Client) ([]map[string]any, error) {
		res, err := c.One(ctx, client.Command{Command: "fibery.entity/query", Args: map[string]any{"query": map[string]any{
			"q/from": "fibery/user", "q/select": map[string]any{"id": []any{"fibery/id"}, "e": []any{"user/email"}, "n": []any{"user/name"}},
			"q/limit": "q/no-limit",
		}}})
		if err != nil {
			return nil, fmt.Errorf("read users: %w", err)
		}
		var rows []map[string]any
		return rows, json.Unmarshal(res, &rows)
	}
	sr, err := read(src)
	if err != nil {
		return nil, err
	}
	dr, err := read(dst)
	if err != nil {
		return nil, err
	}
	u := &userMap{ids: map[string]string{}, names: map[string]string{}}
	if err := onClient(dst, func() (err error) { u.fallback, err = currentUserID(ctx); return err }); err != nil {
		return nil, err
	}
	byEmail := map[string]string{}
	for _, r := range dr {
		byEmail[strings.ToLower(asStr(r["e"]))] = asStr(r["id"])
		if asStr(r["id"]) == u.fallback {
			u.fallbackEmail = asStr(r["e"])
		}
	}
	for _, r := range sr {
		if id, ok := byEmail[strings.ToLower(asStr(r["e"]))]; ok && asStr(r["e"]) != "" {
			u.ids[asStr(r["id"])] = id
		} else {
			n := asStr(r["n"])
			if n == "" {
				n = asStr(r["e"])
			}
			u.names[asStr(r["id"])] = n
		}
	}
	return u, nil
}

// to returns the target id for a source user.
func (u *userMap) to(id string) string {
	if v, ok := u.ids[id]; ok {
		return v
	}
	return u.fallback
}

// lost returns the name of a source user who has no target account, else "".
func (u *userMap) lost(id string) string {
	if _, ok := u.ids[id]; ok || id == "" {
		return ""
	}
	if n := u.names[id]; n != "" {
		return n
	}
	return id
}

// countUnmapped tallies, per user name, the rows and comments that will fall
// back to the target token's user.
func (u *userMap) countUnmapped(r map[string]any, out map[string]int) {
	if n := u.lost(asStr(r["by"])); n != "" {
		out[n]++
	}
	for _, c := range mustSlice(r["comments"]) {
		if cm, ok := c.(map[string]any); ok {
			if n := u.lost(asStr(cm["author"])); n != "" {
				out[n]++
			}
		}
	}
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
func stateNames(ctx context.Context, c *client.Client, db string) (ids, names []string, err error) {
	res, err := c.One(ctx, client.Command{Command: "fibery.entity/query", Args: map[string]any{"query": map[string]any{
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
	ctx                                context.Context
	src, dst                           *client.Client
	users                              *userMap // nil inside one workspace
	label                              string   // source of legacy ids
	schema                             map[string]any
	p                                  movePlan
	legacy                             string
	consts                             map[string]any
	states                             map[string]string
	stateMap                           map[string]string
	enums                              map[string]any    // enumType + "\x00" + name → ref
	ids                                map[string]string // source id → copy id
	docs                               []string          // target doc secrets that mention source entities
	nComments, nFiles, nImages, nViews int
}

func ref2(id string) any { return map[string]any{"fibery/id": id} }

// user returns the target id of a source user.
func (m *mover) user(id string) string {
	if m.users == nil {
		return id
	}
	return m.users.to(id)
}

// lostUser names a source user with no target account ("" when mapped).
func (m *mover) lostUser(id string) string {
	if m.users == nil {
		return ""
	}
	return m.users.lost(id)
}

func (m *mover) enumRef(typ, name string) (any, error) {
	if m.enums == nil {
		m.enums = map[string]any{}
	}
	k := typ + "\x00" + name
	if v, ok := m.enums[k]; ok {
		return v, nil
	}
	var v map[string]any
	if err := onClient(m.dst, func() (err error) { v, err = resolveEnumByName(m.ctx, typ, name); return err }); err != nil {
		return nil, err
	}
	m.enums[k] = v
	return v, nil
}

func (m *mover) addItems(id, field string, items []any) error {
	return onClient(m.dst, func() error { return addCollectionItems(m.ctx, m.p.Dst, id, field, items) })
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
		e["fibery/created-by"] = ref2(m.user(v))
	}
	if r["rank"] != nil {
		e["fibery/rank"] = r["rank"]
	}
	e[m.legacy] = legacyIDValue(m.label, asStr(r["pid"]))
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
			e[f.Dst] = ref2(m.user(asStr(v)))
		case mvRelText:
			var text string
			if f.SrcMany {
				var ts []string
				for _, x := range mustSlice(v) {
					if xm, ok := x.(map[string]any); ok && asStr(xm["t"]) != "" {
						ts = append(ts, asStr(xm["t"]))
					}
				}
				text = strings.Join(ts, ", ")
			} else {
				text = asStr(v)
			}
			if text != "" {
				// Several --text into one field: "Module: CFO · Version: v0.13".
				part := localName(f.Src) + ": " + text
				if prev, ok := e[f.Dst].(string); ok && prev != "" {
					part = prev + " · " + part
				}
				e[f.Dst] = part
			}
		default:
			after = append(after, later{f, v})
		}
	}
	if _, err := m.dst.One(ctx, client.Command{Command: "fibery.entity/create", Args: map[string]any{"type": p.Dst, "entity": e}}); err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	m.ids[asStr(r["id"])] = nid

	for _, a := range after {
		switch a.f.Kind {
		case mvUsers:
			seen := map[string]bool{}
			var items []any
			add := func(id string) {
				if id = m.user(id); id != "" && !seen[id] {
					seen[id] = true
					items = append(items, ref2(id))
				}
			}
			if a.f.SrcMany {
				for _, x := range mustSlice(a.v) {
					if xm, ok := x.(map[string]any); ok {
						add(asStr(xm["id"]))
					}
				}
			} else if s := asStr(a.v); s != "" {
				add(s)
			}
			if len(items) > 0 {
				if err := m.addItems(nid, a.f.Dst, items); err != nil {
					return nid, fmt.Errorf("%s: %w", a.f.Dst, err)
				}
			}
		case mvDoc:
			srcSecret := asStr(a.v)
			if srcSecret == "" {
				continue
			}
			var dstSecret string
			if err := onClient(m.dst, func() (err error) {
				dstSecret, err = resolveDocSecretByID(ctx, p.Dst, nid, a.f.Dst)
				return err
			}); err != nil {
				return nid, err
			}
			if err := m.copyDoc(srcSecret, dstSecret, ""); err != nil {
				return nid, fmt.Errorf("%s: %w", a.f.Dst, err)
			}
		case mvFiles:
			var items []any
			for _, x := range mustSlice(a.v) {
				xm, ok := x.(map[string]any)
				if !ok {
					continue
				}
				data, ct, err := m.src.DownloadFile(ctx, asStr(xm["secret"]))
				if err != nil {
					return nid, fmt.Errorf("download %q: %w", asStr(xm["name"]), err)
				}
				if s := asStr(xm["ct"]); s != "" {
					ct = s
				}
				fi, err := m.dst.UploadFile(ctx, asStr(xm["name"]), ct, data)
				if err != nil {
					return nid, err
				}
				items = append(items, ref2(fi.ID))
			}
			if len(items) > 0 {
				if err := m.addItems(nid, a.f.Dst, items); err != nil {
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
			ce["comment/author"] = ref2(m.user(v))
		}
		if v := asStr(cm["by"]); v != "" {
			ce["fibery/created-by"] = ref2(m.user(v))
		}
		if par := cmap[asStr(cm["parent"])]; par != "" {
			ce["comments/parent"] = ref2(par)
		}
		if _, err := m.dst.One(ctx, client.Command{Command: "fibery.command/batch", Args: map[string]any{"commands": []any{
			map[string]any{"command": "fibery.entity/create", "args": map[string]any{"type": "comments/comment", "entity": ce}},
			map[string]any{"command": "fibery.entity/add-collection-items", "args": map[string]any{
				"type": p.Dst, "entity": ref2(nid), "field": "comments/comments", "items": []any{ref2(cid)},
			}},
		}}}); err != nil {
			return nid, fmt.Errorf("comment: %w", err)
		}
		cmap[asStr(cm["id"])] = cid
		if err := m.dst.CreateDocumentMarkdown(ctx, secret, ""); err != nil {
			return nid, fmt.Errorf("comment body: %w", err)
		}
		if s := asStr(cm["secret"]); s != "" {
			prefix := ""
			if n := m.lostUser(asStr(cm["author"])); n != "" {
				prefix = n + ": "
			}
			if err := m.copyDoc(s, secret, prefix); err != nil {
				return nid, fmt.Errorf("comment body: %w", err)
			}
		}
		m.nComments++
	}
	return nid, nil
}

// copyDoc copies a document as JSON. Images it points at are uploaded again and
// relinked, so the copy does not depend on files owned by the source. prefix,
// when set, opens the body (the name of an author the target does not have).
func (m *mover) copyDoc(srcSecret, dstSecret, prefix string) error {
	c, n, err := copyDocContent(m.ctx, m.src, m.dst, srcSecret, m.users, prefix)
	if err != nil {
		return err
	}
	m.nImages += n.images
	m.nViews += n.views
	if strings.Contains(string(c.Doc), m.p.SrcTypeID) {
		m.docs = append(m.docs, dstSecret)
	}
	return m.dst.SetDocumentJSON(m.ctx, dstSecret, *c)
}

type docCopyCounts struct{ images, views int }

// copyDocContent reads a document from src and returns its content ready for
// dst: images re-uploaded to dst and relinked; across workspaces (users != nil)
// inline-comment authors mapped and embedded views dropped, since both belong
// to the source workspace.
func copyDocContent(ctx context.Context, src, dst *client.Client, srcSecret string, users *userMap, prefix string) (*client.DocContent, docCopyCounts, error) {
	var n docCopyCounts
	d, err := src.GetDocumentJSON(ctx, srcSecret)
	if err != nil {
		return nil, n, err
	}
	b, err := json.Marshal(d.Content)
	if err != nil {
		return nil, n, err
	}
	for _, s := range docFileSecrets(b) {
		data, ct, err := src.DownloadFile(ctx, s)
		if err != nil {
			return nil, n, fmt.Errorf("image %s: %w", s, err)
		}
		fi, err := dst.UploadFile(ctx, "image", ct, data)
		if err != nil {
			return nil, n, err
		}
		b = []byte(strings.ReplaceAll(string(b), s, fi.Secret))
		n.images++
	}
	var c client.DocContent
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, n, err
	}
	if c.Comments == nil {
		c.Comments = []any{}
	}
	if users == nil && prefix == "" {
		return &c, n, nil
	}
	var doc any
	if err := json.Unmarshal(c.Doc, &doc); err != nil {
		return nil, n, err
	}
	if users != nil {
		n.views = dropNodes(doc, "fiberyView")
		for _, x := range c.Comments {
			mapCommentAuthors(x, users)
		}
	}
	if prefix != "" {
		prefixDoc(doc, prefix)
	}
	if c.Doc, err = json.Marshal(doc); err != nil {
		return nil, n, err
	}
	return &c, n, nil
}

// mapCommentAuthors rewrites inline-comment authors (and replies') to target
// users; a lost author's name opens the comment body.
func mapCommentAuthors(v any, users *userMap) {
	cm, ok := v.(map[string]any)
	if !ok {
		return
	}
	if a, ok := cm["author"].(map[string]any); ok {
		id := asStr(a["id"])
		if lost := users.lost(id); lost != "" {
			if body, ok := cm["body"].(map[string]any); ok {
				prefixDoc(body["doc"], lost+": ")
			}
		}
		a["id"] = users.to(id)
	}
	for _, r := range mustSlice(cm["replies"]) {
		mapCommentAuthors(r, users)
	}
}

// dropNodes removes every node of type typ from a ProseMirror tree and reports
// how many went.
func dropNodes(v any, typ string) int {
	n := 0
	switch x := v.(type) {
	case map[string]any:
		if kids, ok := x["content"].([]any); ok {
			kept := kids[:0]
			for _, k := range kids {
				if km, ok := k.(map[string]any); ok && km["type"] == typ {
					n++
					continue
				}
				n += dropNodes(k, typ)
				kept = append(kept, k)
			}
			x["content"] = kept
		}
	case []any:
		for _, k := range x {
			n += dropNodes(k, typ)
		}
	}
	return n
}

// prefixDoc puts text at the start of a ProseMirror doc's first paragraph
// (a new paragraph when the doc opens with something else).
func prefixDoc(doc any, text string) {
	d, ok := doc.(map[string]any)
	if !ok {
		return
	}
	node := map[string]any{"type": "text", "text": text}
	kids, _ := d["content"].([]any)
	if len(kids) > 0 {
		if p, ok := kids[0].(map[string]any); ok && p["type"] == "paragraph" {
			pk, _ := p["content"].([]any)
			p["content"] = append([]any{node}, pk...)
			return
		}
	}
	d["content"] = append([]any{map[string]any{"type": "paragraph", "content": []any{node}}}, kids...)
}

// fixMentions rewrites, in the copied documents, mentions of source entities
// that now have copies.
func (m *mover) fixMentions() (int, error) {
	n := 0
	for _, s := range m.docs {
		d, err := m.dst.GetDocumentJSON(m.ctx, s)
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
		if err := m.dst.SetDocumentJSON(m.ctx, s, d.Content); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func init() {
	f := moveCmd.Flags()
	f.StringVar(&moveTo, "to", "", "target database (required)")
	f.StringVar(&moveToConfig, "to-config", "", "copy into the workspace of this account config (default: this one)")
	f.StringVar(&moveLegacy, "legacy-id", "", "text field on the target for \"Source/DB #<id>\" (required; makes re-runs skip copied entities)")
	f.StringArrayVar(&moveSet, "set", nil, "Field=value for every copy (repeatable)")
	f.StringArrayVar(&moveStateMap, "state-map", nil, "Old=New workflow state (repeatable)")
	f.StringArrayVar(&moveText, "text", nil, "SrcRelation=DstTextField: relation titles into a text field (repeatable)")
	f.StringArrayVar(&moveFilter, "filter", nil, "only source entities matching field=value (repeatable, as in list)")
	f.StringVar(&moveWhere, "where", "", "only source entities matching this FQL where-clause (JSON)")
	f.StringVar(&moveParams, "params", "", "query params as JSON for --where")
	f.IntVar(&moveLimit, "limit", 0, "copy at most N entities")
	f.StringVar(&moveOut, "out", "", "write the old → new id map to this JSON file")
	f.BoolVar(&moveYes, "yes", false, "copy (required)")
	f.BoolVar(&moveDryRun, "dry-run", false, "print the plan and stop")
	rootCmd.AddCommand(moveCmd)
}
