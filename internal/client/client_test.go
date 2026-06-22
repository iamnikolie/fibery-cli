package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/langgerone/fibery-cli/internal/client"
)

func TestClient_One_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Token mytoken", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/commands", r.URL.Path)

		var cmds []map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&cmds))
		assert.Equal(t, "fibery.identity/me", cmds[0]["command"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"success": true, "result": map[string]any{"user/name": "Alice"}},
		})
	}))
	defer srv.Close()

	c := client.New("mytoken", srv.URL)
	result, err := c.One(context.Background(), client.Command{
		Command: "fibery.identity/me",
		Args:    map[string]any{},
	})
	require.NoError(t, err)

	var me map[string]any
	require.NoError(t, json.Unmarshal(result, &me))
	assert.Equal(t, "Alice", me["user/name"])
}

func TestClient_One_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]map[string]any{
			{"success": false, "error": map[string]any{"message": "not found"}},
		})
	}))
	defer srv.Close()

	c := client.New("tok", srv.URL)
	_, err := c.One(context.Background(), client.Command{Command: "fibery.entity/query", Args: map[string]any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestClient_One_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := client.New("bad", srv.URL)
	_, err := c.One(context.Background(), client.Command{Command: "fibery.identity/me", Args: map[string]any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestClientVerboseDefault(t *testing.T) {
	c := client.New("mytoken", "https://example.fibery.io")
	assert.False(t, c.Verbose, "Verbose should be false by default")
}

func TestClient_QueryView_ResolvesDocumentSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/views/json-rpc", r.URL.Path)
		assert.Equal(t, "Token tok", r.Header.Get("Authorization"))

		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "2.0", body["jsonrpc"])
		assert.Equal(t, "query-views", body["method"])
		params := body["params"].(map[string]any)
		filter := params["filter"].(map[string]any)
		ids := filter["publicIds"].([]any)
		assert.Equal(t, "280", ids[0])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"result": []map[string]any{
				{
					"fibery/id":            "view-uuid",
					"fibery/name":          "Dev policy summary",
					"fibery/public-id":     "280",
					"fibery/type":          "document",
					"fibery/meta":          map[string]any{"documentSecret": "doc-secret-123"},
					"fibery/container-app": map[string]any{"fibery/id": "app-uuid"},
				},
			},
		})
	}))
	defer srv.Close()

	c := client.New("tok", srv.URL)
	v, err := c.QueryView(context.Background(), "280")
	require.NoError(t, err)
	assert.Equal(t, "Dev policy summary", v.Name)
	assert.Equal(t, "280", v.PublicID)
	assert.Equal(t, "document", v.Type)
	assert.Equal(t, "doc-secret-123", v.DocumentSecret)
	assert.Equal(t, "app-uuid", v.ContainerAppID)
}

func TestClient_QueryAll_PagesUntilShortPage(t *testing.T) {
	var offsets []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/commands", r.URL.Path)
		var cmds []map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&cmds))
		q := cmds[0]["args"].(map[string]any)["query"].(map[string]any)
		assert.EqualValues(t, 2, q["q/limit"])
		offset := int(q["q/offset"].(float64))
		offsets = append(offsets, offset)

		w.Header().Set("Content-Type", "application/json")
		// page 0 → 2 rows (full), page 2 → 1 row (short → stop).
		var rows []map[string]any
		if offset == 0 {
			rows = []map[string]any{{"id": "a"}, {"id": "b"}}
		} else {
			rows = []map[string]any{{"id": "c"}}
		}
		json.NewEncoder(w).Encode([]map[string]any{{"success": true, "result": rows}})
	}))
	defer srv.Close()

	c := client.New("tok", srv.URL)
	out, err := c.QueryAll(context.Background(), map[string]any{
		"q/from":   "Space/DB",
		"q/select": map[string]any{"id": []any{"fibery/id"}},
	}, nil, 2)
	require.NoError(t, err)

	var all []map[string]any
	require.NoError(t, json.Unmarshal(out, &all))
	assert.Len(t, all, 3)
	assert.Equal(t, []int{0, 2}, offsets)
}

func TestClient_QueryView_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "result": []any{}})
	}))
	defer srv.Close()

	c := client.New("tok", srv.URL)
	_, err := c.QueryView(context.Background(), "999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestClient_QueryViews_ReturnsAll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		// No filter passed → params has no filter key.
		params := body["params"].(map[string]any)
		_, hasFilter := params["filter"]
		assert.False(t, hasFilter)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"result": []map[string]any{
				{"fibery/name": "Doc A", "fibery/public-id": "1", "fibery/type": "document", "fibery/meta": map[string]any{"documentSecret": "s1"}},
				{"fibery/name": "Board B", "fibery/public-id": "2", "fibery/type": "board"},
			},
		})
	}))
	defer srv.Close()

	c := client.New("tok", srv.URL)
	vs, err := c.QueryViews(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, vs, 2)
	assert.Equal(t, "document", vs[0].Type)
	assert.Equal(t, "s1", vs[0].DocumentSecret)
	assert.Equal(t, "board", vs[1].Type)
	assert.Empty(t, vs[1].DocumentSecret)
}
