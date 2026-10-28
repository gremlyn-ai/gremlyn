# 2026-10-28 — Artifacts you can read: agent.log, answer.txt, --artifacts-dir

**Type**: feature

**Summary**: After a bad score there was nothing to read: the agent's output lived in memory and only a 400-character tail reached one failure message. Each scenario run now writes `agent.log` and `answer.txt` next to its events, and `--artifacts-dir <path>` keeps them where a CI job can upload them.

**Impact**:
- Core (`pkg/…`): none
- Arena (`internal/arena/…`): none
- CLI / plugin / Action: `arena ci --artifacts-dir` (implies `--keep-artifacts`); `agent.log` and `answer.txt` (0600) in every scenario directory; the text output prints the kept directory. The Action gains an `artifacts-dir` input that uploads `gremlyn-artifacts`, off by default
- Formats: `scenarios[].artifacts` and `runs[].artifacts` in the report when kept. Logs never enter the JSON
- Score comparability: unchanged

**For users**: `gremlyn arena ci --artifacts-dir out/`, then read `out/<scenario>-*/agent.log`. In the Action, `artifacts-dir: gremlyn-artifacts`; leave it off when the agent may print data that the workflow's readers should not see.

**For developers**: `runScenario` takes an `artifacts{keep, dir}` instead of a bool.
