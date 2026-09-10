package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"strings"
	"time"
)

// Client is the Fibery HTTP client. All requests go through request().
type Client struct {
	token   string
	baseURL string
	http    *http.Client
	Verbose bool
}

// Command is a Fibery API command sent to POST /api/commands.
type Command struct {
	Command string `json:"command"`
	Args    any    `json:"args"`
}

// Result is one entry in the /api/commands response array.
type Result struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
	Error   *APIError       `json:"error,omitempty"`
}

// APIError is the error payload returned when Success is false.
type APIError struct {
	Message string `json:"message"`
}

// HistoryFilter is one clause in the /api/history/v2/search where array.
type HistoryFilter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

// HistoryItem is one entry in the history API response.
type HistoryItem struct {
	Action string `json:"action"`
	Date   string `json:"date"`
	Author struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"author"`
	Entity struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		PublicID string `json:"publicId"`
	} `json:"entity"`
	ChangedValues []struct {
		Field struct {
			Title string `json:"title"`
		} `json:"field"`
		CurrentValue  any `json:"currentValue"`
		PreviousValue any `json:"previousValue"`
	} `json:"changedValues"`
}

func New(token, baseURL string) *Client {
	return &Client{
		token:   token,
		baseURL: baseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// request is the single HTTP method for all Fibery API calls.
// It sets auth headers, retries on 429 with exponential backoff, and returns the response body.
// method: HTTP verb; path: relative path e.g. "/api/commands"; body: nil for GET.
func (c *Client) request(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	b, _, err := c.requestRaw(ctx, method, path, body)
	return b, err
}

// requestRaw is request() that also returns the response headers. Used when the
// caller needs metadata from the response (e.g. Content-Type for file downloads).
func (c *Client) requestRaw(ctx context.Context, method, path string, body []byte) ([]byte, http.Header, error) {
	return c.requestRawCT(ctx, method, path, body, "application/json")
}

// requestRawCT is requestRaw with an explicit request Content-Type, so non-JSON
// bodies (e.g. multipart file uploads) set their own. An empty contentType sends
// no Content-Type header.
func (c *Client) requestRawCT(ctx context.Context, method, path string, body []byte, contentType string) ([]byte, http.Header, error) {
	const maxRetries = 3
	wait := time.Second

	for attempt := 0; attempt <= maxRetries; attempt++ {
		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Token "+c.token)
		if body != nil && contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, nil, err
		}
		b, _ := io.ReadAll(resp.Body)
		header := resp.Header
		resp.Body.Close()

		if c.Verbose {
			// File bodies are raw bytes, not JSON — log only the size to avoid dumping binary.
			if isBinaryPath(path) {
				fmt.Fprintf(os.Stderr, "← HTTP %d (%d bytes)\n", resp.StatusCode, len(b))
			} else {
				fmt.Fprintf(os.Stderr, "← HTTP %d\n%s\n", resp.StatusCode, string(b))
			}
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt == maxRetries {
				return nil, nil, fmt.Errorf("HTTP 429: rate limited after %d retries", maxRetries)
			}
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(wait):
			}
			wait *= 2
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return nil, nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
		}
		return b, header, nil
	}
	return nil, nil, fmt.Errorf("unreachable")
}

// isBinaryPath reports whether a path returns raw bytes rather than JSON, so
// verbose logging can avoid dumping binary file contents to stderr.
func isBinaryPath(path string) bool {
	return strings.HasPrefix(path, "/api/files/")
}

// Do sends a batch of commands to POST /api/commands.
func (c *Client) Do(ctx context.Context, commands []Command) ([]Result, error) {
	body, err := json.Marshal(commands)
	if err != nil {
		return nil, fmt.Errorf("client.Do: marshal: %w", err)
	}
	if c.Verbose {
		var pretty bytes.Buffer
		json.Indent(&pretty, body, "", "  ")
		fmt.Fprintf(os.Stderr, "→ POST /api/commands\n%s\n", pretty.String())
	}
	b, err := c.request(ctx, http.MethodPost, "/api/commands", body)
	if err != nil {
		return nil, fmt.Errorf("client.Do: %w", err)
	}
	var results []Result
	if err := json.Unmarshal(b, &results); err != nil {
		return nil, fmt.Errorf("client.Do: decode: %w", err)
	}
	return results, nil
}

// One sends a single command and returns its result.
func (c *Client) One(ctx context.Context, cmd Command) (json.RawMessage, error) {
	results, err := c.Do(ctx, []Command{cmd})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("client.One: empty response")
	}
	r := results[0]
	if !r.Success {
		if r.Error != nil {
			return nil, fmt.Errorf("client.One: %s", r.Error.Message)
		}
		return nil, fmt.Errorf("client.One: command failed: %s", string(r.Result))
	}
	return r.Result, nil
}

// GetDocument fetches a Fibery document as Markdown by its secret.
func (c *Client) GetDocument(ctx context.Context, secret string) (string, error) {
	b, err := c.request(ctx, http.MethodGet, "/api/documents/"+secret+"?format=md", nil)
	if err != nil {
		return "", fmt.Errorf("client.GetDocument: %w", err)
	}
	var envelope struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(b, &envelope) == nil {
		return envelope.Content, nil
	}
	return string(b), nil
}

// FileInfo describes a file entity as returned by POST /api/files.
type FileInfo struct {
	ID          string `json:"fibery/id"`
	Secret      string `json:"fibery/secret"`
	Name        string `json:"fibery/name"`
	ContentType string `json:"fibery/content-type"`
}

// UploadFile uploads raw bytes as a new Fibery file via multipart POST /api/files
// and returns the created file entity. contentType is the MIME type sent for the
// part; empty means none. The file is not attached to any entity — callers link
// it via add-collection-items (a file field) or by referencing its secret in a
// document.
func (c *Client) UploadFile(ctx context.Context, filename, contentType string, data []byte) (*FileInfo, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	part, err := w.CreatePart(h)
	if err != nil {
		return nil, fmt.Errorf("client.UploadFile: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("client.UploadFile: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("client.UploadFile: %w", err)
	}

	b, _, err := c.requestRawCT(ctx, http.MethodPost, "/api/files", buf.Bytes(), w.FormDataContentType())
	if err != nil {
		return nil, fmt.Errorf("client.UploadFile: %w", err)
	}
	// The endpoint may return the file object directly or wrapped in an array.
	var fi FileInfo
	if json.Unmarshal(b, &fi) == nil && fi.Secret != "" {
		return &fi, nil
	}
	var arr []FileInfo
	if json.Unmarshal(b, &arr) == nil && len(arr) > 0 {
		return &arr[0], nil
	}
	return nil, fmt.Errorf("client.UploadFile: unexpected response: %s", string(b))
}

// DownloadFile fetches the raw bytes of a Fibery file attachment by its secret.
// GET /api/files/<secret> returns the file body as-is (not a JSON envelope).
// Returns the file bytes and the response Content-Type header.
func (c *Client) DownloadFile(ctx context.Context, secret string) ([]byte, string, error) {
	b, header, err := c.requestRaw(ctx, http.MethodGet, "/api/files/"+secret, nil)
	if err != nil {
		return nil, "", fmt.Errorf("client.DownloadFile: %w", err)
	}
	return b, header.Get("Content-Type"), nil
}

// SetDocument writes Markdown content to a Fibery document by its secret.
func (c *Client) SetDocument(ctx context.Context, secret, markdown string) error {
	body, err := json.Marshal(map[string]string{"content": markdown, "type": "text/markdown"})
	if err != nil {
		return fmt.Errorf("client.SetDocument: marshal: %w", err)
	}
	if _, err := c.request(ctx, http.MethodPut, "/api/documents/"+secret, body); err != nil {
		return fmt.Errorf("client.SetDocument: %w", err)
	}
	return nil
}

// DocContent is the rich-text document payload in the ?format=json envelope. Doc is kept as
// raw bytes so the document body round-trips byte-identically; Comments is the mutable
// inline-comment array.
type DocContent struct {
	Doc      json.RawMessage `json:"doc"`
	Comments []any           `json:"comments"`
}

// DocJSON is the GET /api/documents/<secret>?format=json envelope.
type DocJSON struct {
	Secret           string     `json:"secret"`
	Content          DocContent `json:"content"`
	ModificationDate string     `json:"modificationDate"`
}

// GetDocumentJSON fetches a document as ProseMirror JSON, including inline comments
// (content.comments). Use this instead of GetDocument when inline comments matter.
func (c *Client) GetDocumentJSON(ctx context.Context, secret string) (*DocJSON, error) {
	b, err := c.request(ctx, http.MethodGet, "/api/documents/"+secret+"?format=json", nil)
	if err != nil {
		return nil, fmt.Errorf("client.GetDocumentJSON: %w", err)
	}
	var d DocJSON
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("client.GetDocumentJSON: decode: %w", err)
	}
	if d.Content.Comments == nil {
		d.Content.Comments = []any{}
	}
	return &d, nil
}

// SetDocumentJSON writes a document's ProseMirror JSON content (doc + inline comments).
// The endpoint requires ?format=json with content as a JSON object (NOT a stringified
// blob, and NOT a "type" field — those route content through the markdown path and either
// error or overwrite the document body).
func (c *Client) SetDocumentJSON(ctx context.Context, secret string, content DocContent) error {
	body, err := json.Marshal(map[string]any{"content": content})
	if err != nil {
		return fmt.Errorf("client.SetDocumentJSON: marshal: %w", err)
	}
	if _, err := c.request(ctx, http.MethodPut, "/api/documents/"+secret+"?format=json", body); err != nil {
		return fmt.Errorf("client.SetDocumentJSON: %w", err)
	}
	return nil
}

// NewUUID returns a random UUID v4 (exported wrapper around the internal generator).
func NewUUID() string { return newUUID() }

// QueryAll runs an entity query repeatedly with an increasing q/offset, paging in
// chunks of pageSize until a page returns fewer than pageSize rows, and returns the
// concatenated result as a single JSON array. This sidesteps Fibery's per-query
// row cap (q/limit max 3001). The query should include a stable q/order-by so paging
// is deterministic; q/limit and q/offset are set per page. params may be nil.
func (c *Client) QueryAll(ctx context.Context, query map[string]any, params map[string]any, pageSize int) (json.RawMessage, error) {
	if pageSize <= 0 {
		pageSize = 1000
	}
	all := []json.RawMessage{}
	for offset := 0; ; offset += pageSize {
		page := make(map[string]any, len(query)+2)
		for k, v := range query {
			page[k] = v
		}
		page["q/limit"] = pageSize
		page["q/offset"] = offset

		args := map[string]any{"query": page}
		if params != nil {
			args["params"] = params
		}
		raw, err := c.One(ctx, Command{Command: "fibery.entity/query", Args: args})
		if err != nil {
			return nil, err
		}
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, fmt.Errorf("client.QueryAll: decode page: %w", err)
		}
		all = append(all, rows...)
		if len(rows) < pageSize {
			break
		}
	}
	out, err := json.Marshal(all)
	if err != nil {
		return nil, fmt.Errorf("client.QueryAll: marshal: %w", err)
	}
	return out, nil
}

// ViewRecord is a Fibery "view" (board, list, document, …) as returned by the
// undocumented query-views RPC. For space/wiki documents, DocumentSecret is set
// and Type == "document"; other view types leave DocumentSecret empty.
type ViewRecord struct {
	ID             string
	Name           string
	PublicID       string
	Type           string
	DocumentSecret string
	ContainerAppID string
}

// viewRPCResponse / viewRaw decode the query-views JSON-RPC response.
type viewRPCResponse struct {
	Result []viewRaw `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type viewRaw struct {
	ID       string `json:"fibery/id"`
	Name     string `json:"fibery/name"`
	PublicID string `json:"fibery/public-id"`
	Type     string `json:"fibery/type"`
	Meta     struct {
		DocumentSecret string `json:"documentSecret"`
	} `json:"fibery/meta"`
	ContainerApp struct {
		ID string `json:"fibery/id"`
	} `json:"fibery/container-app"`
}

func (r viewRaw) toRecord() ViewRecord {
	return ViewRecord{
		ID:             r.ID,
		Name:           r.Name,
		PublicID:       r.PublicID,
		Type:           r.Type,
		DocumentSecret: r.Meta.DocumentSecret,
		ContainerAppID: r.ContainerApp.ID,
	}
}

// QueryViews calls the undocumented POST /api/views/json-rpc "query-views" method
// and returns the matching views. A nil filter returns every view in the workspace.
// NOTE: this endpoint is undocumented and may change without notice.
func (c *Client) QueryViews(ctx context.Context, filter map[string]any) ([]ViewRecord, error) {
	params := map[string]any{}
	if filter != nil {
		params["filter"] = filter
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "query-views",
		"params":  params,
	})
	if err != nil {
		return nil, fmt.Errorf("client.QueryViews: marshal: %w", err)
	}
	b, err := c.request(ctx, http.MethodPost, "/api/views/json-rpc", body)
	if err != nil {
		return nil, fmt.Errorf("client.QueryViews: %w", err)
	}
	var resp viewRPCResponse
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, fmt.Errorf("client.QueryViews: decode: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("client.QueryViews: %s", resp.Error.Message)
	}
	out := make([]ViewRecord, len(resp.Result))
	for i, r := range resp.Result {
		out[i] = r.toRecord()
	}
	return out, nil
}

// QueryView resolves a single view by its public ID (the trailing number in a
// space document/view URL). Returns an error if no view has that public ID.
func (c *Client) QueryView(ctx context.Context, publicID string) (*ViewRecord, error) {
	views, err := c.QueryViews(ctx, map[string]any{"publicIds": []string{publicID}})
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return nil, fmt.Errorf("view #%s not found", publicID)
	}
	v := views[0]
	return &v, nil
}

// AddComment adds a Markdown comment to an entity via the 2-step Fibery comments API.
// When parentCommentID is non-empty the new comment is threaded as a reply: it is
// still linked to the host entity's comments collection, but also points at the
// parent comment via comments/parent.
func (c *Client) AddComment(ctx context.Context, db, entityID, content, parentCommentID string) error {
	commentID := newUUID()
	docSecret := newUUID()

	commentEntity := map[string]any{
		"fibery/id":               commentID,
		"comment/document-secret": docSecret,
	}
	if parentCommentID != "" {
		commentEntity["comments/parent"] = map[string]any{"fibery/id": parentCommentID}
	}

	// Step 1: create comment entity + link it to the parent entity's comments collection
	if _, err := c.One(ctx, Command{
		Command: "fibery.command/batch",
		Args: map[string]any{
			"commands": []any{
				map[string]any{
					"command": "fibery.entity/create",
					"args": map[string]any{
						"type":   "comments/comment",
						"entity": commentEntity,
					},
				},
				map[string]any{
					"command": "fibery.entity/add-collection-items",
					"args": map[string]any{
						"type":   db,
						"entity": map[string]any{"fibery/id": entityID},
						"field":  "comments/comments",
						"items":  []any{map[string]any{"fibery/id": commentID}},
					},
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("client.AddComment: create: %w", err)
	}

	// Step 2: write comment content to the document
	body, _ := json.Marshal(map[string]any{
		"command": "create-or-update-documents",
		"args":    []any{map[string]any{"secret": docSecret, "content": content}},
	})
	if _, err := c.request(ctx, http.MethodPost, "/api/documents/commands?format=md", body); err != nil {
		return fmt.Errorf("client.AddComment: set content: %w", err)
	}
	return nil
}

// QueryHistory queries /api/history/v2/search for entity change history.
func (c *Client) QueryHistory(ctx context.Context, filters []HistoryFilter, since, until string, limit int) ([]HistoryItem, error) {
	payload := map[string]any{
		"limit": limit,
		"where": filters,
		"excludeAutomaticChanges": []any{
			map[string]any{"field": "field", "operator": "in", "value": []any{
				map[string]any{"field": "fibery/rank"},
				map[string]any{"field": "fibery/modification-date"},
			}},
		},
	}
	if since != "" || until != "" {
		tf := map[string]any{}
		if since != "" {
			tf["start"] = since
		}
		if until != "" {
			tf["end"] = until
		}
		payload["timeframe"] = tf
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("client.QueryHistory: marshal: %w", err)
	}
	b, err := c.request(ctx, http.MethodPost, "/api/history/v2/search", body)
	if err != nil {
		return nil, fmt.Errorf("client.QueryHistory: %w", err)
	}
	var result struct {
		Items []HistoryItem `json:"items"`
	}
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, fmt.Errorf("client.QueryHistory: decode: %w", err)
	}
	return result.Items, nil
}

// newUUID generates a random UUID v4.
func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
