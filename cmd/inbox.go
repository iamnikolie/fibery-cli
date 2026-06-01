package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/langgerone/fibery-cli/internal/cache"
	"github.com/langgerone/fibery-cli/internal/client"
)

var (
	inboxHours    int
	inboxAbsolute bool
)

// formatWhen returns the timestamp formatted for inbox display. Default is a
// relative "5h ago" — good for humans but non-deterministic across snapshots.
// --absolute prints the original RFC3339 datetime so two runs are comparable.
func formatWhen(dateStr string, absolute bool) string {
	if absolute {
		return dateStr
	}
	return formatAge(dateStr)
}

var inboxCmd = &cobra.Command{
	Use:   "inbox <database> [database...]",
	Short: "Show recent activity on my entities in the given databases",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInbox(cmd.Context(), args, inboxHours)
	},
}

func runInbox(ctx context.Context, databases []string, hours int) error {
	// 1. Get current user
	meResult, err := cli.One(ctx, client.Command{
		Command: "fibery.entity/query",
		Args: map[string]any{
			"query": map[string]any{
				"q/from":   "fibery/user",
				"q/select": map[string]any{"Email": "user/email", "UUID": "fibery/id"},
				"q/where":  []any{"=", []any{"fibery/id"}, "$my-id"},
				"q/limit":  1,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("inbox: get current user: %w", err)
	}
	var users []map[string]any
	if err := json.Unmarshal(meResult, &users); err != nil || len(users) == 0 {
		return fmt.Errorf("inbox: could not get current user")
	}
	myEmail := asStr(users[0]["Email"])
	myUUID := asStr(users[0]["UUID"])

	// 2. Query all specified databases for entities assigned to me
	schema, _ := cache.LoadSchema(account)
	var myPIDs []any
	myEntityIDs := map[string]bool{}
	pidToName := map[string]string{}
	pidToType := map[string]string{}

	for _, db := range databases {
		nameField := findTitleField(schema, db)
		sel := map[string]any{
			"UUID":  []any{"fibery/id"},
			"PID":   []any{"fibery/public-id"},
			"State": []any{"workflow/state", "enum/name"},
			"Final": []any{"workflow/state", "workflow/Final"},
			"Assignees": map[string]any{
				"q/from":   "assignments/assignees",
				"q/select": map[string]any{"Email": "user/email"},
				"q/limit":  10,
			},
		}
		if nameField != "" {
			sel["Name"] = []any{nameField}
		}

		result, err := cli.One(ctx, client.Command{
			Command: "fibery.entity/query",
			Args: map[string]any{
				"query": map[string]any{
					"q/from":     db,
					"q/select":   sel,
					"q/order-by": []any{[]any{[]any{"fibery/creation-date"}, "q/desc"}},
					"q/limit":    "q/no-limit",
				},
			},
		})
		if err != nil {
			continue // skip databases that fail (e.g. no access)
		}
		var rows []map[string]any
		if err := json.Unmarshal(result, &rows); err != nil {
			continue
		}
		for _, row := range rows {
			if row["Final"] == true {
				continue
			}
			assignees, _ := row["Assignees"].([]any)
			for _, a := range assignees {
				am, _ := a.(map[string]any)
				if strings.EqualFold(asStr(am["Email"]), myEmail) {
					pid := asStr(row["PID"])
					myPIDs = append(myPIDs, pid)
					myEntityIDs[asStr(row["UUID"])] = true
					pidToName[pid] = asStr(row["Name"])
					// Store short db name: "Product Management/User-Story" → "User-Story"
					if idx := strings.Index(db, "/"); idx >= 0 {
						pidToType[pid] = db[idx+1:]
					}
					break
				}
			}
		}
	}

	if len(myPIDs) == 0 {
		fmt.Println("No active items assigned to you.")
		return nil
	}

	// 3. Query history for those entities
	now := time.Now().UTC()
	since := now.Add(-time.Duration(hours) * time.Hour).Format(time.RFC3339)
	until := now.Format(time.RFC3339)

	items, err := cli.QueryHistory(ctx, []client.HistoryFilter{
		{Field: "entityPublicId", Operator: "in", Value: myPIDs},
		// Skip noisy auto-updated system fields
		{Field: "field", Operator: "not-in", Value: []any{
			map[string]any{"field": "fibery/rank"},
			map[string]any{"field": "fibery/modification-date"},
		}},
	}, since, until, 200)
	if err != nil {
		return fmt.Errorf("inbox: query history: %w", err)
	}

	// Filter out history items for entities we don't own — same publicId can exist
	// in multiple databases (e.g. two types both have entity #42).
	filtered := items[:0]
	for _, item := range items {
		if myEntityIDs[item.Entity.ID] {
			filtered = append(filtered, item)
		}
	}
	items = filtered

	if len(items) == 0 {
		fmt.Printf("No activity in the last %dh.\n", hours)
		return nil
	}

	// 4. Render: group by entity, then by author
	type authorGroup struct {
		who     string
		byMe    bool
		when    string
		actions []string
	}
	type entityEntry struct {
		pid    string
		groups []*authorGroup
		byKey  map[string]*authorGroup
	}

	entityMap := map[string]*entityEntry{}
	entityOrder := []string{}

	for _, item := range items {
		pid := item.Entity.PublicID
		if pid == "" {
			continue
		}
		detail := actionDetail(item)
		if detail == "" {
			continue
		}
		// Skip system events (no author = auto-computed formula updates)
		if item.Author.Name == "" && item.Author.ID == "" {
			continue
		}
		byMe := item.Author.ID == myUUID
		who := item.Author.Name
		if byMe {
			who = "you"
		}

		e, exists := entityMap[pid]
		if !exists {
			e = &entityEntry{pid: pid, byKey: map[string]*authorGroup{}}
			entityMap[pid] = e
			entityOrder = append(entityOrder, pid)
		}
		ag, ok := e.byKey[who]
		if !ok {
			ag = &authorGroup{who: who, byMe: byMe, when: formatWhen(item.Date, inboxAbsolute)}
			e.groups = append(e.groups, ag)
			e.byKey[who] = ag
		}
		if len(ag.actions) == 0 || ag.actions[len(ag.actions)-1] != detail {
			ag.actions = append(ag.actions, detail)
		}
	}

	totalEvents := 0
	for _, e := range entityMap {
		for _, g := range e.groups {
			totalEvents += len(g.actions)
		}
	}

	fmt.Fprintf(os.Stdout, "## inbox — last %dh (%d events, %d items)\n\n",
		hours, totalEvents, len(entityOrder))

	for _, pid := range entityOrder {
		e := entityMap[pid]
		name := strings.TrimSpace(pidToName[pid])
		if name == "" {
			name = "?"
		}
		dbLabel := pidToType[pid]
		if dbLabel != "" {
			fmt.Fprintf(os.Stdout, "**#%s** [%s] %s\n", pid, dbLabel, name)
		} else {
			fmt.Fprintf(os.Stdout, "**#%s** %s\n", pid, name)
		}
		for _, ag := range e.groups {
			marker := "●"
			if ag.byMe {
				marker = "·"
			}
			var summary string
			if len(ag.actions) > 4 {
				summary = strings.Join(ag.actions[:3], ", ") + fmt.Sprintf(" +%d more", len(ag.actions)-3)
			} else {
				summary = strings.Join(ag.actions, ", ")
			}
			fmt.Fprintf(os.Stdout, "  %s %s — %s  _%s_\n", marker, ag.who, summary, ag.when)
		}
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

// actionDetail returns a short human-readable summary of a history item.
func actionDetail(item client.HistoryItem) string {
	switch item.Action {
	case "fibery.entity/add-collection-items", "fibery.entity/remove-collection-items":
		for _, cv := range item.ChangedValues {
			if strings.Contains(cv.Field.Title, "omment") {
				return "added comment"
			}
			if strings.Contains(cv.Field.Title, "ssignee") {
				if item.Action == "fibery.entity/remove-collection-items" {
					return "removed assignee"
				}
				return "added assignee"
			}
		}
		return ""
	case "fibery.entity/create":
		return "created"
	case "fibery.entity/delete":
		return "deleted"
	case "fibery.entity/update":
		var parts []string
		for _, cv := range item.ChangedValues {
			title := cv.Field.Title
			if title == "" {
				continue
			}
			prev := valueStr(cv.PreviousValue)
			curr := valueStr(cv.CurrentValue)
			if strings.HasPrefix(curr, "map[") {
				continue
			}
			if prev != "" && curr != "" {
				parts = append(parts, fmt.Sprintf("%s: %s → %s", title, prev, curr))
			} else if curr != "" {
				parts = append(parts, fmt.Sprintf("set %s: %s", title, curr))
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
		return ""
	}
	return ""
}

func valueStr(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		if name, ok := val["name"].(string); ok {
			return name
		}
	case []any:
		var names []string
		for _, item := range val {
			if m, ok := item.(map[string]any); ok {
				if n, ok := m["name"].(string); ok {
					names = append(names, n)
				}
			}
		}
		return strings.Join(names, ", ")
	case bool:
		if val {
			return "true"
		}
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func formatAge(dateStr string) string {
	t, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05.000Z", dateStr)
		if err != nil {
			return dateStr
		}
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func init() {
	inboxCmd.Flags().IntVar(&inboxHours, "hours", 48, "show activity from last N hours")
	inboxCmd.Flags().BoolVar(&inboxAbsolute, "absolute", false, "show absolute timestamps instead of relative ages (diffable across runs)")
	rootCmd.AddCommand(inboxCmd)
}
