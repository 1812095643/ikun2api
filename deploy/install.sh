#!/bin/bash

set -e

if [ -z "${BASH_VERSION:-}" ]; then
    echo "Error: This installer must be run with Bash 4.0 or later." >&2
    echo "Please install Bash 4+ and run it with that interpreter." >&2
    exit 1
fi

BASH_MAJOR_VERSION="${BASH_VERSION%%.*}"
if [ "$BASH_MAJOR_VERSION" -lt 4 ]; then
    echo "Error: Bash 4.0 or later is required. Current version: $BASH_VERSION" >&2
    echo "Please install Bash 4+ and retry with that interpreter." >&2
    exit 1
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

GITHUB_REPO="1812095643/ikun2api"
INSTALL_DIR="/opt/ikun2api"
SERVICE_NAME="ikun2api"
SERVICE_USER="ikun2api"
CONFIG_DIR="/etc/ikun2api"

SERVER_HOST="0.0.0.0"
SERVER_PORT="8080"

LANG_CHOICE="en"


declare -A MSG_EN=(
    ["info"]="INFO"
    ["success"]="SUCCESS"
    ["warning"]="WARNING"
    ["error"]="ERROR"

    ["select_lang"]="Select language"
    ["lang_zh"]="English"
    ["lang_en"]="English"
    ["enter_choice"]="Enter your choice (default: 1)"

    ["install_title"]="IKUN2api Installation Script"
    ["run_as_root"]="Please run as root (use sudo)"
    ["detected_platform"]="Detected platform"
    ["unsupported_arch"]="Unsupported architecture"
    ["unsupported_os"]="Unsupported OS"
    ["missing_deps"]="Missing dependencies"
    ["install_deps_first"]="Please install them first"
    ["fetching_version"]="Fetching latest version..."
    ["latest_version"]="Latest version"
    ["failed_get_version"]="Failed to get latest version"
    ["downloading"]="Downloading"
    ["download_failed"]="Download failed"
    ["verifying_checksum"]="Verifying checksum..."
    ["checksum_verified"]="Checksum verified"
    ["checksum_failed"]="Checksum verification failed"
    ["checksum_not_found"]="Could not verify checksum (checksums.txt not found)"
    ["extracting"]="Extracting..."
    ["binary_installed"]="Binary installed to"
    ["user_exists"]="User already exists"
    ["creating_user"]="Creating system user"
    ["user_created"]="User created"
    ["setting_up_dirs"]="Setting up directories..."
    ["dirs_configured"]="Directories configured"
    ["installing_service"]="Installing systemd service..."
    ["service_installed"]="Systemd service installed"
    ["ready_for_setup"]="Ready for Setup Wizard"

    ["install_complete"]="IKUN2api installation completed!"
    ["install_dir"]="Installation directory"
    ["next_steps"]="NEXT STEPS"
    ["step1_check_services"]="Make sure PostgreSQL and Redis are running:"
    ["step2_start_service"]="Start IKUN2api service:"
    ["step3_enable_autostart"]="Enable auto-start on boot:"
    ["step4_open_wizard"]="Open the Setup Wizard in your browser:"
    ["wizard_guide"]="The Setup Wizard will guide you through:"
    ["wizard_db"]="Database configuration"
    ["wizard_redis"]="Redis configuration"
    ["wizard_admin"]="Admin account creation"
    ["useful_commands"]="USEFUL COMMANDS"
    ["cmd_status"]="Check status"
    ["cmd_logs"]="View logs"
    ["cmd_restart"]="Restart"
    ["cmd_stop"]="Stop"

    ["upgrading"]="Upgrading IKUN2api..."
    ["current_version"]="Current version"
    ["stopping_service"]="Stopping service..."
    ["backup_created"]="Backup created"
    ["starting_service"]="Starting service..."
    ["upgrade_complete"]="Upgrade completed!"

    ["installing_version"]="Installing specified version"
    ["version_not_found"]="Specified version not found"
    ["same_version"]="Already at this version, no action needed"
    ["rollback_complete"]="Version rollback completed!"
    ["install_version_complete"]="Specified version installed!"
    ["validating_version"]="Validating version..."
    ["available_versions"]="Available versions"
    ["fetching_versions"]="Fetching available versions..."
    ["not_installed"]="IKUN2api is not installed. Please run a fresh install first"
    ["fresh_install_hint"]="Usage"

    ["uninstall_confirm"]="This will remove IKUN2api from your system."
    ["are_you_sure"]="Are you sure? (y/N)"
    ["uninstall_cancelled"]="Uninstall cancelled"
    ["removing_files"]="Removing files..."
    ["removing_install_dir"]="Removing installation directory..."
    ["removing_user"]="Removing user..."
    ["config_not_removed"]="Config directory was NOT removed."
    ["remove_manually"]="Remove it manually if you no longer need it."
    ["removing_install_lock"]="Removing install lock file..."
    ["install_lock_removed"]="Install lock removed. Setup wizard will appear on next install."
    ["purge_prompt"]="Also remove config directory? This will delete all config and data [y/N]: "
    ["removing_config_dir"]="Removing config directory..."
    ["uninstall_complete"]="IKUN2api has been uninstalled"

    ["usage"]="Usage"
    ["cmd_none"]="(none)"
    ["cmd_install"]="Install IKUN2api"
    ["cmd_upgrade"]="Upgrade to the latest version"
    ["cmd_uninstall"]="Remove IKUN2api"
    ["cmd_install_version"]="Install/rollback to a specific version"
    ["cmd_list_versions"]="List available versions"
    ["opt_version"]="Specify version to install (e.g., v1.0.0)"

    ["server_config_title"]="Server Configuration"
    ["server_config_desc"]="Configure IKUN2api server listen address"
    ["server_host_prompt"]="Server listen address"
    ["server_host_hint"]="0.0.0.0 listens on all interfaces, 127.0.0.1 for local only"
    ["server_port_prompt"]="Server port"
    ["server_port_hint"]="Recommended range: 1024-65535"
    ["server_config_summary"]="Server configuration"
    ["invalid_port"]="Invalid port number, please enter a number between 1-65535"

    ["starting_service"]="Starting service..."
    ["service_started"]="Service started"
    ["service_start_failed"]="Service failed to start, please check logs"
    ["enabling_autostart"]="Enabling auto-start on boot..."
    ["autostart_enabled"]="Auto-start enabled"
    ["getting_public_ip"]="Getting public IP..."
    ["public_ip_failed"]="Failed to get public IP, using local IP"
)

msg() {
    local key="$1"
    echo "${MSG_EN[$key]}"
}

print_info() {
    echo -e "${BLUE}[$(msg 'info')]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[$(msg 'success')]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[$(msg 'warning')]${NC} $1"
}

print_error() {
    echo -e "${RED}[$(msg 'error')]${NC} $1"
}

is_interactive() {
    [ -e /dev/tty ] && [ -r /dev/tty ] && [ -w /dev/tty ]
}

select_language() {
    LANG_CHOICE="en"
}

validate_port() {
    local port="$1"
    if [[ "$port" =~ ^[0-9]+$ ]] && [ "$port" -ge 1 ] && [ "$port" -le 65535 ]; then
        return 0
    fi
    return 1
}

configure_server() {
    if ! is_interactive; then
        print_info "$(msg 'server_config_summary'): ${SERVER_HOST}:${SERVER_PORT} (default)"
        return
    fi

    echo ""
    echo -e "${CYAN}=============================================="
    echo "  $(msg 'server_config_title')"
    echo "==============================================${NC}"
    echo ""
    echo -e "${BLUE}$(msg 'server_config_desc')${NC}"
    echo ""

    echo -e "${YELLOW}$(msg 'server_host_hint')${NC}"
    read -p "$(msg 'server_host_prompt') [${SERVER_HOST}]: " input_host < /dev/tty
    if [ -n "$input_host" ]; then
        SERVER_HOST="$input_host"
    fi

    echo ""

    echo -e "${YELLOW}$(msg 'server_port_hint')${NC}"
    while true; do
        read -p "$(msg 'server_port_prompt') [${SERVER_PORT}]: " input_port < /dev/tty
        if [ -z "$input_port" ]; then
            break
        elif validate_port "$input_port"; then
            SERVER_PORT="$input_port"
            break
        else
            print_error "$(msg 'invalid_port')"
        fi
    done

    echo ""
    print_info "$(msg 'server_config_summary'): ${SERVER_HOST}:${SERVER_PORT}"
    echo ""
}

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        print_error "$(msg 'run_as_root')"
        exit 1
    fi
}

detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$ARCH" in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            print_error "$(msg 'unsupported_arch'): $ARCH"
            exit 1
            ;;
    esac

    case "$OS" in
        linux)
            OS="linux"
            ;;
        darwin)
            OS="darwin"
            ;;
        *)
            print_error "$(msg 'unsupported_os'): $OS"
            exit 1
            ;;
    esac

    print_info "$(msg 'detected_platform'): ${OS}_${ARCH}"
}

check_dependencies() {
    local missing=()

    if ! command -v curl &> /dev/null; then
        missing+=("curl")
    fi

    if ! command -v tar &> /dev/null; then
        missing+=("tar")
    fi

    for dependency in openssl sha256sum base64; do
        command -v "$dependency" >/dev/null || missing+=("$dependency")
    done
    if [ ${#missing[@]} -gt 0 ]; then
        print_error "$(msg 'missing_deps'): ${missing[*]}"
        print_info "$(msg 'install_deps_first')"
        exit 1
    fi
}

github_api_curl() {
    local arg
    local expect_value=false
    local url

    if [ "$#" -lt 1 ]; then
        echo "github_api_curl requires exactly one GitHub API URL" >&2
        return 2
    fi
    url="${!#}"

    for arg in "${@:1:$#-1}"; do
        if [ "$expect_value" = true ]; then
            expect_value=false
            continue
        fi
        case "$arg" in
            -s|--silent)
                ;;
            --connect-timeout|--max-time|-o|--output|-w|--write-out)
                expect_value=true
                ;;
            *)
                echo "Unsafe github_api_curl argument: $arg" >&2
                return 2
                ;;
        esac
    done

    if [ "$expect_value" = true ] || [[ "$url" != https://api.github.com/* ]]; then
        echo "github_api_curl requires exactly one GitHub API URL" >&2
        return 2
    fi

    if [ -n "${UPDATE_GITHUB_TOKEN:-}" ]; then
        if [[ "$UPDATE_GITHUB_TOKEN" == *$'\n'* || "$UPDATE_GITHUB_TOKEN" == *$'\r'* || "$UPDATE_GITHUB_TOKEN" == *'"'* || "$UPDATE_GITHUB_TOKEN" == *'\'* ]]; then
            echo "UPDATE_GITHUB_TOKEN contains unsupported characters" >&2
            return 2
        fi
        printf 'header = "Authorization: Bearer %s"\n' "$UPDATE_GITHUB_TOKEN" | UPDATE_GITHUB_TOKEN= GITHUB_TOKEN= GH_TOKEN= curl -q --globoff --config - "$@"
    else
        UPDATE_GITHUB_TOKEN= GITHUB_TOKEN= GH_TOKEN= curl -q --globoff "$@"
    fi
}

get_latest_version() {
    print_info "$(msg 'fetching_version')"
    LATEST_VERSION=$(github_api_curl -s --connect-timeout 10 --max-time 30 "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" 2>/dev/null | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')

    if [ -z "$LATEST_VERSION" ]; then
        print_error "$(msg 'failed_get_version')"
        print_info "Please check your network connection or try again later."
        exit 1
    fi

    print_info "$(msg 'latest_version'): $LATEST_VERSION"
}

list_versions() {
    print_info "$(msg 'fetching_versions')"

    local versions
    versions=$(github_api_curl -s --connect-timeout 10 --max-time 30 "https://api.github.com/repos/${GITHUB_REPO}/releases" 2>/dev/null | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' | head -20)

    if [ -z "$versions" ]; then
        print_error "$(msg 'failed_get_version')"
        print_info "Please check your network connection or try again later."
        exit 1
    fi

    echo ""
    echo "$(msg 'available_versions'):"
    echo "----------------------------------------"
    echo "$versions" | while read -r version; do
        echo "  $version"
    done
    echo "----------------------------------------"
    echo ""
}

validate_version() {
    local version="$1"

    if [ -z "$version" ]; then
        print_error "$(msg 'opt_version')" >&2
        exit 1
    fi

    if [[ ! "$version" =~ ^v ]]; then
        version="v$version"
    fi

    print_info "$(msg 'validating_version') $version" >&2

    local http_code
    http_code=$(github_api_curl -s -o /dev/null -w "%{http_code}" --connect-timeout 10 --max-time 30 "https://api.github.com/repos/${GITHUB_REPO}/releases/tags/${version}" 2>/dev/null)

    if [ -z "$http_code" ] || ! [[ "$http_code" =~ ^[0-9]+$ ]]; then
        print_error "Network error: Failed to connect to GitHub API" >&2
        exit 1
    fi

    if [ "$http_code" != "200" ]; then
        print_error "$(msg 'version_not_found'): $version" >&2
        echo "" >&2
        list_versions >&2
        exit 1
    fi

    echo "$version"
}

get_current_version() {
    if [ -f "$INSTALL_DIR/ikun2api" ]; then
        "$INSTALL_DIR/ikun2api" --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown"
    else
        echo "not_installed"
    fi
}

download_and_extract() {
    local version_num=${LATEST_VERSION#v}
    local archive_name="ikun2api_${version_num}_${OS}_${ARCH}.tar.gz"
    local download_url="https://github.com/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/${archive_name}"
    local checksum_url="https://github.com/${GITHUB_REPO}/releases/download/${LATEST_VERSION}/checksums.txt"

    print_info "$(msg 'downloading') ${archive_name}..."

    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    if ! curl -q -fsSL --proto '=https' --connect-timeout 15 --max-time 900 "$download_url" -o "$TEMP_DIR/$archive_name"; then
        print_error "$(msg 'download_failed')"
        exit 1
    fi

    print_info "$(msg 'verifying_checksum')"
    if curl -q -fsSL --proto '=https' --connect-timeout 15 --max-time 90 "$checksum_url" -o "$TEMP_DIR/checksums.txt" 2>/dev/null; then
        curl -q -fsSL --proto '=https' --connect-timeout 15 --max-time 90 "$checksum_url.sig" | base64 -d > "$TEMP_DIR/checksums.sig"
        printf '%s' 'MCowBQYDK2VwAyEAFjAf8K7CSX0LstSKwFGX2sL03GcQ14tUXsbNAAsrGnc=' | base64 -d > "$TEMP_DIR/release-public.der"
        openssl pkeyutl -verify -pubin -keyform DER -inkey "$TEMP_DIR/release-public.der" -rawin -in "$TEMP_DIR/checksums.txt" -sigfile "$TEMP_DIR/checksums.sig" >/dev/null
        local expected_checksum=$(awk -v name="$archive_name" '$2 == name {print $1}' "$TEMP_DIR/checksums.txt")
        local actual_checksum=$(sha256sum "$TEMP_DIR/$archive_name" | awk '{print $1}')

        if [ "$expected_checksum" != "$actual_checksum" ]; then
            print_error "$(msg 'checksum_failed')"
            print_error "Expected: $expected_checksum"
            print_error "Actual: $actual_checksum"
            exit 1
        fi
        print_success "$(msg 'checksum_verified')"
    else
        print_error "$(msg 'checksum_not_found')"
        exit 1
    fi

    print_info "$(msg 'extracting')"
    tar -xzf "$TEMP_DIR/$archive_name" -C "$TEMP_DIR"

    mkdir -p "$INSTALL_DIR"

    install -m 0755 "$TEMP_DIR/ikun2api" "$INSTALL_DIR/.ikun2api.new"
    mv -f "$INSTALL_DIR/.ikun2api.new" "$INSTALL_DIR/ikun2api"
    chmod +x "$INSTALL_DIR/ikun2api"

    if [ -d "$TEMP_DIR/deploy" ]; then
        cp -r "$TEMP_DIR/deploy/"* "$INSTALL_DIR/" 2>/dev/null || true
    fi

    print_success "$(msg 'binary_installed') $INSTALL_DIR/ikun2api"
}

create_user() {
    if id "$SERVICE_USER" &>/dev/null; then
        print_info "$(msg 'user_exists'): $SERVICE_USER"
        local current_shell
        current_shell=$(getent passwd "$SERVICE_USER" 2>/dev/null | cut -d: -f7)
        if [ "$current_shell" = "/bin/false" ] || [ "$current_shell" = "/sbin/nologin" ]; then
            print_info "Fixing user shell for sudo compatibility..."
            if usermod -s /bin/sh "$SERVICE_USER" 2>/dev/null; then
                print_success "User shell updated to /bin/sh"
            else
                print_warning "Failed to update user shell. Service restart may not work automatically."
                print_warning "Manual fix: sudo usermod -s /bin/sh $SERVICE_USER"
            fi
        fi
    else
        print_info "$(msg 'creating_user') $SERVICE_USER..."
        useradd -r -s /bin/sh -d "$INSTALL_DIR" "$SERVICE_USER"
        print_success "$(msg 'user_created')"
    fi
}

setup_directories() {
    print_info "$(msg 'setting_up_dirs')"

    mkdir -p "$INSTALL_DIR"
    mkdir -p "$INSTALL_DIR/data"
    mkdir -p "$CONFIG_DIR"

    chown -R "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR"
    chown -R "$SERVICE_USER:$SERVICE_USER" "$CONFIG_DIR"

    print_success "$(msg 'dirs_configured')"
}

install_service() {
    print_info "$(msg 'installing_service')"

    cat > /etc/systemd/system/ikun2api.service << EOF
[Unit]
Description=IKUN2api - AI API Gateway Platform
Documentation=https://github.com/1812095643/ikun2api
After=network.target postgresql.service redis.service
Wants=postgresql.service redis.service

[Service]
Type=simple
User=ikun2api
Group=ikun2api
WorkingDirectory=/opt/ikun2api
ExecStart=/opt/ikun2api/ikun2api --supervise
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=ikun2api

NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/opt/ikun2api

Environment=GIN_MODE=release
Environment=SERVER_HOST=${SERVER_HOST}
Environment=SERVER_PORT=${SERVER_PORT}

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload

    print_success "$(msg 'service_installed')"
}

prepare_for_setup() {
    print_success "$(msg 'ready_for_setup')"
}

get_public_ip() {
    print_info "$(msg 'getting_public_ip')"

    local response
    response=$(curl -s --connect-timeout 5 --max-time 10 "https://ipinfo.io/json" 2>/dev/null)

    if [ -n "$response" ]; then
        PUBLIC_IP=$(echo "$response" | grep -o '"ip": *"[^"]*"' | sed 's/"ip": *"\([^"]*\)"/\1/')
        if [ -n "$PUBLIC_IP" ]; then
            print_success "Public IP: $PUBLIC_IP"
            return 0
        fi
    fi

    print_warning "$(msg 'public_ip_failed')"
    PUBLIC_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "YOUR_SERVER_IP")
    return 1
}

start_service() {
    print_info "$(msg 'starting_service')"

    if systemctl start ikun2api; then
        print_success "$(msg 'service_started')"
        return 0
    else
        print_error "$(msg 'service_start_failed')"
        print_info "sudo journalctl -u ikun2api -n 50"
        return 1
    fi
}

enable_autostart() {
    print_info "$(msg 'enabling_autostart')"

    if systemctl enable ikun2api 2>/dev/null; then
        print_success "$(msg 'autostart_enabled')"
        return 0
    else
        print_warning "Failed to enable auto-start"
        return 1
    fi
}

print_completion() {
    local display_host="${PUBLIC_IP:-YOUR_SERVER_IP}"
    if [ "$SERVER_HOST" = "127.0.0.1" ]; then
        display_host="127.0.0.1"
    fi

    echo ""
    echo "=============================================="
    print_success "$(msg 'install_complete')"
    echo "=============================================="
    echo ""
    echo "$(msg 'install_dir'): $INSTALL_DIR"
    echo "$(msg 'server_config_summary'): ${SERVER_HOST}:${SERVER_PORT}"
    echo ""
    echo "=============================================="
    echo "  $(msg 'step4_open_wizard')"
    echo "=============================================="
    echo ""
    print_info "     http://${display_host}:${SERVER_PORT}"
    echo ""
    echo "     $(msg 'wizard_guide')"
    echo "     - $(msg 'wizard_db')"
    echo "     - $(msg 'wizard_redis')"
    echo "     - $(msg 'wizard_admin')"
    echo ""
    echo "=============================================="
    echo "  $(msg 'useful_commands')"
    echo "=============================================="
    echo ""
    echo "  $(msg 'cmd_status'):   sudo systemctl status ikun2api"
    echo "  $(msg 'cmd_logs'):     sudo journalctl -u ikun2api -f"
    echo "  $(msg 'cmd_restart'):  sudo systemctl restart ikun2api"
    echo "  $(msg 'cmd_stop'):     sudo systemctl stop ikun2api"
    echo ""
    echo "=============================================="
}

upgrade() {
    if [ ! -f "$INSTALL_DIR/ikun2api" ]; then
        print_error "$(msg 'not_installed')"
        print_info "$(msg 'fresh_install_hint'): $0 install"
        exit 1
    fi

    print_info "$(msg 'upgrading')"

    CURRENT_VERSION=$("$INSTALL_DIR/ikun2api" --version 2>/dev/null | grep -oE 'v?[0-9]+\.[0-9]+\.[0-9]+' || echo "unknown")
    print_info "$(msg 'current_version'): $CURRENT_VERSION"

    if systemctl is-active --quiet ikun2api; then
        print_info "$(msg 'stopping_service')"
        systemctl stop ikun2api
    fi

    cp "$INSTALL_DIR/ikun2api" "$INSTALL_DIR/ikun2api.backup"
    print_info "$(msg 'backup_created'): $INSTALL_DIR/ikun2api.backup"

    get_latest_version
    download_and_extract

    chown "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR/ikun2api"

    print_info "$(msg 'starting_service')"
    systemctl start ikun2api

    print_success "$(msg 'upgrade_complete')"
}

install_version() {
    local target_version="$1"

    if [ ! -f "$INSTALL_DIR/ikun2api" ]; then
        print_error "$(msg 'not_installed')"
        print_info "$(msg 'fresh_install_hint'): $0 install -v $target_version"
        exit 1
    fi

    target_version=$(validate_version "$target_version")

    print_info "$(msg 'installing_version'): $target_version"

    local current_version
    current_version=$(get_current_version)
    print_info "$(msg 'current_version'): $current_version"

    if [ "$current_version" = "$target_version" ] || [ "$current_version" = "${target_version#v}" ]; then
        print_warning "$(msg 'same_version')"
        exit 0
    fi

    if systemctl is-active --quiet ikun2api; then
        print_info "$(msg 'stopping_service')"
        systemctl stop ikun2api
    fi

    if [ -f "$INSTALL_DIR/ikun2api" ]; then
        local backup_name
        if [ "$current_version" != "unknown" ] && [ "$current_version" != "not_installed" ]; then
            backup_name="ikun2api.backup.${current_version}"
        else
            backup_name="ikun2api.backup.$(date +%Y%m%d%H%M%S)"
        fi
        cp "$INSTALL_DIR/ikun2api" "$INSTALL_DIR/$backup_name"
        print_info "$(msg 'backup_created'): $INSTALL_DIR/$backup_name"
    fi

    LATEST_VERSION="$target_version"

    download_and_extract

    chown "$SERVICE_USER:$SERVICE_USER" "$INSTALL_DIR/ikun2api"

    print_info "$(msg 'starting_service')"
    if systemctl start ikun2api; then
        print_success "$(msg 'service_started')"
    else
        print_error "$(msg 'service_start_failed')"
        print_info "sudo journalctl -u ikun2api -n 50"
    fi

    local new_version
    new_version=$(get_current_version)
    echo ""
    echo "=============================================="
    print_success "$(msg 'install_version_complete')"
    echo "=============================================="
    echo ""
    echo "  $(msg 'current_version'): $new_version"
    echo ""
}

uninstall() {
    print_warning "$(msg 'uninstall_confirm')"

    if ! is_interactive; then
        if [ "${FORCE_YES:-}" != "true" ]; then
            print_error "Non-interactive mode detected. Use 'curl ... | bash -s -- uninstall -y' to confirm."
            exit 1
        fi
    else
        read -p "$(msg 'are_you_sure') " -n 1 -r < /dev/tty
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            print_info "$(msg 'uninstall_cancelled')"
            exit 0
        fi
    fi

    print_info "$(msg 'stopping_service')"
    systemctl stop ikun2api 2>/dev/null || true
    systemctl disable ikun2api 2>/dev/null || true

    print_info "$(msg 'removing_files')"
    rm -f /etc/systemd/system/ikun2api.service
    systemctl daemon-reload

    print_info "$(msg 'removing_install_dir')"
    rm -rf "$INSTALL_DIR"

    print_info "$(msg 'removing_user')"
    userdel "$SERVICE_USER" 2>/dev/null || true

    print_info "$(msg 'removing_install_lock')"
    rm -f "$CONFIG_DIR/.installed" 2>/dev/null || true
    rm -f "$INSTALL_DIR/.installed" 2>/dev/null || true
    print_success "$(msg 'install_lock_removed')"

    local remove_config=false
    if [ "${PURGE:-}" = "true" ]; then
        remove_config=true
    elif is_interactive; then
        read -p "$(msg 'purge_prompt')" -n 1 -r < /dev/tty
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            remove_config=true
        fi
    fi

    if [ "$remove_config" = true ]; then
        print_info "$(msg 'removing_config_dir')"
        rm -rf "$CONFIG_DIR"
    else
        print_warning "$(msg 'config_not_removed'): $CONFIG_DIR"
        print_warning "$(msg 'remove_manually')"
    fi

    print_success "$(msg 'uninstall_complete')"
}

main() {
    local target_version=""
    local positional_args=()

    while [[ $# -gt 0 ]]; do
        case "$1" in
            -y|--yes)
                FORCE_YES="true"
                shift
                ;;
            --purge)
                PURGE="true"
                shift
                ;;
            -v|--version)
                if [ -n "${2:-}" ] && [[ ! "$2" =~ ^- ]]; then
                    target_version="$2"
                    shift 2
                else
                    echo "Error: --version requires a version argument"
                    exit 1
                fi
                ;;
            --version=*)
                target_version="${1#*=}"
                if [ -z "$target_version" ]; then
                    echo "Error: --version requires a version argument"
                    exit 1
                fi
                shift
                ;;
            *)
                positional_args+=("$1")
                shift
                ;;
        esac
    done

    set -- "${positional_args[@]}"

    select_language

    echo ""
    echo "=============================================="
    echo "       $(msg 'install_title')"
    echo "=============================================="
    echo ""

    case "${1:-}" in
        upgrade|update)
            check_root
            detect_platform
            check_dependencies
            if [ -n "$target_version" ]; then
                install_version "$target_version"
            else
                upgrade
            fi
            exit 0
            ;;
        install)
            check_root
            detect_platform
            check_dependencies
            if [ -n "$target_version" ]; then
                if [ -f "$INSTALL_DIR/ikun2api" ]; then
                    install_version "$target_version"
                else
                    configure_server
                    LATEST_VERSION=$(validate_version "$target_version")
                    download_and_extract
                    create_user
                    setup_directories
                    install_service
                    prepare_for_setup
                    get_public_ip
                    start_service
                    enable_autostart
                    print_completion
                fi
            else
                configure_server
                get_latest_version
                download_and_extract
                create_user
                setup_directories
                install_service
                prepare_for_setup
                get_public_ip
                start_service
                enable_autostart
                print_completion
            fi
            exit 0
            ;;
        rollback)
            if [ -z "$target_version" ] && [ -n "${2:-}" ]; then
                target_version="$2"
            fi
            if [ -z "$target_version" ]; then
                print_error "$(msg 'opt_version')"
                echo ""
                echo "Usage: $0 rollback -v <version>"
                echo "       $0 rollback <version>"
                echo ""
                list_versions
                exit 1
            fi
            check_root
            detect_platform
            check_dependencies
            install_version "$target_version"
            exit 0
            ;;
        list-versions|versions)
            list_versions
            exit 0
            ;;
        uninstall|remove)
            check_root
            uninstall
            exit 0
            ;;
        --help|-h)
            echo "$(msg 'usage'): $0 [command] [options]"
            echo ""
            echo "Commands:"
            echo "  $(msg 'cmd_none')            $(msg 'cmd_install')"
            echo "  install              $(msg 'cmd_install')"
            echo "  upgrade              $(msg 'cmd_upgrade')"
            echo "  rollback <version>   $(msg 'cmd_install_version')"
            echo "  list-versions        $(msg 'cmd_list_versions')"
            echo "  uninstall            $(msg 'cmd_uninstall')"
            echo ""
            echo "Options:"
            echo "  -v, --version <ver>  $(msg 'opt_version')"
            echo "  -y, --yes            Skip confirmation prompts (for uninstall)"
            echo ""
            echo "Examples:"
            echo "  $0                        # Install latest version"
            echo "  $0 install -v v0.1.0      # Install specific version"
            echo "  $0 upgrade                # Upgrade to latest"
            echo "  $0 upgrade -v v0.2.0      # Upgrade to specific version"
            echo "  $0 rollback v0.1.0        # Rollback to v0.1.0"
            echo "  $0 list-versions          # List available versions"
            echo ""
            exit 0
            ;;
    esac

    check_root
    detect_platform
    check_dependencies

    if [ -n "$target_version" ]; then
        if [ -f "$INSTALL_DIR/ikun2api" ]; then
            install_version "$target_version"
        else
            configure_server
            LATEST_VERSION=$(validate_version "$target_version")
            download_and_extract
            create_user
            setup_directories
            install_service
            prepare_for_setup
            get_public_ip
            start_service
            enable_autostart
            print_completion
        fi
    else
        configure_server
        get_latest_version
        download_and_extract
        create_user
        setup_directories
        install_service
        prepare_for_setup
        get_public_ip
        start_service
        enable_autostart
        print_completion
    fi
}

main "$@"
