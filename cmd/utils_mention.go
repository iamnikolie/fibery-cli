package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// buildMentionToken returns the Fibery rich-text mention shorthand for a
// typeId/entityId pair. Fibery renders [[#@<typeId>/<entityId>]] in document
// markdown as a live mention node — a user ping (typeId = fibery/user) or a
// reference to any other entity (typeId = that entity's database type id).
func buildMentionToken(typeID, entityID string) string {
	return fmt.Sprintf("[[#@%s/%s]]", typeID, entityID)
}

// prependTokens places mention/reference tokens at the front of a comment body.
// Tokens are space-separated and followed by the body when present. An empty
// token slice returns the body unchanged; an empty body returns just the tokens.
func prependTokens(tokens []string, body string) string {
	if len(tokens) == 0 {
		return body
	}
	prefix := strings.Join(tokens, " ")
	if strings.TrimSpace(body) == "" {
		return prefix
	}
	return prefix + " " + body
}

// findTypeID returns the fibery/id UUID of the named type (database) from the
// cached schema, or "" if the type is absent. Used to turn a database name into
// the typeId half of a mention token.
func findTypeID(schema map[string]any, typeName string) string {
	types, _ := schema["fibery/types"].([]any)
	for _, t := range types {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if asStr(tm["fibery/name"]) == typeName {
			return asStr(tm["fibery/id"])
		}
	}
	return ""
}

// resolveRefTarget turns a --ref value into a (database, entityID) pair.
// A full Fibery URL is self-contained (database parsed from the URL); any other
// form (UUID, "DT-42", "42") is resolved against hostDB — the database of the
// entity being commented on.
func resolveRefTarget(cmd *cobra.Command, ref, hostDB string) (db, entityID string, err error) {
	if strings.HasPrefix(ref, "http") {
		return resolveURLToEntityID(cmd, ref)
	}
	if hostDB == "" {
		return "", "", fmt.Errorf("--ref %q needs a database: pass a full Fibery URL for it", ref)
	}
	id, err := resolveEntityRef(cmd.Context(), hostDB, ref)
	if err != nil {
		return "", "", err
	}
	return hostDB, id, nil
}
