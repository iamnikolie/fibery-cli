---
name: fibery-cli
description: Use when interacting with Fibery workspace via the fibery CLI tool — searching, reading, creating, updating, or deleting entities, documents, or workflow states.
---

# fibery CLI

## Setup (one-time per account)

```bash
fibery config init          # prompts for workspace + API token → ~/.fibery/config.yaml
fibery schema sync          # fetches workspace schema → ~/.fibery/schema.json
```

Multiple accounts use subdirectories:
```bash
fibery --config personal config init
fibery --config personal schema sync
fibery --config personal search "something"
```
Env alt: `FIBERY_CONFIG=personal fibery ...`

## Database naming

All `--db` values use `"Space/Database"` format exactly as in Fibery:
- `"Development/Dev Task"`, `"Support platform/Support ticket"`, `"Product Management/User-Story"`

When `--db` is unknown, call `fibery search "x"` without `--db` — in non-TTY mode it prints all available databases.

## Command reference

| Command | Description | Key flags |
|---------|-------------|-----------|
| `fibery search <text>` | Find entities by name | `--db`, `--limit` (default 20) |
| `fibery get <id>` | Fetch one entity by public ID, prefixed ID, or UUID — shows all fields | `--db` (required), `--id-only`, `--fields` |
| `fibery resolve <url>` | Fetch entity by Fibery URL | `--id-only`, `--fields` |
| `fibery list <database>` | List entities in a database | `--limit` (default 50), `--sort` (e.g. `-created`, `-modified`), `--fields` |
| `fibery query <json>` | Raw FQL query | `--params '{"$var":"val"}'` |
| `fibery create <db> [field=value...]` | Create entity; repeat field for multi-select | `--doc "Field=content"`, `--id-only` |
| `fibery update <id> [field=value...]` | Update entity; repeat field to append to multi-select | `--db` (required), `--doc "Field=content"` |
| `fibery delete <id>` | Delete entity (requires `--yes`) | `--db` (required), `--yes` (required) |
| `fibery exec <json>` | Send any raw Fibery command; destructive ones require `--yes` | `--yes` (for delete/remove/drop) |
| `fibery import` | Bulk create from JSON array; JSON arrays in values → collection fields | `--db` (required), `--file` (required) |
| `fibery state <id> <state-name>` | Set workflow state (case-insensitive) | `--db` (required) |
| `fibery comment <url-or-id> [text]` | Add comment; optionally @mention users, reference entities, or reply | `--db`, `--mention <email>`, `--ref <url-or-id>`, `--reply-to <comment-id>` |
| `fibery comments list <id>` | List all comments on an entity with author, date, and markdown body | `--db` (required) |
| `fibery doc get <secret-or-id>` | Get document as Markdown | `--db`, `--field` (when entity ID given) |
| `fibery doc set <secret-or-id> <md>` | Set document content | `--db`, `--field` (when entity ID given) |
| `fibery files list <entity-id>` | List file attachments on an entity | `--db` (required), `--field` |
| `fibery files download <entity-id>` | Download attachments to disk | `--db` (required), `--field`, `--out`, `--secret`, `--name` |
| `fibery files upload <entity-id> <file>...` | Upload local files and attach to a file field | `--db` (required), `--field`, `--no-attach` |
| `fibery files embed <entity-id> <image>...` | Upload images and embed them inline into a doc field | `--db` (required), `--field` (required) |
| `fibery me` | Show current user | — |
| `fibery inbox <db> [db...]` | Recent activity on my entities in given databases | `--hours` (default 48), `--absolute` |
| `fibery schema` | Show full schema JSON | — |
| `fibery schema sync` | Refresh schema cache | — |
| `fibery schema show <db>` | Field table (name / type / kind / required) | — |
| `fibery schema enums <db>` | Enum field values with fibery/id | — |
| `fibery skill` | Print this skill reference | — |

## Global flags

| Flag | Description |
|------|-------------|
| `--config <name>` | Use `~/.fibery/<name>/` account directory |
| `--format table\|json\|csv\|tsv` | Output format (default: rendered table/KV) |
| `--json` | Alias for `--format json` |
| `--verbose` | Print API request and response JSON to stderr |

## ID formats

All entity commands (`get`, `update`, `state`, `comment`, `comments list`, `delete`, `doc`) accept:
- UUID: `550e8400-e29b-41d4-a716-446655440000`
- Public ID: `42`
- Prefixed public ID: `DT-42` (prefix is stripped automatically)

The CLI resolves public IDs to UUIDs internally via a one-shot FQL lookup, so
no separate `get --id-only` round-trip is needed.

## Enum and user field resolution

`create`, `update`, and `import` auto-resolve field values:
- `{...}` → pass through as JSON
- UUID → wrap as `{"fibery/id": "..."}` reference
- Named enum value → looked up by name (case-insensitive) via API
- User email → looked up via `user/email` query
- Plain string → passed as-is

Field names are also case-insensitive: `Development/name` auto-corrects to
`Development/Name` (the canonical name from the cached schema) before the call.

## --doc escape sequences

`--doc "Field=line1\nline2"` interprets `\n`, `\t`, `\r`, and `\\` in content.
Use `\\` to keep a literal backslash. To pass a file: `--doc "Field=$(cat f.md)"`.

## FQL param prefix

`fibery query` accepts both `$id` (CLI style) and `?id` (native Fibery style)
as parameter references — they are normalized before the call. Use whichever
your muscle memory prefers.

## Script-friendly ID extraction

Every read command that returns an entity exposes `--id-only` for scripting:

```bash
TICKET_UUID=$(fibery resolve https://x.fibery.io/Dev/Bug-42 --id-only)
TICKET_UUID=$(fibery get 42 --db "Development/Dev Task" --id-only)
NEW_ID=$(fibery create "Development/Dev Task" "Development/Name=hi" --id-only)
```

Plain `--json` output always includes `fibery/id` too if you need more fields
alongside the UUID.

## Token-efficient reads with --fields

`get`, `resolve`, and `list` accept `--fields "alias1,alias2,..."` to return
only those columns. Aliases match the schema field names case-insensitively
(strip the space prefix — e.g. `Development/Priority` → `Priority`).

```bash
fibery get 42 --db "Development/Dev Task" --fields "Name,State,Priority"
fibery list "Development/Dev Task" --fields "Name,Priority" --limit 100
fibery resolve <url> --fields "Name,State"
```

The always-keep set (`fibery/id`, `Public ID`, `Name`) is preserved regardless,
so the entity stays identifiable.

## Reading discussions

```bash
fibery comments list 42 --db "Development/Dev Task"
fibery comments list DT-42 --db "Development/Dev Task"
```

Returns each comment as a Markdown section with author, RFC3339 datetime, and
the body content (fetched per-comment via the documents API).

## Commenting: mentions, references, replies

```bash
# Plain comment
fibery comment 42 --db "Development/Dev Task" "Fixed in PR #42"

# @mention a user by email — prepends a live mention that notifies them
fibery comment 42 --db "Development/Dev Task" "please review" --mention dev@acme.com

# Reference another entity — by URL (any database) or by ID within the host database
fibery comment 42 --db "Development/Dev Task" "dup of" --ref DT-99
fibery comment 42 --db "Development/Dev Task" "related" --ref https://acme.fibery.io/Support_platform/Support_ticket/X-100

# Reply to a comment (thread). --reply-to takes the parent comment's UUID or
# public ID; the positional arg is still the host entity.
fibery comment 42 --db "Development/Dev Task" "agreed" --reply-to 36129

# Combine — flags are repeatable; body is optional when a --mention/--ref is given
fibery comment 42 --db "Development/Dev Task" "see context" \
  --mention dev@acme.com --ref DT-99 --reply-to 36129
```

`--mention` and `--ref` both render as Fibery mention nodes via the
`[[#@<typeId>/<entityId>]]` document shorthand — users get notified, entities
become clickable references. Tokens are prepended to the body in the order given.

## Inbox snapshots

`fibery inbox <db>...` shows recent activity on your entities with relative ages (`5h ago`).
Pass one or more database names. For deterministic snapshots comparable across runs, pass `--absolute`:

```bash
fibery inbox "Development/Dev Task" --absolute --hours 24
fibery inbox "Development/Dev Task" "Development/bug" --hours 72
```

## Destructive operations

`delete` and `exec`-of-destructive-commands require `--yes`:

```bash
fibery delete 42 --db "Development/Dev Task" --yes
fibery exec '{"command":"fibery.entity/delete","args":{...}}' --yes
```

Without `--yes` the command refuses with a clear error — no usage spam.

## Pagination signal

`list` and `search` print a stderr note when the result count hits `--limit`,
so agents know to widen the window:

```
(showing 50 results — limit reached; pass --limit 100 for more)
```

## Schema freshness

If the cached schema is older than 7 days, a stderr warning suggests
`fibery schema sync`. The warning never blocks the command.

## Typical workflows

**Find and read an entity:**
```bash
fibery search "login bug" --db "Development/bug"
fibery get 42 --db "Development/bug"
```

**List entities in a database:**
```bash
fibery list "Development/Dev Task"
fibery list "Development/Dev Task" --limit 100 --sort -modified
```

**Fetch by URL (easiest when you have the link):**
```bash
fibery resolve https://acme.fibery.io/Development/Fix-login-5621
```

**Create and update:**
```bash
fibery create "Development/Dev Task" "Development/Name=Fix login bug" "Development/Priority=High"
fibery update <uuid> --db "Development/Dev Task" "Development/Name=New title"
fibery state <uuid> "In Progress" --db "Development/Dev Task"
```

**Multi-select / collection fields — repeat the field name:**
```bash
fibery create "Development/Dev Task" "Development/Name=Fix login" \
  "Development/Tags=Backend" "Development/Tags=API"

fibery update <uuid> --db "Development/Dev Task" \
  "Development/Tags=Urgent" "Development/Tags=Reviewed"
```

**Create with inline document content:**
```bash
fibery create "Development/Dev Task" "Development/Name=Fix login" \
  --doc "Development/Description=# Summary\n\nDetailed description here"
```

**Update a document field inline:**
```bash
fibery update <uuid> --db "Development/Dev Task" \
  --doc "Development/Description=# Updated content\n\nNew paragraph"
```

**Script-friendly ID capture:**
```bash
ID=$(fibery create "Development/Dev Task" "Development/Name=Fix login" --id-only)
fibery state $ID "In Progress" --db "Development/Dev Task"
```

**Delete an entity:**
```bash
fibery delete <uuid> --db "Development/Dev Task"
```

**Bulk import from JSON (with collection fields):**
```bash
# items.json:
# [{"Development/Name":"Task A","Development/Priority":"High","Development/Tags":["Backend","API"]}]
fibery import --db "Development/Dev Task" --file items.json
```

**Raw FQL (complex queries):**
```bash
fibery query '{"q/from":"Development/Dev Task","q/select":{"ID":["fibery/public-id"],"Name":["Development/Name"],"State":["workflow/state","enum/name"]},"q/limit":20}'

# Parameterised query — $vars in --params, NOT inside the query JSON
fibery query '{"q/from":"Development/Dev Task","q/select":{"ID":["fibery/public-id"]},"q/where":["=",["fibery/public-id"],"$id"],"q/limit":1}' \
  --params '{"$id":"42"}'
```

**Raw Fibery command (any mutation):**
```bash
fibery exec '{"command":"fibery.entity/delete","args":{"type":"Space/Database","entity":{"fibery/id":"<uuid>"}}}'
```

**Read/write rich text document:**
```bash
# By document secret
fibery doc get abc123secret
fibery doc set abc123secret "# Title\nContent here"

# By entity UUID (auto-fetches secret)
fibery doc get <uuid> --db "Development/Dev Task" --field "Development/Description"
fibery doc set <uuid> "# Title\nContent" --db "Development/Dev Task" --field "Development/Description"
```

**List / download file attachments:**
```bash
# List files attached to an entity (all file fields, or one via --field)
fibery files list 75 --db "Development/Dev Task"
fibery files list DT-75 --db "Development/Dev Task" --field "Files/Files" --format json

# Download every attachment to a directory (names sanitized, collisions de-duped)
fibery files download 75 --db "Development/Dev Task" --out /tmp/fibtest

# Download a single file directly by its secret (no entity lookup)
fibery files download --secret <file-secret> --name report.csv --out /tmp
```

**Upload / attach / embed files:**
```bash
# Upload local files and attach them to an entity's file field
# (--field auto-resolves when the DB has a single file field)
fibery files upload 75 --db "Development/Dev Task" diagram.png screenshot.png
fibery files upload DT-75 --db "Development/Dev Task" --field "Files/Files" report.pdf

# Upload only, no attach — prints "secret  id  name" (building block for scripts)
fibery files upload --no-attach diagram.png

# Embed image(s) INLINE into a rich-text field (renders inside the document body)
fibery files embed 75 --db "Development/Dev Task" --field "Development/description" diagram.png
```

**Workflow — create a ticket with images:**
```bash
ID=$(fibery create "Development/Dev Task" "Development/Name=Repro: broken chart" --id-only)
fibery files upload "$ID" --db "Development/Dev Task" shot1.png shot2.png
```

**Workflow — generate a Mermaid diagram locally and put it in Fibery:**
```bash
# Render locally (mermaid-cli), then attach AND/OR embed inline
mmdc -i flow.mmd -o flow.png
fibery files upload "$ID" --db "Development/Dev Task" flow.png            # as an attachment
fibery files embed  "$ID" --db "Development/Dev Task" \
  --field "Development/description" flow.png                              # inline in the description
```

**Inspect schema:**
```bash
fibery schema show "Development/Dev Task"      # field names, types, kinds, required flag
fibery schema enums "Development/Dev Task"     # enum values with fibery/id
```

**Output formats:**
```bash
fibery search "bug" --db "Development/bug" --format csv
fibery query '...' --format tsv
fibery get 42 --db "Development/bug" --format json
```

**Debug API calls:**
```bash
fibery --verbose get 42 --db "Development/Dev Task"   # prints request + response JSON to stderr
```

## Limitations

### Collection fields — append only

`create`, `update`, and `import` use `fibery.entity/add-collection-items` which **appends** new values to multi-select fields; it does not replace existing ones.

To replace existing values: remove first, then add:
```bash
# Get the UUID of the tag to remove:
fibery schema enums "Development/Dev Task"

# Remove existing tag:
fibery exec '{"command":"fibery.entity/remove-collection-items","args":{"type":"Development/Dev Task","field":"Development/Tags","entity":{"fibery/id":"<entity-uuid>"},"items":[{"fibery/id":"<tag-uuid>"}]}}'

# Add new tag:
fibery exec '{"command":"fibery.entity/add-collection-items","args":{"type":"Development/Dev Task","field":"Development/Tags","entity":{"fibery/id":"<entity-uuid>"},"items":[{"fibery/id":"<new-tag-uuid>"}]}}'
```

### Document fields in import

`fibery import` does not support document/rich-text fields. Set them after bulk import:
```bash
fibery doc set <uuid> "# Content" --db "Development/Dev Task" --field "Development/Description"
```

### Single entity references

Pass UUID values as plain strings — they are auto-wrapped as `{"fibery/id": "..."}` by the resolver.
