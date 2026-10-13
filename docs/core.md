# Core: proxy, protocol, CLI

Gremlyn ships as one binary, `gremlyn`. Everything goes through one of two proxies: the
stdio proxy, which launches an MCP server as a child process and sits on its stdin and
stdout, and the Streamable HTTP proxy, which listens on a loopback address and forwards to a
remote MCP endpoint. The chaos engine plugs into both as the same two pipeline stages.

## Layout

```
cmd/gremlyn/                 cobra root command
pkg/protocol/                MCP / JSON-RPC message types, newline-delimited framing, SSE
pkg/proxy/                   the stdio wrap proxy, the Streamable HTTP proxy, the pipeline
pkg/models/                  shared domain models: sessions, events, outcomes
pkg/datadir/                 ~/.gremlyn resolution, GREMLYN_DATA_DIR overrides it
internal/cli/                the commands
internal/arena/chaos/        gremlin handler, observer, rubric, event files
internal/arena/gremlins/     the eight failure injectors
internal/arena/scoring/      events to resilience report, pure
internal/arena/storage/      run history as plain files
action/                      the GitHub Action
.claude-plugin/ skills/ commands/ scripts/   the Claude Code plugin
```

`pkg/` never imports `internal/`.

## The stdio proxy

`proxy.NewWrapProxy` starts the child, then runs two pumps: client to server and server to
client. Every message goes through `Pipeline.Process` before it is forwarded.

- **Framing is one JSON object per line**, which is what MCP's stdio transport specifies. A
  peer that speaks `Content-Length` framing gets an explicit diagnostic.
- **A single message is capped in size** in both directions. Both peers are untrusted, and
  an uncapped read is memory exhaustion inline with the agent.
- **Shutdown drains in-flight responses** before the child is stopped, so the agent never
  loses an answer to a request it already sent.
- **A client that ends the session with SIGTERM ended it normally.** MCP's stdio transport
  allows it, and Claude Code does it.

## The Streamable HTTP proxy

`proxy.NewHTTPProxy` fronts one MCP endpoint speaking Streamable HTTP, revision 2025-06-18
(and 2025-03-26, which it extends). `arena ci` runs it in-process when `mcp_server` has a
`url`; `gremlyn wrap --upstream <url>` runs it by hand.

- **It listens on loopback only**, `127.0.0.1` on a free port by default. The bound address
  is checked after `listen`, so a `localhost` that resolves elsewhere is refused. A `Host` or
  `Origin` that is not loopback is refused, which is the DNS rebinding guard the spec asks of
  a local server.
- **A per-run secret path gates access.** The endpoint is `/mcp/<32 hex characters>` drawn
  from `crypto/rand` and compared in constant time; any other path is a 404 and nothing is
  forwarded. The proxy adds the upstream's credentials to what reaches it, so this is what
  keeps another local process, or a web page, from borrowing them. The path is not a
  credential to the upstream: it only opens the proxy. `arena ci` writes the full URL into the
  agent's `mcp.json` (mode 0600), `wrap --upstream` prints it on stderr, and the logs record
  the listen address only. Browser requests whose `Sec-Fetch-Site` is `cross-site` or
  `same-site`, or whose `Sec-Fetch-Mode` is `navigate` or `no-cors`, are refused too.
- **`POST`, `GET` and `DELETE`** are forwarded; anything else is a 405.
- **Every JSON-RPC message goes through `Pipeline.Process`**: the request body, a JSON
  response, and each `data:` event of an SSE stream, on the POST's stream as well as on the
  `GET` stream. The observer and the gremlins do not know which transport they run on.
- **SSE is re-emitted event by event**, flushed as it goes, never buffered whole. `id:`,
  `event:` and `retry:` survive, so `Last-Event-ID` resumption keeps working.
- **An untouched message is forwarded from its raw bytes.** A no-op is byte-exact per
  JSON-RPC message over HTTP, where stdio re-serializes. On an SSE stream the payload of each
  event is byte-exact; the framing around it is normalized: `data:x` is re-emitted as
  `data: x`, and CR or CRLF line endings as LF. A line may end in CR, LF or CRLF, as the SSE
  format allows.
- **Correlation is the pipeline's.** The request is tracked before it is forwarded, and its
  response is matched by session and id on whichever stream carries it: the JSON body, the
  POST's SSE stream or the `GET` stream. `MessageContext.Session` is the `Mcp-Session-Id` of
  the agent's request.
- **Headers.** `Mcp-Session-Id`, `MCP-Protocol-Version`, `Last-Event-ID` and other
  end-to-end headers pass; hop-by-hop headers (and those `Connection` names), `Host`,
  `Origin` and `Accept-Encoding` do not. Configured headers are set last. `WWW-Authenticate`
  is not returned to the agent: its challenge names the upstream's OAuth metadata, which the
  agent cannot use through the proxy.
- **Sizes are capped** at the payload limit (`WithMaxPayload`, 16 MiB by default): the agent's
  request body, a JSON response, each SSE event and each SSE line. An SSE event also holds at
  most 4096 fields, and every line costs its length plus a fixed charge, so a flood of empty
  fields is refused before it is held in memory.
- **Upstream timeouts are explicit.** `request_timeout` (default 60s, must be positive) bounds
  the wait for response headers and the silence between two events of a POST's SSE stream,
  until its response has crossed; a `GET` stream may stay silent. While the server waits for
  the agent's answer to a request it sent on that stream (`elicitation/create`,
  `sampling/createMessage`), the limit is paused until the answer crosses. A real timeout
  answers the agent `504` with a JSON-RPC error that carries the request id and code -32603.
  It never goes through the gremlins, so it never becomes an event.
- **Upstream failures are counted, never swallowed**: a 5xx, a 404 on a request that names a
  session, an unreachable server, an oversize message, a broken SSE stream (even a `GET`
  stream, or a POST's stream after its response). A JSON body of an error status still goes
  through the pipeline. Timeouts and failures are written to the session summary, the
  scenario result and the session record; they fail the scenario and mark the run `failed`,
  and `wrap --upstream` exits non-zero, because the agent then reacted to a failure no
  gremlin owns.
- **Nothing the upstream chose is logged.** A message that is not JSON-RPC is forwarded
  untouched and logged with a fixed reason and its length.
- **Redirects are not followed**, so a credential never leaves for another URL. A plain
  `http` upstream is dialed only to a loopback IP, whatever its name resolves to, and never
  through an environment proxy.
- **TLS verification is always on.** There is no option to turn it off.
- **Shutdown drains in-flight exchanges** for up to 10 seconds, after closing the `GET`
  streams, so a response already on its way still reaches the agent.

Refused with a diagnostic, rather than half supported:

- **JSON-RPC batches**, removed in 2025-06-18: a JSON array body is a 400 with a JSON-RPC
  error.
- **The legacy HTTP+SSE transport** of 2024-11-05. When an `initialize` POST gets a 4xx, the
  proxy runs the spec's own backwards-compatibility probe, a `GET` whose first event would be
  `endpoint`; a `GET` stream that starts with `endpoint` is caught the same way. `arena ci`
  fails the scenario with that diagnostic.
- **The OAuth authorization flow.** See below.

### Credentials

`mcp_server.headers` takes static headers. Header values and the `url` expand `${VAR}` from
the environment and nothing else; an unset or empty variable is a validation error. `GH_TOKEN`
and every `GITHUB_*` and `ACTIONS_*` variable are refused, so a scenario file changed in a pull
request cannot send a CI token to a host it names. Header names are case-insensitive and may
appear once. The `url` must be `https`, or `http` on a loopback address, so a bearer token
never crosses a network in clear, and it may not carry userinfo.

Header values, and a URL path that may carry a key, are secrets. They live in the gremlyn
process only:

- the MCP config handed to the agent points at the local proxy and holds no header;
- the variables the `url` and the headers read are removed from the agent's environment, and
  `agent.env` may not set them, so the agent process never sees the token;
- the recorded server is the URL's scheme and host only, `https://host/…`, in `session.json`,
  `summary.json`, the logs, `wrap`'s stderr and `arena replay`;
- nothing in the logs, `mcp.json`, `session.json`, `events.jsonl`, `summary.json`, the report
  or the PR comment contains a header value.

The GitHub Action unsets its GitHub token (`GH_TOKEN`, `GITHUB_TOKEN`) for the `arena ci` call,
so neither the agent nor the proxy can read it.

**Getting a token for your own server.** Use whatever your server accepts as a static
credential: an API key, a personal access token, or a long-lived token minted for a test
account. If your server only issues tokens through OAuth, complete the flow once with an MCP
client that implements it (the MCP Inspector, for example), copy the access token, and export
it in the variable the header names. Prefer a test account with the narrowest scope that lets
the scenarios run: the agent under test can call every tool the token allows.

**What OAuth support would take.** The agent connects to the proxy's URL, not the
upstream's. Passing the upstream's `401` and its metadata through would make the agent ask
for a token whose `resource` is a URL it is not talking to, and an upstream that validates
the audience, as the spec requires, would reject a token bound to the proxy's URL. The proxy
would have to be the OAuth client itself: read the upstream's protected resource metadata,
run the authorization code flow with PKCE and the `resource` parameter set to the upstream's
canonical URI, store and refresh the token, and present the agent with a server that needs no
authorization.

## The pipeline

The pipeline is the only extension point: adding a gremlin never requires a change in
`pkg/proxy`.

A stage implements `proxy.Handler`: a name, a priority, a direction, and `HandleMessage`,
which returns a decision: pass, modify, redact or block. Stages run in priority order:

| Priority | Stage | Role |
|---|---|---|
| 50 | Arena observer | records what the agent did after an injection |
| 100 | Arena gremlins | mutate at most one tool exchange |

The order is a contract. The observer runs first so it sees what the agent sent, not what a
gremlin rewrote; reordering changes what a score means.

A stage that panics is contained: `Pipeline.callHandler` recovers, treats it as an error,
skips that stage and runs the rest. A `block` is answered to the agent as a JSON-RPC error,
never as a silent drop, which the agent would experience as a hang.

## The CLI

| Command | What it does |
|---|---|
| `gremlyn arena ci` | run the scenarios in `.gremlyn/arena.yaml` against an agent and score them |
| `gremlyn arena report <report.json> [--baseline base.json]` | render a report as the Markdown a pull request shows |
| `gremlyn arena replay <summary.json>` | rebuild the exact `wrap` command of a past run |
| `gremlyn arena sessions` | list recorded runs |
| `gremlyn arena list-gremlins` | describe the gremlins |
| `gremlyn wrap [flags] -- <server command>` | run a stdio MCP server behind the proxy; `--chaos-*` flags enable gremlins |
| `gremlyn wrap --upstream <url> [flags]` | front a Streamable HTTP MCP server; `--upstream-header`, `--listen`, `--upstream-timeout` |
| `gremlyn version` | print the version |

There is no service and no database. Run history is plain files under
`~/.gremlyn/arena/sessions/`.

## Building

```bash
make build
make check
make plugin-check
```

`make check` is the gate: vet, lint, the race-enabled test suite and govulncheck.
`make plugin-check` validates the plugin manifests and the shell scripts.
