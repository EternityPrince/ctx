#!/usr/bin/env sh
set -eu

APP_NAME="ctx"
ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
BUILD_CACHE="${GOCACHE:-$ROOT_DIR/.cache/go-build}"
BINDIR="${CTX_BINDIR:-}"

if [ -z "$BINDIR" ]; then
    OLD_IFS=$IFS
    IFS=:
    for dir in $PATH; do
        if [ -n "$dir" ] && [ -d "$dir" ] && [ -w "$dir" ]; then
            BINDIR="$dir"
            break
        fi
    done
    IFS=$OLD_IFS
fi

if [ -z "$BINDIR" ]; then
    BINDIR="$HOME/.local/bin"
fi

mkdir -p "$BINDIR" "$BUILD_CACHE"

cd "$ROOT_DIR"
GOCACHE="$BUILD_CACHE" go build -o "$BINDIR/$APP_NAME" ./cmd/ctx

printf 'Installed %s to %s\n' "$APP_NAME" "$BINDIR/$APP_NAME"

case ":$PATH:" in
    *":$BINDIR:"*)
        printf '%s is already available in PATH\n' "$APP_NAME"
        ;;
    *)
        printf 'Add this line to your shell config to use %s everywhere:\n' "$APP_NAME"
        printf 'export PATH="%s:$PATH"\n' "$BINDIR"
        ;;
esac
