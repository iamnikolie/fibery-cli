package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

// spaceIDByName resolves one of the workspace's own spaces by name.
func spaceIDByName(cmd *cobra.Command, name string) (string, error) {
	apps, err := loadUserSpaces(cmd)
	if err != nil {
		return "", err
	}
	for id, n := range apps {
		if n == name {
			return id, nil
		}
	}
	return "", fmt.Errorf("space %q not found — 'fibery space list' shows them", name)
}

// splitFolderPath turns "A / B/C" into ["A", "B", "C"].
func splitFolderPath(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// walkFolderPath follows segs from the space root through existing folders.
// It returns the deepest existing folder id (parent for what is missing) and
// the index of the first missing segment (len(segs) when all exist).
func walkFolderPath(folders []client.FolderRecord, appID string, segs []string) (parentID string, missingFrom int) {
	for i, seg := range segs {
		found := ""
		for _, f := range folders {
			if f.ContainerAppID == appID && f.ParentID == parentID && f.Name == seg {
				found = f.ID
				break
			}
		}
		if found == "" {
			return parentID, i
		}
		parentID = found
	}
	return parentID, len(segs)
}

// ensureFolderPath returns the id of folder path in the space, creating the
// missing tail. created lists the folder names made.
func ensureFolderPath(cmd *cobra.Command, appID, path string) (id string, created []string, err error) {
	segs := splitFolderPath(path)
	if len(segs) == 0 {
		return "", nil, nil
	}
	folders, err := cli.QueryFolders(cmd.Context())
	if err != nil {
		return "", nil, err
	}
	parent, from := walkFolderPath(folders, appID, segs)
	for _, seg := range segs[from:] {
		fid := client.NewUUID()
		val := map[string]any{
			"fibery/id":            fid,
			"fibery/name":          seg,
			"fibery/container-app": map[string]any{"fibery/id": appID},
		}
		if parent != "" {
			val["fibery/Parent Folder"] = map[string]any{"fibery/id": parent}
		}
		if err := cli.CreateFolders(cmd.Context(), []map[string]any{val}); err != nil {
			return "", created, err
		}
		created = append(created, seg)
		parent = fid
	}
	return parent, created, nil
}

func ref(id string) any {
	if id == "" {
		return nil
	}
	return map[string]any{"fibery/id": id}
}

var folderCreateCmd = &cobra.Command{
	Use:   "create <space> <path>",
	Short: "Create a folder path (missing parents included)",
	Long: `Create "A/B/C" in a space's left navigation; existing folders on the path are
reused, so running it twice changes nothing.

Example:
  fibery folder create Development "Archive/LINC/Concept"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		appID, err := spaceIDByName(cmd, args[0])
		if err != nil {
			return err
		}
		id, created, err := ensureFolderPath(cmd, appID, args[1])
		if err != nil {
			return err
		}
		if len(created) == 0 {
			fmt.Printf("Folder %s/%s already exists (%s).\n", args[0], args[1], id)
			return nil
		}
		fmt.Printf("Created %s in %s (%s).\n", strings.Join(created, " → "), args[0], id)
		return nil
	},
}

var (
	viewMoveSpace  string
	viewMoveFolder string
)

var viewMoveCmd = &cobra.Command{
	Use:   "move <view>...",
	Short: "Move views or wiki documents to another space and folder",
	Long: `Move views (wiki documents included) to --space, into --folder (a path,
created when missing; omit for the space root). A document nested under another
page is taken out of it and placed directly in the folder.

Example:
  fibery view move 102 103 --space Development --folder "Archive/LINC/Web3"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if viewMoveSpace == "" {
			return fmt.Errorf("--space is required")
		}
		appID, err := spaceIDByName(cmd, viewMoveSpace)
		if err != nil {
			return err
		}
		folderID, _, err := ensureFolderPath(cmd, appID, viewMoveFolder)
		if err != nil {
			return err
		}
		for _, a := range args {
			v, err := findView(cmd.Context(), a)
			if err != nil {
				return err
			}
			if err := cli.UpdateViews(cmd.Context(), []map[string]any{{"id": v.ID, "values": map[string]any{
				"fibery/container-app":  ref(appID),
				"fibery/Folder":         ref(folderID),
				"fibery/parent-page-id": nil,
			}}}); err != nil {
				return fmt.Errorf("move #%s: %w", v.PublicID, err)
			}
			got, err := findView(cmd.Context(), v.ID)
			if err != nil {
				return err
			}
			if got.ContainerAppID != appID || got.FolderID != folderID || got.ParentPageID != "" {
				return fmt.Errorf("#%s %q did not land in %s/%s (space %s, folder %s)", v.PublicID, v.Name, viewMoveSpace, viewMoveFolder, got.ContainerAppID, got.FolderID)
			}
			fmt.Printf("Moved %s #%s %q → %s/%s\n", v.Type, v.PublicID, v.Name, viewMoveSpace, viewMoveFolder)
		}
		return nil
	},
}

var (
	wikiCreateSpace, wikiCreateFolder, wikiCreateName string
	wikiCreateFile, wikiCreateFrom, wikiCreateFromDB  string
	wikiCreateFromField                               string
)

var wikiCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a wiki document from a Markdown file or an entity's rich text",
	Long: `Create a space/wiki document in --space (and --folder, created when missing).
The body comes from --file (Markdown) or is copied from an entity's rich-text
field (--from <id> --from-db <db>, --from-field defaults to its first rich-text
field) as Fibery JSON, so formatting and inline comments are kept.

Examples:
  fibery wiki create --space Development --folder Archive --name "Notes" --file notes.md
  fibery wiki create --space Development --folder "Archive/Templates" --name "architecture/README.md" \
    --from 11 --from-db "Space/Document"`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()
		if wikiCreateSpace == "" || wikiCreateName == "" {
			return fmt.Errorf("--space and --name are required")
		}
		if (wikiCreateFile == "") == (wikiCreateFrom == "") {
			return fmt.Errorf("give exactly one of --file or --from")
		}
		appID, err := spaceIDByName(cmd, wikiCreateSpace)
		if err != nil {
			return err
		}
		secret := client.NewUUID()
		if wikiCreateFile != "" {
			md, err := os.ReadFile(wikiCreateFile)
			if err != nil {
				return err
			}
			if err := cli.CreateDocumentMarkdown(ctx, secret, string(md)); err != nil {
				return err
			}
		} else {
			if wikiCreateFromDB == "" {
				return fmt.Errorf("--from needs --from-db")
			}
			uuid, err := resolveEntityRef(ctx, wikiCreateFromDB, wikiCreateFrom)
			if err != nil {
				return err
			}
			field := wikiCreateFromField
			if field == "" {
				schema, _ := loadSchemaOrNil()
				f, ok := findDescriptionField(schema, wikiCreateFromDB)
				if !ok {
					return fmt.Errorf("%s has no rich-text field — pass --from-field", wikiCreateFromDB)
				}
				field = f
			}
			src, err := resolveDocSecretByID(ctx, wikiCreateFromDB, uuid, field)
			if err != nil {
				return err
			}
			doc, err := cli.GetDocumentJSON(ctx, src)
			if err != nil {
				return err
			}
			if err := cli.CreateDocumentMarkdown(ctx, secret, ""); err != nil {
				return err
			}
			if err := cli.SetDocumentJSON(ctx, secret, doc.Content); err != nil {
				return err
			}
		}
		folderID, _, err := ensureFolderPath(cmd, appID, wikiCreateFolder)
		if err != nil {
			return err
		}
		id := client.NewUUID()
		if err := cli.CreateViews(ctx, []map[string]any{{
			"fibery/id":             id,
			"fibery/name":           wikiCreateName,
			"fibery/type":           "document",
			"fibery/meta":           map[string]any{"documentSecret": secret},
			"fibery/container-type": "app",
			"fibery/container-app":  ref(appID),
			"fibery/Folder":         ref(folderID),
		}}); err != nil {
			return err
		}
		got, err := findView(ctx, id)
		if err != nil {
			return fmt.Errorf("document created but cannot be read back: %w", err)
		}
		if got.FolderID != folderID {
			if err := cli.UpdateViews(ctx, []map[string]any{{"id": id, "values": map[string]any{"fibery/Folder": ref(folderID)}}}); err != nil {
				return fmt.Errorf("document #%s created at the space root, folder not set: %w", got.PublicID, err)
			}
		}
		fmt.Printf("Created document #%s %q in %s/%s\n", got.PublicID, got.Name, wikiCreateSpace, wikiCreateFolder)
		return nil
	},
}

func init() {
	folderCmd.AddCommand(folderCreateCmd)

	viewMoveCmd.Flags().StringVar(&viewMoveSpace, "space", "", "target space (required)")
	viewMoveCmd.Flags().StringVar(&viewMoveFolder, "folder", "", "target folder path, e.g. \"Archive/LINC\" (created when missing)")
	viewCmd.AddCommand(viewMoveCmd)

	f := wikiCreateCmd.Flags()
	f.StringVar(&wikiCreateSpace, "space", "", "space (required)")
	f.StringVar(&wikiCreateFolder, "folder", "", "folder path (created when missing)")
	f.StringVar(&wikiCreateName, "name", "", "document title (required)")
	f.StringVar(&wikiCreateFile, "file", "", "Markdown file for the body")
	f.StringVar(&wikiCreateFrom, "from", "", "copy the body from this entity (id, public id, prefixed id)")
	f.StringVar(&wikiCreateFromDB, "from-db", "", "database of --from")
	f.StringVar(&wikiCreateFromField, "from-field", "", "rich-text field of --from (default: its first)")
	wikiCmd.AddCommand(wikiCreateCmd)
}

func loadSchemaOrNil() (map[string]any, error) {
	return cache.LoadSchema(account)
}
