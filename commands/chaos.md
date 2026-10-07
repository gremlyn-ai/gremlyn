---
description: Run the gremlyn chaos scenarios against your agent and report the scores, without changing any code
argument-hint: "[scenario-name ...]"
allowed-tools: Bash, Read
---

Run the chaos scenarios in `.gremlyn/arena.yaml` and report the result. Do not edit any file.

If `.gremlyn/arena.yaml` does not exist, stop and tell the user to run `/gremlyn:chaos-test`,
which writes the file for their agent.

Each scenario launches the user's agent once, so it costs real LLM calls. Say so in one line,
then run:

```bash
CLAUDE_PLUGIN_DATA="${CLAUDE_PLUGIN_DATA}" "${CLAUDE_PLUGIN_ROOT}/scripts/gremlyn" arena ci \
  --config .gremlyn/arena.yaml --format json --out .gremlyn/report.json $SCENARIO_FLAGS
```

Build `$SCENARIO_FLAGS` from the arguments: one `--scenario <name>` per name in
"$ARGUMENTS". With no arguments, run every scenario.

A non-zero exit means a threshold was missed. That is a result, not a failure of the command.

Then render the result exactly as the pull-request check shows it, and print that output:

```bash
"${CLAUDE_PLUGIN_ROOT}/scripts/gremlyn" arena report .gremlyn/report.json
```

Add one sentence per failing scenario on what the agent did wrong. A scenario with zero
injections measured nothing: say it is a setup problem, not a score.

End by offering `/gremlyn:chaos-test` to fix what failed.
