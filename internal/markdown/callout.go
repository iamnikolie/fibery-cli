// Package markdown rewrites agent-friendly Markdown into the dialect Fibery's
// document import understands.
package markdown

import (
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Fibery's markdown import and export both carry a callout as a blockquote whose
// first line is this comment; the attributes map 1:1 onto the ProseMirror callout
// node (color, iconType, icon). Which icon names the UI renders is undocumented, so
// only icon/color pairs copied from callouts made in the Fibery UI are listed.
var calloutKinds = map[string]string{
	"NOTE":    "> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)",
	"CAUTION": "> [//]: # (callout;icon-type=icon;icon=x;color=#d40915)",
}

var reAlert = regexp.MustCompile(`^( {0,3})>[ \t]?\[!([A-Za-z]+)\][ \t]*(.*)$`)

// ExpandCallouts turns GitHub alert blockquotes (`> [!NOTE]`, `> [!CAUTION]`) into
// Fibery callouts. Only a marker on the first line of a blockquote counts; text
// following it on the same line becomes the callout's first line. Unknown kinds are
// left untouched, and a body with no alert comes back unchanged.
//
// Which lines open a blockquote is decided by a CommonMark parser, so a marker in
// fenced or indented code, in HTML, or in the middle of a quote stays literal
// whatever lists and quotes contain it.
func ExpandCallouts(md string) string {
	if !strings.Contains(md, "[!") {
		return md
	}
	starts := quoteStartLines(md)
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		head, ok := "", false
		m := reAlert.FindStringSubmatch(line)
		if m != nil && starts[i] {
			head, ok = calloutKinds[strings.ToUpper(m[2])]
		}
		if !ok {
			out = append(out, line)
			continue
		}
		out = append(out, m[1]+head)
		if rest := strings.TrimSpace(m[3]); rest != "" {
			out = append(out, m[1]+"> "+rest)
		}
	}
	return strings.Join(out, "\n")
}

// quoteStartLines returns the 0-based numbers of the lines on which a blockquote's
// first block begins.
func quoteStartLines(md string) map[int]bool {
	src := []byte(md)
	var lineStarts []int
	for i, b := range src {
		if b == '\n' {
			lineStarts = append(lineStarts, i+1)
		}
	}
	lineOf := func(offset int) int {
		return sort.SearchInts(lineStarts, offset+1)
	}
	starts := map[int]bool{}
	doc := goldmark.DefaultParser().Parse(text.NewReader(src))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != ast.KindBlockquote {
			return ast.WalkContinue, nil
		}
		// A code or HTML first block lists its content lines too: only text counts.
		first := n.FirstChild()
		if first == nil || first.Lines().Len() == 0 {
			return ast.WalkContinue, nil
		}
		switch first.Kind() {
		case ast.KindParagraph, ast.KindTextBlock, ast.KindHeading:
			starts[lineOf(first.Lines().At(0).Start)] = true
		}
		return ast.WalkContinue, nil
	})
	return starts
}
