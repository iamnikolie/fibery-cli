package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/langgerone/fibery-cli/internal/client"
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

// resolveFieldValue resolves a field value: JSON passthrough → UUID wrap → enum/user lookup → plain string.
func resolveFieldValue(ctx context.Context, schema map[string]any, db, field, value string) (any, error) {
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
		return resolveEnumByName(ctx, fieldType, value)
	}

	return value, nil
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
