# Review Uncommitted Changes

Review all uncommitted files (staged and unstaged) before committing.

## Instructions

1. Determine **which repo(s)** have changes — Gremlyn is four independent git repositories:
   ```bash
   for d in gremlyn-core gremlyn-shield gremlyn-arena gremlyn-dashboard; do
     echo "=== $d"; git -C "$d" status --short
   done
   ```
2. For each changed file, read the diff (`git diff`, `git diff --cached` for staged)
3. Review against the checklists below

---

## Debugging artifacts (must fix before commit)

**Go**
- `fmt.Println`, `fmt.Printf`, `println` — zerolog only in shipped code
- `log.Printf`, `log.Println` — stdlib log is not the project logger
- `panic(` added for debugging
- `spew.Dump`, `pp.Print`
- A commented-out `t.Skip` or a `t.Skip` left in a test

**TypeScript**
- `console.log`, `console.debug`, `debugger`
- `alert(`

---

## Go checks

### Critical
- `map[string]interface{}` for a known shape → real struct
- A discarded error (`_ = err`, or an `err` assigned and never checked)
- An error returned unwrapped where context is needed
- SQL built by string concatenation / `fmt.Sprintf` → **injection**
- A schema change in only one store (SQLite **or** PostgreSQL, not both)
- An edited **already-shipped** embedded SQLite migration → append instead
- Missing `ctx context.Context` as the first param of an IO function; `context.TODO()` shipped
- A goroutine with no exit path on ctx cancel
- `io.ReadAll` on a network body with no size cap → memory DoS on an inline proxy
- A bare `//nolint` with no reason, or any `//nolint` in `internal/detection/` or `internal/policy/`
- New CGO dependency → breaks `CGO_ENABLED=0` static builds

### High
- Missing godoc on an exported symbol (`revive` will catch it — fix before it does)
- Missing `json` tag, or `yaml` tag on a config field
- Business logic in an HTTP handler
- A missing `defer` on an acquired resource; a `sql.Rows` loop without `rows.Err()`
- Global mutable state or an init-time side effect
- Tests missing for new behavior; a detection change with **positive cases only**
- A gremlin whose not-injected path isn't a byte-exact no-op, or that isn't seeded
- Scoring code with a clock, randomness, IO, or map-iteration order in a decision
- An unbounded `.*` regex on attacker-controlled input → ReDoS

---

## TypeScript / Next.js checks

### Critical
- **`any`** — anywhere, including a cast, a `catch`, or behind an eslint-disable
- `dangerouslySetInnerHTML` → event payloads are attacker-controlled, this is stored XSS
- `fetch` outside `lib/api/`
- A secret in `NEXT_PUBLIC_*`

### High
- `lib/api/types.ts` not updated after a Go DTO change
- `useEffect` used for data fetching
- A new route with no `error.tsx`
- Constants / label maps / thresholds inlined in a `.tsx` → `lib/constants/`
- A hook declared inside a component file
- WebSocket not closed in effect teardown; unbounded retained buffer
- Wrong section accent (green on Arena, red on Shield)
- A rounded corner, a light-mode style, or a raw hex instead of a token
- An edit to `reference/*.html` (read-only design source of truth)

---

## Project-wide

- Hardcoded credentials, API keys, tokens
- A hardcoded `localhost` URL that should be config
- Commented-out code blocks
- `TODO` / `FIXME` / `HACK` with no reference
- Trailing whitespace, missing final newline, mixed indentation
- Diff > 400 lines → flag for a split
- **Changes spanning `pkg/` and a consumer in one go** → must be separate commits, core first
- A `replace` directive edit that would ship (breaks clean-clone builds)

---

## Output Format

Group by repo, then by file:

```
# gremlyn-shield

## internal/detection/regex.go

### Critical (must fix)
- Line 42: unbounded `.*` in the injection pattern — ReDoS on attacker input

### Warnings (review recommended)
- Line 88: new pattern has no negative corpus case

### Suggestions (optional)
- Line 12: pattern could be precompiled once at construction
```

If nothing is found: "All uncommitted changes look good." — and say which repos you checked.

---

## Fixing

After reporting, ask:

**"Would you like me to fix these issues?"**

1. **Fix all** — critical + warnings
2. **Fix critical only** — debugging artifacts, secrets, injection, `any`
3. **Review one by one**
4. **No thanks** — leave the report

When fixing:
- Remove debugging statements, trailing whitespace, add missing final newlines
- Anything requiring judgment (a `TODO`, a pattern's scope, a missing test) → ask first
- **Never** silently widen or narrow a detection pattern as a "fix" — that's a behavior change

## Start Review

Review my uncommitted changes now.
