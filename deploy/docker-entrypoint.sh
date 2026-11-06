#!/bin/sh
set -e

if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/data
    chown -R ikun2api:ikun2api /app/data 2>/dev/null || true
    exec su-exec ikun2api "$0" "$@"
fi

if [ "${1#-}" != "$1" ]; then
    set -- /app/ikun2api "$@"
fi

if [ "$1" = "/app/ikun2api" ]; then
    runtime=/app/data/runtime
    # Reuse the official binary updater for a newer image; keep newer web updates.
    binary=$(/app/ikun2api --prepare-runtime "$runtime")
    shift
    set -- "$binary" --supervise "$@"
fi
exec "$@"
