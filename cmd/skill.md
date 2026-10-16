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

When `--db` is unknown, call `fibery search "x"` without `--db` — in non-TTY mode it prints all available databases. If you pass a `--db` that doesn't exist, the error suggests the closest real database name.

## Reading an entity — start with `context`

To grok a ticket/entity, reach for **`fibery context <id>`** first: it returns the
entity (curated fields + document bodies + all comments) **and** its headline graph
(parent / children / blockers / MRs / bugs) in one call. Drop to `fibery get <id-or-url>`
(its `resolve` alias also takes URLs) when you want a single entity without the graph, or
`fibery comment list <id>` for just the discussion.

## Command reference

| Command | Description | Key flags |
|---------|-------------|-----------|
| `fibery search <text>` | Find entities by name (sugar for `list <db> --filter "Name~text"`; without `--db` it lists all databases) | `--db`, `--limit` (default 20) |
| `fibery get <id-or-url>` | Fetch one entity by public ID, prefixed ID, UUID, **or Fibery URL** — all scalar/relation fields + URL; doc bodies hidden by default | `--db` (required unless a URL is given), `--id-only`, `--fields`, `--docs`, `--no-docs` |
| `fibery resolve <url>` | **Alias of `get`** (`get` accepts URLs directly). A URL may target an entity, a space (→ lists its databases), or a space/wiki document | same as `get` |
| `fibery context init <db>` | Print a heuristic draft context profile (keep/docs/edges) for a database; `--save` persists it | `--save` |
| `fibery context <id>` | One-shot curated context bundle: root entity (fields + docs + all comments) + headline graph (parent/children/blockers/MRs/bugs) | `--db`, `--parents-full`, `--no-comments`, `--no-docs`, `--format` |
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
| `fibery comment <url-or-id> [text]` | Add comment (explicit form `comment add`); optionally @mention users, reference entities, reply, or embed screenshots/images inline | `--db`, `--mention <email>`, `--ref <url-or-id>`, `--reply-to <comment-id>`, `--image <path>` (repeatable), `--clipboard` |
| `fibery comment list <id>` | List comments on an entity with author, date, comment id, and markdown body (`comments list` also works) | `--db` (required), `--limit` (latest N), `--since` (RFC3339\|24h\|7d) |
| `fibery comment edit <comment-id> <text>` | Replace a comment's body (host entity inferred) | — |
| `fibery comment delete <comment-id>` | Delete a comment (host entity inferred) | `--yes` (required) |
| `fibery comment-inline list <url-or-id>` | List inline (document) comments: thread id, state, anchored text, body, replies | `--db`, `--field` |
| `fibery comment-inline add <url-or-id> --on "<text>" "<body>"` | Add an inline comment anchored to a text substring | `--db`, `--field`, `--on` (required), `--occurrence <N>` |
| `fibery comment-inline reply <url-or-id> --thread <id> "<body>"` | Reply within an inline comment thread | `--db`, `--field`, `--thread` (required) |
| `fibery comment-inline resolve <url-or-id> --thread <id>` | Resolve or reopen a thread | `--db`, `--field`, `--thread` (required), `--reopen` |
| `fibery comment-inline delete <url-or-id> --thread <id>` | Delete an inline comment thread | `--db`, `--field`, `--thread` (required) |
| `fibery doc get <secret\|id\|url>` | Get document as Markdown | `--db`, `--field` (entity ID), `--secret` (raw UUID secret) |
| `fibery doc set <secret\|id\|url> <md>` | Set document content (full replace) | `--db`, `--field`, `--secret` |
| `fibery doc append <secret\|id\|url> <md>` | Append Markdown to the end of a document; existing content (inline comments, views, mentions) is kept untouched | `--db`, `--field`, `--secret` |
| `fibery wiki list` | List space/wiki documents (left-nav pages); `docs list` also works | `--space`, `--limit` |
| `fibery view create` | Create a list/table view over a database; `--embed` puts it inline in an entity's rich text | `--db`, `--name` (required), `--type list\|table`, `--filter` (repeatable), `--any`, `--where`, `--params`, `--embed`, `--embed-db`, `--embed-field`, `--after-heading` |
| `fibery view embed <view>` | Insert an existing view (uuid or public id) inline into a rich-text field | `--embed` (required), `--embed-db`, `--embed-field`, `--after-heading` |
| `fibery view list` | List views (not wiki docs) | `--space`, `--type`, `--owner <public-id>` |
| `fibery view delete <view>` | Delete a view; an entity-owned view's inline blocks are removed too | `--yes` (required) |
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
| `fibery version` | Print version, commit and build date | also `--version` |

## Global flags

| Flag | Description |
|------|-------------|
| `--config <name>` | Use `~/.fibery/<name>/` account directory |
| `--format table\|json\|csv\|tsv` | Output format (default: rendered table/KV) |
| `--json` | Alias for `--format json` |
| `--verbose` | Print API request and response JSON to stderr |

## ID formats

All entity commands (`get`, `update`, `state`, `comment`, `comment list`, `comment-inline`, `delete`, `doc`, `url`) accept:
- UUID: `550e8400-e29b-41d4-a716-446655440000`
- Public ID: `42`
- Prefixed public ID: `DT-42` (prefix is stripped automatically)

The CLI resolves public IDs to UUIDs internally via a one-shot FQL lookup, so
no separate `get --id-only` round-trip is needed.

`comment edit`/`comment delete` take a comment id (the UUID or public id shown by
`comment list`). Comments live in the system database `comments/comment` — you
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

## Views (lists/tables), incl. inline views in rich text

`fibery view` talks to Fibery's undocumented views JSON-RPC API. A view embedded
with `--embed` is owned by that entity (it does not appear in the left nav) and
shows up as an inline view block, exactly like `/view` in the editor.

```bash
# Inline table of bugs whose name carries a tag, at the end of the "Scope" section
fibery view create --db "Development/bug" --name "ii bugs" --type table \
  --filter "Development/name~[ii]" \
  --embed 3947 --embed-db "Development/bug" --embed-field "Development/steps-to-reproduce" \
  --after-heading Scope

# Several filters are ANDed (--any ORs them). Enum by name, user by email, relation by public id
fibery view create --db "Development/Dev Task" --name "Mine, open" \
  --filter "workflow/state=In Progress" --filter "Development/Dev epic=2319" --embed DT-42 --embed-db "Development/Dev Task"

fibery view list --owner 3947                 # views owned by entity #3947
fibery view embed 2930 --embed 3947 --embed-db "Development/bug" --embed-field "Development/steps-to-reproduce"
fibery view delete 2930 --yes                 # also removes its inline block from #3947
```

- Filters are stored by field id, the way the UI filter editor writes them, so they stay
  editable in Fibery. `~` is case-sensitive "contains" (as in the UI). `--filter` does not
  take collection fields (`assignments/assignees`): use `--where` with ids for those.
- A `member` token cannot create left-nav views (`missing a create view permission on
  space`); entity-owned views (`--embed`) only need edit rights on the entity.
- Embedding goes through the JSON document endpoint: the rest of the body and its inline
  comments are untouched (comment anchors after the insert point are shifted).
- `--after-heading` matches a heading by substring (case-insensitive) and inserts before the
  next heading of the same or higher level. It is checked before the view is created.

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

## Context bundles

`fibery context <id>` assembles, in one call, everything an agent needs to grok an
entity: the root rendered full (curated fields + document bodies + all comments)
plus its connected graph — parent epic/story, children, blockers, merge requests,
bugs — as a headline table. It replaces the manual `get`/`resolve` + `comment list` +
per-neighbor `get` dance.

Two phases. Curation is driven by a per-database **profile** the agent bootstraps
once from a heuristic, reviews, and saves; the runtime never re-runs the heuristic.

```bash
# 1. bootstrap: print the heuristic draft, review/edit, then persist
fibery context init "Development/Dev Task"            # draft YAML → stdout
fibery context init "Development/Dev Task" --save     # save to ~/.fibery/context.yaml

# 2. one-shot context (a profile for the db must exist, else "run init first")
fibery context DT-5683 --db "Development/Dev Task"
fibery context DT-5683 --db "Development/Dev Task" --parents-full   # expand epic/story full
fibery context DT-5683 --db "Development/Dev Task" --no-comments    # skip comments
fibery context DT-5683 --db "Development/Dev Task" --no-docs        # skim: header + graph + comments, doc bodies hidden
fibery context DT-5683 --db "Development/Dev Task" --json           # structured bundle
```

Empty to-many edges (no blockers/MRs/bugs) collapse into one `_empty: …_` line
rather than a section each. `--no-docs` is the skim mode — drop the (often large)
document bodies and keep the header, comments, and graph.

The heuristic keeps the core (title, state, priority, assignees, dates), allow-lists
real document fields (description, knowledge transfer, QA summary — never the
lookup-mirror Story/Epic Description docs), drops noise (int/decimal rollups, bool
flags, `*_deleted`, transition dates), and classifies relation fields into edge
roles by name. Anything it cannot classify is listed in a `# unclassified:` comment
in the draft for the agent to promote.

Profiles live in the **per-account** `context.yaml` (`~/.fibery/[<account>/]context.yaml`),
isolated exactly like `config.yaml`/`schema.json` — each `--config <name>` has its
own profile set, matching that workspace's schema.

The `<id>` accepts a public id, `DT-42`, a UUID, or a full Fibery URL (URL infers
the db). `--parents-full` expands each parent node to full docs + comments (parents
that resolve to the same entity are expanded once).

## Reading discussions

```bash
fibery comment list 42 --db "Development/Dev Task"      # `comments list` also works
fibery comment list DT-42 --db "Development/Dev Task"

# Incremental re-reads — fetch only the bodies you need:
fibery comment list 42 --db "Development/Dev Task" --limit 3      # latest 3, oldest-first
fibery comment list 42 --db "Development/Dev Task" --since 24h    # added in the last day
fibery comment list 42 --db "Development/Dev Task" --since 2026-06-20T00:00:00Z
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

`fibery comment list` prints each comment's id (`_id: <uuid> · #<public-id>_`).
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

Deleted fields and databases stay in the Fibery schema forever, renamed to
`<name>_<hash>_deleted`. The CLI hides them, so they never appear in `get`,
`schema show` or `--fields` — and a database that has some is readable again.

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

# Append instead of replacing (any of secret / id / url). Safe on documents with
# inline comments or embedded views: only the appended part goes through Markdown.
fibery doc append <uuid> "## New section\n\nmore" --db "Development/Dev Task" --field "Development/Description"

# If a raw document secret happens to be UUID-shaped, force secret mode with --secret
fibery doc get 550e8400-e29b-41d4-a716-446655440000 --secret
```

**Space/wiki documents (left-nav pages):**
These are not entities — they live behind Fibery's (undocumented) views API. The CLI
resolves them by URL or public id.
```bash
# List documents (optionally one space). `docs list` is a back-compat alias.
fibery wiki list
fibery wiki list --space Development --limit 50

# Read / write / append by the document's URL
fibery resolve "https://acme.fibery.io/Development/My-Doc-280"       # prints name + secret + content
fibery resolve "https://acme.fibery.io/Development/My-Doc-280" --id-only   # → documentSecret
fibery doc get    "https://acme.fibery.io/Development/My-Doc-280"
fibery doc set    "https://acme.fibery.io/Development/My-Doc-280" "# New body"
fibery doc append "https://acme.fibery.io/Development/My-Doc-280" "appended line"
```
Note: `doc set` round-trips through Fibery's markdown normalizer, which drops
trailing-whitespace hard line breaks (cosmetic) and rewrites the whole body —
inline comments and embedded views are lost. `doc append` does not touch the
existing body.

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
