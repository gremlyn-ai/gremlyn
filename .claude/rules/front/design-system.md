---
paths:
  - "dashboard/**/*.tsx"
  - "dashboard/**/*.css"
  - "dashboard/tailwind.config.ts"
  - "dashboard/lib/theme.ts"
---

# GREMLYN_OS Design System — Rules

Always-on visual contract for the dashboard. Full spec: `docs/dashboard.md`.
**Source of truth: `dashboard/reference/arena.html` and `reference/dashboard.html`.**

> **Read the matching reference file before building any component.** They are prototypes from the design phase and they are **READ-ONLY** — never edit them. Every component must visually match them.

---

## 1. Invariants (a violation is a blocking finding, not a preference)

- **Dark mode ONLY.** No light mode, no `dark:` variants, no theme toggle. Ever.
- **`border-radius: 0` everywhere.** Sharp edges. The only exception is a pill (`rounded-full` / `9999px`).
- **Green `#8eff71` = Shield. Red `#ff7168` = Arena.** Never cross an accent into the other section. A green primary button on an Arena page is a bug.
- **Use the token, never the raw hex**, in every component. Tokens live in `tailwind.config.ts` and `lib/theme.ts`.
- **Tailwind utilities only.** The *only* permitted custom CSS is the scanline overlay and the range-slider thumb, in `globals.css`. No CSS modules, no styled-components, no inline `style` for anything Tailwind can express.
- **Material Symbols Outlined exclusively** for icons: `<span className="material-symbols-outlined">bolt</span>`. No lucide, no heroicons, no SVG icon packs.

## 2. Palette

```
surface-container-lowest  #000000   sidebar background
background / surface      #0e0e0e   main background
surface-container-low     #131313   card background, hover
surface-container         #1a1919
surface-container-high    #201f1f   elevated surfaces
surface-container-highest #262626   inputs, toggles, active nav
surface-bright            #2c2c2c

primary                   #8eff71   Gremlyn green — SHIELD accent, main brand
primary-dim               #2be800
primary-container         #2ff801

secondary                 #ff7168   Arena red — ARENA accent, danger
secondary-dim             #e2242a
secondary-container       #c00018

tertiary                  #83ff95   success
error                     #ff7351

on-surface                #ffffff   primary text
on-surface-variant        #adaaaa   secondary / muted text
outline                   #777575
outline-variant           #494847   borders, dividers
```

**Contrast is a real risk in this palette.** `#adaaaa` on `#131313` passes; muted-on-muted does not. Check every new pairing against WCAG 4.5:1 for body text. A security dashboard nobody can read is worse than a plain one.

**Severity must never be encoded by colour alone.** Red-vs-green badges are invisible to colourblind users and ambiguous in a screenshot pasted into an incident thread. Always pair colour with a label or an icon.

## 3. Typography

| Role | Font | Usage |
|---|---|---|
| Headlines | **Space Grotesk** | bold, `tracking-tight`, uppercase |
| Body | **Inter** | prose, descriptions |
| Data / labels / tables / status / code | **JetBrains Mono** | the workhorse — used heavily |

All navigation items and labels: `font-mono text-xs uppercase tracking-tight`.

## 4. Component patterns (from the reference HTML)

- **Sidebar** — fixed left, `w-64`, pure black, logo top-left, `material-symbols-outlined` nav icons. Active state: `bg-[#262626]` + `border-l-4` + accent text.
- **Top bar** — `h-16`, `bg-[#0e0e0e]`, `GREMLYN_OS` title with a pulsing dot, agent status badges.
- **Cards** — `bg-surface-container-low`, no radius, `border-l-2` accent on hover.
- **Tables** — `font-mono text-xs`, `divide-y` with very subtle dividers.
- **Toggles** — custom flat toggles, no radius, accent colour when active.
- **Buttons** — no radius, `font-mono font-bold tracking-widest uppercase`.
- **Primary action** — full-width, large padding (`py-10`), accent background, `font-headline text-3xl font-black`.
- **Status footer** — fixed bottom bar: system status, latency, thread pool.

Extend these before inventing a new pattern. A new token or a new pattern is a system decision — document why the existing set didn't cover it.

## 5. The four states — all of them, or the screen isn't done

Every data screen in this product is fed by two independent network services. Design and implement all four:

1. **Empty** — the default on a fresh install, and the most-neglected state here. A Shield dashboard with zero events is **correct** but reads as broken. It must say "watching, nothing to report" with enough intent that it doesn't look unfinished. This is the first impression, not an edge case.
2. **Loading** — data arrives over the network from two services.
3. **Error / partial** — **Shield (:8081) and Arena (:8082) fail independently.** Shield being down must not blank an Arena page. Every route has an `error.tsx`, and degraded state is **per-section**, never a global "API down".
4. **Live / streaming** — Arena sessions stream over WebSocket. A healthy stream, a stalled stream, and a dropped connection must be visually distinguishable. A silently dead terminal looks identical to a stalled session — that's a defect.

## 6. Motion

- Sharp and immediate — this is a terminal aesthetic. **No spring easing, no bounce, no confetti.**
- Terminal-flavoured loading (cursor, scan line) over spinners.
- The live indicator may pulse.
- **Respect `prefers-reduced-motion`** — the scanline overlay and any pulse must be suppressible.

## 7. Copy in the UI

- Labels uppercase, `font-mono`. Military/systems naming: `INITIATE_BREACH`, `SYSTEM_LOGS`, `ARENA_LIVE`, `LAUNCH CHAOS`, `THREAT_VECTOR`.
- **No emoji in product UI copy** — it renders inconsistently in a monospace terminal aesthetic. Emoji in the README is fine.
- **The aesthetic is theatrical; the information is not.** A button can say `LAUNCH CHAOS`. A block reason, a score breakdown, and an error message must be precise and literal — a user acts on those, and a charming-but-vague security message produces false confidence. See `.claude/agents/design/brand-guardian.md`.

## 8. Security constraint on rendering

**Never `dangerouslySetInnerHTML`.** Event payloads and captured tool results are **attacker-controlled by definition** — that's the whole point of the product. Rendering a captured injection payload as HTML is a stored XSS delivered by the exact attacker you're monitoring.

Render captured content as text in `<pre>` / `<code>`. Validate any URL from event data before it lands in an `href` (no `javascript:`).

## 9. Bounded rendering

Terminal panels and event logs must **cap retained entries**. A one-hour chaos session streaming events must not grow the DOM without limit. State the cap in the component.
