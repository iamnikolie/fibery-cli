package cmd

import "fmt"

// pmParagraphDoc builds a minimal single-paragraph ProseMirror doc for a comment body.
func pmParagraphDoc(text, guid string) map[string]any {
	return map[string]any{
		"type": "doc",
		"content": []any{
			map[string]any{
				"type":  "paragraph",
				"attrs": map[string]any{"guid": guid},
				"content": []any{
					map[string]any{"type": "text", "text": text},
				},
			},
		},
	}
}

// newInlineThread builds a top-level inline comment thread anchored to [from,to).
func newInlineThread(from, to int, bodyDoc map[string]any, authorID, id string, dateMs int64) map[string]any {
	return map[string]any{
		"from":     from,
		"to":       to,
		"id":       id,
		"body":     map[string]any{"doc": bodyDoc, "comments": []any{}},
		"date":     dateMs,
		"author":   map[string]any{"id": authorID},
		"thread":   nil,
		"state":    "open",
		"detached": false,
	}
}

// newReply builds a reply object nested under a parent thread's body.comments.
// Shape per Task 0 spike: a reply is not anchored (no from/to).
func newReply(bodyDoc map[string]any, authorID, id string, dateMs int64) map[string]any {
	return map[string]any{
		"id":     id,
		"body":   map[string]any{"doc": bodyDoc, "comments": []any{}},
		"date":   dateMs,
		"author": map[string]any{"id": authorID},
		"state":  "open",
	}
}

// inlineFindIdx returns the index of the thread with the given id, or -1.
func inlineFindIdx(threads []any, id string) int {
	for i, t := range threads {
		if m, ok := t.(map[string]any); ok {
			if asStr(m["id"]) == id {
				return i
			}
		}
	}
	return -1
}

// inlineSetState sets the state of a thread by id (open|resolved).
func inlineSetState(threads []any, id, state string) error {
	i := inlineFindIdx(threads, id)
	if i < 0 {
		return fmt.Errorf("inline comment %q not found", id)
	}
	threads[i].(map[string]any)["state"] = state
	return nil
}

// inlineDelete returns threads with the thread of the given id removed.
func inlineDelete(threads []any, id string) ([]any, error) {
	i := inlineFindIdx(threads, id)
	if i < 0 {
		return nil, fmt.Errorf("inline comment %q not found", id)
	}
	return append(threads[:i:i], threads[i+1:]...), nil
}

// inlineReply appends a reply to the parent thread's body.comments array.
func inlineReply(threads []any, parentID string, reply map[string]any) error {
	i := inlineFindIdx(threads, parentID)
	if i < 0 {
		return fmt.Errorf("inline comment %q not found", parentID)
	}
	parent := threads[i].(map[string]any)
	body, ok := parent["body"].(map[string]any)
	if !ok {
		body = map[string]any{}
		parent["body"] = body
	}
	existing, _ := body["comments"].([]any)
	body["comments"] = append(existing, reply)
	return nil
}
