#!/bin/sh
# resumer installer — downloads the latest release binary.
#
#   curl -fsSL https://raw.githubusercontent.com/QuietSugar/resumer/main/install.sh | sh
#
# Env overrides:
#   RESUMER_INSTALL_DIR   target dir (default: ~/.local/bin)
#   RESUMER_VERSION       tag to install (default: latest, e.g. v0.2.0)
set -eu

REPO="QuietSugar/resumer"
INSTALL_DIR="${RESUMER_INSTALL_DIR:-$HOME/.local/bin}"

os="$(uname -s)"
case "$os" in
  Darwin) os="darwin" ;;
  Linux) os="linux" ;;
  *) echo "error: unsupported OS: $os (darwin/linux only; Windows is on the roadmap)" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *) echo "error: unsupported architecture: $arch" >&2; exit 1 ;;
esac

tag="${RESUMER_VERSION:-}"
if [ -z "$tag" ]; then
  # Resolve the latest tag from the releases/latest redirect (no API quota).
  tag="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')"
fi
if [ -z "$tag" ] || [ "$tag" = "latest" ]; then
  echo "error: could not resolve latest release tag" >&2
  exit 1
fi
version="${tag#v}"

archive="resumer_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading resumer $tag ($os/$arch)..."
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

cd "$tmp"
# Extract the expected line first: a grep miss inside a pipe would otherwise
# feed the checker empty stdin and could pass vacuously (sh has no pipefail).
expected="$(grep " $archive\$" checksums.txt || true)"
if [ -z "$expected" ]; then
  echo "error: $archive not found in checksums.txt — aborting" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  printf '%s\n' "$expected" | sha256sum -c - >/dev/null
else
  printf '%s\n' "$expected" | shasum -a 256 -c - >/dev/null
fi
echo "Checksum OK."

tar -xzf "$archive"
mkdir -p "$INSTALL_DIR"
install -m 0755 resumer "$INSTALL_DIR/resumer"

echo "Installed: $INSTALL_DIR/resumer"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo ""
    echo "note: $INSTALL_DIR is not on your PATH. Add this to your shell rc:"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac

"$INSTALL_DIR/resumer" --version
