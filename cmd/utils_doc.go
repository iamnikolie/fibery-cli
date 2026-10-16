package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/iamnikolie/fibery-cli/internal/client"
)

// appendDocNodes appends the top-level nodes of addition (a ProseMirror doc) to
// the end of content.Doc. Existing nodes stay byte-identical and inline-comment
// anchors need no shift, since nothing moves. A document holding only the empty
// paragraph Fibery creates for a blank field is replaced rather than extended.
func appendDocNodes(content *client.DocContent, addition json.RawMessage) error {
	top, raws, nodes, err := parseDocNodes(content.Doc)
	if err != nil {
		return err
	}
	_, add, _, err := parseDocNodes(addition)
	if err != nil {
		return err
	}
	if len(add) == 0 {
		return fmt.Errorf("nothing to append: the Markdown converted to an empty document")
	}
	if len(nodes) == 1 && isEmptyParagraph(nodes[0]) {
		raws = nil
	}
	rawContent, err := json.Marshal(append(raws, add...))
	if err != nil {
		return err
	}
	top["content"] = rawContent
	doc, err := json.Marshal(top)
	if err != nil {
		return err
	}
	content.Doc = doc
	return nil
}

func isEmptyParagraph(node map[string]any) bool {
	t, _ := node["type"].(string)
	return t == "paragraph" && len(mustSlice(node["content"])) == 0
}
