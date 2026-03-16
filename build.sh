#!/bin/zsh
set -e

BUILD_DIR=".build"
BUILD_LOG="$BUILD_DIR/run.log"
mkdir -p "$BUILD_DIR"

log() {
  echo "[build.sh $(date '+%H:%M:%S')] $1" | tee -a "$BUILD_LOG"
}

# Build React UI to ui/dist/ (embedded into Go binary via go:embed).
# Only rebuild UI if source files changed (avoids unnecessary npm run build on Go-only changes).
UI_SRC_HASH=$(find ui/src -type f \( -name '*.ts' -o -name '*.tsx' -o -name '*.css' -o -name '*.html' \) -exec stat -f '%m' {} + 2>/dev/null | sort | md5)
HASH_FILE="$BUILD_DIR/.ui_hash"

if [ ! -f "$HASH_FILE" ] || [ "$(cat "$HASH_FILE" 2>/dev/null)" != "$UI_SRC_HASH" ]; then
  log "UI sources changed, rebuilding React..."
  if (cd ui && npm run build 2>&1 | tee -a "$BUILD_LOG"); then
    echo "$UI_SRC_HASH" > "$HASH_FILE"
    log "UI build succeeded."
  else
    log "WARNING: UI build failed — continuing with existing ui/dist/"
    # Don't abort — Go binary can still be built with the last good ui/dist/
  fi
else
  log "UI unchanged, skipping React build."
fi

# Build Go binary
log "Building Go binary..."
if go build -o "$BUILD_DIR/hyperax" ./cmd/hyperax 2>&1 | tee -a "$BUILD_LOG"; then
  log "Go build succeeded."
else
  log "ERROR: Go build failed."
  exit 1
fi
