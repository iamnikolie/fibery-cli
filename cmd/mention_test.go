package cmd

import "testing"

func TestBuildMentionToken(t *testing.T) {
	got := buildMentionToken("1b728600-af9c-11e9-95b5-2985503542d7", "13cb2065-844f-4048-952c-6e890299fa33")
	want := "[[#@1b728600-af9c-11e9-95b5-2985503542d7/13cb2065-844f-4048-952c-6e890299fa33]]"
	if got != want {
		t.Fatalf("buildMentionToken = %q, want %q", got, want)
	}
}

func TestPrependTokens(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		body   string
		want   string
	}{
		{"no tokens", nil, "hello", "hello"},
		{"no tokens empty body", nil, "", ""},
		{"one token with body", []string{"[[#@t/a]]"}, "hi there", "[[#@t/a]] hi there"},
		{"one token empty body", []string{"[[#@t/a]]"}, "", "[[#@t/a]]"},
		{"one token blank body", []string{"[[#@t/a]]"}, "   ", "[[#@t/a]]"},
		{"two tokens", []string{"[[#@t/a]]", "[[#@t/b]]"}, "look", "[[#@t/a]] [[#@t/b]] look"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prependTokens(tt.tokens, tt.body); got != tt.want {
				t.Fatalf("prependTokens(%v, %q) = %q, want %q", tt.tokens, tt.body, got, tt.want)
			}
		})
	}
}

func TestFindTypeID(t *testing.T) {
	schema := map[string]any{
		"fibery/types": []any{
			map[string]any{"fibery/name": "fibery/user", "fibery/id": "1b728600-af9c-11e9-95b5-2985503542d7"},
			map[string]any{"fibery/name": "Development/Dev Task", "fibery/id": "301ef4d4-3ecf-4733-9564-4d8dedf3abf7"},
		},
	}

	if got := findTypeID(schema, "fibery/user"); got != "1b728600-af9c-11e9-95b5-2985503542d7" {
		t.Fatalf("findTypeID(fibery/user) = %q", got)
	}
	if got := findTypeID(schema, "Development/Dev Task"); got != "301ef4d4-3ecf-4733-9564-4d8dedf3abf7" {
		t.Fatalf("findTypeID(Development/Dev Task) = %q", got)
	}
	if got := findTypeID(schema, "Missing/Type"); got != "" {
		t.Fatalf("findTypeID(missing) = %q, want empty", got)
	}
	if got := findTypeID(map[string]any{}, "fibery/user"); got != "" {
		t.Fatalf("findTypeID(empty schema) = %q, want empty", got)
	}
}
