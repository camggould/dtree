#!/usr/bin/env sh
# dtree installer — downloads the right pre-built binary from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/camggould/dtree/main/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- --version v0.2.0
#   curl -fsSL .../install.sh | sh -s -- --prefix ~/.local/bin
#
# What it does (and what it doesn't):
# - Detects the host OS (linux/darwin) and architecture (amd64/arm64).
# - Resolves the latest published release, or the explicit --version.
# - Downloads the matching tarball + its .sha256, verifies, extracts,
#   and installs to /usr/local/bin (or --prefix).
# - Falls back to ~/.local/bin without sudo if /usr/local/bin isn't
#   writable.
# - Verifies the install by running `dtree version`.
#
# Does NOT install Go, Node, npm, or any other build tooling.
# The dtree binary ships everything it needs (UI is embedded; SQLite
# with FTS5 is statically linked; no runtime deps beyond libc).

set -eu

REPO="camggould/dtree"
PREFIX="/usr/local/bin"
VERSION=""

usage() {
  cat <<EOF >&2
Usage: $0 [--version vX.Y.Z] [--prefix /install/dir]

Options:
  --version  vX.Y.Z   Specific release tag to install (default: latest).
  --prefix   DIR      Install directory (default: /usr/local/bin, falls back
                      to \$HOME/.local/bin if /usr/local/bin isn't writable).
  --help              Show this message.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --prefix)  PREFIX="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage; exit 1 ;;
  esac
done

err() { echo "error: $*" >&2; exit 1; }
info() { echo "→ $*"; }

# --- Detect platform ---
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux|darwin) ;;
  *) err "unsupported OS: $OS (only linux and darwin are published)" ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) err "unsupported architecture: $ARCH (only amd64 and arm64 are published)" ;;
esac

# --- Required tools ---
need() { command -v "$1" >/dev/null 2>&1 || err "missing required tool: $1"; }
need curl
need tar
need uname
if ! command -v shasum >/dev/null 2>&1 && ! command -v sha256sum >/dev/null 2>&1; then
  err "need either 'shasum' or 'sha256sum' for checksum verification"
fi

# --- Resolve version ---
if [ -z "$VERSION" ]; then
  info "Resolving latest release..."
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' \
    | head -1)"
  [ -n "$VERSION" ] || err "could not resolve latest release tag"
fi
info "Installing dtree ${VERSION} for ${OS}/${ARCH}"

# --- Download ---
ASSET="dtree-${VERSION}-${OS}-${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

info "Downloading ${URL}"
curl -fsSL -o "${TMPDIR}/${ASSET}" "$URL" \
  || err "failed to download ${URL} (does the release / asset exist?)"

curl -fsSL -o "${TMPDIR}/${ASSET}.sha256" "${URL}.sha256" \
  || err "failed to download checksum"

# --- Verify checksum ---
info "Verifying checksum"
(
  cd "$TMPDIR"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -c "${ASSET}.sha256" >/dev/null \
      || err "checksum verification failed"
  else
    sha256sum -c "${ASSET}.sha256" >/dev/null \
      || err "checksum verification failed"
  fi
)

# --- Extract ---
info "Extracting"
tar -C "$TMPDIR" -xzf "${TMPDIR}/${ASSET}"

# The tarball contains a single binary named dtree-<version>-<os>-<arch>.
BINARY="${TMPDIR}/dtree-${VERSION}-${OS}-${ARCH}"
[ -f "$BINARY" ] || err "tarball did not contain expected binary: ${BINARY}"

# --- Choose install dir ---
if [ ! -w "$PREFIX" ] && [ "$PREFIX" = "/usr/local/bin" ]; then
  alt="$HOME/.local/bin"
  info "/usr/local/bin not writable; falling back to ${alt}"
  PREFIX="$alt"
fi
mkdir -p "$PREFIX"

DEST="${PREFIX}/dtree"
info "Installing to ${DEST}"
install -m 0755 "$BINARY" "$DEST" 2>/dev/null || {
  # 'install' isn't on every platform; cp+chmod is the fallback.
  cp "$BINARY" "$DEST" && chmod 0755 "$DEST"
}

# --- Verify ---
case ":${PATH}:" in
  *":${PREFIX}:"*) ;;
  *)
    cat <<EOF
✓ Installed to ${DEST}, but ${PREFIX} is not on your PATH.
  Add this to your shell profile:

    export PATH="${PREFIX}:\$PATH"

EOF
    exit 0
    ;;
esac

info "Installed:"
"$DEST" version || err "binary installed but failed to run"

cat <<EOF

✓ dtree ${VERSION} is installed.

Next steps:
  dtree init            # initialise a .decisions/ directory in your repo
  dtree ui              # open the web UI
  dtree mcp --as you    # expose decision tools to an AI agent

Docs: https://github.com/${REPO}#readme
EOF
