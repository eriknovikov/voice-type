#!/bin/bash
# install.sh - voice-type installation script

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${NC}  ${GREEN}Voice Type - System-wide Speech-to-Text for Linux${NC}"
echo ""

# Check for curl
if ! command -v curl >/dev/null 2>&1; then
    echo -e "${RED}ERROR: curl is not installed${NC}"
    echo ""
    echo -e "${YELLOW}Please install curl first:${NC}"
    echo "  Debian/Ubuntu: sudo apt install curl"
    echo "  Fedora: sudo dnf install curl"
    echo "  Arch: sudo pacman -S curl"
    echo "  openSUSE: sudo zypper install curl"
    exit 1
fi

# Check for system dependencies
echo -e "${BLUE}Checking system dependencies...${NC}"
MISSING_DEPS=()

if ! command -v dotool >/dev/null 2>&1; then
    MISSING_DEPS+=("dotool")
fi

if ! command -v notify-send >/dev/null 2>&1; then
    MISSING_DEPS+=("libnotify")
fi

if ! command -v paplay >/dev/null 2>&1; then
    MISSING_DEPS+=("pulseaudio-utils")
fi

if ! command -v google-chrome >/dev/null 2>&1 && ! command -v google-chrome-stable >/dev/null 2>&1; then
    MISSING_DEPS+=("Google Chrome")
fi

# If any dependencies are missing, exit with error
if [ ${#MISSING_DEPS[@]} -ne 0 ]; then
    echo -e "${RED}✗ Missing dependencies${NC}"
    echo ""
    echo -e "${YELLOW}The following system dependencies are required:${NC}"
    for dep in "${MISSING_DEPS[@]}"; do
        echo -e "  ${RED}✗${NC} $dep"
    done
    echo ""
    echo -e "${YELLOW}Please install them manually and run this script again.${NC}"
    echo ""
    echo -e "${BLUE}Installation instructions:${NC}"
    echo ""
    echo -e "${GREEN}Debian/Ubuntu:${NC}"
    echo "  sudo apt install curl libnotify-bin pulseaudio-utils"
    echo "  # dotool must be built from source: https://sr.ht/~geb/dotool/"
    echo ""
    echo -e "${GREEN}Fedora:${NC}"
    echo "  sudo dnf install curl libnotify pulseaudio-utils"
    echo "  sudo dnf copr enable atim/dotool"
    echo "  sudo dnf install dotool"
    echo "  # dotool is available in COPR (community repository)"
    echo ""
    echo -e "${GREEN}Arch Linux:${NC}"
    echo "  sudo pacman -S --needed curl libnotify pulseaudio"
    echo "  yay -S --needed dotool"
    echo "  # dotool is available in the AUR (community repository)"
    echo ""
    echo -e "${GREEN}openSUSE:${NC}"
    echo "  sudo zypper install curl libnotify-tools pulseaudio-utils"
    echo "  # dotool must be built from source: https://sr.ht/~geb/dotool/"
    echo ""
    echo -e "${YELLOW}Note: Google Chrome must be installed separately from https://www.google.com/chrome/${NC}"
    exit 1
fi

echo -e "${GREEN}✓ All system dependencies are installed${NC}"
echo ""

# Detect architecture
ARCH=$(uname -m)
case $ARCH in
    x86_64)
        BINARY_NAME="voice-type-linux-x64"
        ;;
    aarch64|arm64)
        BINARY_NAME="voice-type-linux-arm64"
        ;;
    *)
        echo -e "${RED}ERROR: Unsupported architecture: $ARCH${NC}"
        echo "Voice Type currently supports x86_64 and ARM64 (aarch64) only."
        exit 1
        ;;
esac

# Download latest binary
echo -e "${BLUE}Downloading Voice Type binary ($ARCH)...${NC}"
LATEST_URL="https://github.com/E-nkv/voice-type/releases/latest/download/$BINARY_NAME"
if ! curl -L -o /tmp/voice-type "$LATEST_URL"; then
    echo -e "${RED}ERROR: Failed to download binary${NC}"
    echo "Please check your internet connection and try again."
    exit 1
fi
chmod +x /tmp/voice-type
echo -e "${GREEN}✓ Binary downloaded${NC}"

# Download assets (sounds)
echo -e "${BLUE}Downloading sound assets...${NC}"
ASSETS_DIR="/usr/local/share/voice-type"
if ! curl -L -o /tmp/sounds.tar.gz "https://github.com/E-nkv/voice-type/releases/latest/download/sounds.tar.gz"; then
    echo -e "${YELLOW}WARNING: Failed to download sound assets${NC}"
    echo "Voice Type will work without sound notifications."
else
    sudo mkdir -p "$ASSETS_DIR/sounds"
    sudo tar -xzf /tmp/sounds.tar.gz -C "$ASSETS_DIR/sounds" 2>/dev/null || true
    echo -e "${GREEN}✓ Sound assets downloaded${NC}"
fi

# Install binary
echo -e "${BLUE}Installing binary to /usr/local/bin...${NC}"
if ! sudo mv /tmp/voice-type /usr/local/bin/voice-type; then
    echo -e "${RED}ERROR: Failed to install binary${NC}"
    echo "You may need sudo privileges to install to /usr/local/bin"
    exit 1
fi
echo -e "${GREEN}✓ Binary installed${NC}"

# Cleanup
rm -f /tmp/sounds.tar.gz

# Summary
echo ""
echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║${NC}  ${GREEN}Installation complete!${NC}                                          ${GREEN}║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}Quick Start:${NC}"
echo ""
echo "  1. Start the daemon:"
echo -e "     ${GREEN}voice-type &${NC}"
echo ""
echo "  2. Test it:"
echo -e "     ${GREEN}curl http://127.0.0.1:3232/start${NC}"
echo ""
echo "  3. Set up keyboard shortcuts (F9/F10) in your system settings"
echo ""
echo -e "${BLUE}For more information, see:${NC}"
echo "  - Usage Guide: https://github.com/E-nkv/voice-type/blob/main/docs/USAGE.md"
echo "  - Installation Guide: https://github.com/E-nkv/voice-type/blob/main/docs/INSTALLATION.md"
echo ""
echo -e "${YELLOW}To uninstall:${NC}"
echo "  sudo rm /usr/local/bin/voice-type"
echo "  sudo rm -rf /usr/local/share/voice-type"
echo ""
