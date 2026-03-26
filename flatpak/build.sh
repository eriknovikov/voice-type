#!/bin/bash
set -e

APP_ID="io.github.eriknovikov.voice_type"
MANIFEST="flatpak/${APP_ID}.json"

echo "Building Voice Type Flatpak package..."

if ! command -v flatpak &> /dev/null; then
    echo "Error: flatpak is not installed."
    exit 1
fi

if ! command -v flatpak-builder &> /dev/null; then
    echo "Error: flatpak-builder is not installed. Please install it using your distro's package manager"
    exit 1
fi

flatpak remote-add --user --if-not-exists flathub https://flathub.org/repo/flathub.flatpakrepo

echo "Installing required SDKs and runtimes..."
# Added --user and -y to avoid prompts. Added the Go SDK extension.
flatpak install --user -y flathub org.freedesktop.Platform//24.08
flatpak install --user -y flathub org.freedesktop.Sdk//24.08
flatpak install --user -y flathub org.freedesktop.Sdk.Extension.golang//24.08
flatpak install --user -y flathub org.electronjs.Electron2.BaseApp//24.08

BUILD_DIR="flatpak/build"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"

echo "Building Flatpak..."
flatpak-builder --force-clean --install --user \
    --repo=flatpak/repo \
    "$BUILD_DIR" \
    "$MANIFEST"

echo "Flatpak built and installed successfully!"
echo "Run with: flatpak run $APP_ID"