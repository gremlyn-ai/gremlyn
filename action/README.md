# Gremlyn Arena — GitHub Action

Inject controlled failures into your AI agent's MCP tools on every pull request,
and fail the build when it gets less resilient.

```yaml
name: Agent resilience

on: pull_request

permissions:
  contents: read
  pull-requests: write   # only needed for comment-on-pr

jobs:
  arena:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: gremlyn-ai/gremlyn/action@v1
        with:
          config: .gremlyn/arena.yaml
          min-score: 70
```

## Adopting this on an existing project

Start without the gate. Get a few runs of real data before you pick a number,
because a threshold chosen by guessing will either never fire or fire constantly,
and a check that cries wolf gets deleted.

```yaml
      - uses: gremlyn-ai/gremlyn/action@v1
        with:
          fail-on-threshold: false   # report the score, don't block
```

The score still appears in the job summary and as a PR comment. Turn the gate on
once you know what your agent normally scores.

## Inputs

| Input | Default | Description |
|---|---|---|
| `config` | `.gremlyn/arena.yaml` | Scenario config |
| `version` | `latest` | Gremlyn version to install |
| `min-score` | — | Overrides `thresholds.min_overall`, so a workflow can tighten the bar without editing the config |
| `report` | `gremlyn-report.json` | Where the JSON report is written |
| `comment-on-pr` | `true` | Post the result as a PR comment, updating it in place rather than adding one per push |
| `fail-on-threshold` | `true` | Set `false` to report without gating |

## Outputs

| Output | Description |
|---|---|
| `passed` | `true` when every scenario met its thresholds |
| `score` | Lowest overall resilience score across scenarios |
| `report` | Path to the JSON report |

The report is always uploaded as an artifact, including on failure — the event log
is where you look to find out *why* a score dropped.

## Reading the result

```
| Scenario           | Score | Grade | Coverage | Status |
|--------------------|------:|-------|----------|--------|
| unresponsive-tool  |    40 | D     | 2/2      | ✅     |
| corrupted-result   |    10 | F     | 1/1      | ❌     |
```

**Coverage is the column to read first.** It is `observed/injected`. A scenario
showing `0/0` injected nothing, which means the agent never called a tool and
nothing was tested — that fails rather than passing, because a green check that
tested nothing is worse than no check.

## What it does under the hood

The action installs `gremlyn`, **verifies the release checksum** before running it,
and calls `gremlyn arena ci`. That command launches your agent headlessly with its
MCP server proxied through gremlyn in chaos mode, watches how the agent reacts to
each injected failure, and scores it.

No service and no database are involved. Everything happens inside the job.

## Requirements

Your agent must be launchable headlessly, and its MCP servers must be
configurable. In practice that means the `agent.command` in your config contains
`{{mcp_config}}` — the action refuses a config without it, because an agent that
keeps its own MCP servers never crosses a gremlin and the run would measure
nothing while appearing to pass.

See the [main README](../README.md) for the config format.
