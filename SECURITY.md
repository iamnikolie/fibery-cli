# Security Policy

## Supported versions

The latest release is the supported one. Fixes land on `main` and go out in the
next tag.

## Reporting a vulnerability

Please do **not** open a public issue for a security problem.

Use GitHub's private vulnerability reporting instead:
[Security → Report a vulnerability](https://github.com/iamnikolie/fibery-cli/security/advisories/new).
That opens a private advisory visible only to the maintainers.

Include what you did, what happened, and the impact you think it has. Expect a
first response within a week — this is a spare-time project, not a product with
an on-call rotation.

## Scope notes

Some things are known and by design rather than vulnerabilities:

- **The API token is stored in plain text** in `~/.fibery/config.yaml` with mode
  0600 — the same posture as `~/.aws/credentials` or `.netrc`. Anyone who can
  read your home directory can read the token. Use `FIBERY_API_TOKEN` from a
  secret manager if you need better than that.
- **`--verbose` prints request and response bodies to stderr.** The
  `Authorization` header is never logged, but entity content is. Redact before
  pasting output into an issue.
- **The CLI trusts the workspace it is pointed at.** It renders whatever the
  Fibery API returns; it does not sanitize workspace content for the terminal.

A token that leaks is revoked in Fibery under Settings → API tokens.
