#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="${OUT_DIR:-dist}"
APP="pomogayka"
MIGRATE="pomogayka-migrate"

mkdir -p "$OUT"
rm -f "$OUT"/${APP}-* "$OUT"/${MIGRATE}-* "$OUT"/SHA256SUMS.txt 2>/dev/null || true

LDFLAGS="-s -w -X main.version=${VERSION}"
export CGO_ENABLED=0

build() {
  local goos="$1"
  local goarch="$2"
  local goarm="${3:-}"
  local suffix="$4"
  local pkg="$5"
  local bin="$6"

  local name="${bin}-${suffix}"
  if [[ "$goos" == "windows" ]]; then
    name="${name}.exe"
  fi

  echo "==> ${name} (GOOS=${goos} GOARCH=${goarch}${goarm:+ GOARM=$goarm})"
  if [[ -n "$goarm" ]]; then
    env GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
      go build -trimpath -ldflags "$LDFLAGS" -o "${OUT}/${name}" "$pkg"
  else
    env GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags "$LDFLAGS" -o "${OUT}/${name}" "$pkg"
  fi
}

build linux amd64 "" "linux-amd64" ./cmd/server "$APP"
build linux amd64 "" "linux-amd64" ./cmd/migrate-sqlite-to-postgres "$MIGRATE"

build windows amd64 "" "windows-amd64" ./cmd/server "$APP"
build windows amd64 "" "windows-amd64" ./cmd/migrate-sqlite-to-postgres "$MIGRATE"

build linux arm64 "" "linux-arm64" ./cmd/server "$APP"
build linux arm64 "" "linux-arm64" ./cmd/migrate-sqlite-to-postgres "$MIGRATE"

build linux arm 7 "linux-armv7" ./cmd/server "$APP"
build linux arm 7 "linux-armv7" ./cmd/migrate-sqlite-to-postgres "$MIGRATE"

(
  cd "$OUT"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 ${APP}-* ${MIGRATE}-* > SHA256SUMS.txt
  else
    sha256sum ${APP}-* ${MIGRATE}-* > SHA256SUMS.txt
  fi
)

echo
echo "Version: ${VERSION}"
echo "Artifacts in ${OUT}/:"
ls -lh "$OUT"
