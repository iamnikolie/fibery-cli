package cmd

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/client"
)

// viewTypes are the view kinds `view create` can build. Both share the
// items[0].{query,params,filter,units} meta shape; board/timeline/calendar need
// axis config this command does not author.
var viewTypes = map[string]bool{"list": true, "table": true}

// schemaField is one field of a database in the cached schema.
type schemaField struct {
	ID         string
	Name       string
	Type       string
	Collection bool
}

// lookupSchemaField finds a field of db by name, case-insensitively.
func lookupSchemaField(schema map[string]any, db, name string) (schemaField, bool) {
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok || !strings.EqualFold(asStr(fm["fibery/name"]), name) {
				continue
			}
			meta, _ := fm["fibery/meta"].(map[string]any)
			coll, _ := meta["fibery/collection?"].(bool)
			return schemaField{
				ID:         asStr(fm["fibery/id"]),
				Name:       asStr(fm["fibery/name"]),
				Type:       asStr(fm["fibery/type"]),
				Collection: coll,
			}, true
		}
	}
	return schemaField{}, false
}

// fieldPathIDs turns a field-name path ("workflow/state", "enum/name") into the
// field-id path views store, following each field's type to resolve the next.
func fieldPathIDs(schema map[string]any, db string, path []string) ([]any, error) {
	out := make([]any, 0, len(path))
	cur := db
	for _, name := range path {
		f, ok := lookupSchemaField(schema, cur, name)
		if !ok {
			return nil, fmt.Errorf("field %q not found on %s (run 'fibery schema sync' if it's new)", name, cur)
		}
		out = append(out, f.ID)
		cur = f.Type
	}
	return out, nil
}

// translateWhereNames rewrites every field path in an FQL where-expression from
// field names to field ids. A path is an all-string array whose first element is
// a field of db; operators, $params and q/ keywords are left alone. Sub-query
// maps pass through untouched — their fields belong to another database, so they
// must already be written with ids.
func translateWhereNames(schema map[string]any, db string, expr any) (any, error) {
	arr, ok := expr.([]any)
	if !ok {
		return expr, nil
	}
	if path, ok := asFieldPath(schema, db, arr); ok {
		return fieldPathIDs(schema, db, path)
	}
	out := make([]any, len(arr))
	for i, e := range arr {
		t, err := translateWhereNames(schema, db, e)
		if err != nil {
			return nil, err
		}
		out[i] = t
	}
	return out, nil
}

func asFieldPath(schema map[string]any, db string, arr []any) ([]string, bool) {
	if len(arr) == 0 {
		return nil, false
	}
	path := make([]string, len(arr))
	for i, e := range arr {
		s, ok := e.(string)
		if !ok || strings.HasPrefix(s, "$") || strings.HasPrefix(s, "q/") {
			return nil, false
		}
		path[i] = s
	}
	if _, ok := lookupSchemaField(schema, db, path[0]); !ok {
		return nil, false
	}
	return path, true
}

// viewFilter is one leaf of a view's filter: an expression over field ids and the
// $whereN params it references.
type viewFilter struct {
	Expression any
	Params     map[string]any
}

// refResolver turns a user-typed value for a reference field (enum name, user
// email, related entity public id) into the referenced entity's fibery/id.
type refResolver func(f schemaField, value string) (string, error)

// buildViewFilters turns "field<op>value" strings into view filter leaves, the
// way the Fibery filter editor writes them: references compare by fibery/id via
// q/in, text uses q/contains for "~". Params are named $where1.. in order.
func buildViewFilters(schema map[string]any, db string, filters []string, resolve refResolver) ([]viewFilter, error) {
	out := make([]viewFilter, 0, len(filters))
	for i, raw := range filters {
		name, op, value, ok := splitFilter(raw)
		if !ok {
			return nil, fmt.Errorf("invalid --filter %q: expected field=value (operators: =, !=, ~)", raw)
		}
		f, ok := lookupSchemaField(schema, db, strings.TrimSpace(name))
		if !ok {
			return nil, fmt.Errorf("field %q not found on %s (run 'fibery schema sync' if it's new)", strings.TrimSpace(name), db)
		}
		if f.Collection {
			return nil, fmt.Errorf("--filter on collection field %q is not supported; use --where", f.Name)
		}
		p := fmt.Sprintf("$where%d", i+1)
		leaf, err := buildViewFilter(schema, f, op, value, p, resolve)
		if err != nil {
			return nil, err
		}
		out = append(out, leaf)
	}
	return out, nil
}

func buildViewFilter(schema map[string]any, f schemaField, op, value, param string, resolve refResolver) (viewFilter, error) {
	if !primitiveFieldTypes[f.Type] && f.Type != "fibery/url" {
		if op == "~" {
			return viewFilter{}, fmt.Errorf("operator ~ is not supported on reference field %q; use = or !=", f.Name)
		}
		idField, ok := lookupSchemaField(schema, f.Type, "fibery/id")
		if !ok {
			return viewFilter{}, fmt.Errorf("type %s of field %q not in schema cache", f.Type, f.Name)
		}
		id, err := resolve(f, value)
		if err != nil {
			return viewFilter{}, err
		}
		fn := "q/in"
		if op == "!=" {
			fn = "q/not-in"
		}
		return viewFilter{
			Expression: []any{fn, []any{f.ID, idField.ID}, param},
			Params:     map[string]any{param: []any{id}},
		}, nil
	}
	v, err := typedFilterValue(f, value)
	if err != nil {
		return viewFilter{}, err
	}
	fn := map[string]string{"=": "=", "!=": "!=", "~": "q/contains"}[op]
	return viewFilter{
		Expression: []any{fn, []any{f.ID}, param},
		Params:     map[string]any{param: v},
	}, nil
}

func typedFilterValue(f schemaField, value string) (any, error) {
	switch f.Type {
	case "fibery/bool":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("field %q is boolean, got %q", f.Name, value)
		}
		return b, nil
	case "fibery/int", "fibery/decimal":
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("field %q is numeric, got %q", f.Name, value)
		}
		return n, nil
	}
	return value, nil
}

// viewSpec is everything needed to build a view record.
type viewSpec struct {
	ID      string
	Name    string
	Type    string // list | table
	DBType  string // fibery/id of the source database type
	AppID   string // container app (space)
	Filters []viewFilter
	AnyOf   bool // OR the filters instead of AND
	Units   []any
	OrderBy []any
	// Owner, when set, makes the view entity-owned (container-type "object") —
	// what Fibery creates for a view embedded in that entity's rich text.
	Owner *viewOwner
}

type viewOwner struct {
	TypeID   string
	UUID     string
	PublicID string
}

// buildViewRecord assembles the create-views record in the shape the Fibery UI
// stores: query + flat params + the editor's filter tree, all keyed by field id.
func buildViewRecord(s viewSpec) map[string]any {
	query := map[string]any{"q/from": s.DBType}
	params := map[string]any{}
	exprs := make([]any, 0, len(s.Filters))
	leaves := make([]any, 0, len(s.Filters))
	for _, f := range s.Filters {
		exprs = append(exprs, f.Expression)
		maps.Copy(params, f.Params)
		leaves = append(leaves, map[string]any{
			"id": client.NewUUID(), "icon": "", "kind": "filter", "name": "",
			"pinned": false, "disabled": false, "description": "",
			"value": map[string]any{"params": f.Params, "expression": f.Expression},
		})
	}
	op := "and"
	if s.AnyOf {
		op = "or"
	}
	switch len(exprs) {
	case 0:
	case 1:
		query["q/where"] = exprs[0]
	default:
		query["q/where"] = append([]any{op}, exprs...)
	}
	if len(s.OrderBy) > 0 {
		query["q/order-by"] = s.OrderBy
	}
	item := map[string]any{
		"query":              query,
		"params":             params,
		"units":              s.Units,
		"groupBy":            nil,
		"groupingExpression": nil,
	}
	if len(leaves) > 0 {
		item["filter"] = map[string]any{
			"id": client.NewUUID(), "icon": "", "kind": "group", "name": "",
			"pinned": false, "disabled": false, "description": "",
			"operator": op, "value": leaves,
		}
	}
	rec := map[string]any{
		"fibery/id":             s.ID,
		"fibery/name":           s.Name,
		"fibery/type":           s.Type,
		"fibery/meta":           map[string]any{"items": []any{item}},
		"fibery/container-app":  map[string]any{"fibery/id": s.AppID},
		"fibery/container-type": "app",
	}
	if s.Owner != nil {
		rec["fibery/container-type"] = "object"
		rec["fibery/container-entity-type"] = map[string]any{"fibery/id": s.Owner.TypeID}
		rec["fibery/container-entity-id"] = s.Owner.PublicID
		rec["fibery/container-entity-uid"] = s.Owner.UUID
	}
	return rec
}

// defaultViewUnits shows the title, the workflow state when the database has
// one, and the public id.
func defaultViewUnits(schema map[string]any, db string) []any {
	units := []any{map[string]any{"kind": "field", "type": "title", "checked": true}}
	if f, ok := lookupSchemaField(schema, db, "workflow/state"); ok {
		units = append(units, map[string]any{"kind": "field", "type": "reference", "checked": true, "expression": []any{f.ID}})
	}
	return append(units, map[string]any{"kind": "field", "type": "id", "checked": true})
}

// defaultViewOrder sorts newest first when the database has a creation date.
func defaultViewOrder(schema map[string]any, db string) []any {
	f, ok := lookupSchemaField(schema, db, "fibery/creation-date")
	if !ok {
		return nil
	}
	return []any{[]any{[]any{f.ID}, "q/desc"}}
}

// viewEmbedNode is the block node Fibery's editor writes for an inline view
// ("/view"). The sizes are the editor's defaults for a page-width embed.
func viewEmbedNode(viewID string) map[string]any {
	return map[string]any{
		"type": "fiberyView",
		"attrs": map[string]any{
			"viewId":        viewID,
			"id":            viewID,
			"nodeHeight":    400,
			"nodeWidth":     900,
			"nodePageWidth": true,
			"nodeFullWidth": false,
		},
	}
}

// pmLeafTypes are node types that occupy one ProseMirror position.
var pmLeafTypes = map[string]bool{
	"hard_break": true, "horizontal_rule": true, "image": true, "imageBlock": true,
	"fiberyView": true, "entity": true, "mention": true, "emoji": true, "file": true,
}

// pmNodeSize is a node's ProseMirror nodeSize. Unlike pmCollectRunes it counts an
// empty container (a paragraph with no content) as 2, as ProseMirror does.
func pmNodeSize(node map[string]any) int {
	t, _ := node["type"].(string)
	if t == "text" {
		n := 0
		txt, _ := node["text"].(string)
		for _, r := range txt {
			n += pmUnitLen(r)
		}
		return n
	}
	content, ok := node["content"].([]any)
	if !ok {
		if pmLeafTypes[t] {
			return 1
		}
		return 2
	}
	size := 2
	for _, c := range content {
		if cm, ok := c.(map[string]any); ok {
			size += pmNodeSize(cm)
		}
	}
	return size
}

// pmNodeText concatenates the text inside a node.
func pmNodeText(node map[string]any) string {
	if t, _ := node["type"].(string); t == "text" {
		s, _ := node["text"].(string)
		return s
	}
	var b strings.Builder
	for _, c := range mustSlice(node["content"]) {
		if cm, ok := c.(map[string]any); ok {
			b.WriteString(pmNodeText(cm))
		}
	}
	return b.String()
}

func headingLevel(node map[string]any) (int, bool) {
	if t, _ := node["type"].(string); t != "heading" {
		return 0, false
	}
	attrs, _ := node["attrs"].(map[string]any)
	lvl, _ := attrs["level"].(float64)
	return int(lvl), true
}

// embedsView reports whether the document already holds an inline view node for viewID.
func embedsView(nodes []map[string]any, viewID string) bool {
	for _, n := range nodes {
		if t, _ := n["type"].(string); t == "fiberyView" {
			attrs, _ := n["attrs"].(map[string]any)
			if asStr(attrs["viewId"]) == viewID {
				return true
			}
		}
	}
	return false
}

// blockInsertIndex returns the top-level index to insert a block at: the end of
// the section under the first heading whose text contains afterHeading
// (case-insensitive) — just before the next heading of the same or a higher
// level — or the end of the document when afterHeading is empty.
func blockInsertIndex(nodes []map[string]any, afterHeading string) (int, error) {
	if afterHeading == "" {
		return len(nodes), nil
	}
	want := strings.ToLower(strings.TrimSpace(afterHeading))
	for i, n := range nodes {
		lvl, ok := headingLevel(n)
		if !ok || !strings.Contains(strings.ToLower(pmNodeText(n)), want) {
			continue
		}
		for j := i + 1; j < len(nodes); j++ {
			if l, ok := headingLevel(nodes[j]); ok && l <= lvl {
				return j, nil
			}
		}
		return len(nodes), nil
	}
	return 0, fmt.Errorf("no heading containing %q in the document", afterHeading)
}

// parseDocNodes splits a ProseMirror doc into its top-level wrapper, the raw
// top-level nodes (kept byte-identical) and their decoded form.
func parseDocNodes(doc json.RawMessage) (map[string]json.RawMessage, []json.RawMessage, []map[string]any, error) {
	var top map[string]json.RawMessage
	if len(doc) == 0 || string(doc) == "null" {
		top = map[string]json.RawMessage{"type": json.RawMessage(`"doc"`)}
	} else if err := json.Unmarshal(doc, &top); err != nil {
		return nil, nil, nil, fmt.Errorf("parse document: %w", err)
	}
	var raws []json.RawMessage
	if raw, ok := top["content"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &raws); err != nil {
			return nil, nil, nil, fmt.Errorf("parse document content: %w", err)
		}
	}
	nodes := make([]map[string]any, len(raws))
	for i, r := range raws {
		if err := json.Unmarshal(r, &nodes[i]); err != nil {
			return nil, nil, nil, fmt.Errorf("parse document node %d: %w", i, err)
		}
	}
	return top, raws, nodes, nil
}

// checkInsertPoint verifies that afterHeading resolves in the document, so a
// view is not created only to fail at the embed step.
func checkInsertPoint(doc json.RawMessage, afterHeading string) error {
	_, _, nodes, err := parseDocNodes(doc)
	if err != nil {
		return err
	}
	_, err = blockInsertIndex(nodes, afterHeading)
	return err
}

// insertDocBlock inserts block into the document at the end of the section named
// by afterHeading (or at the end), and shifts inline-comment anchors that sit
// after the insertion point. Untouched nodes are kept as raw bytes.
func insertDocBlock(content *client.DocContent, block map[string]any, afterHeading string) error {
	top, raws, nodes, err := parseDocNodes(content.Doc)
	if err != nil {
		return err
	}
	if attrs, _ := block["attrs"].(map[string]any); attrs != nil && block["type"] == "fiberyView" {
		if id := asStr(attrs["viewId"]); embedsView(nodes, id) {
			return fmt.Errorf("view %s is already embedded in this document", id)
		}
	}
	idx, err := blockInsertIndex(nodes, afterHeading)
	if err != nil {
		return err
	}
	pos := 0
	for _, n := range nodes[:idx] {
		pos += pmNodeSize(n)
	}
	b, err := json.Marshal(block)
	if err != nil {
		return err
	}
	raws = append(raws[:idx], append([]json.RawMessage{b}, raws[idx:]...)...)
	rawContent, err := json.Marshal(raws)
	if err != nil {
		return err
	}
	top["content"] = rawContent
	doc, err := json.Marshal(top)
	if err != nil {
		return err
	}
	content.Doc = doc
	shiftCommentAnchors(content.Comments, pos, pmNodeSize(block))
	return nil
}

// shiftCommentAnchors moves inline-comment from/to anchors at or after pos by delta.
func shiftCommentAnchors(comments []any, pos, delta int) {
	for _, c := range comments {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		for _, k := range []string{"from", "to"} {
			if v, ok := cm[k].(float64); ok && int(v) >= pos {
				cm[k] = v + float64(delta)
			}
		}
	}
}

// removeViewEmbeds drops the top-level inline-view nodes of viewID and shifts
// inline-comment anchors after each removed node back. Returns how many it removed.
func removeViewEmbeds(content *client.DocContent, viewID string) (int, error) {
	top, raws, nodes, err := parseDocNodes(content.Doc)
	if err != nil {
		return 0, err
	}
	keep := raws[:0:0]
	removed, pos := 0, 0
	for i, n := range nodes {
		size := pmNodeSize(n)
		if embedsView([]map[string]any{n}, viewID) {
			shiftCommentAnchors(content.Comments, pos+size, -size)
			removed++
			continue
		}
		keep = append(keep, raws[i])
		pos += size
	}
	if removed == 0 {
		return 0, nil
	}
	rawContent, err := json.Marshal(keep)
	if err != nil {
		return 0, err
	}
	top["content"] = rawContent
	doc, err := json.Marshal(top)
	if err != nil {
		return 0, err
	}
	content.Doc = doc
	return removed, nil
}

// typeNameByID returns the name of the schema type with the given fibery/id.
func typeNameByID(schema map[string]any, id string) string {
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		if tm, ok := t.(map[string]any); ok && asStr(tm["fibery/id"]) == id {
			return asStr(tm["fibery/name"])
		}
	}
	return ""
}

// richTextFields lists the rich-text (document) fields of db.
func richTextFields(schema map[string]any, db string) []string {
	var out []string
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok || asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			if fm, ok := f.(map[string]any); ok && asStr(fm["fibery/type"]) == "Collaboration~Documents/Document" {
				out = append(out, asStr(fm["fibery/name"]))
			}
		}
	}
	return out
}
