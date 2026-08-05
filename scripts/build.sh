#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
GOROOT=/opt/hec/toolchains/go/1.26.2
PATH="$GOROOT/bin:$PATH"
export GOROOT PATH

VERSION=${HEC_VERSION:-0.0.1}
COMMIT=$(git -C "$ROOT_DIR" rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
BUILD_DATE=${SOURCE_DATE_EPOCH:+$(date -u -d "@$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ)}
BUILD_DATE=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
OUT=${HEC_OUTPUT:-$ROOT_DIR/dist/hec}

mkdir -p "$(dirname -- "$OUT")"
cd "$ROOT_DIR"
"$GOROOT/bin/go" build \
  -trimpath \
  -ldflags "-s -w -X github.com/StealthEyeLLC/hec/internal/hec.Version=$VERSION -X github.com/StealthEyeLLC/hec/internal/hec.BuildCommit=$COMMIT -X github.com/StealthEyeLLC/hec/internal/hec.BuildDate=$BUILD_DATE" \
  -o "$OUT" \
  ./cmd/hec
