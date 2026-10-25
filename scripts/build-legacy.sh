#!/usr/bin/env bash
# build-legacy.sh — produces the LEGACY Pluri-Deliver binaries that run on
# Windows 7 SP1 / 8 / 8.1 (x64 + x86).
#
# Why a separate pipeline:
#   * Go 1.21+ officially requires Windows 10+ — Windows 7/8/8.1 support was
#     dropped from the toolchain. The legacy build uses Go 1.20.14 (the last
#     toolchain that officially supports them).
#   * wails v2.10.1 (the version of the main build) requires Go >= 1.22.
#     The newest Wails that builds with Go 1.20 is v2.8.1 — hence the
#     dependency-downgraded source copy this script maintains.
#   * beeep (PowerShell/WinRT toasts) needs Go 1.21+; the legacy build is
#     compiled with the `legacywin` tag, which switches notifications to
#     tray balloons (Shell_NotifyIcon) — available on every Windows since
#     the nineties, rendered as toasts on 10/11.
#
# Requirements (see README.md "Windows compatibility"):
#   * Go 1.20.14 at $GO120 (default /home/z/.local/go-1.20)
#   * frontend/dist already built (vite) — it is copied, not rebuilt
#   * network access for `go mod tidy` (first run only)
#
# Usage:  bash scripts/build-legacy.sh [output-dir]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO120="${GO120:-/home/z/.local/go-1.20}"
WORK="${LEGACY_WORK:-$ROOT/.legacy-build}"
OUT="${1:-$WORK/bin}"
WAILS281_GOBIN="${WAILS281_GOBIN:-$HOME/.local/bin-wails281}"

if [ ! -x "$GO120/bin/go" ]; then
        echo "ERROR: Go 1.20 toolchain not found at $GO120" >&2
        echo "Install it with:" >&2
        echo "  curl -sL https://go.dev/dl/go1.20.14.linux-amd64.tar.gz -o /tmp/go1.20.14.tar.gz" >&2
        echo "  mkdir -p \$(dirname $GO120) && tar -C \$(dirname $GO120) -xzf /tmp/go1.20.14.tar.gz" >&2
        echo "  mv \$(dirname $GO120)/go $GO120" >&2
        exit 1
fi
if [ ! -d "$ROOT/frontend/dist" ]; then
        echo "ERROR: frontend/dist missing — run 'npm run build' in frontend/ first" >&2
        exit 1
fi

export PATH="$GO120/bin:$PATH"
echo ">> using $(go version)"

# 1) Fresh source copy (the main tree keeps its modern dependencies).
echo ">> staging legacy source at $WORK"
rm -rf "$WORK"
mkdir -p "$WORK"
rsync -a --delete \
        --exclude '.git' \
        --exclude '.legacy-build' \
        --exclude 'build/bin' \
        --exclude 'frontend/node_modules' \
        "$ROOT/" "$WORK/"

# 2) Dependency set that builds with Go 1.20 / targets Windows 7 SP1+.
cat > "$WORK/go.mod" <<'EOF'
module plurideliver

go 1.20

require (
        github.com/wailsapp/wails/v2 v2.8.1
        github.com/yuin/gopher-lua v1.1.1
        golang.org/x/sys v0.30.0
)
EOF
rm -f "$WORK/go.sum"

echo ">> go mod tidy (legacy deps)"
(cd "$WORK" && go mod tidy) >/dev/null

# The wails v2.8.1 CLI creates placeholder directories for go:embed GLOB
# patterns (a literal 'carriers/*.lua' dir + gitkeep — fixed upstream in
# v2.10 by an extension check), which then breaks go:embed. Directives in
# the staged tree must therefore avoid globs.
if grep -rq 'go:embed.*\*' "$WORK" --include='*.go'; then
        echo "ERROR: go:embed glob patterns present — replace with directory embeds (see scripts/build-legacy.sh)" >&2
        grep -rn 'go:embed.*\*' "$WORK" --include='*.go' >&2
        exit 1
fi

# 3) Wails CLI v2.8.1 (must match the module version), isolated GOBIN.
if [ ! -x "$WAILS281_GOBIN/wails" ]; then
        echo ">> installing wails CLI v2.8.1 into $WAILS281_GOBIN"
        mkdir -p "$WAILS281_GOBIN"
        GOBIN="$WAILS281_GOBIN" go install github.com/wailsapp/wails/v2/cmd/wails@v2.8.1
fi
export PATH="$WAILS281_GOBIN:$PATH"

# 4) Build both architectures with the legacywin tag.
mkdir -p "$OUT"
for ARCH in amd64 386; do
        echo ">> building windows/$ARCH (legacy)"
        (cd "$WORK" && CGO_ENABLED=0 wails build \
                -platform "windows/$ARCH" \
                -skipbindings -s \
                -tags legacywin \
                -clean) | tail -1
        cp "$WORK/build/bin/Pluri-Deliver.exe" "$OUT/Pluri-Deliver-Legacy-$([ "$ARCH" = amd64 ] && echo x64 || echo x86).exe"
done

echo ">> done:"
ls -la "$OUT"
