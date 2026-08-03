---
name: dashboard-from-reference
description: Build or refactor a gremlyn-dashboard page or component so it matches the GREMLYN_OS reference HTML prototypes exactly — reading reference/arena.html or reference/dashboard.html first, then wiring real API data, all four states, and tests. Use when the user says "build this page", "match the reference", "implement the design", "refactor this component", or runs /dashboard-from-reference.
---

# Dashboard From Reference

Build a dashboard page or component that matches the GREMLYN_OS prototypes exactly, wired to real Shield/Arena data.

## ⛔ Step 0 — Read the reference. Every time.

```
dashboard/reference/arena.html      → Arena pages (chaos testing, RED accent)
dashboard/reference/dashboard.html  → Shield pages (firewall, GREEN accent)
```

These are the **single source of truth for every visual decision** and they are **READ-ONLY** — the `PreToolUse` hook blocks edits to them. Open the relevant one and find the closest existing pattern before writing a line of TSX. Do not design from the description in this file; the HTML is the spec.

## ⛔ Frontend only — hard boundary

This skill touches **only `dashboard/`**. It is a pure presentation change.

- **Never** edit Go code — no `gremlyn-core/`, `gremlyn-shield/`, `gremlyn-arena/`. No handlers, services, repositories, migrations.
- **Never** change an API contract, endpoint path, or response shape. The design changes how data is *displayed*, never what is fetched.
- Existing hooks, `lib/api/*`, and stores are **read-only inputs** — reuse them verbatim.
- If the design appears to need a field the API doesn't return: **stop and tell the user.** Do not add it backend-side. Mock or omit it in the frontend and flag it as a follow-up.

If the work genuinely requires a Go change, hand it to `go-backend-developer` as a separate ticket.

## Ground truth to read before mapping

- Existing components to extend: `components/{Sidebar,TopBar,StatusFooter,GlobalTerminal,ClientShell}.tsx`, `app/shield/components/*`, `app/arena/components/*`
- Tokens: `tailwind.config.ts` and `lib/theme.ts` — **use the token, never the hex**
- API client: `lib/api/{client,shield,arena,types}.ts`
- Hooks: `lib/hooks/{useShieldData,useArenaData,useArenaWebSocket}.ts`
- Stores: `lib/stores/{shieldStore,arenaStore,terminalStore}.ts`
- Full rules: `.claude/rules/front/design-system.md` and `.claude/rules/front/lint-typecheck.md`

## The invariants (a violation is a bug, not a preference)

- **Dark mode ONLY.** No light mode, no `dark:` variants, no toggle.
- **`border-radius: 0`** everywhere. Pills (`rounded-full`) are the only exception.
- **Green `#8eff71` = Shield · Red `#ff7168` = Arena.** Never cross an accent.
- **Material Symbols Outlined only**: `<span className="material-symbols-outlined">bolt</span>`.
- **Space Grotesk** headlines · **Inter** body · **JetBrains Mono** data/labels/tables.
- Tailwind utilities only. No custom CSS beyond the scanline and the slider thumb.
- Labels uppercase, `font-mono`, `text-xs`.
- **No emoji in UI copy.**
- **No `dangerouslySetInnerHTML`** — event payloads are attacker-controlled; that's stored XSS. Render captured content as text in `<pre>`/`<code>`.

## Workflow

### 1. Locate the pattern in the reference
Find the closest existing block in the reference HTML. Note its exact structure: element nesting, spacing utilities, border treatment, hover state, typography classes. Reuse it rather than approximating.

### 2. Place the files correctly
```
app/<section>/page.tsx              → route entry ONLY (composes, passes props)
app/<section>/error.tsx             → MANDATORY for every route
app/<section>/components/X.tsx      → section-scoped component
components/X.tsx                    → only if a SECOND section actually uses it
lib/constants/<domain>.ts           → labels, colour maps, thresholds
lib/utils/<domain>.ts               → pure transforms
lib/hooks/use<Thing>.ts             → hooks (never inside a .tsx)
```

### 3. Keep the `.tsx` JSX-only
The component, its `*Props` interface, and truly-local presentational sub-parts. Everything else moves out **immediately**:

| In a component file… | Goes to |
|---|---|
| `formatScore`, `severityRank`, `parseEventKind` | `lib/utils/<domain>.ts` |
| `GREMLIN_COLORS`, `ACTION_BADGE`, `SEVERITY_LABEL` | `lib/constants/<domain>.ts` |
| `TERMINAL_SESSION_STATES`, `isTerminal()` | `lib/constants/<domain>.ts` |
| A magic threshold (`70`, `>= 40`) | a named `*_THRESHOLDS` const |
| `useX` | `lib/hooks/` |

Why: an inlined constant gets duplicated the moment a second consumer needs it, the copies drift, and the UI ends up disagreeing with itself about what "high severity" means.

### 4. Wire real data
- Data comes from a hook or a server component. **No `fetch` in a component. No `useEffect` for fetching.**
- Types come from `lib/api/types.ts`, mirroring the Go DTOs exactly.
- **No `any`.** Use `unknown` + narrowing if a shape is genuinely uncertain — and if it is, the type is probably wrong, which is a finding.

### 5. Implement all four states — the screen isn't done without them

1. **Empty** — the default on a fresh install and the most-neglected state here. A Shield dashboard with zero events is **correct** but reads as broken. It must say "watching, nothing to report" with enough intent that it doesn't look unfinished.
2. **Loading** — data over the network from two services.
3. **Error / partial** — **Shield (:8081) and Arena (:8082) fail independently.** Shield being down must not blank an Arena page. `error.tsx` per route, degraded state **per section**, never a global "API down".
4. **Live / streaming** (Arena) — a healthy stream, a stalled stream, and a dropped connection must look different. A silently dead terminal is indistinguishable from a stalled session, and that's a defect.

### 6. WebSocket, if the component is live
- Reuse `lib/hooks/useArenaWebSocket.ts` and `lib/websocket.ts`. Don't open a second connection.
- **Close it in the effect teardown** — a leaked socket per navigation is the classic bug.
- **Bound the retained buffer.** A one-hour session must not grow the DOM without limit. State the cap.
- **Batch high-frequency events** (coalesce per frame or per N ms) and use narrow store selectors, so only the terminal re-renders, not the score panel.
- On reconnect, **refetch over REST** rather than assuming the stream can resume. Persistence is truth; the socket is a view.

### 7. Accessibility inside a dark, low-contrast aesthetic
- Check contrast on every new pairing (4.5:1 for body text). `#adaaaa` on `#131313` passes; muted-on-muted does not.
- **Severity never by colour alone** — pair it with a label or an icon. Red-vs-green is invisible to some readers and ambiguous in a screenshot pasted into an incident thread.
- Visible focus states on `radius-0` elements.
- Respect `prefers-reduced-motion` — the scanline and any pulse must be suppressible.

### 8. Tests
```bash
npx vitest run
```
- Beside the code (`MetricCards.test.tsx`) or in `__tests__/` — match the neighbours.
- MSW for API mocks, **per service**, so a Shield failure can be simulated independently of Arena.
- Test the four states, user interactions, and store transitions — not implementation details.

## Done means

```bash
cd gremlyn-dashboard
npm run typecheck    # zero errors
npm run lint
npx vitest run
npm run build        # MUST pass — server/client boundary errors surface only here
```

Plus the visual check:
- [ ] Read the matching `reference/*.html` and the result matches it
- [ ] Correct section accent (green Shield / red Arena)
- [ ] `radius-0`, dark-only, tokens not hexes, Material Symbols only
- [ ] All four states implemented
- [ ] No `any`, no `fetch` in a component, no `useEffect` fetching
- [ ] No constants/thresholds inlined in a `.tsx`
- [ ] `error.tsx` present for any new route
- [ ] WebSocket cleaned up, buffer bounded
- [ ] No `dangerouslySetInnerHTML`
- [ ] Contrast checked, severity not colour-only

Then run it: `npm run dev` with shield (:8081) and arena (:8082) up, and check the page against the reference side by side — including with one service deliberately stopped.
