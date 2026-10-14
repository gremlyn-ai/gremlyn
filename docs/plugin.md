# The Claude Code plugin

The repository root is the plugin. Claude Code reads four places:

```
.claude-plugin/plugin.json        name, version, description
.claude-plugin/marketplace.json   lets `/plugin marketplace add gremlyn-ai/gremlyn` find it
skills/chaos-test/SKILL.md        /gremlyn:chaos-test, the fix loop
commands/chaos.md                 /gremlyn:chaos, run and report only
scripts/gremlyn                   the launcher both of them call
```

## The launcher

`scripts/gremlyn` runs the `gremlyn` binary for the skill and the command:

1. A `gremlyn` already on `PATH` wins, so a source build or a `go install` is used as is.
2. Otherwise it reads the version from `plugin.json`, runs `install.sh` for that exact tag,
   and installs into `${CLAUDE_PLUGIN_DATA}/bin`. `install.sh` verifies the checksum, and the
   cosign signature when cosign is present.
3. Later calls reuse that binary.

The plugin version and the release tag must therefore be equal. `GREMLYN_FORCE_DOWNLOAD=1`
skips step 1, to test the download path.

## The skill

`/gremlyn:chaos-test` is the loop around `gremlyn arena ci`:

1. Find or write `.gremlyn/arena.yaml`, reading the repository to learn how the agent runs
   headlessly and which MCP server it uses.
2. Run the scenarios.
3. Read the report: coverage first, then the score per scenario.
4. Fix the agent, with the fix that matches each gremlin.
5. Re-run only the failed scenarios, with `--scenario`.

When the report shows a hijacked call (a destructive tool called right after an injected
instruction) or a duplicate write (a non-idempotent write called again after a timeout), the
skill also proposes the server-side fix, if the user owns the server: a confirmation step or
a narrower credential for the destructive tool, an idempotency key for the write, correct
tool annotations. It applies a server change only with the user's agreement; with `--apply` it
writes it as a proposal in the summary. When it writes a new scenario file for a server whose
tools carry no annotations, it declares under `mcp_server.annotations` only what the user
confirms or the server's code states.

It never edits the test to make it pass: no lowered threshold, no removed scenario, no
softened prompt, no tool re-declared to change its score.

| Argument | Effect |
|---|---|
| `--apply` | unattended: never asks, applies the fixes, re-runs; for `claude -p` and CI |
| `--scenario <name>` | work only on these scenarios |

Without `--apply`, the skill confirms before the first run, which costs LLM calls, and
shows each fix before applying it.

## Testing the plugin locally

```bash
make build
export PATH="$PWD/bin:$PATH"
make plugin-check
claude --plugin-dir "$PWD"
```

Or install it the way a user would, from the checkout:

```bash
claude plugin marketplace add ./
claude plugin install gremlyn@gremlyn
```

`examples/demo-agent` is a deliberately fragile agent to run the skill against.
