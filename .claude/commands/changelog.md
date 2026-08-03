# Generate Weekly Changelog

Generate the weekly changelog based on commits since the last one, across all four Gremlyn repos.

## Instructions

1. List files in `.changelogs/` to find the most recent **weekly** changelog — the latest pure `YYYY-MM-DD.md` (ignore feature-suffixed ones like `2026-08-01-rugpull.md`). That date is the period START.
2. Get today's date for the period END.
3. Collect commits **from all four repos** (see below).
4. Read `.changelogs/INSTRUCTIONS.md` if it exists for any project-specific formatting.

## Collecting commits — four repos, no monorepo ⚠️

This is the part people get wrong. There is no single `git log` that covers this project.

```bash
mkdir -p /tmp/gremlyn-changelog
for d in gremlyn-core gremlyn-shield gremlyn-arena gremlyn-dashboard; do
  git -C "$d" log --since="<start> 00:00" --no-merges \
    --pretty="$d|%h|%ad|%s" --date=short \
    >> /tmp/gremlyn-changelog/all_commits.txt 2>/dev/null
done
sort -t'|' -k3 /tmp/gremlyn-changelog/all_commits.txt
wc -l < /tmp/gremlyn-changelog/all_commits.txt   # true commit count
```

Then **read that file with the Read tool**, not `cat`/`grep`, so nothing gets truncated.

### Two gotchas

1. **A date is not a git ref.** `2026-08-01..HEAD` fails — `2026-08-01` is not a revision. Use `--since`. Do **not** add `--until`; it can clip same-day commits.
2. **One logical change appears as N commits across N repos.** A core `pkg/` change plus its two consumer bumps is *one* feature and three commits. Group by feature in the changelog, not by repo — but name the repos, because "which repo" is the information a reader actually needs.

### Coverage check
```bash
for d in gremlyn-core gremlyn-shield gremlyn-arena gremlyn-dashboard; do
  echo -n "$d: "; git -C "$d" log --since="<start> 00:00" --no-merges --oneline | wc -l
done
```
A changelog missing a repo is wrong — re-collect.

## Also detect

- **New tags** (a release happened): `for d in …; do git -C "$d" tag --sort=-creatordate | head -3; done`
- **New research**: `git -C <repo> log --since="<start>" --name-only --pretty=format: -- docs/Research/ | sort -u`
- **Migrations added**: any new file under `migrations/` or a new entry in `internal/storage/sqlite/migrations.go`

## Changelog Structure

- **Intro** — total commit count, per-repo breakdown, the week's headline threads
- **⚠️ Breaking changes** — first section if any exist. Every `pkg/` API change goes here with the version chain (`core v0.4.0 → shield v0.3.1, arena v0.3.1`). A score-weight change goes here too, because it breaks historical comparability
- **Releases** — any tags cut, per repo
- **Shield** — detection, policy, behavioral, alerting. Note any **false-positive impact** — that's the user-visible one
- **Arena** — gremlins, sessions, scoring. Note any **scoring change** and whether old scores stay comparable
- **Core** — proxy, protocol, pipeline, CLI, config
- **Dashboard** — pages, components, UX
- **Infrastructure & Performance** — build, CI, release, perf work (with the before/after numbers)
- **Migrations** — schema changes, both stores, and whether a user's local DB migrates automatically on next launch
- **Documentation** — new or updated docs, new research
- **Stats footer** — commit count per repo, focus areas, milestones

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
