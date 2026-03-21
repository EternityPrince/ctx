#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
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

CTX_BINDIR="$BINDIR" "$ROOT_DIR/scripts/uninstall.sh"
CTX_BINDIR="$BINDIR" "$ROOT_DIR/scripts/install.sh"
