---
name: release-build
description: Cut a release across the Gremlyn repos — the core-first version chain, clean-clone verification, cross-compiled binaries with checksums, and release notes. Use when the user says "cut a release", "release", "tag a version", "build the binaries", "publish", or runs /release-build.
---

# Release Build

Cut a release. Gremlyn is **four independent git repositories** with a hard dependency ordering, so the order of operations here is not advisory — getting it wrong produces a tag pinned to a version that doesn't exist, and Go's module proxy caches that mistake permanently.

## Usage

```
/release-build core v0.4.0        # release core, then bump consumers
/release-build shield v0.3.1      # consumer-only release (no core change)
/release-build all                # coordinated release across the stack
/release-build --dry-run          # verify everything, tag nothing
```

## ⛔ The ordering rule — read before anything else

`gremlyn-shield` and `gremlyn-arena` import `github.com/gremlyn-ai/gremlyn/pkg/...`.

**The order is fixed:**

1. Verify + tag **`gremlyn-core`**
2. In **each** consumer: `go get github.com/gremlyn-ai/gremlyn-core@vX.Y.Z`, build, test
3. Tag the consumers

Doing 2 before 1 pins a nonexistent version. And:

- **Never move a published tag.** The Go module proxy caches by version forever; a moved tag serves stale content to everyone, including you, and no amount of `GOFLAGS` gymnastics fixes it cleanly. Cut a patch instead.
- **A `replace` directive must not ship in a tag.** `replace … => ../gremlyn-core` is correct for local dev and fatal in a release — anyone cloning shield alone gets a broken build.
- If the repos aren't published at `github.com/gremlyn-ai/...` yet, **say so and stop**: local dev works via `replace`, but tagging requires that module path to be fetchable.

---

## Step 1 — Pre-flight, every repo in scope

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
for d in gremlyn-core gremlyn-shield gremlyn-arena gremlyn-dashboard; do
  echo "=== $d  $(git -C $d rev-parse --abbrev-ref HEAD)"
  git -C "$d" status --short
  git -C "$d" tag --sort=-creatordate | head -3
done
```

Blockers:
- [ ] Uncommitted changes → commit or stash. **Never tag a dirty tree**; the tag won't match what you tested.
- [ ] Not on the release branch → confirm with the user.
- [ ] The proposed version already exists as a tag → pick the next one. Do not reuse.

---

## Step 2 — The gate, every Go repo

```bash
for d in gremlyn-core gremlyn-shield gremlyn-arena; do
  echo "=== $d"; (cd "$d" && make check) 2>&1 | tail -15
done
```

`make check` = `go vet` + `golangci-lint run` + `go test -race`. All three must be clean. **`-race` failing blocks the release** — a data race on a policy decision is a vulnerability, not a flake.

Dashboard:
```bash
cd gremlyn-dashboard && npm ci && npm run typecheck && npm run lint && npx vitest run && npm run build
```
`npm ci`, not `npm install` — the lockfile is the contract. **`npm run build` must pass**; a dev server that renders while the build fails is a common Next.js gap.

---

## Step 3 — Release-specific verification

### Clean-clone build (consumers only)
The check that catches a shipped `replace`:
```bash
grep -n '^replace' gremlyn-shield/go.mod gremlyn-arena/go.mod
```
If present, confirm it will be removed (or is excluded) for the tagged commit, then verify the build resolves core from the module proxy rather than from disk:
```bash
(cd gremlyn-shield && go list -m github.com/gremlyn-ai/gremlyn-core)
```

### CGO must stay off
```bash
(cd gremlyn-core && CGO_ENABLED=0 go build -o /tmp/cgotest ./cmd/gremlyn && echo OK)
```
Failure blocks the release — pure-Go SQLite (`modernc.org/sqlite`) exists precisely so static cross-compiled binaries ship.

### Zero-config path
```bash
docker compose down 2>/dev/null
(cd gremlyn-shield && timeout 5 go run ./cmd/shield)   # must start with NOTHING else running
```
SQLite default, no Postgres, no Redis, no sidecar. If it needs a service to boot, the zero-config promise is broken and that's a release blocker.

### Startup migration timing
A migration runs on the **user's machine at launch** and can't be hotfixed. If this release adds one, time it against a realistic DB:
```bash
ls -lh ~/.gremlyn/*.db
time (cd gremlyn-shield && timeout 30 go run ./cmd/shield)
```
> 5s of migration stall needs release-note treatment. Loop in `database-engineer`.

---

## Step 4 — Tag core (if core is in scope)

```bash
cd gremlyn-core
git tag -a v0.4.0 -m "v0.4.0"
git push origin v0.4.0
```

Then wait for the module proxy to see it before step 5 — `GOPROXY` can lag briefly:
```bash
GOPROXY=proxy.golang.org go list -m github.com/gremlyn-ai/gremlyn-core@v0.4.0
```

---

## Step 5 — Bump and tag consumers

Per consumer:
```bash
cd gremlyn-shield
go get github.com/gremlyn-ai/gremlyn-core@v0.4.0
go mod tidy
make check                       # must be green against the TAGGED core, not the local one
git add go.mod go.sum && git commit -m "chore: bump gremlyn-core to v0.4.0"
git tag -a v0.3.1 -m "v0.3.1" && git push origin main v0.3.1
```

Repeat for `gremlyn-arena`. **Both consumers must be green before either is tagged** — a core change that breaks arena while shield passes is still a broken release.

---

## Step 6 — Cross-compile the CLI

The user-facing artifact. Targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`.

```bash
cd gremlyn-core
VERSION=v0.4.0
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS="-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE"

mkdir -p dist && rm -f dist/*
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  GOOS=${target%/*}; GOARCH=${target#*/}
  ext=""; [ "$GOOS" = "windows" ] && ext=".exe"
  CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH \
    go build -trimpath -ldflags="$LDFLAGS" -o "dist/gremlyn-$GOOS-$GOARCH$ext" ./cmd/gremlyn
done
ls -lh dist/
```

Then **verify the stamping actually worked** — a binary that can't report its own version is unsupportable in the field:
```bash
./dist/gremlyn-linux-amd64 version   # must print v0.4.0, the commit, and the date
```

### Checksums — mandatory
```bash
cd dist && sha256sum * > SHA256SUMS && cat SHA256SUMS
```
A security tool shipping unverifiable binaries is not shippable.

---

## Step 7 — Publish

```bash
gh release create v0.4.0 dist/* \
  --title "gremlyn-core v0.4.0" \
  --notes-file RELEASE_NOTES.md
```

Release notes must include:
- **⚠️ Breaking changes first.** Every `pkg/` API change, with the version chain spelled out: `core v0.4.0 → shield v0.3.1, arena v0.3.1`
- **Score-weight changes**, if any — they break comparability with every historical resilience report
- **Migrations** — which store(s), and whether a user's local DB migrates automatically on next launch (and how long that takes)
- **Detection changes** with their false-positive impact — that's the user-visible risk
- Install instructions + the checksum verification command
- What changed per repo

---

## Post-release verification

```bash
# a real user's path
curl -sL <release-url>/gremlyn-linux-amd64 -o /tmp/g && chmod +x /tmp/g
/tmp/g version
/tmp/g doctor
/tmp/g wrap -- npx @modelcontextprotocol/server-memory
```

If the downloaded binary can't wrap a real MCP server, the release is broken regardless of what CI said.

---

## Blockers — do not release

- 🔴 `make check` failing in any repo (especially `-race`)
- 🔴 `npm run build` failing
- 🔴 `CGO_ENABLED=0` build failing
- 🔴 A `replace` directive in a tagged consumer
- 🔴 A consumer tagged before core
- 🔴 Zero-config startup requiring an external service
- 🔴 `gremlyn version` not reporting the injected version
- 🔴 Missing checksums
- 🔴 A dirty working tree at tag time
- 🔴 Reusing or moving an existing tag

## Rules

- Always state **which repo** each command runs in.
- **Never move a published tag.** Cut a patch.
- Core first. Always.
- `--dry-run` runs every verification step and tags nothing — prefer it for the first pass.
- Both consumers green before either is tagged.
