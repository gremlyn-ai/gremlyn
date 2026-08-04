---
name: gremlyn-to-design
description: >
  Export a live the dashboard page into Claude Design or Figma as a self-contained
  static snapshot, so it can be reworked visually without touching code. Use when the user
  runs /gremlyn-to-design [PAGE], or says "pousse cette page dans Claude Design",
  "exporte le dashboard en design", "snapshot cette page", "je veux retravailler ce
  design". Inverse of /design-to-gremlyn.
---

# Gremlyn Dashboard → Design

Take a live dashboard page and export it as a **self-contained static snapshot** into Claude Design (or a Figma file), so it can be redesigned without touching the codebase.

**Mechanism:** capture the rendered page (browser automation on the running dev server), inline everything it needs (CSS, fonts as needed, data as static markup), and push it as a standalone HTML file to the design tool. The result must render identically with **no network, no JS, no API**.

## ⛔ READ-ONLY on the codebase

This skill **reads** the dashboard and **writes** to the design tool. It never modifies the repo.

- No edits to `dashboard/` — not components, not styles, not config.
- No edits to Go code.
- `reference/*.html` is **not** the export target — those are the canonical spec and are read-only (the `PreToolUse` hook blocks edits). A snapshot is a new artifact, not a replacement for them.

## ⛔ The data problem — read this before capturing

The dashboard displays **real inspected MCP traffic**. That means real tool call arguments, real credentials in flight, real customer data, real captured injection payloads.

**Never export real event data to a design tool.** A design tool is an external service; pushing a snapshot there publishes whatever is on screen, and it may be cached or indexed even if deleted later.

Before capturing, do one of these:

1. **Preferred — seed synthetic data.** Point the dashboard at a fresh DB with fabricated events:
   ```bash
   mv ~/.gremlyn/shield.db ~/.gremlyn/shield.db.real     # keep the real one safe
   # start shield, generate synthetic traffic against a test MCP server
   ```
2. **Or scrub after capture** — replace every payload, hostname, path, key fragment, and identifier in the captured HTML with obvious placeholders (`acme-corp`, `tool_result_placeholder`, `sk-REDACTED`).

**Confirm with the user which path was taken, and show them the scrubbed content before pushing.** If you cannot confirm the data is synthetic or scrubbed, do not export.

## Workflow

### 1. Bring the page up with safe data
```bash
cd Shield    && go run ./cmd/shield      # :8081
cd Arena     && go run ./cmd/arena       # :8082
cd dashboard && npm run dev              # :3000
```

Target routes: `/shield`, `/shield/rules`, `/shield/events`, `/arena`, `/arena/sessions`, `/arena/sessions/[id]`, `/servers`, `/settings`.

### 2. Capture the rendered page
Load the browser tools (`claude-in-chrome` skill), navigate to the route, and capture:
- A **screenshot** at a realistic viewport (and note the breakpoint).
- The **rendered DOM** and the **computed styles** needed to reproduce it.

### 3. Build the self-contained snapshot
The output is one HTML file that renders with no network:
- **Inline all CSS.** Tailwind is compiled — extract the classes actually used rather than shipping the whole framework.
- **Inline the fonts** or fall back to a documented system stack, and say which. Space Grotesk / Inter / JetBrains Mono come from Google Fonts, so a strict-CSP snapshot can't fetch them.
- **Material Symbols is a font too** — icons render as boxes without it. Either inline it or substitute inline SVG and note the substitution.
- **Freeze the data as static markup.** No fetch, no JS, no WebSocket.
- **Include the scanline overlay** — it's part of the identity.

### 4. Capture the states, not just the ideal one
A snapshot of only the populated happy path is the least useful thing to redesign. Export the states that actually need design work:
- **Empty** (the fresh-install default — the highest-value one to rework)
- **Loading**
- **Error / partial** (one service down)
- **Live / streaming**, if the page has it

Name the files so the state is obvious: `shield-events-empty.html`, `arena-session-live.html`.

### 5. Push to the design tool
- **Claude Design**: `DesignSync` write methods, one file per state.
- **Figma**: load the `figma-use` skill first (mandatory before any `use_figma` call), and `figma-generate-design` for the layout workflow.

### 6. Hand off with the constraints attached
A designer reworking this needs to know what is fixed. Include a note with the snapshot:

```markdown
## GREMLYN_OS constraints (do not break)
- Dark mode ONLY. No light mode.
- border-radius: 0 everywhere. Pills (9999px) are the only exception.
- Green #8eff71 = Shield · Red #ff7168 = Arena. Never cross them.
- Material Symbols Outlined only.
- Space Grotesk (headlines) · Inter (body) · JetBrains Mono (data/labels/tables).
- Labels uppercase, mono, tight/wide tracking.
- Contrast ≥4.5:1; severity never encoded by colour alone.
- Precision beats charm in anything the user acts on (block reasons, scores, errors).

## Free to rework
- Layout, spacing, hierarchy, density
- Which data is emphasised
- Empty / error / loading state treatment
- New patterns, if they respect the invariants above
```

## Verification

- [ ] Snapshot renders with **no network** (open the file offline)
- [ ] No real payloads, hostnames, credentials, or identifiers — **confirmed with the user**
- [ ] Icons and fonts resolve, or the substitution is documented
- [ ] All relevant states exported, named clearly
- [ ] Section accent correct per page
- [ ] Zero files in the repo modified
- [ ] Constraints note attached

## When NOT to use
- Bringing a reworked design **back** into the app → `/design-to-gremlyn`
- Building from the existing reference HTML → `/dashboard-from-reference`
