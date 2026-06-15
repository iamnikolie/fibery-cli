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
fibery schema show "Development/Dev Task"   # field table (name / type / kind)
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

# Search by name within a database
fibery search "webhook" --db "Support platform/Support ticket"
fibery search "auth" --db "Development/bug" --limit 10

# Get a single entity by public ID, prefixed ID, or UUID
fibery get 42 --db "Development/Dev Task"
fibery get DT-42 --db "Development/Dev Task"
fibery get 550e8400-e29b-41d4-a716-446655440000 --db "Development/Dev Task"

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

# Create with inline document content
fibery create "Development/Dev Task" "Development/Name=Fix login" \
  --doc "Development/Description=# Summary\n\nDetailed description here"

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

# Read all comments on an entity (oldest first, markdown bodies)
fibery comments list 42 --db "Development/Dev Task"

# Delete entity — requires --yes
fibery delete 42 --db "Development/Dev Task" --yes

# Bulk create from JSON array file (enum/user fields resolved by name)
# items.json: [{"Development/Name":"Task A","Development/Priority":"High"}, ...]
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
```

You can also get a document secret manually via FQL:

```bash
fibery query '{"q/from":"Development/Dev Task","q/select":{"secret":["Development/Description","Collaboration~Documents/secret"]},"q/limit":1}'
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
| `--id-only` | (get/resolve/create) Print only the fibery/id UUID |
| `--yes` | (delete/exec) Confirm destructive operation — required |

## Notes

- All entity commands (`get`, `update`, `state`, `delete`, `comment`, `comments list`, `doc`) accept UUID, `DT-42`, or `42` — the CLI resolves public IDs internally
- `fibery get` returns exit 1 when an entity is not found (not silent success)
- `fibery state` looks up state UUIDs automatically — just use the state name
- `fibery search` without `--db` in non-TTY mode lists all available databases
- `fibery create` / `fibery update` / `fibery import` resolve enum values and user emails by name automatically
- Field names are case-insensitive: `Development/name` auto-corrects to `Development/Name` (canonical name from the cached schema)
- `--doc "Field=...\n..."` interprets `\n`, `\t`, `\r`, `\\` escapes — use `\\` for a literal backslash
- `fibery query` accepts both `$id` and `?id` param-reference styles — both normalize to `$id` before the call
- `get`, `resolve`, and `create` all expose `--id-only` for `UUID=$(...)` scripting
- `--json` output on `get` and `resolve` always carries `fibery/id` so scripts don't need a second FQL query
- `--fields "Name,State"` on `get`/`resolve`/`list` saves significant tokens in agent loops
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
