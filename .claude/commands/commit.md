# Auto-Generate Commit Message

Generate a commit message for staged changes and commit them.

## One repo, one commit

Gremlyn is a single git repository and a single Go module (`github.com/gremlyn-ai/gremlyn`). A change that spans `pkg/`, `internal/shield/`, `internal/arena/` and `dashboard/` is **one commit** — that's the point of the merge. Don't carve it up by area.

## Instructions

1. `git status --short` — what's staged vs unstaged
2. `git diff --cached --stat` — see the staged files
3. `git diff --cached` — read the actual changes
4. `git log --oneline -10` — match the existing message style

## Commit Message Guidelines

- **Conventional Commits**: `type: description`
- Types: `feat`, `fix`, `refactor`, `docs`, `style`, `test`, `chore`, `perf`
- First line under 72 characters, imperative mood ("add" not "added")
- Focus on **why**, not just what
- Body only when the why isn't obvious from the subject

## Gremlyn-specific message rules

- **A `pkg/` API change is an internal refactor now** — both consumers move in the same commit, so there is nothing to bump. Say what moved and name the call sites that followed:
  ```
  feat: add short-circuit support to the pipeline contract

  Pipeline.Run now returns (Decision, error) so a stage can stop a
  message. Shield's policy engine and Arena's session runner updated
  with it.
  ```
  Reserve `BREAKING:` for what a **user** sees — a config key, a CLI flag, an API response shape, a score.
- A schema change mentions **both stores**: `feat: add confidence to events (sqlite + postgres)`. A message that names only one store hides half the change.
- A detection change states the FP impact: `fix: narrow base64 injection pattern to cut false positives on UUIDs`.
- A score weight change is breaking for comparability: say `BREAKING: existing resilience scores are no longer comparable`.
- Never mention an agent, a skill, or the workflow in a commit message.

## Pre-commit sanity

If the diff is Go, confirm the gate ran (`make check` at the repo root — one run covers `pkg/` and both services). If it's dashboard, confirm `make dashboard-check`. Don't run them unasked, but if the user hasn't and the diff is non-trivial, say so before committing.

## Output

1. Show the generated commit message
2. Ask for confirmation before committing
3. If confirmed, run the commit
4. If related changes are still unstaged, say so — they probably belong in this same commit

## Start

Analyze my staged changes and generate a commit message.
