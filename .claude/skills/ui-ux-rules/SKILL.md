---
name: ui-ux-rules
description: >
  UX/UI rules for building or refactoring the Gremlyn dashboard — pages, screens,
  components, forms, modals, live views, empty states, loading states, first-run.
  Use when building/refactoring UI, designing a new screen, reviewing a component for
  UX, or when the user mentions "UX", "UI rules", "design rules", "clean UI", "optimistic
  UI", "empty state", "loading state", "onboarding", "friction", "Fitts", "Hick",
  "Jakob", "skeleton", "undo", "smart defaults", or asks "is this screen good UX?".
---

# UX/UI Rules — Gremlyn Dashboard

Reference for building or refactoring UI. Each rule = name + principle + concrete directive. When in doubt, optimize for: **less friction, instant feedback, reversibility, respecting user attention.**

## How to use

- Treat every rule as a **default**, not dogma. When two conflict, state the tradeoff and choose by the target user for that screen.
- For any new screen/component, audit against the **Core Checklist** (bottom). Start there.
- **Gremlyn overrides come first** (§0) — the visual invariants and the security constraints are not negotiable by a UX law.
- When rules collide, see **Conflict Resolution**.

---

## 0. Gremlyn overrides — these win over any rule below

The dashboard is a **security and chaos-testing tool** with a fixed, opinionated aesthetic. Four things that reverse or constrain generic SaaS advice:

1. **The aesthetic is locked.** Dark-only, `border-radius: 0`, Material Symbols, Space Grotesk / Inter / JetBrains Mono, green = Shield, red = Arena. See `.claude/rules/front/design-system.md`. A UX rule never justifies a rounded corner, a light mode, or a crossed accent. "Restrained confetti" is out — the motion idiom is sharp and instant, no spring easing.

2. **Precision beats charm in anything the user acts on.** A block reason, a score breakdown, and an error message must be literal and specific. A vague-but-friendly security message produces false confidence, which is worse than no message. Personality lives in empty states and gremlin copy only.

3. **Optimistic UI is dangerous on security state.** "Ghost Editing" is correct for a UI preference and **wrong** for disabling a policy rule — showing a rule as off before the server confirms means the user believes they're unprotected (or protected) when they aren't. **Security-state mutations wait for server confirmation.** Optimistic UI is fine for filters, sorting, and view preferences.

4. **Never render captured content as HTML.** Event payloads are attacker-controlled by definition. No `dangerouslySetInnerHTML` — ever. Captured payloads go in `<pre>`/`<code>` as text.

Plus two structural facts that shape every screen:

- **Shield (:8081) and Arena (:8082) fail independently.** Degraded state is per-section, never a global "API down". Every route has an `error.tsx`.
- **The empty state is the default on a fresh install**, and a quiet firewall is a *working* firewall. Making "nothing to report" read as intentional rather than broken is the single highest-value UX job in this product.

---

## 1. Signature Rules (named patterns)

- **The Invisible Button** — Best action = one the user never takes. Auto-save vs explicit "Save". Smart defaults, context detection. If intent is reliably inferable, act + make it reversible. *(Gremlyn: applies to view state, not to policy changes.)*
- **The IKEA Effect** — People value what they help build. Let users configure something small early (name a session, pick their first gremlins). Avoid zero-input first runs.
- **Ghost Editing (Optimistic UI)** — Change looks applied before the server confirms. Update local state instantly, reconcile in background, roll back gracefully on failure. **Not for security state** — see §0.3.
- **The Save-My-Life Button (Undo over Confirm)** — Prefer ubiquitous Undo over "Are you sure?" modals. Reversible actions act immediately + a toast with Undo (≈5–8s). Hard confirm only for genuinely irreversible, high-blast-radius actions. *(Gremlyn: disabling a rule is reversible but security-relevant — confirm-free with Undo is fine, optimistic rendering is not.)*
- **Impact X-Ray** — Before a destructive or bulk action, show concretely what changes. Numbers, not vague warnings: "This will delete 12,847 events from the last 30 days." Quantify the blast radius. *(Critical for anything that prunes the event DB — that's the user's history.)*
- **Fitts's Law** — Time-to-target depends on size and distance. Primary buttons large and near the cursor; destructive targets smaller and farther. `LAUNCH CHAOS` is deliberately huge — that's Fitts, not decoration.
- **Lazy UX** — The shortest path must be the correct path. Minimize clicks, pre-fill, remember last choices, default to the most probable.

## 2. Foundational Laws

- **Hick's Law** — Decision time grows with choices. Limit **3–5 per step**; hide advanced; sequence decisions. *(Eight gremlins with per-gremlin knobs is a lot of choices — default a sensible preset and put the knobs behind disclosure.)*
- **Miller's Law (7±2)** — Working memory is ~7±2 items. Reduce *recall* burden; chunk information. Visible, well-grouped data is fine beyond 7.
- **Jakob's Law** — Users expect yours to work like other products. Respect conventions (clickable logo → home, gear = settings, search top-right). Spend the novelty budget only where it adds value. *(The aesthetic is the novelty; the plumbing should be boring.)*
- **Tesler's Law (Conservation of Complexity)** — Irreducible complexity exists; the question is who absorbs it. Absorb it server-side. Smart defaults, inference, presets — don't push heavy manual configuration.
- **Postel's Law (Robustness)** — Liberal in input, strict in output. Accept a pasted URL with or without scheme, a rule pattern with or without anchors — normalize internally. Never make the user match your format.
- **Von Restorff Effect** — The standout element is remembered. **One** primary (accent) button per screen. If everything shouts, nothing is heard.
- **Serial Position Effect** — Users remember first and last in a list. Key actions and items at the start or end, never buried mid-list.
- **Zeigarnik Effect** — Unfinished tasks stay top of mind. Progress indicators, checkable setup lists. Visible incompleteness pulls users to finish.
- **Goal-Gradient Effect** — Motivation rises near the goal. Show progress already partly complete.
- **Aesthetic-Usability Effect** — Attractive interfaces are perceived as more usable and forgive minor issues. Invest in polish — but never let aesthetics mask a real usability defect, and in a security tool never let it mask an unexplained decision.

## 3. Perception & Feedback

- **Doherty Threshold (<400ms)** — Below ~400ms, users stay in flow. Optimize real latency or simulate speed (skeletons, prefetch).
- **Skeleton over Spinner** — Skeletons convey speed and structure; spinners only announce waiting. Layout-matching skeletons for content loads. *(Gremlyn: terminal-flavoured loading — a cursor, a scan line — over both, where it fits.)*
- **Immediate Feedback (<100ms)** — Every interaction needs a visible reaction under 100ms or users re-click. Hover/pressed/focus/active states always. Disable-and-indicate on submit (no double-fire).
- **Busy vs Empty Waiting** — Active waiting feels shorter. Never a frozen blank screen.
- **The Five States Rule** — Every data component handles **empty, loading, error, partial, ideal**. Design all five upfront; the happy path is 1 of 5. *(Gremlyn adds a sixth: **live/streaming** — and a stalled or dropped stream must look wrong, not charming. A silently dead terminal is indistinguishable from a stalled session.)*

## 4. Reducing Friction

- **One Decision per Screen** — Sequencing beats density in high-stakes flows.
- **Smart Defaults** — A good default eliminates most decisions. Pre-select the most likely option; make the default safe and reversible. *(A new session should have a working gremlin preset, not eight toggles at zero.)*
- **Progressive Disclosure** — Show complexity only when needed. Advanced knobs behind "Advanced".
- **Law of Least Astonishment** — No surprising side effects. The button does exactly what its label says. *(`LAUNCH CHAOS` had better launch chaos and nothing else.)*
- **Zero Cost to Enter** — Delay friction until value is felt. *(Gremlyn's version: zero-config. SQLite, no Docker, nothing to install. Protect that.)*
- **Recognition over Recall** — Dropdowns, autocomplete, recently-used over remembering.
- **Workflow-Fit over Feature-Fit** — Organize around tasks, not the feature list.

## 5. Trust & Psychological Safety

- **Reversibility by Default** — Nothing permanent without strong confirmation. *(Event pruning and DB reset destroy the user's history — treat as irreversible.)*
- **Proportional Confirmation Friction** — Friction scales with irreversibility. Zero for reversible; high (type the name) for irreversible. Never invert.
- **System Transparency** — Users always know where they are and what's happening. Visible connection state, session state, last-updated. *(In a security tool this is load-bearing: a user must be able to tell at a glance whether Shield is actually inspecting traffic.)*
- **Human Errors, Not Technical Ones** — Tell users what to do, not what crashed. "Couldn't reach Shield on :8081 — is it running?" not "ECONNREFUSED". State the problem and the next action.
- **Prevention over Error Messages** — Stopping a mistake beats reporting it. Disable invalid submits — but always show *why*. Never a dead button unexplained.

## 6. Visual Hierarchy & Cleanliness

- **Law of Prägnanz** — The eye seeks the simplest form. Align, group, remove. Whitespace is structure.
- **Law of Proximity** — Close items read as related. Group with spacing more than with borders. *(Suits a `radius-0`, border-light aesthetic well.)*
- **Single Information Density per Zone** — Don't mix a dense event table with an airy hero in the same region. Transition density deliberately.
- **Limited Type Scale** — 3–4 text sizes max; weight contrast over many grays.
- **Contrast = Importance** — Clear primary/secondary/tertiary. *(Guard this hard: in this palette `#adaaaa` on `#131313` passes and muted-on-muted doesn't. Check every new pairing at 4.5:1.)*
- **8-Point Grid** — Spacing in multiples of 8 (or 4), via Tailwind's scale.

## 7. Engagement & Retention

- **Peak-End Rule** — Judged by the emotional peak and the ending. Engineer a strong "aha" and a satisfying end-of-task screen. *(Gremlyn's aha is a real block or a first resilience report — make that moment land.)*
- **Fast Aha Moment (Time-to-Value)** — Reach core value in the first minutes. Relentlessly shorten time-to-first-success. *(The first run has real anxiety attached: the user is putting a proxy between their working agent and its tools. Show quickly that nothing broke.)*
- **Completion Gratification** — Small wins build commitment. Subtle micro-rewards. *(Sharp and restrained — no confetti; the motion idiom is terminal.)*
- **Onboarding by Doing, Not Touring** — Action beats a skipped carousel. First real session on a real MCP server.
- **Actionable Empty States** — A blank screen is a design failure. Every empty list offers a first step, a one-line "what this is", and a low-friction primary action. *(The Shield exception: an empty events list is **correct**, not a missing first step. It says "watching, nothing to report" — with enough intent to read as deliberate. Getting this wrong makes a working firewall look broken.)*

## 8. Often-Forgotten Hygiene

- **Focus Management** — Fully keyboard-usable, logical visible focus order, focus trapped in modals and restored on close. *(Focus rings need explicit care on `radius-0` elements in a dark theme.)*
- **Touch Targets ≥44px** — Even on a desktop-first tool.
- **Consistent Verbs** — One term per action everywhere. Never "Remove" here and "Delete" there. *(And use the domain vocabulary from `brand-guardian`: block/redact/alert/throttle, inject, session.)*
- **Preserve User Work** — Never lose typed input. Auto-draft a rule being written; survive reloads and back-nav.
- **Accessibility Baseline** — WCAG AA contrast, labels on all inputs, ARIA where needed, **no information by colour alone** — mandatory here, since severity is the thing being communicated and red/green is ambiguous in a dark theme and in a pasted screenshot.
- **Bounded rendering** — Terminal and event log buffers must cap retained entries. A one-hour session must not grow the DOM without limit.

---

## Core Checklist (audit any screen against this)

- [ ] **One** clear primary action (Von Restorff)
- [ ] Primary large & reachable; destructive small & distant (Fitts)
- [ ] ≤ 3–5 choices per step; advanced hidden (Hick / Progressive Disclosure)
- [ ] Instant feedback <100ms; perceived response <400ms (Doherty)
- [ ] Optimistic UI for view state — **NOT for security state** (Gremlyn override)
- [ ] Reversible actions use Undo, not confirm modals (Save-My-Life)
- [ ] Irreversible actions (prune, DB reset) show quantified impact + strong confirm (Impact X-Ray)
- [ ] All states designed: empty / loading / error / partial / ideal / **live**
- [ ] Empty state reads as intentional — and for Shield, as "watching", not "set me up"
- [ ] **Per-section** degraded state; `error.tsx` present; Shield down ≠ Arena blank
- [ ] Live stream: healthy / stalled / dropped are visually distinct
- [ ] Smart defaults & presets; remembers last choices (Lazy UX)
- [ ] Inputs forgiving and normalized (Postel)
- [ ] Error messages human and actionable, and **security messages literal**
- [ ] Connection / inspection state always visible (Transparency)
- [ ] Consistent verbs from the domain vocabulary; 8pt spacing; 3–4 type sizes
- [ ] Keyboard-navigable, visible focus on `radius-0` elements; targets ≥44px
- [ ] Contrast ≥4.5:1 on every new pairing; **severity never colour-only**
- [ ] `prefers-reduced-motion` respected (scanline, pulse)
- [ ] Buffers bounded
- [ ] No `dangerouslySetInnerHTML`; captured payloads rendered as text
- [ ] Visual invariants held: dark-only, `radius-0`, correct section accent, tokens not hexes

## Conflict Resolution Guide

| Tension | Resolution |
|---|---|
| Hick's Law (fewer options) vs power-user density | Progressive disclosure: preset by default, knobs on demand |
| Speed (Doherty) vs correctness | Optimistic for view state; **server-confirmed for security state** |
| Jakob's Law (convention) vs the GREMLYN_OS aesthetic | The aesthetic IS the novelty budget — spend it on skin, keep the plumbing conventional |
| Friction reduction vs safety | Reversible → zero friction. Irreversible (prune, reset) → proportional friction. Never invert |
| Aesthetics vs usability | Polish freely; never let the terminal skin hide an unexplained security decision |
| Personality (whimsy) vs precision | Personality in empty states and gremlin copy; **literal everywhere the user acts on the output** |
| Actionable empty state vs "nothing is correct" | Shield-empty = "watching". Arena-empty = offer the first session |

---

*Synthesized from established Laws of UX (Fitts, Hick, Jakob, Tesler, Miller, Postel, Doherty, Von Restorff, Zeigarnik, Peak-End, Serial Position, Goal-Gradient, Prägnanz, Proximity, Aesthetic-Usability) + SaaS UX research on onboarding, empty states, optimistic UI and activation — with Gremlyn-specific overrides for a security tool with a locked aesthetic.*
