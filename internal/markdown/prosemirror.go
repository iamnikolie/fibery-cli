package markdown

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// ToDoc converts Markdown to a ProseMirror document in Fibery's rich-text schema,
// locally, without a round trip through a Fibery document. The node shapes, attrs
// and mark order copy what Fibery's own Markdown import produces for the same
// input; newID supplies the fresh attrs.id/guid every block node carries, so the
// nodes can be spliced into an existing document. Callouts are expanded first, as
// in every other Markdown write.
//
// Covered: headings, paragraphs (a line break is a hard break), em/strong/strike
// (two tildes only)/code/link marks, autolinks, bullet, ordered and task lists,
// blockquotes, callouts, fenced and indented code, rules, GFM tables, images
// (always block images, splitting their paragraph), [[#@type/id]] mentions,
// [[#^type/id]] entity references and the emoji shortcodes in knownEmoji.
// Raw HTML stays literal text, except <br> (a hard break) and <span> (dropped).
func ToDoc(md string, newID func() string) (json.RawMessage, error) {
	c := &converter{newID: newID, sentinel: "fiberycallout" + randomHex() + "n"}
	src := c.markCallouts([]byte(ExpandCallouts(md)))
	c.src = src
	root := newParser().Parse(text.NewReader(src))
	return json.Marshal(&pmNode{Type: "doc", Content: c.blocks(root)})
}

type pmNode struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []*pmNode      `json:"content,omitempty"`
	Marks   []pmMark       `json:"marks,omitempty"`
	Text    string         `json:"text,omitempty"`
}

type pmMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// markRank is the order Fibery lists marks on a text node.
var markRank = map[string]int{"link": 0, "em": 1, "strong": 2, "code": 3, "strike": 4}

type converter struct {
	src      []byte
	newID    func() string
	sentinel string
	callouts []map[string]any
}

func newParser() parser.Parser {
	p := goldmark.New(goldmark.WithExtensions(east.Table, east.TaskList)).Parser()
	p.AddOptions(parser.WithInlineParsers(util.Prioritized(&strikeParser{}, 500)))
	return p
}

func randomHex() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// reCalloutHead is the native callout line: a quote whose line is the callout
// comment. As a link reference definition it would vanish in parsing.
var reCalloutHead = regexp.MustCompile(`^([ \t]*(?:>[ \t]?)+)\[//\]: # \(callout;([^)]*)\)[ \t]*$`)

// markCallouts replaces each native callout line outside code with a sentinel
// paragraph (followed by an empty quote line, so the quote's text does not join
// it) that blockquote conversion recognises.
func (c *converter) markCallouts(src []byte) []byte {
	if !bytes.Contains(src, []byte("(callout;")) {
		return src
	}
	code := codeLines(src)
	lines := strings.Split(string(src), "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		m := reCalloutHead.FindStringSubmatch(line)
		if m == nil || code[i] {
			out = append(out, line)
			continue
		}
		c.callouts = append(c.callouts, calloutAttrs(m[2]))
		out = append(out, m[1]+c.sentinel+strconv.Itoa(len(c.callouts)-1), strings.TrimRight(m[1], " \t"))
	}
	return []byte(strings.Join(out, "\n"))
}

// codeLines returns the 0-based numbers of the lines that are content of a code
// or HTML block.
func codeLines(src []byte) map[int]bool {
	var starts []int
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	lines := map[int]bool{}
	doc := newParser().Parse(text.NewReader(src))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindCodeBlock, ast.KindFencedCodeBlock, ast.KindHTMLBlock:
			for i := 0; i < n.Lines().Len(); i++ {
				lines[sort.SearchInts(starts, n.Lines().At(i).Start+1)] = true
			}
		}
		return ast.WalkContinue, nil
	})
	return lines
}

// calloutAttrs maps "icon-type=icon;icon=x;color=#d40915" onto the callout node's attrs.
func calloutAttrs(spec string) map[string]any {
	attrs := map[string]any{}
	for _, kv := range strings.Split(spec, ";") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "icon-type":
			attrs["iconType"] = strings.TrimSpace(v)
		case "icon":
			attrs["icon"] = strings.TrimSpace(v)
		case "color":
			attrs["color"] = strings.TrimSpace(v)
		}
	}
	return attrs
}

// calloutIndex returns the callout a sentinel paragraph stands for, or -1.
func (c *converter) calloutIndex(n ast.Node) int {
	if len(c.callouts) == 0 {
		return -1
	}
	if _, ok := n.(*ast.Paragraph); !ok {
		return -1
	}
	s := strings.TrimSpace(string(n.Lines().Value(c.src)))
	if !strings.HasPrefix(s, c.sentinel) {
		return -1
	}
	i, err := strconv.Atoi(strings.TrimPrefix(s, c.sentinel))
	if err != nil || i < 0 || i >= len(c.callouts) {
		return -1
	}
	return i
}

func (c *converter) blocks(parent ast.Node) []*pmNode {
	var out []*pmNode
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		if c.calloutIndex(n) >= 0 {
			continue // a callout line not at the start of a quote: nothing to show
		}
		out = append(out, c.block(n)...)
	}
	return out
}

func (c *converter) block(n ast.Node) []*pmNode {
	switch n := n.(type) {
	case *ast.Heading:
		return []*pmNode{{Type: "heading", Attrs: c.textAttrs(map[string]any{"level": n.Level}), Content: imagesAsText(c.inlineContent(n))}}
	case *ast.Paragraph, *ast.TextBlock:
		return c.paragraphs(c.inlineContent(n))
	case *ast.ThematicBreak:
		return []*pmNode{{Type: "horizontal_rule", Attrs: map[string]any{"id": c.newID()}}}
	case *ast.FencedCodeBlock:
		lang := ""
		if n.Info != nil {
			lang = decodeText(n.Language(c.src))
		}
		return []*pmNode{c.codeBlock(n, lang)}
	case *ast.CodeBlock:
		return []*pmNode{c.codeBlock(n, "")}
	case *ast.Blockquote:
		return []*pmNode{c.quote(n)}
	case *ast.List:
		return []*pmNode{c.list(n)}
	case *ast.HTMLBlock:
		return c.htmlBlock(n)
	case *extast.Table:
		return []*pmNode{c.table(n)}
	}
	if n.HasChildren() {
		return c.blocks(n)
	}
	return nil
}

// textAttrs adds the id and guid that paragraph-like nodes carry.
func (c *converter) textAttrs(attrs map[string]any) map[string]any {
	if attrs == nil {
		attrs = map[string]any{}
	}
	id := c.newID()
	attrs["id"], attrs["guid"] = id, id
	return attrs
}

func (c *converter) emptyParagraph() *pmNode {
	return &pmNode{Type: "paragraph", Attrs: c.textAttrs(nil)}
}

// paragraphs wraps inline nodes in a paragraph, splitting it around block images
// as Fibery does. Pieces holding only whitespace are dropped.
func (c *converter) paragraphs(inline []*pmNode) []*pmNode {
	var out, cur []*pmNode
	flush := func() {
		cur = trimBreaks(cur)
		if !blankInline(cur) {
			out = append(out, &pmNode{Type: "paragraph", Attrs: c.textAttrs(nil), Content: cur})
		}
		cur = nil
	}
	for _, n := range inline {
		if n.Type == "imageBlock" {
			flush()
			out = append(out, n)
			continue
		}
		cur = append(cur, n)
	}
	flush()
	return out
}

func trimBreaks(nodes []*pmNode) []*pmNode {
	for len(nodes) > 0 && nodes[len(nodes)-1].Type == "hard_break" {
		nodes = nodes[:len(nodes)-1]
	}
	return nodes
}

func blankInline(nodes []*pmNode) bool {
	for _, n := range nodes {
		if n.Type != "hard_break" && (n.Type != "text" || strings.TrimSpace(n.Text) != "") {
			return false
		}
	}
	return true
}

// imagesAsText keeps a heading inline: an image in it becomes its alt text.
func imagesAsText(nodes []*pmNode) []*pmNode {
	var out []*pmNode
	for _, n := range nodes {
		if n.Type == "imageBlock" {
			if alt, _ := n.Attrs["alt"].(string); alt != "" {
				out = append(out, &pmNode{Type: "text", Text: alt})
			}
			continue
		}
		out = append(out, n)
	}
	return mergeTexts(trimBreaks(out))
}

func (c *converter) codeBlock(n ast.Node, lang string) *pmNode {
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		b.Write(seg.Value(c.src))
	}
	attrs := c.textAttrs(map[string]any{
		"language": lang, "wrapCode": false, "mode": "split",
		"nodeHeight": 400, "nodeWidth": 900, "nodePageWidth": true, "nodeFullWidth": false,
	})
	node := &pmNode{Type: "code_block", Attrs: attrs}
	if s := strings.TrimSuffix(b.String(), "\n"); s != "" {
		node.Content = []*pmNode{{Type: "text", Text: s}}
	}
	return node
}

// quote converts a blockquote, or a callout when its first block is a callout sentinel.
func (c *converter) quote(n *ast.Blockquote) *pmNode {
	node := &pmNode{Type: "blockquote", Attrs: map[string]any{"id": c.newID()}}
	if first := n.FirstChild(); first != nil {
		if i := c.calloutIndex(first); i >= 0 {
			node.Type = "callout"
			for k, v := range c.callouts[i] {
				node.Attrs[k] = v
			}
		}
	}
	node.Content = c.blocks(n)
	if len(node.Content) == 0 {
		node.Content = []*pmNode{c.emptyParagraph()}
	}
	return node
}

func (c *converter) list(n *ast.List) *pmNode {
	node := &pmNode{Type: "bullet_list", Attrs: map[string]any{"id": c.newID()}}
	if n.IsOrdered() {
		node.Type = "ordered_list"
		node.Attrs["order"] = n.Start
	}
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		var state any
		if first := item.FirstChild(); first != nil {
			if box, ok := first.FirstChild().(*extast.TaskCheckBox); ok {
				state = "TODO"
				if box.IsChecked {
					state = "DONE"
				}
			}
		}
		li := &pmNode{Type: "list_item", Attrs: map[string]any{"state": state, "id": c.newID()}, Content: c.blocks(item)}
		if len(li.Content) == 0 {
			li.Content = []*pmNode{c.emptyParagraph()}
		}
		node.Content = append(node.Content, li)
	}
	return node
}

// htmlBlock keeps raw HTML as literal text, one hard break per line.
func (c *converter) htmlBlock(n *ast.HTMLBlock) []*pmNode {
	lines := strings.Split(strings.TrimRight(string(n.Lines().Value(c.src)), "\r\n"), "\n")
	if n.HasClosure() {
		lines = append(lines, strings.TrimRight(string(n.ClosureLine.Value(c.src)), "\r\n"))
	}
	var inline []*pmNode
	for i, l := range lines {
		if i > 0 {
			inline = append(inline, &pmNode{Type: "hard_break"})
		}
		if l != "" {
			inline = append(inline, &pmNode{Type: "text", Text: l})
		}
	}
	return c.paragraphs(inline)
}

func (c *converter) table(n *extast.Table) *pmNode {
	table := &pmNode{Type: "table", Attrs: map[string]any{"id": c.newID()}}
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		cellType := "table_cell"
		if _, ok := row.(*extast.TableHeader); ok {
			cellType = "table_header"
		}
		tr := &pmNode{Type: "table_row", Attrs: map[string]any{"id": c.newID()}}
		for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
			content := c.paragraphs(c.inlineContent(cell))
			if len(content) == 0 {
				content = []*pmNode{c.emptyParagraph()}
			}
			tr.Content = append(tr.Content, &pmNode{Type: cellType, Attrs: map[string]any{
				"colspan": 1, "rowspan": 1, "colwidth": nil, "align": nil, "background": nil, "id": c.newID(),
			}, Content: content})
		}
		table.Content = append(table.Content, tr)
	}
	return table
}

// inlineContent converts a block's inline children: text nodes with sorted marks,
// adjacent equal runs merged, mentions and emoji split out of plain text.
func (c *converter) inlineContent(n ast.Node) []*pmNode {
	var out []*pmNode
	c.inlines(n, nil, &out)
	out = mergeTexts(out)
	var expanded []*pmNode
	for _, node := range out {
		expanded = append(expanded, expandTokens(node)...)
	}
	return expanded
}

func (c *converter) inlines(parent ast.Node, marks []pmMark, out *[]*pmNode) {
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		c.inline(n, marks, out)
	}
}

func (c *converter) inline(n ast.Node, marks []pmMark, out *[]*pmNode) {
	emit := func(s string, marks []pmMark) {
		if s != "" {
			*out = append(*out, &pmNode{Type: "text", Text: s, Marks: marks})
		}
	}
	switch n := n.(type) {
	case *ast.Text:
		emit(decodeText(n.Segment.Value(c.src)), marks)
		if n.SoftLineBreak() || n.HardLineBreak() {
			*out = append(*out, &pmNode{Type: "hard_break"})
		}
	case *ast.String:
		emit(string(n.Value), marks)
	case *ast.CodeSpan:
		var b strings.Builder
		for t := n.FirstChild(); t != nil; t = t.NextSibling() {
			if s, ok := t.(*ast.Text); ok {
				b.Write(s.Segment.Value(c.src))
			} else if s, ok := t.(*ast.String); ok {
				b.Write(s.Value)
			}
		}
		emit(strings.ReplaceAll(b.String(), "\n", " "), withMark(marks, pmMark{Type: "code"}))
	case *ast.Emphasis:
		m := "em"
		if n.Level >= 2 {
			m = "strong"
		}
		c.inlines(n, withMark(marks, pmMark{Type: m}), out)
	case *extast.Strikethrough:
		c.inlines(n, withMark(marks, pmMark{Type: "strike"}), out)
	case *ast.Link:
		c.inlines(n, withMark(marks, linkMark(decodeText(n.Destination), n.Title)), out)
	case *ast.AutoLink:
		label := string(n.Label(c.src))
		href := string(n.URL(c.src))
		if n.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(href), "mailto:") {
			href = "mailto:" + href
		}
		emit(label, withMark(marks, linkMark(href, nil)))
	case *ast.Image:
		var alt any
		if s := plainText(n, c.src); s != "" {
			alt = s
		}
		var title any
		if len(n.Title) > 0 {
			title = decodeText(n.Title)
		}
		*out = append(*out, &pmNode{Type: "imageBlock", Attrs: map[string]any{
			"src": decodeText(n.Destination), "alt": alt, "title": title, "size": nil, "type": nil,
			"nodeHeight": 400, "nodeWidth": 900, "nodePageWidth": true, "nodeFullWidth": false, "id": c.newID(),
		}})
	case *ast.RawHTML:
		raw := string(n.Segments.Value(c.src))
		switch {
		case reBr.MatchString(raw):
			*out = append(*out, &pmNode{Type: "hard_break"})
		case !reSpan.MatchString(raw):
			emit(raw, marks)
		}
	case *extast.TaskCheckBox:
		// The list item's state carries it; drop the space that followed the box.
		if t, ok := n.NextSibling().(*ast.Text); ok {
			v := t.Segment.Value(c.src)
			t.Segment = t.Segment.WithStart(t.Segment.Start + len(v) - len(bytes.TrimLeft(v, " \t")))
		}
	default:
		c.inlines(n, marks, out)
	}
}

// Fibery's import turns <br> into a hard break and drops <span> tags (keeping
// their text); any other inline HTML stays literal.
var (
	reBr   = regexp.MustCompile(`(?i)^<br\s*/?>$`)
	reSpan = regexp.MustCompile(`(?i)^</?span(\s[^>]*)?>$`)
)

func linkMark(href string, title []byte) pmMark {
	var t any
	if len(title) > 0 {
		t = decodeText(title)
	}
	return pmMark{Type: "link", Attrs: map[string]any{"href": href, "title": t}}
}

// withMark returns marks plus m in Fibery's order; a mark already present stays once.
func withMark(marks []pmMark, m pmMark) []pmMark {
	out := make([]pmMark, 0, len(marks)+1)
	for _, x := range marks {
		if x.Type != m.Type {
			out = append(out, x)
		}
	}
	out = append(out, m)
	sort.SliceStable(out, func(i, j int) bool { return markRank[out[i].Type] < markRank[out[j].Type] })
	return out
}

func sameMarks(a, b []pmMark) bool {
	if len(a) != len(b) {
		return false
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

func mergeTexts(nodes []*pmNode) []*pmNode {
	var out []*pmNode
	for _, n := range nodes {
		if k := len(out) - 1; k >= 0 && n.Type == "text" && out[k].Type == "text" && sameMarks(out[k].Marks, n.Marks) {
			out[k] = &pmNode{Type: "text", Text: out[k].Text + n.Text, Marks: n.Marks}
			continue
		}
		out = append(out, n)
	}
	return out
}

var reToken = regexp.MustCompile(`\[\[#([@^])([0-9a-fA-F-]{36})/([0-9a-fA-F-]{36})\]\]|:([a-z0-9_+-]+):`)

// expandTokens splits a text node that is not code into text, mention, entity and
// emoji nodes, all carrying the text's marks.
func expandTokens(n *pmNode) []*pmNode {
	if n.Type != "text" || hasMark(n.Marks, "code") {
		return []*pmNode{n}
	}
	var out []*pmNode
	last := 0
	for _, m := range reToken.FindAllStringSubmatchIndex(n.Text, -1) {
		var node *pmNode
		switch {
		case m[2] >= 0:
			kind := "mention"
			if n.Text[m[2]:m[3]] == "^" {
				kind = "entity"
			}
			node = &pmNode{Type: kind, Attrs: map[string]any{
				"id": n.Text[m[6]:m[7]], "typeId": n.Text[m[4]:m[5]], "meta": map[string]any{},
			}, Marks: n.Marks}
		case knownEmoji[n.Text[m[8]:m[9]]]:
			node = &pmNode{Type: "emoji", Attrs: map[string]any{"code": n.Text[m[0]:m[1]]}, Marks: n.Marks}
		default:
			continue
		}
		if m[0] > last {
			out = append(out, &pmNode{Type: "text", Text: n.Text[last:m[0]], Marks: n.Marks})
		}
		out = append(out, node)
		last = m[1]
	}
	if last == 0 {
		return []*pmNode{n}
	}
	if last < len(n.Text) {
		out = append(out, &pmNode{Type: "text", Text: n.Text[last:], Marks: n.Marks})
	}
	return out
}

func hasMark(marks []pmMark, t string) bool {
	for _, m := range marks {
		if m.Type == t {
			return true
		}
	}
	return false
}

// plainText is the text of n's inline descendants, as an image's alt.
func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			b.WriteString(decodeText(t.Segment.Value(src)))
		case *ast.String:
			b.Write(t.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

var reEntity = regexp.MustCompile(`&(?:#[xX][0-9a-fA-F]{1,6}|#[0-9]{1,7}|[A-Za-z][A-Za-z0-9]{1,31});`)

// decodeText resolves backslash escapes and HTML entities the way a Markdown
// renderer does for text: an escaped character is never part of an entity.
func decodeText(b []byte) string {
	var out strings.Builder
	s := string(b)
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\\' && i+1 < len(s) && util.IsPunct(s[i+1]):
			out.WriteByte(s[i+1])
			i += 2
		case s[i] == '&':
			if loc := reEntity.FindStringIndex(s[i:]); loc != nil && loc[0] == 0 {
				ent := s[i : i+loc[1]]
				if dec := html.UnescapeString(ent); dec != ent {
					out.WriteString(dec)
					i += loc[1]
					continue
				}
			}
			out.WriteByte('&')
			i++
		default:
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// strikeParser is goldmark's strikethrough with exactly two tildes, as in
// Fibery's import: a single "~" (as in "~5 min") stays text.
type strikeParser struct{}

type strikeDelimiter struct{}

func (strikeDelimiter) IsDelimiter(b byte) bool { return b == '~' }

func (strikeDelimiter) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char && opener.OriginalLength == closer.OriginalLength
}

func (strikeDelimiter) OnMatch(int) ast.Node { return extast.NewStrikethrough() }

func (*strikeParser) Trigger() []byte { return []byte{'~'} }

func (*strikeParser) Parse(_ ast.Node, block text.Reader, pc parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()
	node := parser.ScanDelimiter(line, before, 1, strikeDelimiter{})
	if node == nil || node.OriginalLength != 2 || before == '~' {
		return nil
	}
	node.Segment = segment.WithStop(segment.Start + node.OriginalLength)
	block.Advance(node.OriginalLength)
	pc.PushDelimiter(node)
	return node
}

func (*strikeParser) CloseBlock(ast.Node, parser.Context) {}
