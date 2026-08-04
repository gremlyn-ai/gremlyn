---
paths:
  - "dashboard/**/*.ts"
  - "dashboard/**/*.tsx"
  - "dashboard/package.json"
  - "dashboard/tsconfig.json"
  - "dashboard/vitest.config.ts"
---

# Lint & Type-check Gate — Dashboard

## 1. The commands

All run from `dashboard/`:

```bash
npm run typecheck    # tsc --noEmit
npm run lint         # ESLint (next lint)
npx vitest run       # unit + component tests
npm run build        # production build — the final proof
```

`npm run dev` for the dev server on `:3000`. In CI use **`npm ci`**, never `npm install` — the lockfile is the contract.

## 2. The bar

- **`npm run typecheck` must report zero errors.** TypeScript is `strict: true`. There is no tolerated-warning tier here.
- **`npm run lint` must pass clean.**
- **`npx vitest run` green.**
- **`npm run build` must succeed** before anything is called done. A dev server that renders and a build that fails is a common Next.js gap — server/client boundary errors and unused-export issues often surface only at build time.

Formatting is applied automatically on save via the `PostToolUse` hook in `.claude/settings.json` (eslint `--fix` on `.ts`/`.tsx`).

## 3. `any` is forbidden — and here's how to avoid it

**No `any`.** Not in a cast, not in a `catch`, not "temporarily", not with an eslint-disable. This is the single most enforced rule in the dashboard, because the Go↔TS type boundary is the only thing catching cross-repo API drift, and one `any` blinds it.

Common friction and the correct fix:

- **API response of unknown shape** → don't `as any`. The response type belongs in `lib/api/types.ts`, mirroring the Go DTO. If the shape is genuinely unknown at that point, type it `unknown` and narrow with a type guard.
- **`catch (e)`** → `e` is `unknown` under strict mode, which is correct. Narrow it: `if (e instanceof Error) …`, else treat it as an unknown failure. Never `catch (e: any)`.
- **A third-party lib with weak types** (Chart.js option objects are the usual culprit) → type the *narrow* shape you actually pass, not the library's whole surface. A local `interface` beats `any`.
- **Zustand store typing** → type the store's state and actions interface explicitly and let `create<State>()` infer from it. Don't reach for `any` to escape a generic mismatch.
- **A field you know exists but the type doesn't have** → the type is wrong. Fix `lib/api/types.ts`; the type is a claim about the Go struct, and if it's out of date that's the bug you just found.
- **Genuine dynamic access** → `Record<string, T>` with a known `T`, or a discriminated union. Not an index signature of `any`.

`unknown` + narrowing is always available. If you can't find a type, that's a signal the boundary is wrong — surface it, don't paper over it.

## 4. The Go ↔ TypeScript contract

`dashboard/lib/api/types.ts` **mirrors the Go DTOs exactly** — every field name from its `json` tag, every optionality, every enum value.

- A Go DTO change and this file change **in the same piece of work**. Not "later".
- When shield's or arena's `internal/<product>/api/types.go` moves, run `npm run typecheck` — that's the detector. Type errors here are the *feature*, not an obstacle.
- Optionality matters: a Go `*string` / `omitempty` field is `field?: string`, and a Go non-pointer field is required. Getting this wrong produces runtime `undefined` that the compiler was supposed to catch.

## 5. Structural rules the linter can't see (still blocking)

- **HTTP only through `lib/api/client.ts`.** No `fetch` in a component, no second wrapper.
- **No `useEffect` for data fetching.** Server components or a hook in `lib/hooks/`. `useEffect` is for subscriptions (WebSocket) and DOM effects.
- **Every route has an `error.tsx`.** Shield and Arena fail independently — a missing boundary turns one dead service into a blank app.
- **No constants, label maps, or thresholds inlined in a `.tsx`** — they go to `lib/constants/`. Two inline copies of "what counts as high severity" is a shipped inconsistency.
- **Hooks live in `lib/hooks/`**, never declared inside a component file.
- **WebSocket closed in effect teardown**, retained buffer bounded.

See `.claude/rules/front/design-system.md` for the visual invariants (dark-only, `radius-0`, section accents) — those are equally blocking and equally invisible to ESLint.

## 6. Environment variables

- `.env.local` holds the two API base URLs.
- **`NEXT_PUBLIC_*` values are baked into the client bundle at build time.** Never a secret. Changing one requires a rebuild, not a restart.

## 7. Tests

- Beside the code (`MetricCards.test.tsx`) or in `__tests__/` — both patterns already exist in `lib/api/` and `lib/stores/`. Match the neighbours.
- **MSW for API mocks**, with per-service handlers so a Shield failure can be simulated independently of Arena.
- Store tests are first-class: transitions, selectors, reset, and high-frequency event batching.
- Test user-visible behavior and store state, not implementation details.
