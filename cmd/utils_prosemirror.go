package cmd

import (
	"fmt"
	"strings"
)

// pmRunePos pairs a text rune with its absolute ProseMirror position.
type pmRunePos struct {
	Pos int
	R   rune
}

// pmUnitLen returns the UTF-16 code-unit count of r (ProseMirror/JS position semantics).
func pmUnitLen(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
}

// pmCollectRunes walks a ProseMirror document node (the value of content.doc) and returns
// every text rune with its absolute position. Rules: a text node's runes occupy
// consecutive positions (UTF-16 units each); a non-leaf node spends one position on its
// opening token and one on its closing token; a leaf node spends one position. Top-level
// doc children start at absolute position 0.
func pmCollectRunes(doc map[string]any) []pmRunePos {
	var out []pmRunePos
	var walk func(node map[string]any, pos int) int
	walk = func(node map[string]any, pos int) int {
		if t, _ := node["type"].(string); t == "text" {
			txt, _ := node["text"].(string)
			for _, r := range txt {
				out = append(out, pmRunePos{Pos: pos, R: r})
				pos += pmUnitLen(r)
			}
			return pos
		}
		content, ok := node["content"].([]any)
		if !ok {
			return pos + 1 // leaf node (hard_break, horizontal_rule, image, …)
		}
		p := pos + 1 // opening token
		for _, c := range content {
			if cm, ok := c.(map[string]any); ok {
				p = walk(cm, p)
			}
		}
		return p + 1 // closing token
	}
	content, _ := doc["content"].([]any)
	p := 0
	for _, c := range content {
		if cm, ok := c.(map[string]any); ok {
			p = walk(cm, p)
		}
	}
	return out
}

// pmFindRange locates target as a contiguous run of text runes and returns the ProseMirror
// from/to positions. occurrence is 1-based; 0 requires a unique match. Within-block anchors
// only (text within a single block is contiguous in both string and position space).
func pmFindRange(doc map[string]any, target string, occurrence int) (int, int, error) {
	if target == "" {
		return 0, 0, fmt.Errorf("empty anchor text")
	}
	runes := pmCollectRunes(doc)
	flat := make([]rune, len(runes))
	for i, rp := range runes {
		flat[i] = rp.R
	}
	tr := []rune(target)
	var matches [][2]int // [startRuneIdx, endRuneIdx]
	for i := 0; i+len(tr) <= len(flat); i++ {
		ok := true
		for j := range tr {
			if flat[i+j] != tr[j] {
				ok = false
				break
			}
		}
		if ok {
			matches = append(matches, [2]int{i, i + len(tr) - 1})
		}
	}
	if len(matches) == 0 {
		return 0, 0, fmt.Errorf("anchor text %q not found in document", target)
	}
	if occurrence <= 0 {
		if len(matches) > 1 {
			return 0, 0, fmt.Errorf("anchor text %q matches %d places; pass --occurrence (1-%d)", target, len(matches), len(matches))
		}
		occurrence = 1
	}
	if occurrence > len(matches) {
		return 0, 0, fmt.Errorf("anchor text %q has %d matches; --occurrence %d out of range", target, len(matches), occurrence)
	}
	m := matches[occurrence-1]
	from := runes[m[0]].Pos
	last := runes[m[1]]
	to := last.Pos + pmUnitLen(last.R)
	return from, to, nil
}

// pmSliceText returns the document text in absolute position range [from, to).
func pmSliceText(doc map[string]any, from, to int) string {
	var b strings.Builder
	for _, rp := range pmCollectRunes(doc) {
		if rp.Pos >= from && rp.Pos < to {
			b.WriteRune(rp.R)
		}
	}
	return b.String()
}

// pmDocText returns the full concatenated text of a ProseMirror document.
func pmDocText(doc map[string]any) string {
	var b strings.Builder
	for _, rp := range pmCollectRunes(doc) {
		b.WriteRune(rp.R)
	}
	return b.String()
}
