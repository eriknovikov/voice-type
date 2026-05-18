#!/bin/sh
set -e

REPO="eriknovikov/voice-type"
BINARY_NAME="voice-type"

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    printf "${GREEN}[INFO]${NC} %s\n" "$1" >&2
}

log_warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$1" >&2
}

log_error() {
    printf "${RED}[ERROR]${NC} %s\n" "$1" >&2
}

# Detect architecture
detect_arch() {
    ARCH="$(uname -m)"
    case "$ARCH" in
        x86_64|amd64)
            echo "x64"
            ;;
        arm64|aarch64)
            echo "arm64"
            ;;
        *)
            log_error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac
}

# Parse mode flags
MODE="prod"
VERSION=""
while [ $# -gt 0 ]; do
    case "$1" in
        --local)
            MODE="local"
            shift
            ;;
        --version)
            VERSION="$2"
            MODE="version"
            shift 2
            ;;
        -*)
            shift
            ;;
        *)
            shift
            ;;
    esac
done

# Get latest version from GitHub
get_latest_version() {
    curl -sS "https://api.github.com/repos/${REPO}/releases/latest" | \
        grep '"tag_name":' | \
        sed -E 's/.*"([^"]+)".*/\1/'
}

# Install binary
install_binary() {
    BINARY_PATH="$1"
    TARGET="/usr/local/bin/${BINARY_NAME}"

    if [ -w /usr/local/bin ]; then
        install -m 755 "$BINARY_PATH" "$TARGET"
    else
        sudo install -m 755 "$BINARY_PATH" "$TARGET"
    fi
}

# Install sounds
install_sounds() {
    SOUNDS_DIR="/usr/local/share/voice-type/sounds"

    if [ -w /usr/local/share ]; then
        mkdir -p "$SOUNDS_DIR"
        curl -sSfL "https://raw.githubusercontent.com/${REPO}/main/assets/sounds/start.oga" -o "${SOUNDS_DIR}/start.oga"
        curl -sSfL "https://raw.githubusercontent.com/${REPO}/main/assets/sounds/stop.oga" -o "${SOUNDS_DIR}/stop.oga"
    else
        sudo mkdir -p "$SOUNDS_DIR"
        sudo curl -sSfL "https://raw.githubusercontent.com/${REPO}/main/assets/sounds/start.oga" -o "${SOUNDS_DIR}/start.oga"
        sudo curl -sSfL "https://raw.githubusercontent.com/${REPO}/main/assets/sounds/stop.oga" -o "${SOUNDS_DIR}/stop.oga"
    fi
}

main() {
    log_info "Voice Type Installer"
    log_info "=================="

    ARCH=$(detect_arch)
    log_info "Detected architecture: ${ARCH}"

    # Local mode: build locally and install
    if [ "$MODE" = "local" ]; then
        log_info "LOCAL MODE - Building and installing locally"

        # Build binary
        log_info "Building binary..."
        if ! command -v bun >/dev/null 2>&1; then
            log_error "bun not found. Install bun first."
            exit 1
        fi

        mkdir -p build
        bun build src/index.ts --compile --outfile build/voice-type

        # Create tarball in releases directory (binary only)
        log_info "Creating tarball..."
        mkdir -p "releases"
        mkdir -p "releases/voice-type-${ARCH}"
        cp build/voice-type "releases/voice-type-${ARCH}/"
        tar -czf "releases/voice-type-linux-${ARCH}.tar.gz" -C releases "voice-type-${ARCH}/"

        # Generate checksum
        log_info "Generating checksum..."
        sha256sum "releases/voice-type-linux-${ARCH}.tar.gz"

        # Install binary
        log_info "Installing binary..."
        install_binary "build/voice-type"

        # Install sounds
        log_info "Installing sounds..."
        install_sounds

        log_info "Successfully installed ${BINARY_NAME} to /usr/local/bin/${BINARY_NAME}"

        log_info ""
        log_info "Run 'voice-type --help' to get started"

        return 0
    fi

    # Version mode: fetch specific version
    if [ "$MODE" = "version" ]; then
        log_info "Installing version: ${VERSION}"
        TAG="$VERSION"
    else
        # Prod mode: official latest release
        log_info "Fetching latest stable version..."
        TAG=$(get_latest_version)
        if [ -z "$TAG" ]; then
            log_error "Could not determine latest version"
            exit 1
        fi
        log_info "Installing version: ${TAG}"
    fi

    BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
    FILENAME="voice-type-linux-${ARCH}.tar.gz"
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT

    log_info "Downloading ${FILENAME}..."

    curl -sSfL "${BASE_URL}/${FILENAME}" -o "${TMP_DIR}/${FILENAME}"
    curl -sSfL "${BASE_URL}/checksums.txt" -o "${TMP_DIR}/checksums.txt"

    log_info "Verifying checksum..."
    cd "$TMP_DIR"

    EXPECTED_CHECKSUM=$(grep "${FILENAME}" checksums.txt | awk '{print $1}')
    if [ -z "$EXPECTED_CHECKSUM" ]; then
        log_error "Could not find checksum for ${FILENAME}"
        exit 1
    fi

    if command -v sha256sum > /dev/null 2>&1; then
        ACTUAL_CHECKSUM=$(sha256sum "${FILENAME}" | awk '{print $1}')
    elif command -v shasum > /dev/null 2>&1; then
        ACTUAL_CHECKSUM=$(shasum -a 256 "${FILENAME}" | awk '{print $1}')
    else
        log_error "Neither sha256sum nor shasum found"
        exit 1
    fi

    if [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
        log_error "Checksum mismatch!"
        log_error "Expected: $EXPECTED_CHECKSUM"
        log_error "Actual:   $ACTUAL_CHECKSUM"
        exit 1
    fi

    log_info "Checksum verified"

    log_info "Extracting archive..."
    tar -xzf "${FILENAME}" -C "$TMP_DIR"

    install_binary "${TMP_DIR}/voice-type-${ARCH}/voice-type"
    install_sounds

    log_info "Successfully installed ${BINARY_NAME} to /usr/local/bin/${BINARY_NAME}"

    # Check if /usr/local/bin is in PATH
    case ":$PATH:" in
        *":/usr/local/bin:"*)
            ;;
        *)
            log_warn "/usr/local/bin is not in your PATH. Add it to use voice-type."
            ;;
    esac

    log_info ""
    log_info "Run 'voice-type --help' to get started"
}

main "$@"