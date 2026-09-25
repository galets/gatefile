#!/usr/bin/env bash
# Build a .deb for gatefile (no external deps besides go + dpkg-deb).
# Usage: ./packaging/build-deb.sh [--version 0.1.0] [--arch amd64] [--out dist/]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION=""
ARCH=""
OUT="dist"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --arch) ARCH="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    -h|--help) echo "Usage: $0 [--version X.Y.Z] [--arch ARCH] [--out DIR]"; exit 0 ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

if [[ -z "$VERSION" ]]; then
  VERSION="$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo 0.1.0)"
  VERSION="${VERSION#v}"
fi
if [[ -z "$ARCH" ]]; then
  ARCH="$(dpkg --print-architecture 2>/dev/null || echo amd64)"
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
PKGDIR="$STAGE/gatefile_${VERSION}_${ARCH}"
mkdir -p "$PKGDIR/DEBIAN" "$PKGDIR/usr/bin" \
  "$PKGDIR/usr/share/doc/gatefile"

echo "==> building gatefile $VERSION ($ARCH)"
CGO_ENABLED=0 go -C "$ROOT" build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o "$PKGDIR/usr/bin/gatefile" ./cmd/gatefile

# NOTE: packaging/gatefile.env and packaging/gatefile.service are kept
# in the repo as reference only and are intentionally NOT installed
# by this package (no service setup yet).
install -m 0644 "$ROOT/README.md" "$PKGDIR/usr/share/doc/gatefile/README.md"
install -m 0644 "$ROOT/doc/HOWTO.md" "$PKGDIR/usr/share/doc/gatefile/HOWTO.md"
install -m 0644 "$ROOT/LICENSE" "$PKGDIR/usr/share/doc/gatefile/copyright" 2>/dev/null || true

cat > "$PKGDIR/DEBIAN/control" <<EOF
Package: gatefile
Version: $VERSION
Section: net
Priority: optional
Architecture: $ARCH
Maintainer: galets <gatefile@x11.work>
Description: Single-document sync server with ETags and SSE
 Gatefile is a stateless single-document synchronization server that
 coordinates concurrent edits (optimistic concurrency via ETags) and
 streams real-time updates using Server-Sent Events.
Homepage: https://github.com/galets/gatefile
EOF

chmod 0755 "$PKGDIR/usr/bin/gatefile"

mkdir -p "$ROOT/$OUT"
DEB="$ROOT/$OUT/gatefile_${VERSION}_${ARCH}.deb"
dpkg-deb --root-owner-group --build "$PKGDIR" "$DEB"
echo "==> wrote $DEB"
dpkg-deb -c "$DEB" | head -30
dpkg-deb -f "$DEB" Package Version Architecture Maintainer
