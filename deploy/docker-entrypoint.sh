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
    mkdir -p "$runtime"
    image_version=$(/app/ikun2api --version 2>&1 | head -n 1)
    previous_image=$(cat "$runtime/.image-version" 2>/dev/null || true)
    if [ ! -e "$runtime/ikun2api.upgrade.json" ] && { [ ! -x "$runtime/ikun2api" ] || [ "$image_version" != "$previous_image" ]; }; then
        cp /app/ikun2api "$runtime/.ikun2api.new"
        chmod 755 "$runtime/.ikun2api.new"
        mv -f "$runtime/.ikun2api.new" "$runtime/ikun2api"
        printf '%s\n' "$image_version" > "$runtime/.image-version"
    fi
    shift
    set -- "$runtime/ikun2api" --supervise "$@"
fi
exec "$@"
