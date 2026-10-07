# Gremlyn Arena — GitHub Action

Breaks your AI agent on purpose on every pull request, and reports how it copes:

- a **"Gremlyn" check** on the PR, with the verdict in its title, the full report in the
  Checks tab, and an annotation on the config line of each failing scenario;
- **one PR comment**, updated in place on every push;
- the same report in the **job summary**, plus the JSON report as an artifact.

A ready-to-copy workflow is in [`examples/workflows/gremlyn.yml`](../examples/workflows/gremlyn.yml):

```yaml
name: Gremlyn
on:
  pull_request:
  push:
    branches: [main]   # the run on main is the baseline PRs are compared with

permissions:
  contents: read
  actions: read        # download the baseline report
  checks: write        # publish the Gremlyn check
  pull-requests: write # comment on the PR

jobs:
  arena:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      # ...whatever your agent needs to run, and its API key...
      - uses: gremlyn-ai/gremlyn/action@v0.1.0
        with:
          config: .gremlyn/arena.yaml
```

## Report first, gate later

By default the action **reports and does not block**. A missed threshold makes the check
*neutral*, not red. An LLM agent is not deterministic: the same scenario can score
differently on two runs, and a gate on a single run turns into a flaky check that people
disable. Watch the delta against `main` for a while, then set `fail-on-threshold: "true"`:
the check turns red and the job fails when a threshold is missed.

## What the check says

The title is a one-line verdict, for example:

```text
1 of 3 scenarios below threshold · 1 regression vs base
```

The report shows one row per scenario. Then, for each failing one, what the agent did and
what is usually missing in its code:

| | Scenario | Gremlins | Score | Δ vs base | Measured |
|---|---|---|---:|---:|---|
| ❌ | corrupted-result | corruption | **10** critical | 📉 -80 | 1/1 |
| ✅ | injected-instructions | injection | 90 excellent | = | 1/1 |
| ⚠️ | new-scenario | timeout | not measured | new | 0/0 |

- **Measured** is `observed/injected`. A `0/0` scenario never called a tool, so nothing was
  tested. It shows ⚠️, as a setup problem, and never as a score.
- **📉** marks a drop of 10 points or more against the base branch. Smaller moves are within
  normal run-to-run variation.

Render the same report locally with `gremlyn arena report report.json --baseline base.json`.

## Fixing what it found

Run `/gremlyn:chaos-test` in Claude Code with the gremlyn plugin installed. To automate it,
[`examples/workflows/gremlyn-fix.yml`](../examples/workflows/gremlyn-fix.yml) runs the skill
unattended when a maintainer labels a PR `gremlyn-fix`, and opens a second PR with the fixes.

## Inputs

| Input | Default | Description |
|---|---|---|
| `config` | `.gremlyn/arena.yaml` | Scenario config |
| `version` | `latest` | Gremlyn version to install, or `path` to use a `gremlyn` already on `PATH` |
| `min-score` | — | Overrides `thresholds.min_overall` without editing the config |
| `report` | `gremlyn-report.json` | Where the JSON report is written |
| `check-run` | `true` | Publish the "Gremlyn" check; needs `checks: write` |
| `comment-on-pr` | `true` | Post and update the PR comment; needs `pull-requests: write` |
| `baseline` | — | A previous report to compare with. Empty: the last successful run of this workflow on the base branch; needs `actions: read` |
| `fail-on-threshold` | `false` | Fail the job, and make the check red, when a threshold is missed |
| `github-token` | `github.token` | Token for the baseline, the check and the comment |

A missing permission never fails the job. The action logs a notice and skips that output.

## Outputs

| Output | Description |
|---|---|
| `passed` | `true` when every scenario met its thresholds |
| `score` | Lowest overall resilience score across scenarios |
| `title` | The one-line verdict |
| `report` | Path to the JSON report |

## Under the hood

The action installs `gremlyn` and verifies the release before running it. It checks the
cosign signature when cosign is on the runner, and always checks the checksum. Then it calls
`gremlyn arena ci`. That command launches your agent headlessly, once per scenario, with its
MCP server behind the gremlyn proxy, and scores how the agent reacts to each injected
failure. No service and no database are involved: everything happens inside the job.

Your agent must be launchable headlessly and accept an MCP config path. In practice,
`agent.command` contains `{{mcp_config}}`. The action refuses a config without it, because
an agent that keeps its own MCP servers never crosses a gremlin. See the
[main README](../README.md) for the config format.
