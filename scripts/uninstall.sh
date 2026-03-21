#!/usr/bin/env sh
set -eu

APP_NAME="ctx"
BINDIR="${CTX_BINDIR:-${HOME}/.local/bin}"
TARGET="$BINDIR/$APP_NAME"

if [ ! -e "$TARGET" ]; then
    printf 'Nothing to remove: %s does not exist\n' "$TARGET"
    exit 0
fi

rm -f "$TARGET"
printf 'Removed %s\n' "$TARGET"
