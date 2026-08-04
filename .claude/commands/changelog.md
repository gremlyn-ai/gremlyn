# Generate Weekly Changelog

Generate the weekly changelog based on commits since the last one.

## Instructions

1. List files in `.changelogs/` to find the most recent **weekly** changelog — the latest pure `YYYY-MM-DD.md` (ignore feature-suffixed ones like `2026-08-01-rugpull.md`). That date is the period START.
2. Get today's date for the period END.
3. Collect the commits (see below).
4. Read `.changelogs/INSTRUCTIONS.md` if it exists for any project-specific formatting.

## Collecting commits — one repo, one log

Gremlyn is a single git repository, so one `git log` covers the whole project.

```bash
mkdir -p /tmp/gremlyn-changelog
git log --since="<start> 00:00" --no-merges \
  --pretty="%h|%ad|%s" --date=short --reverse \
  > /tmp/gremlyn-changelog/all_commits.txt
wc -l < /tmp/gremlyn-changelog/all_commits.txt   # true commit count
```

Then **read that file with the Read tool**, not `cat`/`grep`, so nothing gets truncated.

### Two gotchas

1. **A date is not a git ref.** `2026-08-01..HEAD` fails — `2026-08-01` is not a revision. Use `--since`.
2. **Never add `--until`.** It clips same-day commits, so the most recent work silently disappears from the changelog.

### Mapping commits to sections

The sections below are **areas**, and areas are directories. Get them from the log:
```bash
git log --since="<start> 00:00" --no-merges --name-only --pretty=format:'%h|%s'
```
`pkg/`, `cmd/`, `internal/cli/` → Core · `internal/shield/` → Shield · `internal/arena/` → Arena · `dashboard/` → Dashboard · `migrations/` + `internal/*/storage/` → Migrations · `Makefile`, `.golangci.yml`, CI → Infrastructure · `docs/` → Documentation.

## Also detect

- **New tags** (a release happened): `git tag --sort=-creatordate | head -3` — one tag now covers the whole module
- **New research**: `git log --since="<start>" --name-only --pretty=format: -- docs/ | sort -u`
- **Migrations added**: any new file under `migrations/{shield,arena}/` or under `internal/{shield,arena}/storage/sqlite/migrations/`

## Changelog Structure

- **Intro** — total commit count, per-area breakdown, the week's headline threads
- **⚠️ Breaking changes** — first section if any exist. What a **user** sees breaking: a config key, a CLI flag, an API response shape, a migration. A score-weight change goes here too, because it breaks historical comparability. A `pkg/` refactor whose consumers moved in the same commit is *not* a breaking change
- **Releases** — the tag cut, if any (one tag, whole module)
- **Shield** — detection, policy, behavioral, alerting. Note any **false-positive impact** — that's the user-visible one
- **Arena** — gremlins, sessions, scoring. Note any **scoring change** and whether old scores stay comparable
- **Core** — proxy, protocol, pipeline, CLI, config
- **Dashboard** — pages, components, UX
- **Infrastructure & Performance** — build, CI, release, perf work (with the before/after numbers)
- **Migrations** — schema changes, both stores, and whether a user's local DB migrates automatically on next launch
- **Documentation** — new or updated docs, new research
- **Stats footer** — commit count per area, focus areas, milestones

## Output

1. Save the markdown changelog to `.changelogs/YYYY-MM-DD.md` (today's date).
2. Output a **short shareable version** in the conversation — plain text, non-technical, **in French**, for posting somewhere or for the user's own notes:
   - **No markdown markup** — no `#`, no `*bold*`, no backticks. They don't render on paste.
   - **No Unicode bold/italic trick characters** — they render badly in a terminal.
   - **Use emoji for hierarchy**: 📊 intro, 🛡️ Shield, 🔥 Arena, ⚙️ Core, 🖥️ Dashboard, 🔬 research, 🩹 fixes, ⚠️ breaking, 🎉 sign-off. Number emoji (1️⃣ 2️⃣ …) for the headline threads.
   - Convey structure with emoji + plain text: `•` bullets, blank lines between sections, ALL-CAPS or a trailing `:` for light emphasis.
   - Output it inside a fenced code block so it copies cleanly.
3. If a breaking change or a migration shipped, mention it in **both** versions — that's the part that costs someone time if they miss it.

## Start

Generate the weekly changelog now.
