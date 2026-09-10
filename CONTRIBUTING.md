# Contributing

Thanks for taking a look. This is a small, focused CLI — bug reports and pull
requests are welcome, and so is a plain question in an issue.

## Reporting a bug

Include the output of `fibery version`, the exact command you ran, and what you
expected instead. `--verbose` prints the request and response JSON to stderr,
which is usually enough to see what Fibery answered — **redact your workspace
name and anything sensitive before pasting it.**

## Pull requests

Before opening one:

```bash
make fmt           # gofmt -w .
make vet           # go vet ./...
make test          # go test ./...
```

CI runs the same three (plus `-race`) on Linux and macOS, so a green local run
usually means a green PR.

House rules, all of them cheap to follow:

- **One concern per PR.** A bug fix and a refactor in the same diff take three
  times as long to review.
- **Tests for pure logic.** The test suite never touches the network. When a
  feature needs the API, factor the parsing, ID handling or schema walking into
  a function that can be tested without a client — that is how every existing
  command is structured.
- **Update the docs in the same commit.** Any change to the CLI surface (new
  flag, renamed subcommand, changed output) must also update `cmd/skill.md`
  (embedded in the binary and printed by `fibery skill`) and `README.md`.
- **Follow the existing shape.** `CLAUDE.md` documents the layout, how a
  subcommand is wired, the schema cache, and the Fibery API gotchas worth
  knowing before you write anything. Read it first; it will save you an hour.
- **Conventional commit subjects** — `feat:`, `fix:`, `docs:`, `refactor:`,
  `test:`, `chore:`. Release notes are generated from them.

## Adding a subcommand

`CLAUDE.md` has a checklist for exactly this. The short version: new file in
`cmd/`, cobra command registered from `init()`, schema read from the cache
(never from the API), output routed through `outputJSON` so `--json` and
`--format` keep working, tests for the pure parts, docs updated.

## Releases

Maintainer-only. Tag and push:

```bash
git tag -a v1.2.3 -m "v1.2.3"
git push origin v1.2.3
```

GoReleaser builds the archives for linux/darwin/windows on amd64 and arm64 and
publishes the GitHub release with a generated changelog.
