#!/usr/bin/env bash

set -euo pipefail


BIN_PATH=""
SOURCE_PATH=""
INSTALL_DIR="/opt/ikun2api"
DATA_DIR="/var/lib/ikun2api/datamanagement"
SERVICE_FILE_NAME="ikun2api-datamanagementd.service"

function print_help() {
  cat <<'EOF'
Usage:
  install-datamanagementd.sh [--binary <binary-path>] [--source <repository-path>]

Arguments:
  --binary   Install an existing datamanagementd binary
  --source   Build datamanagementd from the supplied repository
  -h, --help Show this help

Examples:
  sudo ./install-datamanagementd.sh --binary ./datamanagement/datamanagementd
  sudo ./install-datamanagementd.sh --source /opt/ikun2api-src
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      BIN_PATH="${2:-}"
      shift 2
      ;;
    --source)
      SOURCE_PATH="${2:-}"
      shift 2
      ;;
    -h|--help)
      print_help
      exit 0
      ;;
    *)
      echo "Unknown argument: $1"
      print_help
      exit 1
      ;;
  esac
done

if [[ -n "$BIN_PATH" && -n "$SOURCE_PATH" ]]; then
  echo "Error: Choose either --binary or --source"
  exit 1
fi

if [[ -z "$BIN_PATH" && -z "$SOURCE_PATH" ]]; then
  echo "Error: Provide --binary or --source"
  exit 1
fi

if [[ "$(id -u)" -ne 0 ]]; then
  echo "Error: Run this installer as root"
  exit 1
fi

if [[ -n "$SOURCE_PATH" ]]; then
  if [[ ! -d "$SOURCE_PATH/datamanagement" ]]; then
    echo "Error: Missing source directory: $SOURCE_PATH/datamanagement"
    exit 1
  fi
  echo "[1/6] Building datamanagementd..."
  (cd "$SOURCE_PATH/datamanagement" && go build -o datamanagementd ./cmd/datamanagementd)
  BIN_PATH="$SOURCE_PATH/datamanagement/datamanagementd"
fi

if [[ ! -f "$BIN_PATH" ]]; then
  echo "Error: Binary not found: $BIN_PATH"
  exit 1
fi

if ! id ikun2api >/dev/null 2>&1; then
  echo "[2/6] Creating system user ikun2api..."
  useradd --system --no-create-home --shell /usr/sbin/nologin ikun2api
else
  echo "[2/6] System user ikun2api already exists"
fi

echo "[3/6] Installing datamanagementd..."
mkdir -p "$INSTALL_DIR"
install -m 0755 "$BIN_PATH" "$INSTALL_DIR/datamanagementd"

echo "[4/6] Preparing the data directory..."
mkdir -p "$DATA_DIR"
chown -R ikun2api:ikun2api /var/lib/ikun2api
chmod 0750 "$DATA_DIR"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICE_TEMPLATE="$SCRIPT_DIR/$SERVICE_FILE_NAME"
if [[ ! -f "$SERVICE_TEMPLATE" ]]; then
  echo "Error: Service template not found: $SERVICE_TEMPLATE"
  exit 1
fi

echo "[5/6] Installing the systemd service..."
cp "$SERVICE_TEMPLATE" "/etc/systemd/system/$SERVICE_FILE_NAME"
systemctl daemon-reload
systemctl enable --now ikun2api-datamanagementd

echo "[6/6] Installation complete"
systemctl --no-pager --full status ikun2api-datamanagementd || true

cat <<'EOF'

Next steps:
1. View logs: sudo journalctl -u ikun2api-datamanagementd -f
2. Mount the socket into the IKUN2api container:
   /tmp/ikun2api-datamanagement.sock:/tmp/ikun2api-datamanagement.sock
3. Open Data Management and confirm agent=enabled

EOF
