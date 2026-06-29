# fibery-cli

A CLI for Fibery — replaces the Fibery MCP server in Claude Code sessions. Instead of Claude calling MCP tools, it reads `fibery` command output directly from Bash.

## Install

**Build from source:**

```bash
git clone git@github.com:langgerone/fibery-cli.git
cd fibery-cli
make install        # installs symlink to ~/.local/bin/fibery
```

**Or install directly with Go:**

```bash
go install github.com/langgerone/fibery-cli@latest
```

## Setup

```bash
fibery config init
```

Prompts for your Fibery workspace (e.g. `acme` for `acme.fibery.io`) and API token.
Config is saved to `~/.fibery/config.yaml` (mode 0600).

**Alternatively, use env vars:**

```bash
export FIBERY_API_TOKEN=your-token
export FIBERY_WORKSPACE=acme
```

### Multiple accounts

Each account gets its own subdirectory under `~/.fibery/`:

```bash
fibery --config personal config init   # → ~/.fibery/personal/config.yaml
fibery --config personal schema sync
fibery --config personal search "something"
```

Env alt: `FIBERY_CONFIG=personal fibery ...`

## Schema cache

On first run, the schema is fetched automatically and cached to `~/.fibery/schema.json`. To refresh:

```bash
fibery schema sync
```

Inspect the schema:

```bash
fibery schema show "Development/Dev Task"   # field table (name / type / kind / required)
fibery schema fields "Development/Dev Task" # alias of "schema show"
fibery schema enums "Development/Dev Task"  # enum values with fibery/id
```

## Commands

### My work

```bash
# Recent activity on my entities across one or more databases
fibery inbox "Development/Dev Task"
fibery inbox "Development/Dev Task" "Development/bug" --hours 24
fibery inbox "Support platform/Support ticket" --absolute
```

### Read

```bash
# Current user
fibery me

# FQL query
fibery query '{"q/from":"Development/Dev Task","q/select":{"ID":["fibery/public-id"],"Name":["Development/Name"],"State":["workflow/state","enum/name"]},"q/limit":20}'

# Parameterised query (--params is a sibling of the query object, not inside it)
fibery query '{"q/from":"Development/Dev Task","q/select":{"ID":["fibery/public-id"]},"q/where":["=",["fibery/public-id"],"$id"],"q/limit":1}' \
  --params '{"$id":"42"}'

# Query builder — no FQL JSON; flags assemble the query
fibery query --db "Development/Dev Task" --select "Development/name" --limit 5
fibery query --db "Development/Dev Task" \
  --where '["=",["workflow/state","enum/name"],"$s"]' --params '{"$s":"Open"}'

# Count entities (server aggregate, falls back to paging on permissioned DBs)
fibery count "Development/Dev Task"
fibery count "Development/Dev Task" --where '["=",["workflow/state","enum/name"],"$s"]' --params '{"$s":"Open"}'

# --filter builds the where-clause for you (no FQL). Repeatable; ANDed.
# Relations match by public id, enums by name, users by email, primitives directly.
fibery list "Development/Dev Task" --filter "Development/Dev epic=2319"
fibery list "Development/Dev Task" --filter "Development/Dev epic=2319" --filter "workflow/state!=Done"
fibery count "Development/Dev Task" --filter "Development/Dev epic=2319"

# --all returns every matching row, paging past Fibery's 3001-row cap (list and query)
fibery list "Development/Dev Task" --where '["!=",["workflow/state","workflow/Final"],true]' --all --json
fibery query --db "GitLab/Merge Request" --select "gitlab/name" --all --json

# Canonical web URL for an entity (paste into comments/docs/Slack)
fibery url 5651 --db "Development/Dev Task"

# Search by name within a database
fibery search "webhook" --db "Support platform/Support ticket"
fibery search "auth" --db "Development/bug" --limit 10

# Get a single entity by public ID, prefixed ID, or UUID
fibery get 42 --db "Development/Dev Task"
fibery get DT-42 --db "Development/Dev Task"
fibery get 550e8400-e29b-41d4-a716-446655440000 --db "Development/Dev Task"

# Rich-text bodies (Description etc.) are hidden by default to save tokens —
# you get every scalar/relation field plus a hint naming the hidden docs.
# Add --docs to include the bodies. Works on get and resolve.
fibery get 42 --db "Development/Dev Task" --docs

# Script-friendly UUID extraction — works on get, resolve, and create
TICKET_UUID=$(fibery get 42 --db "Development/Dev Task" --id-only)
TICKET_UUID=$(fibery resolve https://acme.fibery.io/Development/bug/Some-bug-3245 --id-only)

# Fetch any entity by Fibery URL (full content + document)
fibery resolve https://acme.fibery.io/Development/bug/Some-bug-3245

# Show full cached workspace schema JSON
fibery schema
```

### Write

All write commands accept UUID, `DT-42`, or just `42` as the entity reference.

```bash
# Create entity (enum/user fields resolved by name automatically)
fibery create "Development/Dev Task" "Development/Name=Fix login bug" "Development/Priority=High"

# Forgiving writes: skip values that don't resolve, or create missing enum values on the fly
fibery create "Development/Dev Task" "Development/Name=x" "Development/Priority=Brand New" --create-missing-enum
fibery create "Development/Dev Task" "Development/Name=x" "Development/Priority=??" --skip-invalid
# (both flags also work on update and import; unknown field names get a "did you mean ...?" hint)

# create prints the new entity's public id and URL (no second `get` needed):
#   Created 019ef392-…
#   Public ID: 5659
#   URL: https://acme.fibery.io/Development/Dev_Task/Fix-login-5659
# --public-id prints just the public id; --json adds fibery/public-id + url.

# Create with inline document content
fibery create "Development/Dev Task" "Development/Name=Fix login" \
  --doc "Development/Description=# Summary\n\nDetailed description here"

# Create with a document field from a file (no \n escaping; better for long markdown)
fibery create "Development/Dev Task" "Development/Name=Fix login" \
  --doc-file "Development/Description=./body.md"

# Update fields — public ID works too
fibery update 42 --db "Development/Dev Task" \
  "Development/Name=Updated title" "Development/Priority=Critical"

# Change workflow state (lists available states on error)
fibery state 42 "In Progress" --db "Development/Dev Task"

# Add comment
fibery comment 42 --db "Development/Dev Task" "Fixed in PR #42"

# @mention a user (notifies them), reference an entity, or reply in a thread
fibery comment 42 --db "Development/Dev Task" "please review" --mention dev@acme.com
fibery comment 42 --db "Development/Dev Task" "dup of" --ref DT-99
fibery comment 42 --db "Development/Dev Task" "agreed" --reply-to 36129

# Attach screenshots/images — embedded inline (rendered in the comment body).
# --image is repeatable; --clipboard grabs the current macOS clipboard image.
fibery comment 42 --db "Development/Dev Task" "see repro" --image shot.png --image after.png
fibery comment 42 --db "Development/Dev Task" "repro" --clipboard

# Read all comments on an entity (oldest first, markdown bodies + comment ids)
fibery comments list 42 --db "Development/Dev Task"

# Incremental re-reads — bound the set so only the bodies you need are fetched.
# --limit keeps the latest N (still oldest-first); --since takes RFC3339 or a
# relative form (24h, 7d, 30m). They compose (--since first, then --limit).
fibery comments list 42 --db "Development/Dev Task" --limit 3
fibery comments list 42 --db "Development/Dev Task" --since 24h

# Edit / delete a comment by its id (UUID or public id from `comments list`).
# Host entity is inferred — no --db.
fibery comment edit 36129 "corrected text"
fibery comment delete 36129 --yes

# Inline (document) comments — anchored to a text range inside a rich-text field,
# distinct from the entity comments above. They live in the document body.
# List shows each thread's id, state, anchored text, body, and replies.
fibery comment-inline list https://acme.fibery.io/Development/Dev_epic/X-2319

# Anchor a comment to a substring of the document text (--occurrence N if it repeats).
fibery comment-inline add 42 --db "Development/Dev Task" \
  --on "race condition" "still possible after the lock fix?"

# Reply / resolve / delete by the [thread-id] from `comment-inline list`.
fibery comment-inline reply   42 --db "Development/Dev Task" --thread <id> "fixed by the lock"
fibery comment-inline resolve 42 --db "Development/Dev Task" --thread <id>   # --reopen to undo
fibery comment-inline delete  42 --db "Development/Dev Task" --thread <id>
# --field selects the document field (default: the entity's primary Description doc).

# Delete entity — requires --yes
fibery delete 42 --db "Development/Dev Task" --yes

# Bulk create from JSON array file (enum/user fields by name; arrays → multi-select;
# a string on a rich-text field becomes its document body)
# items.json: [{"Development/Name":"Task A","Development/Priority":"High","Development/Description":"# Body"}, ...]
fibery import --db "Development/Dev Task" --file items.json

# Send any raw Fibery command — destructive ones (delete/remove/drop) require --yes
fibery exec '{"command":"fibery.entity/delete","args":{"type":"Space/DB","entity":{"fibery/id":"<uuid>"}}}' --yes
```

### Documents

```bash
# Read/write by document secret
fibery doc get <document-secret>
fibery doc set <document-secret> "# Title\n\nContent here"

# Read/write by entity UUID (secret fetched automatically)
fibery doc get <uuid> --db "Development/Dev Task" --field "Development/Description"
fibery doc set <uuid> "# Title\n\nContent" --db "Development/Dev Task" --field "Development/Description"

# Append instead of replacing
fibery doc append <uuid> "## More\n\ntext" --db "Development/Dev Task" --field "Development/Description"

# Force secret mode when a raw secret is UUID-shaped (otherwise read as an entity id)
fibery doc get <uuid-shaped-secret> --secret
```

#### Space / wiki documents

Left-nav space documents are not entities; the CLI reaches them through Fibery's
(undocumented) views API, by URL or public id.

```bash
# List space documents (optionally one space)
fibery docs list
fibery docs list --space Development --limit 50

# Read / write / append by URL — resolve also prints the document secret
fibery resolve "https://acme.fibery.io/Development/My-Doc-280"
fibery resolve "https://acme.fibery.io/Development/My-Doc-280" --id-only   # → documentSecret
fibery doc get    "https://acme.fibery.io/Development/My-Doc-280"
fibery doc set    "https://acme.fibery.io/Development/My-Doc-280" "# New body"
fibery doc append "https://acme.fibery.io/Development/My-Doc-280" "appended line"
```

> Writes round-trip through Fibery's markdown normalizer, which strips trailing-whitespace
> hard line breaks (cosmetic — content is preserved). The views endpoint is undocumented
> and may change.

You can also get a document secret manually via FQL:

```bash
fibery query '{"q/from":"Development/Dev Task","q/select":{"secret":["Development/Description","Collaboration~Documents/secret"]},"q/limit":1}'
```

### Files

```bash
# List file attachments on an entity (public ID, prefixed ID, or UUID)
fibery files list 75 --db "Development/Dev Task"
fibery files list 75 --db "Development/Dev Task" --format json

# Limit to one file field (default: all file fields on the database)
fibery files list 75 --db "Development/Dev Task" --field "Files/Files"

# Download every attachment into a directory (--out defaults to ".").
# File names are preserved, sanitized for the filesystem, and de-duped on collision.
fibery files download 75 --db "Development/Dev Task" --out /tmp/fibtest

# Download a single file directly by its secret, no entity lookup needed
fibery files download --secret <file-secret> --name report.csv --out /tmp
```

Upload local files and attach them to a file field (`--field` auto-resolves when
the database has a single file field):

```bash
fibery files upload 75 --db "Development/Dev Task" diagram.png screenshot.png
fibery files upload 75 --db "Development/Dev Task" --field "Files/Files" report.pdf

# Upload only, no attach — prints "secret  id  name" for scripting
fibery files upload --no-attach diagram.png
```

Embed images inline into a rich-text field (they render inside the document body,
exactly like a pasted image):

```bash
fibery files embed 75 --db "Development/Dev Task" --field "Development/description" diagram.png
```

Common workflows:

```bash
# Create a ticket with screenshots attached
ID=$(fibery create "Development/Dev Task" "Development/Name=Repro: broken chart" --id-only)
fibery files upload "$ID" --db "Development/Dev Task" shot1.png shot2.png

# Generate a Mermaid diagram locally, then attach it and embed it inline
mmdc -i flow.mmd -o flow.png
fibery files upload "$ID" --db "Development/Dev Task" flow.png
fibery files embed  "$ID" --db "Development/Dev Task" --field "Development/description" flow.png
```

## Flags

| Flag | Description |
|------|-------------|
| `--config <name>` | Use `~/.fibery/<name>/` account directory (or `FIBERY_CONFIG` env) |
| `--format table\|json\|csv\|tsv` | Output format (default: rendered table/KV) |
| `--json` | Alias for `--format json` |
| `--verbose` | Print API request JSON to stderr |
| `--db` | Database name, e.g. `"Development/Dev Task"` |
| `--limit` | Max results (default 20) |
| `--hours` | Inbox lookback window in hours (default 48) |
| `--absolute` | (inbox) Print absolute timestamps instead of relative "5h ago" |
| `--fields a,b,c` | (get/resolve/list) Return only these field aliases — saves tokens |
| `--docs` | (get/resolve) Include rich-text document bodies (hidden by default to save tokens) |
| `--no-docs` | (get/resolve) Also drop document secret keys from `--json` (rendered bodies already hidden by default) |
| `--id-only` | (get/resolve/create) Print only the fibery/id UUID |
| `--public-id` | (create) Print only the public id of the created entity |
| `--yes` | (delete/exec/comment delete) Confirm destructive operation — required |
| `--image <path>` | (comment) Upload a local image and embed it inline in the comment body (repeatable) |
| `--clipboard` | (comment) Embed the current macOS clipboard image inline in the comment |
| `--filter 'field=value'` | (list/query/count) Simple schema-aware filter (repeatable; ops `= != ~`); builds the where-clause for you |
| `--where '<json>'` | (list/query/count) FQL where-clause as JSON |
| `--doc-file 'Field=path.md'` | (create/update) Set a document field from a file (repeatable) |
| `--all` | (list/query) Return every matching row, paging past the 3001-row cap |
| `--select a,b` | (query builder) Field names to select |
| `--order <field>` | (query builder) Sort field (`-created`, `-modified`, or any field) |
| `--secret` | (doc get/set/append) Treat the argument as a raw document secret |
| `--space <name>` | (docs list) Filter to one space |
| `--skip-invalid` | (create/update) Skip fields/values that don't resolve |
| `--create-missing-enum` | (create/update/import) Create absent enum values by name |

## Notes

- All entity commands (`get`, `update`, `state`, `delete`, `comment`, `comments list`, `comment-inline`, `doc`) accept UUID, `DT-42`, or `42` — the CLI resolves public IDs internally
- `fibery get` returns exit 1 when an entity is not found (not silent success)
- `fibery state` looks up state UUIDs automatically — just use the state name
- `fibery search` without `--db` in non-TTY mode lists all available databases
- `fibery create` / `fibery update` / `fibery import` resolve enum values and user emails by name automatically; unknown field names get a "did you mean …?" hint, and `--skip-invalid` / `--create-missing-enum` make bulk writes forgiving
- `fibery count` counts server-side, falling back to id-paging on databases that reject aggregates (row-level permissions)
- `fibery list` / `fibery query` accept `--all` to stream every matching row past Fibery's 3001-row query cap
- `fibery list` / `fibery query` / `fibery count` accept `--filter "field=value"` (repeatable, ANDed) to build the where-clause without FQL — relations match by public id, enums by name, users by email; mutually exclusive with `--where`
- `fibery create` prints the new entity's public id and URL by default (or `--public-id` for just the id); `--json` adds `fibery/public-id` and `url`
- `fibery url <id> --db ...` prints an entity's canonical web URL; `get` / `resolve` detail output includes a URL line
- `fibery comment edit/delete <comment-id>` edit or remove a comment by the id shown in `comments list` (host entity inferred; `delete` needs `--yes`)
- `fibery create` / `fibery update` accept `--doc-file "Field=path.md"` to load a document field from a file; `fibery import` sets rich-text fields from their string value
- `fibery resolve`, `fibery doc get/set/append`, and `fibery docs list` reach space/wiki documents (left-nav pages) via the views API
- Field names are case-insensitive: `Development/name` auto-corrects to `Development/Name` (canonical name from the cached schema)
- `--doc "Field=...\n..."` interprets `\n`, `\t`, `\r`, `\\` escapes — use `\\` for a literal backslash
- `fibery query` accepts both `$id` and `?id` param-reference styles — both normalize to `$id` before the call
- `get`, `resolve`, and `create` all expose `--id-only` for `UUID=$(...)` scripting
- `--json` output on `get` and `resolve` always carries `fibery/id` so scripts don't need a second FQL query
- `--fields "Name,State"` on `get`/`resolve`/`list` saves significant tokens in agent loops
- `get`/`resolve` hide rich-text bodies (Description etc.) by default — every scalar/relation field is shown plus a one-line hint naming the hidden docs; add `--docs` to include the bodies, or name a doc field in `--fields`. `--json` is unaffected (it only ever carried document secrets, not bodies)
- `fibery comments list` accepts `--limit N` (latest N, oldest-first) and `--since <RFC3339|24h|7d|30m>` for incremental re-reads — bodies are fetched only for the comments actually returned
- `fibery delete` and destructive `fibery exec` commands require `--yes` — no accidental data loss
- `fibery list` and `fibery search` print a stderr signal when results hit `--limit`, so you know there may be more
- Schema cache older than 7 days prints a stderr warning suggesting `fibery schema sync`
- Rate limit: Fibery allows 3 req/s per token; CLI retries automatically on 429

## Config file

`~/.fibery/config.yaml`:

```yaml
api_token: your-fibery-api-token
workspace: your-workspace-name
```

## Claude Code

Run `fibery skill` to print the full Claude skill reference (commands, flags, and workflows).

## Development

```bash
go test ./...      # run tests
go vet ./...       # lint
make build         # build binary locally
make install       # symlink to ~/.local/bin
```
