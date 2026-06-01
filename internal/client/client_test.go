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
