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
VERSION="${1:-1.2.0}"
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
# Aucun postinstall n'est requis pour le build Vite (les binaires esbuild sont
# des optionalDependencies). --ignore-scripts évite aussi qu'un pnpm récent
# bloque sur sa politique allowBuilds lors d'un build propre.
(cd "$ROOT/frontend" && pnpm install --frozen-lockfile --ignore-scripts >/dev/null && pnpm run build:offline)

echo "==> [2/4] Backend (binaire statique, GOAMD64=v1)…"
(cd "$ROOT/backend" && go mod tidy && GOGC=40 go build -p "${GO_BUILD_P:-2}" \
  -trimpath -buildvcs=false \
  -ldflags "-s -w -X main.version=${VERSION}" -o "$OUT_DIR/nemesiscode-server" ./cmd/server)

echo "==> [2b/4] Moteur opencode (binaire baseline x64 — vieux CPU, SSE2)…"
# opencode (https://github.com/anomalyco/opencode) est distribué sur npm :
# le paquet opencode-linux-x64-baseline contient le binaire compilé pour
# x86-64 sans exigence AVX (baseline). Même version que le CLI opencode-ai.
OC_DIR="$OUT_DIR/opencode"
mkdir -p "$OC_DIR"
echo '{"name":"opencode-embed","version":"1.0.0","private":true}' > "$OC_DIR/package.json"
OC_VER="$(npm view opencode-ai version 2>/dev/null || echo latest)"
(cd "$OC_DIR" && npm install --no-audit --no-fund \
  "opencode-ai@$OC_VER" "opencode-linux-x64-baseline@$OC_VER" >/dev/null 2>&1)
if [ ! -x "$OC_DIR/node_modules/opencode-linux-x64-baseline/bin/opencode" ]; then
  echo "❌ opencode-linux-x64-baseline est introuvable." >&2
  echo "   Refus du fallback x64 standard : il utilise AVX et plante sur Core 2 Duo." >&2
  exit 1
fi
"$OC_DIR/node_modules/opencode-linux-x64-baseline/bin/opencode" --version 2>/dev/null \
  || { echo "❌ le binaire opencode baseline ne démarre pas sur cette machine." >&2; exit 1; }

echo "==> [3/4] Assemblage du paquet…"
rm -rf "$STAGE"
mkdir -p "$STAGE/DEBIAN"
mkdir -p "$STAGE/usr/bin"
mkdir -p "$STAGE/usr/share/nemesiscode/web"
mkdir -p "$STAGE/usr/share/doc/nemesiscode"

sed "s/^Version: .*/Version: $VERSION/" "$ROOT/packaging/deb/DEBIAN/control" > "$STAGE/DEBIAN/control"
install -m 0755 "$ROOT/packaging/deb/usr/bin/nemesiscode" "$STAGE/usr/bin/nemesiscode"
install -m 0755 "$OUT_DIR/nemesiscode-server" "$STAGE/usr/share/nemesiscode/nemesiscode-server"
install -m 0755 "$OC_DIR/node_modules/opencode-linux-x64-baseline/bin/opencode" \
  "$STAGE/usr/share/nemesiscode/opencode"
cp -a "$ROOT/frontend/dist/." "$STAGE/usr/share/nemesiscode/web/"
install -m 0644 "$ROOT/docs/deb-package.md" "$STAGE/usr/share/doc/nemesiscode/README.md"
gzip -9n -c "$ROOT/docs/deb-package.md" > "$STAGE/usr/share/doc/nemesiscode/README.md.gz" 2>/dev/null || true
rm -f "$STAGE/usr/share/doc/nemesiscode/README.md"

echo "==> [4/4] dpkg-deb…"
dpkg-deb --root-owner-group --build "$STAGE" "$DEB"
rm -rf "$STAGE" "$OUT_DIR/nemesiscode-server" "$OC_DIR"

echo
echo "✅ Paquet créé : $DEB"
echo "   Installation : sudo dpkg -i $DEB"
echo "   Puis : nemesiscode on  →  http://localhost:5000  (Admin / Admin)"
echo "   Moteur agent opencode EMBARQUÉ (aucune installation requise)."
