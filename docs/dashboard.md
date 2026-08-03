# Gremlyn Dashboard — Web frontend

## Project overview
Gremlyn Dashboard is the Next.js frontend for the Gremlyn platform. It consumes the Shield API (:8081) and Arena API (:8082) via REST and WebSocket. It provides a visual interface for both dev and non-dev users to monitor MCP firewall activity and run chaos tests on AI agents.

## Tech stack
- Next.js 15 (App Router)
- React 19
- TypeScript (strict mode, no any)
- Tailwind CSS 4
- Chart.js for data visualization
- WebSocket (native) for live Arena session streaming
- Zustand for state management (minimal, no Redux)
- Google Material Symbols Outlined for icons
- Fonts: Space Grotesk (headlines), Inter (body), JetBrains Mono (mono/code/labels)

## Design system — CRITICAL, follow exactly

The UI has been prototyped in two reference HTML files that MUST be used as the single source of truth for all design decisions. These files are located in the project:
- `reference/arena.html` → Arena session page (chaos testing)
- `reference/dashboard.html` → Shield dashboard page (firewall monitoring)

ALWAYS open and read these files before building any component. Every component you build must visually match these prototypes.

### Visual identity: "GREMLYN_OS" hacker terminal aesthetic
- Dark mode ONLY. No light mode. Ever.
- Border radius: 0px everywhere. Sharp edges. No rounded corners except `border-radius: 9999px` for pills.
- Scanline overlay: subtle CSS scanline effect on the background (see reference HTML).
- All text labels in uppercase, tracking-widest or tracking-tight, font-mono.
- Military/hacker naming convention: "INITIATE_BREACH", "SYSTEM_LOGS", "ARENA_LIVE", "LAUNCH CHAOS".

### Color palette (from Tailwind config in reference HTML)
```
surface-container-lowest: #000000 (pure black, sidebar bg)
background/surface: #0e0e0e (main bg)
surface-container-low: #131313 (card bg, hover states)
surface-container: #1a1919
surface-container-high: #201f1f (elevated surfaces)
surface-container-highest: #262626 (inputs, toggles)
surface-bright: #2c2c2c

primary: #8eff71 (Gremlyn green — Shield accent, main brand)
primary-dim: #2be800
primary-container: #2ff801

secondary: #ff7168 (Arena red — Arena accent, danger)
secondary-dim: #e2242a
secondary-container: #c00018

tertiary: #83ff95 (success green)
error: #ff7351

on-surface: #ffffff (text primary)
on-surface-variant: #adaaaa (text secondary/muted)
outline: #777575
outline-variant: #494847 (borders, dividers)
```

### Typography
- Headlines: `font-family: 'Space Grotesk'` — bold, tracking-tight, uppercase
- Body: `font-family: 'Inter'`
- Mono/labels/data: `font-family: 'JetBrains Mono'` — used heavily for status text, data values, table content, nav items
- All navigation items and labels: font-mono, text-xs, uppercase, tracking-tight

### Component patterns (from reference HTML)
- Sidebar: fixed left, w-64, pure black bg, Gremlyn logo top left, nav items with material-symbols-outlined icons, active state = bg-[#262626] + border-l-4 + accent color text
- Top bar: h-16, bg-[#0e0e0e], "GREMLYN_OS" title with pulsing dot, agent status badges
- Cards: bg-surface-container-low, no border-radius, border-l-2 accent on hover
- Tables: font-mono, text-xs, divide-y with very subtle dividers
- Toggles: custom flat toggles (no rounded), accent color when active
- Buttons: no border-radius, font-mono, font-bold, tracking-widest, uppercase
- Primary action button: full-width, large padding (py-10), accent bg, font-headline text-3xl font-black
- Status footer: fixed bottom bar with system status, latency, thread pool

### Icons
- Use Google Material Symbols Outlined exclusively
- Import: `https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined`
- Usage: `<span className="material-symbols-outlined">icon_name</span>`
- Key icons used: bolt (arena), shield (shield), settings, description (docs), code, logout, notifications, terminal, warning, play_circle, monitoring, psychology, expand_more

## Architecture
- Dark mode only
- Two main sections: Arena (secondary/red accent) and Shield (primary/green accent)
- Dashboard consumes two separate APIs (Shield :8081, Arena :8082)
- WebSocket connection to Arena for real-time session events
- All API calls go through lib/api/ client layer
- Shield page = green UI accent, Arena page = red UI accent

## Code style — IMPORTANT
- TypeScript strict: true. NEVER use `any`. Define proper types for everything.
- All API response types in lib/api/types.ts, mirroring Go structs exactly.
- Components: functional components only, with named exports.
- File naming: PascalCase for components (MetricCards.tsx), camelCase for utilities (formatScore.ts).
- Tailwind: use Tailwind utilities only. No custom CSS except the scanline effect and slider thumb. No CSS modules.
- NO `useEffect` for data fetching. Use Next.js server components or SWR.
- State management: Zustand stores in lib/stores/. One store per domain (arenaStore.ts, shieldStore.ts).
- Components must be pure: no API calls inside components. Data flows via props or stores.
- Error boundaries: every page MUST have an error.tsx boundary.

## Project structure
```
reference/                          → HTML prototypes (READ-ONLY, design source of truth)
  arena.html                        → Arena page prototype from Stitch
  dashboard.html                    → Shield dashboard prototype from Stitch
public/
  gremlyn-white.png                 → White logo (dark bg)
  gremlyn-black.png                 → Black logo (light bg)
  favicon.svg
app/
  layout.tsx                        → Root layout (sidebar + topbar + scanline overlay)
  page.tsx                          → Redirect to /shield
  arena/
    page.tsx                        → Arena main (new session + gremlin selector)
    session/[id]/page.tsx           → Live session (battle visualization + event log)
    components/
      ChaosArena.tsx                → THE arena battle visualization
      GremlinCard.tsx               → Individual gremlin card with toggle
      EventLog.tsx                  → Scrolling live event log
      ScoreBar.tsx                  → Top score counter
      GremlinSelector.tsx           → Grid of gremlin cards with toggles
      IntensitySlider.tsx           → Custom range slider
      ResilienceReport.tsx          → Post-session score breakdown
  shield/
    page.tsx                        → Shield dashboard (metrics + threat card + chart)
    rules/page.tsx                  → Rules table with toggles
    events/page.tsx                 → Event log with filters
    servers/page.tsx                → Connected MCP servers
    components/
      MetricCards.tsx               → Stat cards (blocked, protected, uptime)
      ThreatCard.tsx                → Latest threat with log replay
      ProtectionToggles.tsx         → On/off toggles for protections
      RuleTable.tsx                 → Rules table with enforcement toggles
      ThreatChart.tsx               → Bar chart (7-day threat vectors)
      EventList.tsx                 → Paginated event list
      ServerList.tsx                → MCP servers with health status
  settings/page.tsx
components/
  Sidebar.tsx                       → Fixed left sidebar (shared layout)
  TopBar.tsx                        → Top header bar (shared layout)
  StatusFooter.tsx                  → Bottom status bar (shared layout)
  GremlynLogo.tsx                   → Logo component (green/red variants)
lib/
  api/
    client.ts                       → Base fetch wrapper
    shield.ts                       → Shield API methods
    arena.ts                        → Arena API methods
    types.ts                        → All TypeScript types
  stores/
    arenaStore.ts                   → Arena state (Zustand)
    shieldStore.ts                  → Shield state (Zustand)
  websocket.ts                      → WebSocket manager for live arena
  theme.ts                          → Color tokens exported as constants
styles/
  globals.css                       → Tailwind base + scanline + slider custom styles
tailwind.config.ts                  → Full color palette matching reference HTML
```

## Commands
```bash
npm run dev         # Dev server (localhost:3000)
npm run build       # Production build
npm run lint        # ESLint
npm run typecheck   # tsc --noEmit
```

## Testing rules
- Use Vitest + React Testing Library.
- Test files next to components: MetricCards.test.tsx next to MetricCards.tsx.
- Test user interactions, not implementation details.
- Mock API calls in tests using MSW (Mock Service Worker).

## Git workflow
- Branch naming: feat/xxx, fix/xxx, refactor/xxx
- Commit messages: conventional commits (feat:, fix:, refactor:, test:, docs:)