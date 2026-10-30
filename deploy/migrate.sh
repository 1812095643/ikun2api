#!/usr/bin/env bash
set -euo pipefail
if [ "$(id -u)" != 0 ]; then echo '请使用 sudo bash 运行迁入脚本。' >&2; exit 1; fi
command -v python3 >/dev/null || { echo '请先安装 Python 3.9+' >&2; exit 1; }
command -v openssl >/dev/null || { echo '请先安装 openssl' >&2; exit 1; }
file=$(mktemp)
trap 'rm -f "$file"' EXIT
curl -q -fsSL --proto '=https' --tlsv1.2 https://raw.githubusercontent.com/1812095643/ikun2api/main/deploy/switch-from-sub2api.py -o "$file"
python3 "$file" "$@"
