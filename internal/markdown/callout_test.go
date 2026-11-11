package markdown

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpandCallouts_Kinds(t *testing.T) {
	in := "intro\n\n> [!NOTE]\n> ## Head\n>\n> * one\n\n> [!caution]\n> careful"
	want := "intro\n\n" +
		"> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)\n> ## Head\n>\n> * one\n\n" +
		"> [//]: # (callout;icon-type=icon;icon=x;color=#d40915)\n> careful"
	assert.Equal(t, want, ExpandCallouts(in))
}

func TestExpandCallouts_TextOnMarkerLine(t *testing.T) {
	assert.Equal(t,
		"> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)\n> Heads up\n> more",
		ExpandCallouts("> [!NOTE] Heads up\n> more"))
}

func TestExpandCallouts_LeavesOtherInputAlone(t *testing.T) {
	cases := map[string]string{
		"no alert":                         "# T\n\n> quote\n\n| a | b |\n| --- | --- |\n| 1 | 2 |",
		"unknown kind":                     "> [!WARNING]\n> text",
		"inside fence":                     "```md\n> [!NOTE]\n```\n",
		"inside tilde":                     "~~~\n> [!NOTE]\n~~~",
		"native form":                      "> [//]: # (callout;icon-type=icon;icon=x;color=#d40915)\n> text",
		"not a blockquote":                 "[!NOTE] inline",
		"four-space indent":                "    > [!NOTE]",
		"tab indent":                       "\t> [!NOTE]",
		"mid-quote":                        "> first\n> [!NOTE]\n> x",
		"second marker":                    "> [!WARNING]\n> a\n> [!NOTE]\n> b",
		"info-string close":                "```\n```go\n> [!NOTE]\n```",
		"fence in a quote":                 "> ```md\n> [!NOTE]\n> ```",
		"quoted fence line inside a fence": "```\n> ```js\n> x\n> ```\n\n> [!NOTE]\n> x\n```",
	}
	for name, in := range cases {
		assert.Equal(t, in, ExpandCallouts(in), name)
	}
}

func TestExpandCallouts_AfterFenceCloses(t *testing.T) {
	in := "````\n```\n> [!NOTE]\n````\n> [!NOTE]\n> x"
	want := "````\n```\n> [!NOTE]\n````\n> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)\n> x"
	assert.Equal(t, want, ExpandCallouts(in))
}

func TestExpandCallouts_QuotedFenceLineDoesNotCloseFence(t *testing.T) {
	in := "```md\n> ```\n> [!NOTE]\n```\n\n> [!NOTE]\n> x"
	want := "```md\n> ```\n> [!NOTE]\n```\n\n> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)\n> x"
	assert.Equal(t, want, ExpandCallouts(in))
}

func TestExpandCallouts_FenceEndsWithItsQuote(t *testing.T) {
	in := "> ```\n> code\n\n```\nplain\n```\n\n> [!NOTE]\n> x"
	want := "> ```\n> code\n\n```\nplain\n```\n\n> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)\n> x"
	assert.Equal(t, want, ExpandCallouts(in))
}

func TestExpandCallouts_Idempotent(t *testing.T) {
	once := ExpandCallouts("> [!NOTE]\n> x")
	assert.Equal(t, once, ExpandCallouts(once))
}

func TestExpandCallouts_InlineCodeIsNotAFence(t *testing.T) {
	in := "``` x ``` y\n\n> [!NOTE]\n> z"
	assert.Equal(t, "``` x ``` y\n\n> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)\n> z", ExpandCallouts(in))
}

func TestExpandCallouts_AfterQuoteEnds(t *testing.T) {
	in := "> plain\n\n> [!CAUTION]\n> x"
	assert.Equal(t, "> plain\n\n> [//]: # (callout;icon-type=icon;icon=x;color=#d40915)\n> x", ExpandCallouts(in))
}
