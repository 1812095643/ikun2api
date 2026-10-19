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

exec "$@"
