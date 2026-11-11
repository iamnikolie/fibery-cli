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

func TestExpandCallouts_FenceInListItem(t *testing.T) {
	note := "> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)"
	caution := "> [//]: # (callout;icon-type=icon;icon=x;color=#d40915)"
	cases := []struct{ name, in, want string }{
		{"backticks after a bullet",
			"- ```markdown\n  > [!NOTE]\n  ```\n\n> [!NOTE]\n> x",
			"- ```markdown\n  > [!NOTE]\n  ```\n\n" + note + "\n> x"},
		{"tildes after a number",
			"1. ~~~\n   > [!CAUTION]\n   ~~~\n\n> [!CAUTION]\n> y",
			"1. ~~~\n   > [!CAUTION]\n   ~~~\n\n" + caution + "\n> y"},
		{"fence at a nested item's indent",
			"- a\n  - b\n\n    ```\n    > [!NOTE]\n    ```\n\n> [!NOTE]\n> x",
			"- a\n  - b\n\n    ```\n    > [!NOTE]\n    ```\n\n" + note + "\n> x"},
		{"fence ends with its list item",
			"- ```\n  code\n\nplain\n\n> [!NOTE]\n> x",
			"- ```\n  code\n\nplain\n\n" + note + "\n> x"},
		{"fence line four spaces past the item's content is code, not a close",
			"- ```markdown\n      ```\n  > [!NOTE]\n  ```\n\n> [!NOTE]\n> x",
			"- ```markdown\n      ```\n  > [!NOTE]\n  ```\n\n" + note + "\n> x"},
		{"fence at the item's content column ends with the item",
			"- item\n  ```markdown\n  code\n\n> [!NOTE]\n> real",
			"- item\n  ```markdown\n  code\n\n" + note + "\n> real"},
		{"fence inside a nested item, closed at that item's column",
			"1. a\n   - ```\n     > [!CAUTION]\n     ```\n\n> [!CAUTION]\n> y",
			"1. a\n   - ```\n     > [!CAUTION]\n     ```\n\n" + caution + "\n> y"},
		{"tab-indented fence inside an item",
			"- a\n\n\t```\n\t> [!NOTE]\n\t```\n\n> [!NOTE]\n> x",
			"- a\n\n\t```\n\t> [!NOTE]\n\t```\n\n" + note + "\n> x"},
		{"indented fence outside a list is not a fence",
			"para\n\n    ```\n\n> [!NOTE]\n> x",
			"para\n\n    ```\n\n" + note + "\n> x"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ExpandCallouts(c.in), c.name)
	}
}

func TestExpandCallouts_ContainersFromTheParser(t *testing.T) {
	note := "> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)"
	cases := []struct{ name, in, want string }{
		{"fence in a quote in a list item",
			"- > ```markdown\n  > [!NOTE]\n  > ```\n\n> [!NOTE]\n> x",
			"- > ```markdown\n  > [!NOTE]\n  > ```\n\n" + note + "\n> x"},
		{"fence after a lazy continuation line stays in the item",
			"- paragraph\nlazy continuation\n  ```markdown\n  code\n\n> [!NOTE]\n> real",
			"- paragraph\nlazy continuation\n  ```markdown\n  code\n\n" + note + "\n> real"},
		{"indented code", "para\n\n    > [!NOTE]\n\n> [!NOTE]\n> x",
			"para\n\n    > [!NOTE]\n\n" + note + "\n> x"},
		{"html block", "<div>\n> [!NOTE]\n</div>\n\n> [!NOTE]\n> x",
			"<div>\n> [!NOTE]\n</div>\n\n" + note + "\n> x"},
		{"alert in a list item", "- item\n\n  > [!NOTE]\n  > x",
			"- item\n\n  " + note + "\n  > x"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, ExpandCallouts(c.in), c.name)
	}
}
