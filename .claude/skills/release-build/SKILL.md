---
name: release-build
description: Cut a Gremlyn release — one tag on one module, the `make check` gate, cross-compiled binaries with checksums, and release notes. Use when the user says "cut a release", "release", "tag a version", "build the binaries", "publish", or runs /release-build.
---

# Release Build

Cut a release. Gremlyn is **one git repo, one Go module** (`github.com/gremlyn-ai/gremlyn`), so a release is **one tag**. There is no dependency ordering left: `pkg/` and everything importing it ship in the same commit, consistent by construction.

What's still easy to get wrong is the artifact — a tag you can't un-publish, a binary that can't report its own version, a build that quietly needs CGO, checksums nobody generated.

## Usage

```
/release-build v0.4.0             # verify, build, tag, publish
/release-build v0.4.1 --dry-run   # every verification step, tags nothing
```

## ⛔ Read before anything else

- **Never move a published tag.** Go's module proxy caches by version forever; a moved tag serves stale content to everyone, including you, and no amount of `GOFLAGS` gymnastics fixes it cleanly. Cut a patch instead.
- **Never tag a dirty tree** — the tag won't match what you tested.
- The module path is `github.com/gremlyn-ai/gremlyn`. If the repo isn't published there yet, **say so and stop**: tagging and `go install` both need that path fetchable.
- There are **no existing tags**. The four pre-merge repos had zero commits and were never published, so there's no legacy version to honour — the first tag sets the baseline. Pick it deliberately.

---

## Step 1 — Pre-flight

```bash
cd /home/sahra/Documents/sahra-perso/gremlyn
git rev-parse --abbrev-ref HEAD
git status --short
git tag --sort=-creatordate | head -5
```

Blockers:
- [ ] Uncommitted changes → commit or stash. **Never tag a dirty tree.**
- [ ] Not on the release branch (`main`) → confirm with the user.
- [ ] The proposed version already exists as a tag → pick the next one. Do not reuse.

---

## Step 2 — The gate

Once, at the repo root — it covers the whole module:

```bash
make check
```

`make check` = `go vet ./...` + `golangci-lint run` + `go test ./... -race`. All three must be clean. **`-race` failing blocks the release** — a data race on a policy decision is a vulnerability, not a flake.

Dashboard:
```bash
cd dashboard && npm ci && npm run typecheck && npm run lint && npx vitest run && npm run build
```
`npm ci`, not `npm install` — the lockfile is the contract. **`npm run build` must pass**; a dev server that renders while the build fails is a common Next.js gap. (`make dashboard-check` runs everything after the `npm ci`.)

---

## Step 3 — Release-specific verification

### CGO must stay off
```bash
CGO_ENABLED=0 go build -o /tmp/cgotest ./cmd/gremlyn && echo OK
```
Failure blocks the release — pure-Go SQLite (`modernc.org/sqlite`) exists precisely so static cross-compiled binaries ship.

### Zero-config path
```bash
docker compose down 2>/dev/null
make build
timeout 5 ./bin/shield        # must start with NOTHING else running
```
SQLite default, no Postgres, no Redis, no ML sidecar. If it needs a service to boot, the zero-config promise is broken and that's a release blocker. Same check for `./bin/arena`.

### Startup migration timing
A migration runs on the **user's machine at launch** and can't be hotfixed. If this release adds one, time it against a realistic DB:
```bash
ls -lh ~/.gremlyn/*.db
time (timeout 30 ./bin/shield)
```
> 5s of migration stall needs release-note treatment. Loop in `database-engineer`.

---

## Step 4 — Cross-compile

Targets: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64` — for all three binaries. A local-first user runs the CLI *and* both services.

```bash
VERSION=v0.4.0
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
CLIPKG=github.com/gremlyn-ai/gremlyn/internal/cli
LDFLAGS="-s -w -X $CLIPKG.Version=$VERSION -X $CLIPKG.Commit=$COMMIT -X $CLIPKG.BuildDate=$DATE"

mkdir -p dist && rm -f dist/*
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
  GOOS=${target%/*}; GOARCH=${target#*/}
  ext=""; [ "$GOOS" = "windows" ] && ext=".exe"
  for bin in gremlyn shield arena; do
    CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH \
      go build -trimpath -ldflags="$LDFLAGS" -o "dist/$bin-$GOOS-$GOARCH$ext" ./cmd/$bin
  done
done
ls -lh dist/
```

The stamp targets `internal/cli.{Version,Commit,BuildDate}`, **not** `main.*` — a stale `-X main.version` fails silently and ships `dev`. So **verify the stamping actually worked**; a binary that can't report its own version is unsupportable in the field:
```bash
./dist/gremlyn-linux-amd64 version   # must print v0.4.0, the commit, and the date
```

GoReleaser replaces this whole step (PLAN.md P1). Until then this loop is the contract.

### Checksums — mandatory
```bash
cd dist && sha256sum * > SHA256SUMS && cat SHA256SUMS
```
A security tool shipping unverifiable binaries is not shippable.

---

## Step 5 — Tag and publish

Tag last, so the tag points at a commit whose full matrix you already built.

```bash
git tag -a v0.4.0 -m "v0.4.0"
git push origin main v0.4.0
gh release create v0.4.0 dist/* \
  --title "gremlyn v0.4.0" \
  --notes-file RELEASE_NOTES.md
```

Then confirm the module proxy sees it — `go install` depends on it and `GOPROXY` can lag briefly:
```bash
GOPROXY=proxy.golang.org go list -m github.com/gremlyn-ai/gremlyn@v0.4.0
```

Release notes must include:
- **⚠️ Breaking changes first.** Every `pkg/` API change, plus every CLI flag, config key, and API-response change. One module means one version to cite — cite it.
- **Score-weight changes**, if any — they break comparability with every historical resilience report
- **Migrations** — which store(s), and whether a user's local DB migrates automatically on next launch (and how long that takes)
- **Detection changes** with their false-positive impact — that's the user-visible risk
- Install instructions (`go install …@v0.4.0`, direct download) + the checksum verification command
- What changed per area: core/proxy, shield, arena, dashboard

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

- 🔴 `make check` failing (especially `-race`)
- 🔴 `npm run build` failing
- 🔴 `CGO_ENABLED=0` build failing
- 🔴 A cross-compile target failing
- 🔴 Zero-config startup requiring an external service
- 🔴 `gremlyn version` not reporting the injected version
- 🔴 Missing checksums
- 🔴 A dirty working tree at tag time
- 🔴 Reusing or moving an existing tag

## Rules

- One repo, one module, one tag. There is no ordering problem left to solve — don't reinvent one.
- Every command runs at the repo root, except the one `cd dashboard`.
- **Never move a published tag.** Cut a patch.
- Build the full matrix before tagging; tag the commit you built.
- `--dry-run` runs every verification step and tags nothing — prefer it for the first pass.
