# Core: proxy, protocol, CLI

Gremlyn ships as one binary, `gremlyn`. Everything goes through one proxy: `wrap`, which
launches a stdio MCP server as a child process and sits on its stdin and stdout. The chaos
engine plugs into that proxy as two pipeline stages.

## Layout

```
cmd/gremlyn/                 cobra root command
pkg/protocol/                MCP / JSON-RPC message types, newline-delimited framing
pkg/proxy/                   the stdio wrap proxy and the pipeline
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

## The proxy

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

HTTP and SSE MCP servers are not proxied.

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
| `gremlyn wrap [flags] -- <server command>` | run an MCP server behind the proxy; `--chaos-*` flags enable gremlins |
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
