#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

docker build -t ikun2api:latest \
    --build-arg GOPROXY=https://goproxy.cn,direct \
    --build-arg GOSUMDB=sum.golang.google.cn \
    -f "${REPO_ROOT}/Dockerfile" \
    "${REPO_ROOT}"
