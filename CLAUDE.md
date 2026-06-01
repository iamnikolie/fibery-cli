# fibery-cli — project instructions for Claude

A Go CLI for the Fibery workspace API. Designed to replace the Fibery MCP server inside Claude Code sessions: Claude reads `fibery <subcommand>` stdout directly via Bash instead of calling MCP tools.

The binary is published as `github.com/langgerone/fibery-cli`. Multi-account is supported via `--config <name>`.

---

## When working in this repo

1. Read this file first — most patterns Claude needs are documented here.
2. Read `cmd/skill.md` second — that file is **embedded into the binary** via `//go:embed` and printed by `fibery skill`. It is the user-facing reference and the single source of truth for command UX. Any CLI surface change (new flag, renamed subcommand, new behavior) **must** also update `cmd/skill.md` and `README.md`.
3. Run `go test ./...` and `go vet ./...` before declaring work done.
4. Default branch is `master`, not `main` or `testing`. PRs go against `master`.

---

## Layout

```
main.go                        thin entrypoint → cmd.Execute()
Makefile                       build / install (symlink to ~/.local/bin/fibery)
README.md                      user-facing docs
cmd/
  root.go                      cobra root + global flags + PersistentPreRunE
  skill.go + skill.md          embedded Claude skill reference
  config.go                    fibery config init
  schema.go                    schema / schema sync / schema show / schema enums
  search.go                    fibery search
  list.go                      fibery list
  query.go                     fibery query (raw FQL)
  get.go                       fibery get (by public-id / prefixed / UUID)
  resolve.go                   fibery resolve (by URL); also hosts most schema-walking helpers
  create.go / update.go        entity create / update
  delete.go                    entity delete
  state.go                     workflow state change
  comment.go                   add comment (URL or UUID)
  doc.go                       doc get / doc set (by secret or by UUID+field)
  import.go                    bulk import from JSON array file
  exec.go                      raw passthrough to POST /api/commands
  me.go                        current user
  inbox.go                     inbox <db>... (recent activity history)
  utils_enum.go                shared resolvers: field value → API value, enum/user/doc lookups
  *_test.go                    per-command unit tests (pure logic, no network)
internal/
  config/                      ~/.fibery/[<acct>/]config.yaml load/save
  cache/                       ~/.fibery/[<acct>/]schema.json load/save
  client/                      HTTP client; one place that talks to Fibery
  render/                      table / KV / CSV / TSV writers (used by --format)
```

Build artifacts: `./fibery` (gitignored). `make install` symlinks `~/.local/bin/fibery → $(CURDIR)/fibery`, so a fresh `make build` is enough to update the installed CLI — no re-install needed.

---

## How a command is wired

Every subcommand follows the same shape. Use this as a template when adding new ones:

```go
package cmd

import (
    "github.com/spf13/cobra"
    "github.com/langgerone/fibery-cli/internal/client"
)

var fooDB string

var fooCmd = &cobra.Command{
    Use:   "foo <arg>",
    Short: "One-line description",
    Long:  "Multi-line help with examples (shown by --help).",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        // 1. Validate flags
        // 2. Build the Fibery command via cli.One / cli.Do
        // 3. Render with outputJSON(result, fallbackRenderFn)
        return nil
    },
}

func init() {
    fooCmd.Flags().StringVar(&fooDB, "db", "", "database name")
    rootCmd.AddCommand(fooCmd)
}
```

Key conventions:

- **`cli` and `cfg` are package-level globals** populated by `rootCmd.PersistentPreRunE` in `root.go`. Do not instantiate a client yourself — just use `cli.One(ctx, cmd)` or `cli.Do(ctx, cmds)`.
- **`account` is the `--config` value** (or `FIBERY_CONFIG` env). Always pass it to `cache.LoadSchema(account)` / `cache.SaveSchema(s, account)` / `config.Load(account)` so multi-account isolation holds.
- **Use `cmd.Context()`** as the context — never `context.Background()` in a `RunE`.
- **Always wire `--db` as a local flag**, not a persistent one. `--db` semantics differ across commands (search/list use it for query target, doc uses it together with `--field`).
- **`init()` registers** the subcommand on `rootCmd` (or its parent like `schemaCmd`, `configCmd`, `docCmd`). Don't expose anything beyond that — the package has no public API.

### Output rendering

All read commands route through `outputJSON(raw, fallbackFn)` in `root.go`:

| `--format` / `--json` | Behavior |
|---|---|
| `--format json` or `--json` | write raw JSON to stdout |
| `--format csv` / `tsv` | `render.CSV` / `render.TSV` |
| (default) | call `fallbackFn` — usually `render.List` (array) or `render.KV` (object) or a hand-rolled LLM-friendly printer |

For LLM-friendly output (entity detail views), use `printEntityLLMFull(ctx, raw, db, docKeys)` in `resolve.go`. It produces Markdown headers, prioritized field order, and appended document sections — this is the format that makes the CLI useful as MCP replacement.

---

## Schema cache — the single most important thing

`fibery.schema/query` is expensive, so the workspace schema is **cached to disk** and read synchronously by almost every command:

- Path: `~/.fibery/schema.json` (default account) or `~/.fibery/<account>/schema.json`.
- Auto-fetched on first run by `PersistentPreRunE` in `root.go` if missing.
- Manually refreshed via `fibery schema sync`.
- Loaded by `cache.LoadSchema(account)` — returns `map[string]any`.

When adding a feature that depends on schema, **never query the API for schema info** — read from the cache. If the schema is missing a field/database the user thinks should exist, instruct them to run `fibery schema sync` rather than papering over it.

Helpers in `cmd/resolve.go` walk this map:

- `findTitleField(schema, db)` — field with `ui/title?: true`
- `findFieldByName(schema, db, name)` / `schemaHasField(...)`
- `findDescriptionField(schema, db)` — first `Collaboration~Documents/Document` field
- `findFieldType(schema, db, field)` (in `utils_enum.go`) — `fibery/type` of a field
- `isCollectionField(schema, db, field)` — `fibery/collection?: true`
- `isEnumLikeType(t)` — type name pattern `Space/Type_Space/Database`
- `listDatabases(schema)` / `spaceDatabases(space)` — non-platform, non-enum DB names
- `buildFullSelect(schema, db)` — comprehensive `q/select` for entity detail (used by `get` and `resolve`)

When writing a new command that needs to know things about the schema, **add a helper to one of these files** rather than re-traversing the map inline.

---

## Field value resolution (create / update / import)

`utils_enum.go::resolveFieldValue(ctx, schema, db, field, value)` is the central converter from "string the user typed" to "value the Fibery API expects":

| Input | Resolution |
|---|---|
| `{...}` (starts with `{`) | parse as JSON object, pass through |
| 36-char UUID | wrap as `{"fibery/id": "<uuid>"}` |
| field type is `fibery/user` | look up by `user/email` → `{"fibery/id": ...}` |
| field type is enum-like | look up enum by name (case-insensitive) → `{"fibery/id": ...}` |
| anything else | pass as-is |

`create`, `update`, and `import` all funnel through this. When you add a new mutation command, reuse it — do not re-implement field parsing.

Multi-select / collection fields **cannot** be set in `fibery.entity/create` or `fibery.entity/update` — they must be added via a separate `fibery.entity/add-collection-items` call. `create.go` and `update.go` both classify fields up front (`isCollectionField`), call create/update for scalars, then iterate collections with `addCollectionItems(...)`. Follow that pattern.

Document/rich-text fields are even more separate — they live behind a `Collaboration~Documents/secret` and are read/written via `/api/documents/<secret>` (see `client.GetDocument` / `client.SetDocument`). To set a doc field from a UUID, use `resolveDocSecretByID(ctx, db, uuid, field)`.

---

## Fibery API gotchas

These are not documented anywhere except in the wild; encode them into helpers rather than rediscovering them:

1. **Batch endpoint is `POST /api/commands`**, but the body is an **array** of commands; `client.Do` does the batching, `client.One` is sugar for a single command.
2. **Rate limit is 3 req/s per token**. `client.request` retries on 429 with exponential backoff up to 3 retries. Do not parallelize aggressively from inside a single command.
3. **`q/limit: "q/no-limit"`** is a string sentinel (not a number) for unbounded queries. Used by enum/state lookups.
4. **Parameters live OUTSIDE the query object** — `{"query": {...}, "params": {"$id": "42"}}`. Common mistake: putting `params` inside `query`.
5. **`fibery.entity/query` returns an array** even when you `q/limit: 1`. Always unwrap.
6. **`q/where` references must use `[\"field\", \"name\"]` arrays** for nested fields: `[\"workflow/state\", \"enum/name\"]` not `\"workflow/state.enum/name\"`.
7. **`workflow/state` selection**: select `[\"workflow/state\", \"enum/name\"]` for the name and `[\"workflow/state\", \"workflow/Final\"]` for the terminal-state flag.
8. **Setting workflow state needs the state's `fibery/id`** — and the state UUIDs live in a per-DB enum type named like `workflow/state_Space/Database`. `state.go::resolveStateUUID` does the lookup.
9. **Comments are a two-step dance**: create a `comments/comment` entity with a `comment/document-secret`, add it to the parent's `comments/comments` collection, then PUT the markdown to `/api/documents/<secret>`. `client.AddComment` encapsulates this.
10. **History endpoint is `/api/history/v2/search`** — different schema from `/api/commands`, with `where` filters and `timeframe`. See `client.QueryHistory`.
11. **Markdown documents endpoint** uses `?format=md`. The response is JSON `{"content": "..."}` for GET; PUT body is `{"content": "...", "type": "text/markdown"}`.
12. **Database names use `/`**: `"Development/Dev Task"`, with a literal space in the DB part. URLs replace spaces with underscores; `resolveURL` and friends translate back.
13. **Enum/relation type names contain `_`** after the `/` (e.g. `Development/State_Development/Dev Task`). `listDatabases` skips these to avoid showing helper types to the user.

---

## ID formats

`get`, `update`, `state`, `comment`, `doc`, `delete` accept different ID forms — be explicit:

| Form | Example | Accepted by |
|---|---|---|
| Public ID (numeric) | `42` | `get` |
| Prefixed public ID | `DT-42` | `get` (prefix is stripped via `rePrefixedID` in `get.go`) |
| UUID (36 chars, hyphenated) | `550e8400-…` | `get`, `update`, `state`, `comment`, `doc`, `delete` |
| Fibery URL | `https://x.fibery.io/Space/Database/Slug-42` | `resolve`, `comment` |

`isUUID(s)` in `get.go` is the canonical check (length 36, hyphens at positions 8/13/18/23). Reuse it.

---

## Multi-account isolation

Account selection is the **`account` package-level var** in `cmd/root.go`, set from `--config <name>` flag or `FIBERY_CONFIG` env. Empty string = default account.

Filesystem layout:

```
~/.fibery/
  config.yaml                  default
  schema.json                  default
  <account>/
    config.yaml
    schema.json
```

`config.fiberyHome(account)` and `cache.fiberyHome(account)` (in `internal/config` and `internal/cache`) are the two places that compute the directory. `FIBERY_HOME` env var overrides the base — used by tests to redirect to a tmpdir.

Every command that touches the schema cache must pass `account`. Same for `config.Load(account)` / `config.Save(token, ws, account)`.

---

## Testing

- `*_test.go` lives next to the code it tests. Use `testify/assert`. No mocks beyond local struct fixtures.
- Tests do **not** hit the network — they exercise pure helpers (URL parsing, ID extraction, schema walking, field classification, etc.). When adding a network-dependent feature, factor the pure logic out so it can be tested without a client.
- `FIBERY_HOME` env var is the test escape hatch for config/cache paths (see `internal/config/config_test.go`, `internal/cache/cache_test.go`).
- Run with `go test ./...` from the repo root.

---

## Adding a new subcommand — checklist

1. New file in `cmd/`, named after the subcommand (e.g. `cmd/archive.go`).
2. Cobra command, `RunE`, registered via `init()` on `rootCmd` (or its parent).
3. If it queries Fibery: use `cli.One` / `cli.Do`; load schema with `cache.LoadSchema(account)` if needed.
4. If it accepts a field=value list: reuse the `fieldMap` + `resolveFieldValue` pattern from `create.go`.
5. If it produces an entity result: route through `outputJSON(raw, renderFn)` so `--json`/`--format` flags work.
6. Add a row to the table in `cmd/skill.md` **and** an example, plus a section in `README.md`.
7. Add a `*_test.go` for any pure logic introduced.
8. Run `go test ./...` and `go vet ./...`.

---

## Things deliberately NOT here

- No global mock packages, no DI framework — this CLI is small enough that wiring through package-level `cli`/`cfg` globals is fine.
- No structured logger — `fmt.Fprintf(os.Stderr, ...)` is the convention. `--verbose` (in `client.Client.Verbose`) is the one debug knob; it prints request/response JSON to stderr.
- No background workers, no daemons. Every command is single-shot.
- No version flag yet. If asked to add one, embed it via `-ldflags "-X cmd.version=..."` in the Makefile rather than a separate config file.
- No linter config — `go vet ./...` is the only static check applied.
