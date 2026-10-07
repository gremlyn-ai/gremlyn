#!/bin/sh
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

verify_signature() {
  cosign verify-blob "$tmp/checksums.txt" "$@" \
    --certificate-identity-regexp "^https://github.com/${REPO}/" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    >/dev/null 2>&1 \
    || die "SIGNATURE verification FAILED for checksums.txt — refusing to install"
}

if [ "${GREMLYN_SKIP_VERIFY:-0}" = "1" ]; then
  say "WARNING: GREMLYN_SKIP_VERIFY=1 — installing without verifying the download."
else
  $DL "$tmp/checksums.txt" "$base/checksums.txt" \
    || die "could not download checksums.txt; refusing to install an unverified binary"

  if command -v cosign >/dev/null 2>&1; then
    verified=""
    if $DL "$tmp/checksums.txt.sigstore.json" "$base/checksums.txt.sigstore.json" 2>/dev/null; then
      verify_signature --bundle "$tmp/checksums.txt.sigstore.json"
      verified=1
    elif $DL "$tmp/checksums.txt.sig" "$base/checksums.txt.sig" 2>/dev/null \
       && $DL "$tmp/checksums.txt.pem" "$base/checksums.txt.pem" 2>/dev/null; then
      verify_signature --signature "$tmp/checksums.txt.sig" --certificate "$tmp/checksums.txt.pem"
      verified=1
    fi
    if [ -n "$verified" ]; then
      say "Signature verified (cosign)."
    else
      say "NOTE: no signature published for this release; verifying the checksum only."
    fi
  else
    say "NOTE: cosign not found; verifying the checksum only. Install cosign to verify"
    say "      that the checksum manifest was really published by the release workflow."
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$tmp" && sha256sum -c checksums.txt --ignore-missing) || die "checksum verification FAILED"
  elif command -v shasum >/dev/null 2>&1; then
    want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
    [ -n "$want" ] || die "no checksum entry for $archive; refusing to install"
    got=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
    [ "$want" = "$got" ] || die "checksum verification FAILED"
  else
    die "no sha256 tool available (need sha256sum or shasum); refusing to install unverified"
  fi
  say "Checksum verified."
fi

need tar
tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$BIN_DIR"
[ -f "$tmp/gremlyn" ] || die "the archive does not contain a gremlyn binary"
install -m 0755 "$tmp/gremlyn" "$BIN_DIR/gremlyn" 2>/dev/null || {
  cp "$tmp/gremlyn" "$BIN_DIR/gremlyn" && chmod 0755 "$BIN_DIR/gremlyn"
}
say "Installed $BIN_DIR/gremlyn"

say ""
"$BIN_DIR/gremlyn" version || true
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say ""; say "$BIN_DIR is not on your PATH. Add it:"; say "  export PATH=\"\$PATH:$BIN_DIR\"" ;;
esac
say ""
say "Next:  gremlyn arena ci --config .gremlyn/arena.yaml"
