---
name: design-to-gremlyn
description: >
  Pull a design from Claude Design (claude.ai/design) or Figma and implement it in the
  the dashboard as real Next.js + TypeScript, mapped onto the existing GREMLYN_OS
  components and tokens. Use when the user runs /design-to-gremlyn [URL] [PAGE], or says
  "implémente ce design", "pousse ce design dans le dashboard", "design to code",
  "code cette page". URL is a claude.ai/design/p/<projectId> link or a figma.com URL;
  PAGE is the target route. Inverse of /gremlyn-to-design.
---

# Design → Gremlyn Dashboard

Take a design captured in Claude Design or Figma and ship it as a real, wired-up page in the dashboard, built on the existing GREMLYN_OS system.

**Mechanism:** read the design (`DesignSync` read methods for Claude Design; `get_design_context` / `get_screenshot` via the Figma tools — load the `figma-design-to-code` skill first for that path), analyse the layout, **map each visual element to an existing component or token**, then build the page in Next.js + TS keeping its real hooks and API calls.

## ⛔ FRONTEND ONLY — hard boundary

This skill touches **only `dashboard/`**. It is a pure presentation change.

- **NEVER** edit Go code — no `pkg/`, `internal/shield/`, `internal/arena/`. No handlers, services, repositories, migrations, config.
- **NEVER** change an API contract, endpoint path, or response shape. The design changes how data is *displayed*, never what is fetched.
- Existing hooks (`lib/hooks/`), API methods (`lib/api/`), stores (`lib/stores/`), and types are **read-only inputs** — reuse them verbatim.
- If the design seems to need a field the API doesn't return: do **not** add it backend-side. Mock or omit it in the frontend and **flag it to the user** as a follow-up ticket.
- If the work genuinely requires a Go change to look right, **stop and tell the user** instead of editing Go.

Every file written by this skill lives under `dashboard/`.

## ⚠️ The reference HTML still wins

`dashboard/reference/{arena,dashboard}.html` remain the **canonical GREMLYN_OS spec**. They are read-only (the `PreToolUse` hook blocks edits).

If the incoming design conflicts with them, that is a **product decision, not an implementation detail**:

1. Name the conflict explicitly (e.g. "this design uses rounded cards and a light surface").
2. Ask the user which wins.
3. Do not silently break an invariant to match a mockup.

The invariants that can't be quietly overridden: **dark-only · `border-radius: 0` (pills excepted) · green = Shield / red = Arena · Material Symbols only · Space Grotesk / Inter / JetBrains Mono**. Full list: `.claude/rules/front/design-system.md`.

## Ground truth to read before mapping

- **Reference**: `reference/arena.html` (red, Arena) · `reference/dashboard.html` (green, Shield)
- **Tokens**: `app/globals.css` (@theme), `lib/theme.ts` — use the token, never the hex
- **Existing components to reuse**: `components/{Sidebar,TopBar,StatusFooter,GlobalTerminal,ClientShell}.tsx`, `app/shield/components/*`, `app/arena/components/*`
- **Data layer**: `lib/api/{client,shield,arena,types}.ts`, `lib/hooks/*`, `lib/stores/*`
- **Rules**: `.claude/rules/front/design-system.md`, `.claude/rules/front/lint-typecheck.md`

## Workflow

### 1. Read the design
- Claude Design: `DesignSync` read methods to pull the self-contained HTML file(s).
- Figma: load the `figma-design-to-code` skill, then `get_design_context` / `get_screenshot`.
- Note the intended route and which section it belongs to (Shield or Arena) — that fixes the accent colour.

### 2. Map, don't reinvent
For every visual element, find the existing equivalent **before** writing new markup:

| In the design | Map to |
|---|---|
| A stat tile | the `MetricCards.tsx` pattern |
| A data table | the `RuleTable.tsx` / event-list pattern (`font-mono text-xs`, subtle `divide-y`) |
| A card | `bg-surface-container-low`, no radius, `border-l-2` accent on hover |
| A toggle | the existing flat toggle |
| A chart | Chart.js as in `ThreatChart.tsx`; constrain the palette to the section accent |
| A log / terminal panel | `TerminalPanel.tsx` / `GlobalTerminal.tsx` |
| A modal | `NewRuleModal.tsx` |
| Any colour | the nearest existing token — **never a new hex** |
| Any icon | a Material Symbols name |

Produce the mapping table **before** coding, and list anything genuinely new. A new token or a new pattern is a system decision — justify why the existing set didn't cover it.

### 3. Place files correctly
```
app/<section>/page.tsx              → route entry ONLY
app/<section>/error.tsx             → MANDATORY
app/<section>/components/X.tsx      → section-scoped
components/X.tsx                    → only if a SECOND section uses it
lib/constants/<domain>.ts           → labels, colour maps, thresholds
lib/utils/<domain>.ts               → pure transforms
lib/hooks/use<Thing>.ts             → hooks (never inside a .tsx)
```

### 4. Wire the real data
- Reuse the existing hook / API method / store. Do not write a new fetch path for a design change.
- Types from `lib/api/types.ts`. **No `any`.**
- No `fetch` in a component. No `useEffect` for fetching.

### 5. All states, not just the mockup
A design file shows the ideal state. Ship all of them:
1. **Empty** — the fresh-install default; Shield-empty must read as "watching", not "broken"
2. **Loading**
3. **Error / partial** — **Shield and Arena fail independently**; per-section degradation, never a global "API down"
4. **Live / streaming** — healthy vs stalled vs dropped must be distinguishable

### 6. Security + accessibility constraints
- **No `dangerouslySetInnerHTML`** — captured payloads are attacker-controlled. Render as text in `<pre>`/`<code>`.
- Contrast ≥4.5:1 on every new pairing; **severity never encoded by colour alone**.
- Visible focus states; respect `prefers-reduced-motion`.
- Bound any retained log/terminal buffer.

### 7. Tests
- Beside the code or in `__tests__/`, matching the neighbours.
- MSW per service, so a Shield failure can be simulated independently.
- Cover the states and the interactions.

## Done means

```bash
cd dashboard
npm run typecheck && npm run lint && npx vitest run && npm run build
```

Plus:
- [ ] Mapping table produced; existing components reused
- [ ] No invariant broken (or the conflict was raised and the user decided)
- [ ] Correct section accent
- [ ] Tokens, not hexes
- [ ] All states implemented
- [ ] Zero Go files touched
- [ ] No API contract changed
- [ ] Any missing-field gap flagged to the user

Then run it against a live stack (shield :8081, arena :8082) and compare side by side with both the design and the reference HTML — including with one service stopped.

## When NOT to use
- Snapshotting a live dashboard page **to** a design tool → `/gremlyn-to-design`
- Building from the reference HTML with no external design → `/dashboard-from-reference`
