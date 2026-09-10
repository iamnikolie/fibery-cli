package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/client"
)

// asStr safely converts any value to string.
func asStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// docAssignment is a resolved rich-text field assignment ready to write to a document.
type docAssignment struct {
	field   string
	content string
}

// collectDocFields parses --doc "Field=content" and --doc-file "Field=path" pairs
// into resolved doc-field assignments. Field names are canonicalized against the
// schema. Inline content has its \n/\t/\r escapes interpreted; file content is used
// verbatim (no escape processing — the file already holds real newlines).
func collectDocFields(schema map[string]any, db string, inline, files []string) ([]docAssignment, error) {
	var out []docAssignment
	for _, pair := range inline {
		field, content, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("--doc: invalid Field=content pair: %q", pair)
		}
		out = append(out, docAssignment{resolveFieldName(schema, db, field), unescapeDocContent(content)})
	}
	for _, pair := range files {
		field, path, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("--doc-file: invalid Field=path pair: %q", pair)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("--doc-file %q: %w", field, err)
		}
		out = append(out, docAssignment{resolveFieldName(schema, db, field), string(b)})
	}
	return out, nil
}

// docEscaper interprets common backslash-escape sequences in --doc content so
// users can pass "line1\nline2" through the shell and get an actual newline in
// the document. `\\` is processed first so it escapes itself.
var docEscaper = strings.NewReplacer(
	`\\`, `\`,
	`\n`, "\n",
	`\t`, "\t",
	`\r`, "\r",
)

// unescapeDocContent converts \n, \t, \r literal sequences to their control
// characters. \\ is the escape hatch — \\n stays as literal "\n".
func unescapeDocContent(s string) string {
	return docEscaper.Replace(s)
}

// parseFieldValue converts a string to a Fibery API value: JSON map, fibery/id ref, or plain string.
func parseFieldValue(value string) (any, error) {
	if strings.HasPrefix(value, "{") {
		var m map[string]any
		if err := json.Unmarshal([]byte(value), &m); err != nil {
			return nil, fmt.Errorf("invalid JSON value %q: %w", value, err)
		}
		return m, nil
	}
	if isUUID(value) {
		return map[string]any{"fibery/id": value}, nil
	}
	return value, nil
}

// resolveFieldName returns the canonical field name from schema using case-insensitive
// matching. Unknown fields pass through unchanged so the API surfaces its own error.
// This rescues users from typos like "Development/name" when the schema has
// "Development/Name" — the tool already knows the right answer, no reason to fail.
func resolveFieldName(schema map[string]any, db, fieldName string) string {
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if asStr(tm["fibery/name"]) != db {
			continue
		}
		for _, f := range mustSlice(tm["fibery/fields"]) {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			name := asStr(fm["fibery/name"])
			if strings.EqualFold(name, fieldName) {
				return name
			}
		}
	}
	return fieldName
}

// findFieldType returns the fibery/type of fieldName in db from schema, or "" if not found.
func findFieldType(schema map[string]any, db, fieldName string) string {
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if asStr(tm["fibery/name"]) != db {
			continue
		}
		fields, _ := tm["fibery/fields"].([]any)
		for _, f := range fields {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if asStr(fm["fibery/name"]) == fieldName {
				return asStr(fm["fibery/type"])
			}
		}
	}
	return ""
}

// resolveEnumByName returns {"fibery/id": uuid} for the named enum value (case-insensitive), or an error listing available values.
func resolveEnumByName(ctx context.Context, enumType, name string) (map[string]any, error) {
	raw, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   enumType,
				"q/select": map[string]any{"id": "fibery/id", "name": "enum/name"},
				"q/limit":  "q/no-limit",
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("querying enum %s: %w", enumType, err)
	}

	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parsing enum response: %w", err)
	}

	lower := strings.ToLower(name)
	var available []string
	for _, item := range items {
		n := asStr(item["name"])
		available = append(available, n)
		if strings.ToLower(n) == lower {
			return map[string]any{"fibery/id": asStr(item["id"])}, nil
		}
	}
	return nil, fmt.Errorf("enum value %q not found in %s; available: %s", name, enumType, strings.Join(available, ", "))
}

// resolveUserByEmail returns {"fibery/id": uuid} for the user with the given email.
func resolveUserByEmail(ctx context.Context, email string) (map[string]any, error) {
	raw, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   "fibery/user",
				"q/select": map[string]any{"id": "fibery/id", "email": "user/email"},
				"q/where":  []any{"=", []any{"user/email"}, "$e"},
				"q/limit":  1,
			},
			"params": map[string]any{"$e": email},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("querying user by email %s: %w", email, err)
	}

	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parsing user response: %w", err)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("user with email %q not found", email)
	}
	return map[string]any{"fibery/id": asStr(items[0]["id"])}, nil
}

// resolveOpts tunes field-value resolution for bulk/forgiving flows.
type resolveOpts struct {
	// createMissingEnum creates an absent enum value (by name) instead of erroring.
	createMissingEnum bool
}

// resolveFieldValueOpts resolves a field value: JSON passthrough → UUID wrap →
// enum/user lookup → plain string. resolveOpts tunes forgiving/bulk behavior.
func resolveFieldValueOpts(ctx context.Context, schema map[string]any, db, field, value string, opts resolveOpts) (any, error) {
	// Fast path: explicit JSON or UUID reference — no schema lookup needed.
	if strings.HasPrefix(value, "{") || isUUID(value) {
		return parseFieldValue(value)
	}

	fieldType := findFieldType(schema, db, field)
	if fieldType == "" {
		return value, nil
	}

	if fieldType == "fibery/user" {
		return resolveUserByEmail(ctx, value)
	}

	if isEnumLikeType(fieldType) {
		v, err := resolveEnumByName(ctx, fieldType, value)
		if err != nil && opts.createMissingEnum && enumValueNotFound(err) {
			return createEnumValue(ctx, fieldType, value)
		}
		return v, err
	}

	return value, nil
}

// enumValueNotFound reports whether err is the "enum value not found" error from
// resolveEnumByName (as opposed to a network/parse error).
func enumValueNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "not found in")
}

// createEnumValue creates a new value (by name) in an enum type and returns a
// {"fibery/id": uuid} reference to it.
func createEnumValue(ctx context.Context, enumType, name string) (map[string]any, error) {
	result, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/create",
		Args:    map[string]any{"type": enumType, "entity": map[string]any{"enum/name": name}},
	})
	if err != nil {
		return nil, fmt.Errorf("create enum value %q in %s: %w", name, enumType, err)
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(result, &m) != nil {
		return nil, fmt.Errorf("create enum value %q: unexpected response", name)
	}
	id := strings.Trim(string(m["fibery/id"]), `"`)
	if id == "" {
		return nil, fmt.Errorf("create enum value %q: no fibery/id in response", name)
	}
	return map[string]any{"fibery/id": id}, nil
}

// resolveDocSecretByID returns the Collaboration document secret for the given entity field.
func resolveDocSecretByID(ctx context.Context, db, entityID, field string) (string, error) {
	raw, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   db,
				"q/select": map[string]any{"secret": []any{field, "Collaboration~Documents/secret"}},
				"q/where":  []any{"=", []any{"fibery/id"}, "$id"},
				"q/limit":  1,
			},
			"params": map[string]any{"$id": entityID},
		},
	})
	if err != nil {
		return "", fmt.Errorf("querying doc secret for %s/%s: %w", db, entityID, err)
	}

	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", fmt.Errorf("parsing doc secret response: %w", err)
	}

	if len(items) == 0 {
		return "", fmt.Errorf("entity %s not found in %s", entityID, db)
	}
	secret := asStr(items[0]["secret"])
	if secret == "" {
		return "", fmt.Errorf("no document secret for field %q on entity %s", field, entityID)
	}
	return secret, nil
}

// currentUserID returns the fibery/id of the token's user (via the $my-id query param).
func currentUserID(ctx context.Context) (string, error) {
	raw, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   "fibery/user",
				"q/select": map[string]any{"id": "fibery/id"},
				"q/where":  []any{"=", []any{"fibery/id"}, "$my-id"},
				"q/limit":  1,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("currentUserID: %w", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil || len(items) == 0 {
		return "", fmt.Errorf("currentUserID: could not resolve current user")
	}
	id := asStr(items[0]["id"])
	if id == "" {
		return "", fmt.Errorf("currentUserID: empty id")
	}
	return id, nil
}

// resolveUserEmails maps fibery user ids to emails (best-effort; missing ids are skipped).
func resolveUserEmails(ctx context.Context, ids []string) map[string]string {
	out := map[string]string{}
	if len(ids) == 0 {
		return out
	}
	anyIDs := make([]any, len(ids))
	for i, s := range ids {
		anyIDs[i] = s
	}
	raw, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   "fibery/user",
				"q/select": map[string]any{"id": "fibery/id", "email": "user/email"},
				"q/where":  []any{"q/in", []any{"fibery/id"}, "$ids"},
				"q/limit":  "q/no-limit",
			},
			"params": map[string]any{"$ids": anyIDs},
		},
	})
	if err != nil {
		return out
	}
	var items []map[string]any
	if json.Unmarshal(raw, &items) != nil {
		return out
	}
	for _, it := range items {
		out[asStr(it["id"])] = asStr(it["email"])
	}
	return out
}
