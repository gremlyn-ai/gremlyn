# Auto-Generate Commit Message

Generate a commit message for staged changes and commit them.

## ⚠️ Four repos — figure out which one first

Gremlyn is four independent git repositories. Before anything else:

```bash
pwd
git rev-parse --show-toplevel   # which repo am I actually in
```

If changes span multiple repos, you need **one commit per repo**. There is no atomic cross-repo commit. Handle them one at a time, and use a shared subject suffix so they're findable together later.

## Instructions

1. `git status --short` — confirm the repo and what's staged vs unstaged
2. `git diff --cached --stat` — see the staged files
3. `git diff --cached` — read the actual changes
4. `git log --oneline -10` — match this repo's existing message style

## Commit Message Guidelines

- **Conventional Commits**: `type: description`
- Types: `feat`, `fix`, `refactor`, `docs`, `style`, `test`, `chore`, `perf`
- First line under 72 characters, imperative mood ("add" not "added")
- Focus on **why**, not just what
- Body only when the why isn't obvious from the subject

## Gremlyn-specific message rules

- **A `pkg/` API change is breaking for two consumers.** Say so in the body and name the version:
  ```
  feat: add short-circuit support to the pipeline contract

  BREAKING: Pipeline.Run now returns (Decision, error). gremlyn-shield
  and gremlyn-arena must bump to v0.4.0.
  ```
- A schema change mentions **both stores**: `feat: add confidence to events (sqlite + postgres)`. A message that names only one store hides half the change.
- A detection change states the FP impact: `fix: narrow base64 injection pattern to cut false positives on UUIDs`.
- A score weight change is breaking for comparability: say `BREAKING: existing resilience scores are no longer comparable`.
- Never mention an agent, a skill, or the workflow in a commit message.

## Pre-commit sanity

If the diff is Go, confirm the gate ran (`make check`). If it's dashboard, confirm `npm run typecheck` and `npm run build`. Don't run them unasked, but if the user hasn't and the diff is non-trivial, say so before committing.

## Output

1. Show the generated commit message
2. Ask for confirmation before committing
3. If confirmed, run the commit — **in the correct repo**
4. If other repos have related staged changes, say so and offer to handle them next

## Start

Analyze my staged changes and generate a commit message.
