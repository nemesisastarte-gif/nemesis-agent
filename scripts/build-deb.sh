#!/usr/bin/env bash
# Construction du paquet .deb tout-en-un de NemesisCode.
#
#   scripts/build-deb.sh [version]
#
# Prérequis : Go 1.25+, Node 22+, pnpm (frontend), dpkg-deb.
# Produit : dist-deb/nemesiscode_<version>_amd64.deb
#
# Particularités du paquet :
#   - binaire backend 100% STATIQUE : CGO_ENABLED=0 + SQLite pure-Go
#     (github.com/ncruces/go-sqlite3) → aucune dépendance glibc, aucun AVX,
#     tourne sur n'importe quel Linux x86-64 (Core 2 Duo inclus) ;
#   - frontend compilé en mode offline et servi par le backend à la racine
#     (route_prefix "/" + fallback SPA) ;
#   - contrôleur `nemesiscode on|off|status|logs` (aucun systemd requis).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-1.0.0}"
OUT_DIR="$ROOT/dist-deb"
STAGE="$OUT_DIR/stage"
DEB="$OUT_DIR/nemesiscode_${VERSION}_amd64.deb"

ARCH="amd64"
export CGO_ENABLED=0
export GOAMD64=v1          # compat Core 2 Duo : pas d'AVX, SSE2 suffit
export GOFLAGS=-mod=mod
export GOSUMDB=off
export GOPROXY=direct

echo "==> [1/4] Frontend (mode offline)…"
(cd "$ROOT/frontend" && pnpm install --frozen-lockfile >/dev/null && pnpm run build:offline)

echo "==> [2/4] Backend (binaire statique, GOAMD64=v1)…"
(cd "$ROOT/backend" && go mod tidy && GOGC=40 go build -p "${GO_BUILD_P:-2}" \
  -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.version=${VERSION}" -o "$OUT_DIR/nemesiscode-server" ./cmd/server)

echo "==> [3/4] Assemblage du paquet…"
rm -rf "$STAGE"
mkdir -p "$STAGE/DEBIAN"
mkdir -p "$STAGE/usr/bin"
mkdir -p "$STAGE/usr/share/nemesiscode/web"
mkdir -p "$STAGE/usr/share/doc/nemesiscode"

sed "s/^Version: .*/Version: $VERSION/" "$ROOT/packaging/deb/DEBIAN/control" > "$STAGE/DEBIAN/control"
install -m 0755 "$ROOT/packaging/deb/usr/bin/nemesiscode" "$STAGE/usr/bin/nemesiscode"
install -m 0755 "$OUT_DIR/nemesiscode-server" "$STAGE/usr/share/nemesiscode/nemesiscode-server"
cp -a "$ROOT/frontend/dist/." "$STAGE/usr/share/nemesiscode/web/"
install -m 0644 "$ROOT/docs/deb-package.md" "$STAGE/usr/share/doc/nemesiscode/README.md"
gzip -9n -c "$ROOT/docs/deb-package.md" > "$STAGE/usr/share/doc/nemesiscode/README.md.gz" 2>/dev/null || true
rm -f "$STAGE/usr/share/doc/nemesiscode/README.md"

echo "==> [4/4] dpkg-deb…"
dpkg-deb --root-owner-group --build "$STAGE" "$DEB"
rm -rf "$STAGE" "$OUT_DIR/nemesiscode-server"

echo
echo "✅ Paquet créé : $DEB"
echo "   Installation : sudo dpkg -i $DEB"
echo "   Puis : nemesiscode on  →  http://localhost:5000  (Admin / Admin)"
