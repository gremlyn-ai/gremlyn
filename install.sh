#!/bin/sh
# Gremlyn installer.
#
#   curl -fsSL https://raw.githubusercontent.com/gremlyn-ai/gremlyn/main/install.sh | sh
#
# Downloads the latest release for this platform, verifies its checksum, and
# installs it. Set GREMLYN_VERSION to pin a version, GREMLYN_BIN_DIR to change the
# destination.
set -eu

REPO="gremlyn-ai/gremlyn"
BIN_DIR="${GREMLYN_BIN_DIR:-$HOME/.local/bin}"

say()  { printf '%s\n' "$*"; }
die()  { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

need uname
need mkdir
if command -v curl >/dev/null 2>&1; then DL="curl -fsSL -o"
elif command -v wget >/dev/null 2>&1; then DL="wget -qO"
else die "curl or wget is required"; fi

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) die "unsupported OS: $os (Windows: download the .zip from the releases page)" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

version="${GREMLYN_VERSION:-}"
if [ -z "$version" ]; then
  need sed
  api="https://api.github.com/repos/$REPO/releases/latest"
  tmp_meta=$(mktemp)
  $DL "$tmp_meta" "$api" || die "could not reach the GitHub API"
  version=$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$tmp_meta" | head -1)
  rm -f "$tmp_meta"
  [ -n "$version" ] || die "could not determine the latest version; set GREMLYN_VERSION"
fi

num=${version#v}
archive="gremlyn_${num}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading gremlyn $version ($os/$arch)…"
$DL "$tmp/$archive" "$base/$archive" || die "download failed: $base/$archive"

# Verifying the checksum is the point of shipping them. A security tool installed
# from an unverified download is a contradiction.
if $DL "$tmp/checksums.txt" "$base/checksums.txt" 2>/dev/null; then
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp" && sha256sum -c checksums.txt --ignore-missing) || die "checksum verification FAILED"
    say "Checksum verified."
  elif command -v shasum >/dev/null 2>&1; then
    want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
    got=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
    [ "$want" = "$got" ] || die "checksum verification FAILED"
    say "Checksum verified."
  else
    say "WARNING: no sha256 tool found, skipping verification."
  fi
else
  say "WARNING: could not download checksums.txt, skipping verification."
fi

need tar
tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$BIN_DIR"
for b in gremlyn shield arena; do
  [ -f "$tmp/$b" ] || continue
  install -m 0755 "$tmp/$b" "$BIN_DIR/$b" 2>/dev/null || {
    cp "$tmp/$b" "$BIN_DIR/$b" && chmod 0755 "$BIN_DIR/$b"
  }
  say "Installed $BIN_DIR/$b"
done

say ""
"$BIN_DIR/gremlyn" version || true
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say ""; say "$BIN_DIR is not on your PATH. Add it:"; say "  export PATH=\"\$PATH:$BIN_DIR\"" ;;
esac
say ""
say "Try it:  gremlyn wrap -- npx -y @modelcontextprotocol/server-memory"
