package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/iamnikolie/fibery-cli/internal/cache"
	"github.com/iamnikolie/fibery-cli/internal/client"
	"github.com/spf13/cobra"
)

var (
	wikiCopyToConfig, wikiCopySpace, wikiCopyToSpace, wikiCopyOut string
	wikiCopyFolders, wikiCopyIDMaps                               []string
	wikiCopyYes, wikiCopyDryRun                                   bool
)

// folderPaths maps each folder id to its path from the space root ("A/B/C").
func folderPaths(folders []client.FolderRecord) map[string]string {
	byID := map[string]client.FolderRecord{}
	for _, f := range folders {
		byID[f.ID] = f
	}
	var path func(id string, depth int) string
	path = func(id string, depth int) string {
		f, ok := byID[id]
		if !ok || depth > 64 {
			return ""
		}
		if f.ParentID == "" {
			return f.Name
		}
		return path(f.ParentID, depth+1) + "/" + f.Name
	}
	out := map[string]string{}
	for id := range byID {
		out[id] = path(id, 0)
	}
	return out
}

// underFolder reports whether folder path lies in prefix (or is it).
func underFolder(path, prefix string) bool {
	p := strings.Join(splitFolderPath(prefix), "/")
	return path == p || strings.HasPrefix(path, p+"/")
}

// idMapping is one source type → target type id map, as 'move --out' writes it.
type idMapping struct {
	SrcType, DstType string
	IDs              map[string]string
}

func readIDMap(path string) (idMapping, error) {
	var f struct {
		SrcType  string              `json:"source_type_id"`
		DstType  string              `json:"target_type_id"`
		Entities []map[string]string `json:"entities"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return idMapping{}, err
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return idMapping{}, fmt.Errorf("%s: %w", path, err)
	}
	if f.SrcType == "" || f.DstType == "" {
		return idMapping{}, fmt.Errorf("%s has no source_type_id/target_type_id — re-run 'fibery move … --out' with this version", path)
	}
	m := idMapping{SrcType: f.SrcType, DstType: f.DstType, IDs: map[string]string{}}
	for _, e := range f.Entities {
		m.IDs[e["old"]] = e["new"]
	}
	return m, nil
}

type wikiCopyDoc struct {
	view client.ViewRecord
	path string
}

var wikiCopyCmd = &cobra.Command{
	Use:   "copy --to-config <account> --space <space> --folder <path>...",
	Short: "Copy wiki documents of folders into another workspace",
	Long: `Copy the wiki documents of one or more folders (subfolders included) into a
space of another workspace (--to-config names its account config). The folder
paths are recreated there; bodies go as Fibery JSON, so formatting and inline
comments are kept, and pasted images are uploaded again. Inline-comment authors
are matched by email; one the target lacks becomes the target token's user, with
their name opening the comment. Embedded views are dropped (they belong to the
source workspace). Mentions between copied documents point at the copies;
--id-map (a 'move --out' file, repeatable) does the same for entity mentions.

A document whose name already exists in the target folder is skipped, so a run
can be repeated. Nested pages are copied as top-level pages of their folder.
Prints the plan; nothing changes without --yes. --out writes the old → new map.

Example:
  fibery wiki copy --to-config other --space Development \
    --folder "Archive/LINC" --folder "Archive/Notes" --id-map work.json --dry-run`,
	Args: cobra.NoArgs,
	RunE: runWikiCopy,
}

func runWikiCopy(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if wikiCopyToConfig == "" || wikiCopySpace == "" || len(wikiCopyFolders) == 0 {
		return fmt.Errorf("--to-config, --space and at least one --folder are required")
	}
	toSpace := wikiCopyToSpace
	if toSpace == "" {
		toSpace = wikiCopySpace
	}
	dcli, err := otherClient(wikiCopyToConfig)
	if err != nil {
		return err
	}
	sschema, err := cache.LoadSchema(account)
	if err != nil {
		return fmt.Errorf("no schema — run: fibery schema sync")
	}
	dschema, err := cache.LoadSchema(wikiCopyToConfig)
	if err != nil {
		return fmt.Errorf("no schema for --to-config %s — run: fibery --config %s schema sync", wikiCopyToConfig, wikiCopyToConfig)
	}
	var maps []idMapping
	for _, p := range wikiCopyIDMaps {
		m, err := readIDMap(p)
		if err != nil {
			return err
		}
		maps = append(maps, m)
	}

	srcApp, err := spaceIDByName(cmd, wikiCopySpace)
	if err != nil {
		return err
	}
	views, err := cli.QueryViews(ctx, nil)
	if err != nil {
		return err
	}
	folders, err := cli.QueryFolders(ctx)
	if err != nil {
		return err
	}
	paths := folderPaths(folders)
	var docs []wikiCopyDoc
	for _, v := range views {
		if v.Type != "document" || v.ContainerType != "app" || v.ContainerAppID != srcApp || v.DocumentSecret == "" {
			continue
		}
		path := paths[v.FolderID]
		for _, f := range wikiCopyFolders {
			if path != "" && underFolder(path, f) {
				docs = append(docs, wikiCopyDoc{v, path})
				break
			}
		}
	}
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].path < docs[j].path })

	var dstApp string
	if err := onClient(dcli, func() (err error) { dstApp, err = spaceIDByName(cmd, toSpace); return err }); err != nil {
		return fmt.Errorf("--to-config %s: %w", wikiCopyToConfig, err)
	}
	dviews, err := dcli.QueryViews(ctx, nil)
	if err != nil {
		return err
	}
	dfolders, err := dcli.QueryFolders(ctx)
	if err != nil {
		return err
	}
	dpaths := folderPaths(dfolders)
	existing := map[string]string{}
	for _, v := range dviews {
		if v.Type == "document" && v.ContainerAppID == dstApp {
			existing[dpaths[v.FolderID]+"\x00"+v.Name] = v.ID
		}
	}

	perFolder, nested, skip := map[string]int{}, 0, 0
	for _, d := range docs {
		perFolder[d.path]++
		if d.view.ParentPageID != "" {
			nested++
		}
		if _, ok := existing[d.path+"\x00"+d.view.Name]; ok {
			skip++
		}
	}
	fmt.Printf("## Copy wiki %s → %s:%s\n\nDocuments: %d to copy, %d already there (same folder and name)\n\n",
		wikiCopySpace, wikiCopyToConfig, toSpace, len(docs)-skip, skip)
	var fs []string
	for f := range perFolder {
		fs = append(fs, f)
	}
	sort.Strings(fs)
	for _, f := range fs {
		fmt.Printf("- %s (%d)\n", f, perFolder[f])
	}
	if nested > 0 {
		fmt.Printf("\n%d nested pages will be top-level pages of their folder.\n", nested)
	}
	fmt.Println()
	if wikiCopyDryRun {
		fmt.Println("Dry run — nothing changed.")
		return nil
	}
	if !wikiCopyYes {
		return fmt.Errorf("re-run with --yes to copy the documents above")
	}

	users, err := loadUserMap(ctx, cli, dcli)
	if err != nil {
		return err
	}
	ids := map[string]string{}
	var copied []string // target doc secrets
	var out []map[string]string
	folderIDs := map[string]string{}
	nImages, nViews := 0, 0
	for i, d := range docs {
		if id, ok := existing[d.path+"\x00"+d.view.Name]; ok {
			ids[d.view.ID] = id
			continue
		}
		c, n, err := copyDocContent(ctx, cli, dcli, d.view.DocumentSecret, users, "")
		if err != nil {
			return fmt.Errorf("#%s %q: %w", d.view.PublicID, d.view.Name, err)
		}
		secret := client.NewUUID()
		folderID, ok := folderIDs[d.path]
		if !ok {
			if err := onClient(dcli, func() (err error) { folderID, _, err = ensureFolderPath(cmd, dstApp, d.path); return err }); err != nil {
				return err
			}
			folderIDs[d.path] = folderID
		}
		id := client.NewUUID()
		if err := dcli.CreateViews(ctx, []map[string]any{{
			"fibery/id":             id,
			"fibery/name":           d.view.Name,
			"fibery/type":           "document",
			"fibery/meta":           map[string]any{"documentSecret": secret},
			"fibery/container-type": "app",
			"fibery/container-app":  ref(dstApp),
			"fibery/Folder":         ref(folderID),
		}}); err != nil {
			return fmt.Errorf("#%s %q: %w", d.view.PublicID, d.view.Name, err)
		}
		if err := writeNewDoc(ctx, dcli, id, secret, func() error { return dcli.SetDocumentJSON(ctx, secret, *c) }); err != nil {
			return fmt.Errorf("#%s %q: %w", d.view.PublicID, d.view.Name, err)
		}
		var got *client.ViewRecord
		if err := onClient(dcli, func() (err error) { got, err = findView(ctx, id); return err }); err != nil {
			return fmt.Errorf("document created but cannot be read back: %w", err)
		}
		if got.FolderID != folderID {
			if err := dcli.UpdateViews(ctx, []map[string]any{{"id": id, "values": map[string]any{"fibery/Folder": ref(folderID)}}}); err != nil {
				return fmt.Errorf("document #%s created at the space root, folder not set: %w", got.PublicID, err)
			}
		}
		ids[d.view.ID] = id
		copied = append(copied, secret)
		nImages += n.images
		nViews += n.views
		out = append(out, map[string]string{"old": d.view.ID, "old_public_id": d.view.PublicID, "new": id, "new_public_id": got.PublicID, "folder": d.path, "name": d.view.Name})
		fmt.Printf("  %d/%d #%s → #%s %s/%s\n", i+1, len(docs), d.view.PublicID, got.PublicID, d.path, d.view.Name)
	}

	maps = append(maps, idMapping{SrcType: typeID(sschema, "fibery/view"), DstType: typeID(dschema, "fibery/view"), IDs: ids})
	nFixed := 0
	for _, s := range copied {
		d, err := dcli.GetDocumentJSON(ctx, s)
		if err != nil {
			return err
		}
		var doc any
		if err := json.Unmarshal(d.Content.Doc, &doc); err != nil {
			return err
		}
		changed := false
		for _, m := range maps {
			if m.SrcType != "" && remapMentions(doc, m.SrcType, m.DstType, m.IDs) {
				changed = true
			}
		}
		if !changed {
			continue
		}
		d.Content.Doc, _ = json.Marshal(doc)
		if err := dcli.SetDocumentJSON(ctx, s, d.Content); err != nil {
			return err
		}
		nFixed++
	}
	fmt.Printf("Copied %d documents, %d images; dropped %d embedded views; remapped mentions in %d documents.\n",
		len(copied), nImages, nViews, nFixed)
	if wikiCopyOut != "" {
		b, _ := json.MarshalIndent(map[string]any{"source": cfg.Workspace + ":" + wikiCopySpace, "target": wikiCopyToConfig + ":" + toSpace,
			"source_type_id": typeID(sschema, "fibery/view"), "target_type_id": typeID(dschema, "fibery/view"), "entities": out}, "", "  ")
		if err := os.WriteFile(wikiCopyOut, b, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", wikiCopyOut, err)
		}
		fmt.Println("Id map written to " + wikiCopyOut)
	}
	return nil
}

// writeNewDoc fills the document of a just-created document view. The view has
// to exist first: a document with a fresh secret and no view yet is refused
// with 403 "No accessible container". On failure the empty view is removed.
func writeNewDoc(ctx context.Context, c *client.Client, viewID, secret string, fill func() error) error {
	err := c.CreateDocumentMarkdown(ctx, secret, "")
	if err == nil && fill != nil {
		err = fill()
	}
	if err != nil {
		if derr := c.DeleteViews(ctx, []string{viewID}); derr != nil {
			return fmt.Errorf("%w; the empty document %s could not be removed (%v)", err, viewID, derr)
		}
	}
	return err
}

// typeID is the fibery/id of a type in a schema ("" when absent).
func typeID(schema map[string]any, name string) string {
	return asStr(typeMap(schema, name)["fibery/id"])
}

func init() {
	f := wikiCopyCmd.Flags()
	f.StringVar(&wikiCopyToConfig, "to-config", "", "account config of the target workspace (required)")
	f.StringVar(&wikiCopySpace, "space", "", "source space (required)")
	f.StringVar(&wikiCopyToSpace, "to-space", "", "target space (default: same name as --space)")
	f.StringArrayVar(&wikiCopyFolders, "folder", nil, "source folder path, subfolders included (repeatable, required)")
	f.StringArrayVar(&wikiCopyIDMaps, "id-map", nil, "'move --out' file whose entity mentions to remap (repeatable)")
	f.StringVar(&wikiCopyOut, "out", "", "write the old → new id map to this JSON file")
	f.BoolVar(&wikiCopyYes, "yes", false, "copy (required)")
	f.BoolVar(&wikiCopyDryRun, "dry-run", false, "print the plan and stop")
	wikiCmd.AddCommand(wikiCopyCmd)
}
