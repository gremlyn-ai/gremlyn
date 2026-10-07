# Security policy

## Reporting a vulnerability

Report it privately through
[GitHub private vulnerability reporting](https://github.com/gremlyn-ai/gremlyn/security/advisories/new).
Do not open a public issue.

Include the version (`gremlyn version`), what you ran, and what happened. You get an
answer within 7 days, and a fix or a decision within 30 days. Credit goes in the advisory
unless you ask otherwise.

## Supported versions

Only the latest release receives security fixes.

## Scope

Gremlyn runs inline between an agent and its MCP server and is fed bytes from both, so
these are in scope:

- the stdio proxy and its JSON-RPC parsing (`pkg/proxy`, `pkg/protocol`): a crash, a hang,
  or traffic altered when no gremlin injected;
- tool results or gremlin payloads written to logs, which must only ever hold identifiers;
- the download path: `install.sh`, the plugin's `scripts/gremlyn`, and the GitHub Action
  installing a binary without verifying it;
- the GitHub Action leaking a token or running untrusted input as code.

Out of scope: an agent misbehaving because of a gremlin. Injecting failures and hostile
text such as `injection` or `identity` payloads is what Gremlyn is for.

## Verifying a release

Every release publishes `checksums.txt`, signed keylessly with cosign by the release
workflow. `install.sh`, the plugin and the Action verify it automatically when cosign is
installed. By hand:

```bash
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/gremlyn-ai/gremlyn/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum -c checksums.txt --ignore-missing
```

v0.1.0 predates the bundle and ships `checksums.txt.sig` and `checksums.txt.pem`: pass
`--signature checksums.txt.sig --certificate checksums.txt.pem` instead of `--bundle`.
