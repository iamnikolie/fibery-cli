package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamnikolie/fibery-cli/internal/client"
)

// nativeNote is the callout line Fibery's Markdown export prints for a blue
// callout; "doc set" writes the same line for "> [!NOTE]".
const nativeNote = "> [//]: # (callout;icon-type=icon;icon=exclamation-circle;color=#199EE3)"

// fakeDocs stands in for Fibery's document endpoints. It stores what the CLI
// writes and answers reads with recorded-shape responses: GET ?format=md returns
// the stored Markdown as Fibery exports it, GET ?format=json the envelope.
type fakeDocs struct {
	mu       sync.Mutex
	md       map[string]string          // secret → Markdown (?format=md)
	doc      map[string]json.RawMessage // secret → ProseMirror doc
	comments map[string][]any
	modified string
	writes   []string // "md <secret>", "json <secret>", "scratch", "delete"
}

func newFakeDocs(t *testing.T) *fakeDocs {
	t.Helper()
	f := &fakeDocs{
		md:       map[string]string{},
		doc:      map[string]json.RawMessage{},
		comments: map[string][]any{},
		modified: "2026-10-14T09:00:00.000Z",
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	prev := cli
	cli = client.New("tok", srv.URL)
	t.Cleanup(func() { cli = prev })
	return f
}

func (f *fakeDocs) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	format := r.URL.Query().Get("format")
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/documents/commands" {
		var c struct {
			Command string           `json:"command"`
			Args    []map[string]any `json:"args"`
		}
		_ = json.Unmarshal(body, &c)
		secret, _ := c.Args[0]["secret"].(string)
		if c.Command == "delete-documents" {
			f.writes = append(f.writes, "delete")
			delete(f.md, secret)
			delete(f.doc, secret)
		} else {
			f.writes = append(f.writes, "scratch")
			md, _ := c.Args[0]["content"].(string)
			f.md[secret] = md
			f.doc[secret] = mdToFakeDoc(md)
		}
		_, _ = w.Write([]byte(`{}`))
		return
	}
	secret := strings.TrimPrefix(r.URL.Path, "/api/documents/")
	switch {
	case r.Method == http.MethodGet && format == "md":
		_ = json.NewEncoder(w).Encode(map[string]string{"content": f.md[secret]})
	case r.Method == http.MethodGet && format == "json":
		comments := f.comments[secret]
		if comments == nil {
			comments = []any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"secret":           secret,
			"content":          map[string]any{"doc": f.doc[secret], "comments": comments},
			"modificationDate": f.modified,
		})
	case r.Method == http.MethodPut && format == "json":
		var p struct {
			Content client.DocContent `json:"content"`
		}
		_ = json.Unmarshal(body, &p)
		f.writes = append(f.writes, "json "+secret)
		f.doc[secret] = p.Content.Doc
		f.comments[secret] = p.Content.Comments
		_, _ = w.Write([]byte(`{}`))
	case r.Method == http.MethodPut:
		var p struct {
			Content string `json:"content"`
			Type    string `json:"type"`
		}
		_ = json.Unmarshal(body, &p)
		f.writes = append(f.writes, "md "+secret)
		// Fibery exports a callout in the same native form it imports.
		f.md[secret] = p.Content
		f.doc[secret] = mdToFakeDoc(p.Content)
		_, _ = w.Write([]byte(`{}`))
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusNotFound)
	}
}

// mdToFakeDoc stands in for Fibery's converter: one paragraph per line.
func mdToFakeDoc(md string) json.RawMessage {
	var nodes []any
	for _, line := range strings.Split(md, "\n") {
		nodes = append(nodes, map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": line}}})
	}
	b, _ := json.Marshal(map[string]any{"type": "doc", "content": nodes})
	return b
}

// runDoc runs a doc subcommand's RunE with the given flag values and returns stdout.
func runDoc(t *testing.T, c func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	prev := os.Stdout
	os.Stdout = w
	runErr := c()
	os.Stdout = prev
	require.NoError(t, w.Close())
	out, _ := io.ReadAll(r)
	return string(out), runErr
}

func resetDocFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		docDB, docField, docSecretArg, docJSONDoc = "", "", true, false
		docFile, docSetJSONFile, docIfUnmodifiedSince = "", "", ""
	}
	reset()
	t.Cleanup(func() {
		reset()
		docSecretArg = false
	})
	for _, c := range []*cobra.Command{docGetCmd, docSetCmd, docAppendCmd} {
		c.SetContext(context.Background())
	}
}

func TestDocSetFile_CalloutReadsBackNative(t *testing.T) {
	f := newFakeDocs(t)
	resetDocFlags(t)
	path := filepath.Join(t.TempDir(), "body.md")
	require.NoError(t, os.WriteFile(path, []byte("# T\n\n> [!NOTE]\n> Heads up\n\nliteral \\n stays"), 0o600))

	docFile = path
	_, err := runDoc(t, func() error { return docSetCmd.RunE(docSetCmd, []string{"sec"}) })
	require.NoError(t, err)
	assert.Equal(t, []string{"md sec"}, f.writes)

	docFile = ""
	out, err := runDoc(t, func() error { return docGetCmd.RunE(docGetCmd, []string{"sec"}) })
	require.NoError(t, err)
	assert.Equal(t, "# T\n\n"+nativeNote+"\n> Heads up\n\nliteral \\n stays", out,
		"--file is verbatim and the short form reads back as the native callout line")

	// Writing the read-back again is lossless.
	path2 := filepath.Join(t.TempDir(), "again.md")
	require.NoError(t, os.WriteFile(path2, []byte(out), 0o600))
	docFile = path2
	_, err = runDoc(t, func() error { return docSetCmd.RunE(docSetCmd, []string{"sec"}) })
	require.NoError(t, err)
	assert.Equal(t, out, f.md["sec"])
}

func TestDocAppendFile_ExpandsCalloutAndKeepsBody(t *testing.T) {
	f := newFakeDocs(t)
	resetDocFlags(t)
	f.doc["sec"] = json.RawMessage(`{"type":"doc","content":[{"type":"fiberyView","attrs":{"viewId":"v1"}}]}`)
	f.comments["sec"] = []any{map[string]any{"id": "c1"}}
	path := filepath.Join(t.TempDir(), "add.md")
	require.NoError(t, os.WriteFile(path, []byte("> [!CAUTION]\n> careful"), 0o600))

	docFile = path
	_, err := runDoc(t, func() error { return docAppendCmd.RunE(docAppendCmd, []string{"sec"}) })
	require.NoError(t, err)
	assert.Equal(t, []string{"scratch", "delete", "json sec"}, f.writes)
	doc := string(f.doc["sec"])
	assert.Contains(t, doc, `"fiberyView"`, "the existing body is spliced, not rewritten")
	assert.Contains(t, doc, "color=#d40915", "the appended Markdown went through ExpandCallouts")
	assert.Len(t, f.comments["sec"], 1, "inline comments survive")
}

func TestDocWrite_IfUnmodifiedSince(t *testing.T) {
	f := newFakeDocs(t)
	resetDocFlags(t)
	f.md["sec"] = "old"
	f.doc["sec"] = mdToFakeDoc("old")
	f.modified = "2026-10-14T09:00:00.001Z"

	docIfUnmodifiedSince = "2026-10-14T09:00:00Z"
	_, err := runDoc(t, func() error { return docSetCmd.RunE(docSetCmd, []string{"sec", "new"}) })
	assert.ErrorContains(t, err, "not written")
	_, err = runDoc(t, func() error { return docAppendCmd.RunE(docAppendCmd, []string{"sec", "more"}) })
	assert.ErrorContains(t, err, "not written")
	assert.NotContains(t, f.writes, "md sec")
	assert.NotContains(t, f.writes, "json sec")

	docIfUnmodifiedSince = f.modified
	_, err = runDoc(t, func() error { return docSetCmd.RunE(docSetCmd, []string{"sec", "new"}) })
	require.NoError(t, err)
	assert.Equal(t, "new", f.md["sec"])

	docIfUnmodifiedSince = "yesterday"
	_, err = runDoc(t, func() error { return docSetCmd.RunE(docSetCmd, []string{"sec", "x"}) })
	assert.ErrorContains(t, err, "RFC 3339")
}

func TestDocJSONDoc_GetSetRoundTrip(t *testing.T) {
	f := newFakeDocs(t)
	resetDocFlags(t)
	f.doc["sec"] = json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`)
	f.comments["sec"] = []any{map[string]any{"id": "c1"}}

	docJSONDoc = true
	out, err := runDoc(t, func() error { return docGetCmd.RunE(docGetCmd, []string{"sec"}) })
	require.NoError(t, err)
	var env client.DocJSON
	require.NoError(t, json.Unmarshal([]byte(out), &env))
	assert.Equal(t, f.modified, env.ModificationDate)
	assert.Len(t, env.Content.Comments, 1)

	// A bare doc without comments keeps the document's inline comments.
	docJSONDoc = false
	path := filepath.Join(t.TempDir(), "doc.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"doc":{"type":"doc","content":[{"type":"heading"}]}}`), 0o600))
	docSetJSONFile = path
	_, err = runDoc(t, func() error { return docSetCmd.RunE(docSetCmd, []string{"sec"}) })
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"doc","content":[{"type":"heading"}]}`, string(f.doc["sec"]))
	assert.Len(t, f.comments["sec"], 1)

	_, err = runDoc(t, func() error { return docSetCmd.RunE(docSetCmd, []string{"sec", "text"}) })
	assert.ErrorContains(t, err, "without text or --file")
	docSetJSONFile = "--file"
	_, err = runDoc(t, func() error { return docSetCmd.RunE(docSetCmd, []string{"sec"}) })
	assert.ErrorContains(t, err, "takes a file path")
}
