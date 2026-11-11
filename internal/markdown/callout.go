// Package markdown rewrites agent-friendly Markdown into the dialect Fibery's
// document import understands.
package markdown

import (
	"regexp"
	"strings"
)

// Fibery's markdown import and export both carry a callout as a blockquote whose
// first line is this comment; the attributes map 1:1 onto the ProseMirror callout
// node (color, iconType, icon). Which icon names the UI renders is undocumented, so
// only icon/color pairs copied from callouts made in the Fibery UI are listed.
var calloutKinds = map[string]string{
	"NOTE":    "> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)",
	"CAUTION": "> [//]: # (callout;icon-type=icon;icon=x;color=#d40915)",
}

var (
	reAlert     = regexp.MustCompile(`^( {0,3})>[ \t]?\[!([A-Za-z]+)\][ \t]*(.*)$`)
	reQuote     = regexp.MustCompile(`^ {0,3}>`)
	reQuoteMark = regexp.MustCompile(`^ {0,3}>[ \t]?`)
	// A backtick fence's info string cannot contain a backtick (CommonMark), which
	// keeps a line that starts with inline code from opening a fence.
	reFence = regexp.MustCompile("^ {0,3}(`{3,}[^`]*|~{3,}.*)$")
)

// fence is an open fenced code block. A fence belongs to the blockquote depth it
// opened at: a fence line at another depth is content of the block, not its close,
// and the fence ends together with the quote holding it.
type fence struct {
	marker string
	depth  int
}

// ExpandCallouts turns GitHub alert blockquotes (`> [!NOTE]`, `> [!CAUTION]`) into
// Fibery callouts. Only a marker that opens a blockquote counts; text following it
// on the same line becomes the callout's first line. Unknown kinds and markers
// inside fenced code are left untouched, and a body with no alert comes back unchanged.
func ExpandCallouts(md string) string {
	if !strings.Contains(md, "[!") {
		return md
	}
	lines := strings.Split(md, "\n")
	out := make([]string, 0, len(lines))
	var open fence
	prevQuoted := false
	for _, line := range lines {
		wasQuoted := prevQuoted
		prevQuoted = reQuote.MatchString(line)
		depth, rest := unquote(line, -1)
		if open.marker != "" && depth < open.depth {
			open = fence{}
		}
		if open.marker != "" {
			_, rest = unquote(line, open.depth)
			if m := reFence.FindStringSubmatch(rest); m != nil {
				open.marker = nextFence(open.marker, m[1])
			}
			out = append(out, line)
			continue
		}
		if m := reFence.FindStringSubmatch(rest); m != nil {
			open = fence{marker: nextFence("", m[1]), depth: depth}
			out = append(out, line)
			continue
		}
		head, ok := "", false
		m := reAlert.FindStringSubmatch(line)
		if m != nil && !wasQuoted {
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

// unquote strips up to limit blockquote markers from line (all of them when limit
// is negative) and returns how many it stripped and the rest of the line.
func unquote(line string, limit int) (int, string) {
	n := 0
	for n != limit {
		loc := reQuoteMark.FindStringIndex(line)
		if loc == nil {
			break
		}
		line = line[loc[1]:]
		n++
	}
	return n, line
}

// nextFence returns the open fence after a fence line whose text (from the fence
// characters on) is s: it opens one when none is open, and closes the open one only
// on a bare run of the same character at least as long — a closing fence carries
// no info string.
func nextFence(open, s string) string {
	run := strings.TrimLeft(s, s[:1])
	marker := s[:len(s)-len(run)]
	if open == "" {
		return marker
	}
	if marker[0] == open[0] && len(marker) >= len(open) && strings.TrimSpace(run) == "" {
		return ""
	}
	return open
}
