package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

// folderCounts returns, per folder id, how many views sit in it and how many
// folders are nested directly under it.
func folderCounts(folders []client.FolderRecord, views []client.ViewRecord) (inViews, kids map[string]int) {
	inViews, kids = map[string]int{}, map[string]int{}
	for _, v := range views {
		if v.FolderID != "" {
			inViews[v.FolderID]++
		}
	}
	for _, f := range folders {
		if f.ParentID != "" {
			kids[f.ParentID]++
		}
	}
	return inViews, kids
}

// folderDeleteOrder checks that every folder in del is empty once the others in
// del are gone (no views; subfolders only from del) and returns them children
// first. A folder outside del that sits under one in del makes it non-empty.
func folderDeleteOrder(del []client.FolderRecord, folders []client.FolderRecord, views []client.ViewRecord) ([]client.FolderRecord, error) {
	inDel := map[string]bool{}
	for _, f := range del {
		inDel[f.ID] = true
	}
	inViews, _ := folderCounts(folders, views)
	for _, f := range del {
		if inViews[f.ID] > 0 {
			return nil, fmt.Errorf("folder %q (#%s) holds %d view(s) — move or delete them first", f.Name, f.PublicID, inViews[f.ID])
		}
		for _, o := range folders {
			if o.ParentID == f.ID && !inDel[o.ID] {
				return nil, fmt.Errorf("folder %q (#%s) holds folder %q (#%s) — name it too", f.Name, f.PublicID, o.Name, o.PublicID)
			}
		}
	}
	depth := func(f client.FolderRecord) int {
		byID := map[string]client.FolderRecord{}
		for _, o := range folders {
			byID[o.ID] = o
		}
		d := 0
		for p := f.ParentID; p != "" && d < 50; p = byID[p].ParentID {
			d++
		}
		return d
	}
	out := append([]client.FolderRecord{}, del...)
	sort.SliceStable(out, func(i, j int) bool { return depth(out[i]) > depth(out[j]) })
	return out, nil
}

func findFolders(folders []client.FolderRecord, refs []string) ([]client.FolderRecord, error) {
	var out []client.FolderRecord
	for _, r := range refs {
		found := false
		for _, f := range folders {
			if f.ID == r || f.PublicID == r {
				out = append(out, f)
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("folder %q not found (use the ID from 'fibery folder list')", r)
		}
	}
	return out, nil
}

var (
	folderListSpace string
	folderListEmpty bool
	folderDeleteYes bool
)

var folderCmd = &cobra.Command{
	Use:   "folder",
	Short: "List and delete left-nav folders",
	Long: `Folders group views and documents in a space's left navigation. They live behind
Fibery's undocumented views JSON-RPC (query-folders / delete-folders).`,
}

var folderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List folders with their space, parent and contents",
	Long: `Examples:
  fibery folder list
  fibery folder list --space Development --empty`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		apps, err := loadAppNames(ctx)
		if err != nil {
			return err
		}
		folders, err := cli.QueryFolders(ctx)
		if err != nil {
			return err
		}
		views, err := cli.QueryViews(ctx, nil)
		if err != nil {
			return err
		}
		inViews, kids := folderCounts(folders, views)
		names := map[string]string{}
		for _, f := range folders {
			names[f.ID] = f.Name
		}
		rows := []map[string]any{}
		for _, f := range folders {
			sp := apps[f.ContainerAppID]
			if folderListSpace != "" && !strings.EqualFold(sp, folderListSpace) {
				continue
			}
			if folderListEmpty && (inViews[f.ID] > 0 || kids[f.ID] > 0) {
				continue
			}
			rows = append(rows, map[string]any{"name": f.Name, "publicId": f.PublicID, "space": sp,
				"parent": names[f.ParentID], "views": inViews[f.ID], "folders": kids[f.ID], "id": f.ID})
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i]["space"].(string) < rows[j]["space"].(string) })
		if outputFormat == "json" || jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(rows)
		}
		fmt.Printf("| Folder | ID | Space | Parent | Views | Folders |\n|---|---|---|---|---|---|\n")
		for _, r := range rows {
			fmt.Printf("| %s | %s | %s | %s | %d | %d |\n", r["name"], r["publicId"], r["space"], r["parent"], r["views"], r["folders"])
		}
		return nil
	},
}

var folderDeleteCmd = &cobra.Command{
	Use:   "delete <id>...",
	Short: "Delete empty folders",
	Long: `Delete folders by public id or UUID. Only empty folders: no views, and nested
folders only when they are named in the same call (deleted children first).
Prints the folders first; nothing changes without --yes.

Example:
  fibery folder delete 9 4 5 7 --yes     # a folder and its three empty subfolders`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		folders, err := cli.QueryFolders(ctx)
		if err != nil {
			return err
		}
		views, err := cli.QueryViews(ctx, nil)
		if err != nil {
			return err
		}
		del, err := findFolders(folders, args)
		if err != nil {
			return err
		}
		ordered, err := folderDeleteOrder(del, folders, views)
		if err != nil {
			return err
		}
		for _, f := range ordered {
			fmt.Printf("- folder %q (#%s)\n", f.Name, f.PublicID)
		}
		if !folderDeleteYes {
			return fmt.Errorf("re-run with --yes to delete the %d folder(s) above", len(ordered))
		}
		for _, f := range ordered {
			if err := cli.DeleteFolders(ctx, []string{f.ID}); err != nil {
				return err
			}
		}
		left, err := cli.QueryFolders(ctx)
		if err == nil {
			for _, f := range ordered {
				for _, l := range left {
					if l.ID == f.ID {
						return fmt.Errorf("folder %q (#%s) is still there after delete-folders", f.Name, f.PublicID)
					}
				}
			}
		}
		fmt.Printf("Deleted %d folder(s).\n", len(ordered))
		return nil
	},
}

// spaceSummary counts what a space holds.
type spaceSummary struct {
	Name      string   `json:"name"`
	ID        string   `json:"id"`
	Databases []string `json:"databases"`
	Views     int      `json:"views"`
	Documents int      `json:"documents"`
	Folders   int      `json:"folders"`
}

func summarizeSpaces(apps map[string]string, dbs []string, views []client.ViewRecord, folders []client.FolderRecord) []spaceSummary {
	byName := map[string]*spaceSummary{}
	for id, n := range apps {
		byName[n] = &spaceSummary{Name: n, ID: id, Databases: []string{}}
	}
	for _, d := range dbs {
		if s := byName[spaceOf(d)]; s != nil {
			s.Databases = append(s.Databases, d)
		}
	}
	for _, v := range views {
		s := byName[apps[v.ContainerAppID]]
		if s == nil || v.ContainerType != "app" {
			continue
		}
		if v.Type == "document" {
			s.Documents++
		} else {
			s.Views++
		}
	}
	for _, f := range folders {
		if s := byName[apps[f.ContainerAppID]]; s != nil {
			s.Folders++
		}
	}
	out := make([]spaceSummary, 0, len(byName))
	for _, s := range byName {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// loadUserSpaces returns id → name of the workspace's own spaces, leaving out
// the ones Fibery preinstalls (Workflow, Comments, User, …).
func loadUserSpaces(cmd *cobra.Command) (map[string]string, error) {
	res, err := cli.One(cmd.Context(), client.Command{Command: "fibery.entity/query", Args: map[string]any{"query": map[string]any{
		"q/from":   "fibery/app",
		"q/select": map[string]any{"id": []any{"fibery/id"}, "name": []any{"fibery/name"}, "pre": []any{"fibery/preinstalled?"}},
		"q/limit":  "q/no-limit",
	}}})
	if err != nil {
		return nil, fmt.Errorf("load spaces: %w", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(res, &rows); err != nil {
		return nil, fmt.Errorf("load spaces: %w", err)
	}
	out := map[string]string{}
	for _, r := range rows {
		if r["pre"] != true {
			out[asStr(r["id"])] = asStr(r["name"])
		}
	}
	return out, nil
}

func loadSpaces(cmd *cobra.Command) ([]spaceSummary, error) {
	ctx := cmd.Context()
	apps, err := loadUserSpaces(cmd)
	if err != nil {
		return nil, err
	}
	schema, err := cache.LoadSchema(account)
	if err != nil {
		return nil, fmt.Errorf("no schema — run: fibery schema sync")
	}
	views, err := cli.QueryViews(ctx, nil)
	if err != nil {
		return nil, err
	}
	folders, err := cli.QueryFolders(ctx)
	if err != nil {
		return nil, err
	}
	return summarizeSpaces(apps, listDatabases(schema), views, folders), nil
}

var spaceCmd = &cobra.Command{
	Use:   "space",
	Short: "List and delete spaces",
}

var spaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List spaces with their databases, views, documents and folders",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		spaces, err := loadSpaces(cmd)
		if err != nil {
			return err
		}
		if outputFormat == "json" || jsonOutput {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(spaces)
		}
		fmt.Printf("| Space | Databases | Views | Documents | Folders |\n|---|---|---|---|---|\n")
		for _, s := range spaces {
			fmt.Printf("| %s | %d | %d | %d | %d |\n", s.Name, len(s.Databases), s.Views, s.Documents, s.Folders)
		}
		return nil
	},
}

var (
	spaceDeleteYes    bool
	spaceDeleteDryRun bool
)

var spaceDeleteCmd = &cobra.Command{
	Use:   "delete <space>",
	Short: "Delete a space that has no databases",
	Long: `Delete a space (fibery.app/uninstall, then fibery.app/delete). Refused while the space still has
databases — delete or archive those first ('fibery export', 'fibery schema db
delete'). The plan lists the views, wiki documents and folders that go with it;
they are deleted after the space (Fibery itself would leave them orphaned).
Nothing changes without --yes. Archive wiki documents first with
'fibery export --space <space>'.

Example:
  fibery space delete "Old Space" --dry-run
  fibery space delete "Old Space" --yes`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		spaces, err := loadSpaces(cmd)
		if err != nil {
			return err
		}
		var sp *spaceSummary
		for i := range spaces {
			if spaces[i].Name == args[0] {
				sp = &spaces[i]
			}
		}
		if sp == nil {
			return fmt.Errorf("space %q not found (built-in spaces are not listed and cannot be deleted) — 'fibery space list' shows them", args[0])
		}
		fmt.Printf("## Delete space %s\n\n- Databases: %d\n- Views: %d\n- Wiki documents: %d\n- Folders: %d\n\n",
			sp.Name, len(sp.Databases), sp.Views, sp.Documents, sp.Folders)
		if len(sp.Databases) > 0 {
			return fmt.Errorf("space %q still has databases (%s) — delete or archive them first", sp.Name, strings.Join(sp.Databases, ", "))
		}
		if spaceDeleteDryRun {
			fmt.Println("Dry run — nothing changed.")
			return nil
		}
		if !spaceDeleteYes {
			return fmt.Errorf("deleting a space is destructive — re-run with --yes to apply the plan above")
		}
		// An installed space must be uninstalled before fibery.app/delete accepts
		// it (delete-installed-app-failed); uninstall alone leaves it listed.
		if _, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.app/uninstall",
			Args:    map[string]any{"fibery/id": sp.ID},
		}); err != nil && !strings.Contains(err.Error(), "app-installed-not-found") {
			return fmt.Errorf("uninstall space %s: %w", sp.Name, err)
		}
		if _, err := cli.One(cmd.Context(), client.Command{
			Command: "fibery.app/delete",
			Args:    map[string]any{"fibery/id": sp.ID},
		}); err != nil {
			return fmt.Errorf("delete space %s: %w", sp.Name, err)
		}
		apps, err := loadUserSpaces(cmd)
		if err == nil {
			if _, still := apps[sp.ID]; still {
				return fmt.Errorf("space %q is still listed after fibery.app/delete", sp.Name)
			}
		}
		// fibery.app/delete leaves the space's views, documents and folders behind,
		// orphaned under a space that no longer exists.
		nv, nf, err := deleteSpaceLeftovers(cmd, sp.ID)
		if err != nil {
			return fmt.Errorf("space %s deleted, but its views/folders were not: %w", sp.Name, err)
		}
		fmt.Printf("Deleted space %s (%d views and documents, %d folders).\n", sp.Name, nv, nf)
		resync(cmd)
		return nil
	},
}

// spaceLeftovers returns the left-nav views (documents included) and folders of
// a space, folders ordered children first.
func spaceLeftovers(spaceID string, views []client.ViewRecord, folders []client.FolderRecord) ([]string, []client.FolderRecord, error) {
	var ids []string
	for _, v := range views {
		if v.ContainerType == "app" && v.ContainerAppID == spaceID {
			ids = append(ids, v.ID)
		}
	}
	var del []client.FolderRecord
	for _, f := range folders {
		if f.ContainerAppID == spaceID {
			del = append(del, f)
		}
	}
	gone := map[string]bool{}
	for _, id := range ids {
		gone[id] = true
	}
	var rest []client.ViewRecord
	for _, v := range views {
		if !gone[v.ID] {
			rest = append(rest, v)
		}
	}
	order, err := folderDeleteOrder(del, folders, rest)
	return ids, order, err
}

func deleteSpaceLeftovers(cmd *cobra.Command, spaceID string) (int, int, error) {
	ctx := cmd.Context()
	views, err := cli.QueryViews(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	folders, err := cli.QueryFolders(ctx)
	if err != nil {
		return 0, 0, err
	}
	ids, order, err := spaceLeftovers(spaceID, views, folders)
	if err != nil {
		return 0, 0, err
	}
	if len(ids) > 0 {
		if err := cli.DeleteViews(ctx, ids); err != nil {
			return 0, 0, err
		}
	}
	if len(order) > 0 {
		fids := make([]string, len(order))
		for i, f := range order {
			fids[i] = f.ID
		}
		if err := cli.DeleteFolders(ctx, fids); err != nil {
			return len(ids), 0, err
		}
	}
	return len(ids), len(order), nil
}

func init() {
	folderListCmd.Flags().StringVar(&folderListSpace, "space", "", "only folders in this space")
	folderListCmd.Flags().BoolVar(&folderListEmpty, "empty", false, "only folders with no views and no subfolders")
	folderDeleteCmd.Flags().BoolVar(&folderDeleteYes, "yes", false, "apply (required)")
	folderCmd.AddCommand(folderListCmd, folderDeleteCmd)

	spaceDeleteCmd.Flags().BoolVar(&spaceDeleteYes, "yes", false, "apply the plan (required)")
	spaceDeleteCmd.Flags().BoolVar(&spaceDeleteDryRun, "dry-run", false, "print the plan and stop")
	spaceCmd.AddCommand(spaceListCmd, spaceDeleteCmd)

	rootCmd.AddCommand(folderCmd, spaceCmd)
}
