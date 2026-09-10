package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewInlineThread(t *testing.T) {
	body := pmParagraphDoc("hello", "g1")
	th := newInlineThread(11, 16, body, "author-uuid", "thread-uuid", 1700000000000)
	assert.Equal(t, 11, th["from"])
	assert.Equal(t, 16, th["to"])
	assert.Equal(t, "thread-uuid", th["id"])
	assert.Equal(t, "open", th["state"])
	assert.Equal(t, false, th["detached"])
	assert.Nil(t, th["thread"])
	assert.Equal(t, int64(1700000000000), th["date"])
	assert.Equal(t, map[string]any{"id": "author-uuid"}, th["author"])
	bodyMap := th["body"].(map[string]any)
	assert.Equal(t, body, bodyMap["doc"])
	assert.Equal(t, []any{}, bodyMap["comments"])
}

func TestInlineFindSetDelete(t *testing.T) {
	threads := []any{
		newInlineThread(1, 3, pmParagraphDoc("a", "g"), "u", "id-A", 1),
		newInlineThread(5, 9, pmParagraphDoc("b", "g"), "u", "id-B", 2),
	}
	assert.Equal(t, 1, inlineFindIdx(threads, "id-B"))
	assert.Equal(t, -1, inlineFindIdx(threads, "nope"))

	assert.NoError(t, inlineSetState(threads, "id-A", "resolved"))
	assert.Equal(t, "resolved", threads[0].(map[string]any)["state"])
	assert.Error(t, inlineSetState(threads, "nope", "resolved"))

	out, err := inlineDelete(threads, "id-A")
	assert.NoError(t, err)
	assert.Len(t, out, 1)
	assert.Equal(t, "id-B", out[0].(map[string]any)["id"])
	_, err = inlineDelete(threads, "nope")
	assert.Error(t, err)
}

func TestInlineReply(t *testing.T) {
	threads := []any{newInlineThread(1, 3, pmParagraphDoc("a", "g"), "u", "id-A", 1)}
	reply := newReply(pmParagraphDoc("re", "g2"), "u2", "id-R", 3)
	assert.NoError(t, inlineReply(threads, "id-A", reply))
	body := threads[0].(map[string]any)["body"].(map[string]any)
	replies := body["comments"].([]any)
	assert.Len(t, replies, 1)
	assert.Equal(t, "id-R", replies[0].(map[string]any)["id"])
	assert.Error(t, inlineReply(threads, "nope", reply))
}
