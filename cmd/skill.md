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
| `fibery get <id>` | Fetch one entity by public ID, prefixed ID, or UUID — all scalar/relation fields + URL; doc bodies hidden by default | `--db` (required), `--id-only`, `--fields`, `--docs`, `--no-docs` |
| `fibery resolve <url>` | Fetch entity OR space/wiki document by Fibery URL — doc bodies hidden by default | `--id-only`, `--fields`, `--docs`, `--no-docs` |
| `fibery url <id>` | Print the canonical web URL for an entity (paste into comments/docs/Slack) | `--db` (required) |
| `fibery list <database>` | List entities in a database | `--limit` (default 50), `--sort`, `--fields`, `--filter`, `--where`, `--params`, `--all` |
| `fibery query [json]` | FQL query — raw JSON, or built from flags | `--params`, `--db`, `--select`, `--filter`, `--where`, `--limit`, `--order`, `--all` |
| `fibery count <database>` | Count entities (server aggregate, falls back to paging) | `--filter`, `--where`, `--params` |
| `fibery create <db> [field=value...]` | Create entity; repeat field for multi-select. Prints public ID + URL | `--doc "Field=content"`, `--doc-file "Field=path.md"`, `--id-only`, `--public-id`, `--skip-invalid`, `--create-missing-enum` |
| `fibery update <id> [field=value...]` | Update entity; repeat field to append to multi-select | `--db` (required), `--doc`, `--doc-file`, `--skip-invalid`, `--create-missing-enum` |
| `fibery delete <id>` | Delete entity (requires `--yes`) | `--db` (required), `--yes` (required) |
| `fibery exec <json>` | Send any raw Fibery command; destructive ones require `--yes` | `--yes` (for delete/remove/drop) |
| `fibery import` | Bulk create from JSON array; JSON arrays in values → collection fields | `--db` (required), `--file` (required), `--create-missing-enum` |
| `fibery state <id> <state-name>` | Set workflow state (case-insensitive) | `--db` (required) |
| `fibery comment <url-or-id> [text]` | Add comment; optionally @mention users, reference entities, reply, or embed screenshots/images inline | `--db`, `--mention <email>`, `--ref <url-or-id>`, `--reply-to <comment-id>`, `--image <path>` (repeatable), `--clipboard` |
| `fibery comment edit <comment-id> <text>` | Replace a comment's body (host entity inferred) | — |
| `fibery comment delete <comment-id>` | Delete a comment (host entity inferred) | `--yes` (required) |
| `fibery comments list <id>` | List comments on an entity with author, date, comment id, and markdown body | `--db` (required), `--limit` (latest N), `--since` (RFC3339\|24h\|7d) |
| `fibery comment-inline list <url-or-id>` | List inline (document) comments: thread id, state, anchored text, body, replies | `--db`, `--field` |
| `fibery comment-inline add <url-or-id> --on "<text>" "<body>"` | Add an inline comment anchored to a text substring | `--db`, `--field`, `--on` (required), `--occurrence <N>` |
| `fibery comment-inline reply <url-or-id> --thread <id> "<body>"` | Reply within an inline comment thread | `--db`, `--field`, `--thread` (required) |
| `fibery comment-inline resolve <url-or-id> --thread <id>` | Resolve or reopen a thread | `--db`, `--field`, `--thread` (required), `--reopen` |
| `fibery comment-inline delete <url-or-id> --thread <id>` | Delete an inline comment thread | `--db`, `--field`, `--thread` (required) |
| `fibery doc get <secret\|id\|url>` | Get document as Markdown | `--db`, `--field` (entity ID), `--secret` (raw UUID secret) |
| `fibery doc set <secret\|id\|url> <md>` | Set document content (full replace) | `--db`, `--field`, `--secret` |
| `fibery doc append <secret\|id\|url> <md>` | Append Markdown to a document | `--db`, `--field`, `--secret` |
| `fibery docs list` | List space/wiki documents (left-nav pages) | `--space`, `--limit` |
| `fibery files list <entity-id>` | List file attachments on an entity | `--db` (required), `--field` |
| `fibery files download <entity-id>` | Download attachments to disk | `--db` (required), `--field`, `--out`, `--secret`, `--name` |
| `fibery files upload <entity-id> <file>...` | Upload local files and attach to a file field | `--db` (required), `--field`, `--no-attach` |
| `fibery files embed <entity-id> <image>...` | Upload images and embed them inline into a doc field | `--db` (required), `--field` (required) |
| `fibery me` | Show current user | — |
| `fibery inbox <db> [db...]` | Recent activity on my entities in given databases | `--hours` (default 48), `--absolute` |
| `fibery schema` | Show full schema JSON | — |
| `fibery schema sync` | Refresh schema cache | — |
| `fibery schema show <db>` (alias `fields`) | Field table (name / type / kind / required) | — |
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

All entity commands (`get`, `update`, `state`, `comment`, `comments list`, `comment-inline`, `delete`, `doc`, `url`) accept:
- UUID: `550e8400-e29b-41d4-a716-446655440000`
- Public ID: `42`
- Prefixed public ID: `DT-42` (prefix is stripped automatically)

The CLI resolves public IDs to UUIDs internally via a one-shot FQL lookup, so
no separate `get --id-only` round-trip is needed.

`comment edit`/`comment delete` take a comment id (the UUID or public id shown by
`comments list`). Comments live in the system database `comments/comment` — you
rarely need that name, but `fibery schema show comments/comment` works if you do.

## Inline (document) comments

`comment-inline` manages comments anchored to a text range *inside* a rich-text field
(the Google-Docs-style highlight comments), distinct from entity comments above. They live
in the document body, not in `comments/comment`.

```bash
# Read inline comments (copy the [thread-id] for reply/resolve/delete)
fibery comment-inline list https://acme.fibery.io/Development/Dev_epic/X-2319

# Anchor a comment to a substring of the document text
fibery comment-inline add DT-42 --db "Development/Dev Task" \
  --on "race condition" "still possible after the lock fix?"

# Reply / resolve / delete by thread id
fibery comment-inline reply   DT-42 --db "Development/Dev Task" --thread <id> "fixed by the lock"
fibery comment-inline resolve DT-42 --db "Development/Dev Task" --thread <id>   # --reopen to undo
fibery comment-inline delete  DT-42 --db "Development/Dev Task" --thread <id>
```

- `--on` matches a contiguous substring of the document text; if it appears more than once,
  pass `--occurrence N` (1-based). Anchors must stay within a single block (paragraph/heading).
- `--field` selects the document field (default: the entity's primary `Description` doc).
  By-id forms prefer a UUID; a URL never needs `--db`.
- Mutations rewrite only the inline-comment array — the document body is left untouched.

## Enum and user field resolution

`create`, `update`, and `import` auto-resolve field values:
- `{...}` → pass through as JSON
- UUID → wrap as `{"fibery/id": "..."}` reference
- Named enum value → looked up by name (case-insensitive) via API
- User email → looked up via `user/email` query
- Plain string → passed as-is

Field names are also case-insensitive: `Development/name` auto-corrects to
`Development/Name` (the canonical name from the cached schema) before the call.
An unknown field that is close to a real one errors with a suggestion
(`field "X" not found — did you mean "Y"?`).

Forgiving flags for `create`/`update`/`import`:
- `--skip-invalid` — skip a field/value that doesn't resolve instead of failing the whole call.
- `--create-missing-enum` — create an absent enum value (by name) and assign it, instead of erroring.

## Document (rich-text) fields

`create` and `update` set document fields two ways:

```bash
# Inline — interprets \n, \t, \r, \\ escapes (use \\ for a literal backslash)
fibery create "Development/Dev Task" "Development/Name=x" \
  --doc "Development/Description=# Heading\n\nbody"

# From a file — no escaping, real newlines preserved. Best for long markdown.
fibery create "Development/Dev Task" "Development/Name=x" \
  --doc-file "Development/Description=./body.md"

fibery update DT-42 --db "Development/Dev Task" \
  --doc-file "Development/Description=./body.md"
```

`--doc` and `--doc-file` are both repeatable and may be combined (different fields).

## Simple filters with --filter (no FQL)

`list`, `count`, and `query` (builder mode) accept `--filter "field=value"` — a
schema-aware shorthand that builds the where-clause for you, so the common
"entities by a relation/field value" query needs no hand-written FQL. Repeatable;
multiple filters are ANDed. Mutually exclusive with `--where`.

Operators: `=` (equals), `!=` (not equals), `~` (contains, case-insensitive).

Value matching is derived from the field's schema type:
- **relation** (link to another database) → matches the related entity's **public id**
- **enum / workflow state** → matches the value **name**
- **user** → matches the **email**
- **primitive** (text/number/date/bool) → matches the value directly

```bash
# Children of an epic — the single most common query, now one flag:
fibery list "Development/Dev Task" --filter "Development/Dev epic=2319"

# Combine filters (ANDed): tasks in an epic that aren't done yet
fibery list "Development/Dev Task" \
  --filter "Development/Dev epic=2319" --filter "workflow/state!=Done"

# Count them
fibery count "Development/Dev Task" --filter "Development/Dev epic=2319"

# Contains match on a text field
fibery list "Development/Dev Task" --filter "Development/Name~login"
```

Use full field names (as in `fibery schema show <db>`), e.g. `workflow/state`,
not the short alias `State`. For matching a relation by *name* (not public id),
operators beyond `= != ~`, or OR logic, drop to raw `--where`.

## Entity URLs

```bash
# Canonical URL for pasting into comments, docs, or Slack
fibery url 5651 --db "Development/Dev Task"
fibery url DT-5651 --db "Development/Dev Task"
```

`get` and `resolve` also print a `**URL:**` line in their detail output, and
`create --json` includes a `url` field. Fibery resolves entities by the trailing
public id, so the title slug in the URL is cosmetic.

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
NEW_PID=$(fibery create "Development/Dev Task" "Development/Name=hi" --public-id)
```

`create` (without `--id-only`/`--public-id`) prints the public id and the entity
URL, so there is no second `fibery get` round-trip to learn the new ticket's id:

```
Created 019ef392-…
Public ID: 5659
URL: https://acme.fibery.io/Development/Dev_Task/hi-5659
```

Plain `--json` output always includes `fibery/id`; on `create` it also carries
`fibery/public-id` and `url`.

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

Rich-text document bodies (`Description` etc.) are the single biggest token
sink, so `get`/`resolve` **hide them by default** — you get every scalar and
relation field plus a one-line hint naming the hidden doc fields. Add `--docs`
when you actually need to read the body:

```bash
fibery get 42 --db "Development/Dev Task"            # fields only; bodies hidden
fibery get 42 --db "Development/Dev Task" --docs     # include Description etc.
fibery resolve <url> --docs
```

`--fields` still wins: naming a doc field in `--fields` returns its body even
without `--docs`. `--json` is unaffected by this default — it never carried the
bodies (only the document secrets), so JSON output is byte-stable. `--no-docs`
additionally strips those secret keys from `--json` for minimal output.

## Reading discussions

```bash
fibery comments list 42 --db "Development/Dev Task"
fibery comments list DT-42 --db "Development/Dev Task"

# Incremental re-reads — fetch only the bodies you need:
fibery comments list 42 --db "Development/Dev Task" --limit 3      # latest 3, oldest-first
fibery comments list 42 --db "Development/Dev Task" --since 24h    # added in the last day
fibery comments list 42 --db "Development/Dev Task" --since 2026-06-20T00:00:00Z
```

Returns each comment as a Markdown section with author, RFC3339 datetime, and
the body content (fetched per-comment via the documents API). `--limit N` keeps
only the latest N (still rendered oldest-first); `--since` takes RFC3339 or a
relative form (`24h`, `7d`, `30m`) and returns only comments created after it.
Both bound the set *before* bodies are fetched, so dropped comments cost nothing.
They compose (`--since` first, then `--limit`); empty result prints `_No comments._`.

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

# Attach screenshots/images — uploaded and embedded inline (rendered in the
# comment body, like a pasted image). --image is repeatable.
fibery comment 42 --db "Development/Dev Task" "see repro" --image shot.png
fibery comment 42 --db "Development/Dev Task" "before/after" --image a.png --image b.png

# Embed the current macOS clipboard image (no need to save it to a file first)
fibery comment 42 --db "Development/Dev Task" "repro" --clipboard

# Combine — flags are repeatable; body is optional when any --mention/--ref/--image/--clipboard is given
fibery comment 42 --db "Development/Dev Task" "see context" \
  --mention dev@acme.com --ref DT-99 --reply-to 36129 --image shot.png
```

### Edit / delete a comment

`fibery comments list` prints each comment's id (`_id: <uuid> · #<public-id>_`).
Pass either form to edit or delete — the host entity is inferred, so no `--db`:

```bash
fibery comment edit 36129 "corrected text"          # replace the body
fibery comment edit <comment-uuid> "corrected text"
fibery comment delete 36129 --yes                    # --yes required (irreversible)
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

**Bulk import from JSON (collection + document fields):**
```bash
# items.json — JSON arrays become multi-select fields; a string on a rich-text
# field becomes its document body (set via the documents API post-create):
# [{"Development/Name":"Task A","Development/Priority":"High",
#   "Development/Tags":["Backend","API"],
#   "Development/Description":"# Summary\n\nMarkdown body."}]
fibery import --db "Development/Dev Task" --file items.json
```

**Raw FQL (complex queries):**
```bash
fibery query '{"q/from":"Development/Dev Task","q/select":{"ID":["fibery/public-id"],"Name":["Development/Name"],"State":["workflow/state","enum/name"]},"q/limit":20}'

# Parameterised query — $vars in --params, NOT inside the query JSON
fibery query '{"q/from":"Development/Dev Task","q/select":{"ID":["fibery/public-id"]},"q/where":["=",["fibery/public-id"],"$id"],"q/limit":1}' \
  --params '{"$id":"42"}'
```

**Query builder mode (no FQL JSON needed):**
```bash
# Omit the JSON and use flags — the CLI assembles the FQL.
fibery query --db "Development/Dev Task" --select "Development/name" --limit 5
fibery query --db "Development/Dev Task" \
  --where '["=",["workflow/state","enum/name"],"$s"]' --params '{"$s":"Open"}'
```
(`--select` field names are case-corrected against the schema. For nested selects
use raw FQL JSON. You cannot mix a positional JSON query with the `--db` flags.)

**Count and page past the 3001-row cap:**
```bash
fibery count "Development/Dev Task"                       # total
fibery count "Development/Dev Task" \
  --where '["=",["workflow/state","enum/name"],"$s"]' --params '{"$s":"Open"}'

# --all streams every matching row (pages with q/offset); works on list and query.
fibery list "Development/Dev Task" --where '["=",["workflow/state","enum/name"],"$s"]' \
  --params '{"$s":"Released"}' --all --json
fibery query --db "GitLab/Merge Request" --select "gitlab/name" --all --json
```
`count` uses a server-side aggregate when allowed; databases with row-level
permissions reject aggregates, so it transparently falls back to paging.

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

# Append instead of replacing (any of secret / id / url)
fibery doc append <uuid> "## New section\n\nmore" --db "Development/Dev Task" --field "Development/Description"

# If a raw document secret happens to be UUID-shaped, force secret mode with --secret
fibery doc get 550e8400-e29b-41d4-a716-446655440000 --secret
```

**Space/wiki documents (left-nav pages):**
These are not entities — they live behind Fibery's (undocumented) views API. The CLI
resolves them by URL or public id.
```bash
# List documents (optionally one space)
fibery docs list
fibery docs list --space Development --limit 50

# Read / write / append by the document's URL
fibery resolve "https://acme.fibery.io/Development/My-Doc-280"       # prints name + secret + content
fibery resolve "https://acme.fibery.io/Development/My-Doc-280" --id-only   # → documentSecret
fibery doc get    "https://acme.fibery.io/Development/My-Doc-280"
fibery doc set    "https://acme.fibery.io/Development/My-Doc-280" "# New body"
fibery doc append "https://acme.fibery.io/Development/My-Doc-280" "appended line"
```
Note: writing a document round-trips through Fibery's markdown normalizer, which
drops trailing-whitespace hard line breaks (cosmetic; content is preserved).

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

### Single entity references

Pass UUID values as plain strings — they are auto-wrapped as `{"fibery/id": "..."}` by the resolver.
