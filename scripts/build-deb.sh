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
VERSION="${1:-1.2.2}"
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

echo "==> [2b/4] Moteur opencode portable (Go, x86-64-v1)…"
# Le binaire officiel actuel repose sur Bun. Même sa variante "baseline"
# exige SSE4.2 (Nehalem+) et plante par SIGILL sur Core 2 Duo. Nous compilons
# donc la dernière version Go d'OpenCode, épinglée et adaptée au protocole
# NemesisCode. GOAMD64=v1 garantit seulement le socle x86-64/SSE2.
OC_DIR="$OUT_DIR/opencode-portable-src"
OC_TAG="v0.0.52"
OC_COMMIT="2b258b14732c9a0f50cc3552a27ebf0f68be4e53"
rm -rf "$OC_DIR"
git clone --quiet --depth 1 --branch "$OC_TAG" https://github.com/sst/opencode.git "$OC_DIR"
if [ "$(git -C "$OC_DIR" rev-parse HEAD)" != "$OC_COMMIT" ]; then
  echo "❌ La source OpenCode $OC_TAG ne correspond pas au commit attendu." >&2
  exit 1
fi
git -C "$OC_DIR" apply "$ROOT/packaging/opencode-portable-v0.0.52.patch"
(cd "$OC_DIR" && gofmt -w cmd internal && go test ./internal/config ./internal/format ./internal/llm/models)
(cd "$OC_DIR" && GOGC=40 go build -p "${GO_BUILD_P:-2}" \
  -trimpath -buildvcs=false \
  -ldflags '-s -w -X github.com/sst/opencode/internal/version.Version=0.0.52-nemesis-portable' \
  -o "$OUT_DIR/opencode-portable" ./main.go)
"$OUT_DIR/opencode-portable" --version | grep -Fx '0.0.52-nemesis-portable' >/dev/null
"$OUT_DIR/opencode-portable" --nemesis-protocol-version | grep -Fx '1' >/dev/null

echo "==> [3/4] Assemblage du paquet…"
rm -rf "$STAGE"
mkdir -p "$STAGE/DEBIAN"
mkdir -p "$STAGE/usr/bin"
mkdir -p "$STAGE/usr/share/nemesiscode/web"
mkdir -p "$STAGE/usr/share/doc/nemesiscode"

sed "s/^Version: .*/Version: $VERSION/" "$ROOT/packaging/deb/DEBIAN/control" > "$STAGE/DEBIAN/control"
install -m 0755 "$ROOT/packaging/deb/usr/bin/nemesiscode" "$STAGE/usr/bin/nemesiscode"
install -m 0755 "$OUT_DIR/nemesiscode-server" "$STAGE/usr/share/nemesiscode/nemesiscode-server"
install -m 0755 "$ROOT/packaging/deb/usr/share/nemesiscode/opencode" \
  "$STAGE/usr/share/nemesiscode/opencode"
install -m 0755 "$OUT_DIR/opencode-portable" \
  "$STAGE/usr/share/nemesiscode/opencode-portable"
install -m 0644 "$OC_DIR/LICENSE" \
  "$STAGE/usr/share/doc/nemesiscode/LICENSE.opencode"
cp -a "$ROOT/frontend/dist/." "$STAGE/usr/share/nemesiscode/web/"
install -m 0644 "$ROOT/docs/deb-package.md" "$STAGE/usr/share/doc/nemesiscode/README.md"
gzip -9n -c "$ROOT/docs/deb-package.md" > "$STAGE/usr/share/doc/nemesiscode/README.md.gz" 2>/dev/null || true
rm -f "$STAGE/usr/share/doc/nemesiscode/README.md"

echo "==> [4/4] dpkg-deb…"
dpkg-deb --root-owner-group --build "$STAGE" "$DEB"
rm -rf "$STAGE" "$OUT_DIR/nemesiscode-server" "$OUT_DIR/opencode-portable" "$OC_DIR"

echo
echo "✅ Paquet créé : $DEB"
echo "   Installation : sudo dpkg -i $DEB"
echo "   Puis : nemesiscode on  →  http://localhost:5000  (Admin / Admin)"
echo "   Moteur agent opencode portable EMBARQUÉ (x86-64-v1, aucune installation requise)."
