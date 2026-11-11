#!/bin/sh
# The single-quoted container scripts below expand $1 inside the container.
# shellcheck disable=SC2016
# Install the built packages in clean distribution containers. This is the gate
# that catches renamed dependencies and packaging drift; it verifies package
# contents and dependencies, then installs and runs the application.
#
# Usage: scripts/verify-packages.sh [package-directory]
#
# Set CONTAINER_RUNTIME=docker|podman to choose a runtime. Set
# PACKAGE_VERIFY_SKIP_RUN=1 to skip the startup check; package contents and
# dependency installation are still checked.
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
PACKAGE_DIR=${1:-"$ROOT/build/packages"}
PACKAGE_DIR=$(CDPATH='' cd -- "$PACKAGE_DIR" && pwd)
SKIP_RUN=${PACKAGE_VERIFY_SKIP_RUN:-0}

RUNTIME=${CONTAINER_RUNTIME:-}
if [ -z "$RUNTIME" ]; then
    for candidate in docker podman; do
        if command -v "$candidate" >/dev/null 2>&1; then
            RUNTIME=$candidate
            break
        fi
    done
fi
[ -n "$RUNTIME" ] || {
    printf '%s\n' 'Install docker or podman, or set CONTAINER_RUNTIME.' >&2
    exit 1
}

case "$(uname -m)" in
    x86_64)
        DEB_ARCH=amd64
        RPM_ARCH=x86_64
        ARCH_ARCH=x86_64
        ;;
    aarch64 | arm64)
        DEB_ARCH=arm64
        RPM_ARCH=aarch64
        # The official Arch Linux distribution does not support aarch64. Arch
        # Linux ARM is a separate distribution, so no Arch package is built.
        ARCH_ARCH=
        ;;
    *)
        printf 'Unsupported architecture: %s\n' "$(uname -m)" >&2
        exit 1
        ;;
esac

TEMP_DIR=$(mktemp -d)
cleanup() { rm -rf "$TEMP_DIR"; }
trap cleanup EXIT

find_one() {
    matches=$(find "$PACKAGE_DIR" -maxdepth 1 -type f -name "$1" | awk 'NF')
    count=$(printf '%s\n' "$matches" | awk 'NF' | wc -l)
    [ "$count" -eq 1 ] || {
        printf 'Expected exactly one %s in %s, found %s.\n' "$1" "$PACKAGE_DIR" "$count" >&2
        exit 1
    }
    printf '%s\n' "$matches"
}

DEB=$(find_one "voice-type_*_${DEB_ARCH}.deb")
RPM=$(find_one "voice-type-*.${RPM_ARCH}.rpm")
ARCHPKG=
if [ -n "$ARCH_ARCH" ]; then
    ARCHPKG=$(find_one "voice-type-*-${ARCH_ARCH}.pkg.tar.zst")
fi
DEB_NAME=$(basename "$DEB")
RPM_NAME=$(basename "$RPM")
ARCH_NAME=
[ -z "$ARCHPKG" ] || ARCH_NAME=$(basename "$ARCHPKG")

expected_files() {
    cat <<'EOF'
/usr/bin/voice-type
/usr/share/applications/io.github.eriknovikov.voice-type.desktop
/usr/share/doc/voice-type/THIRD-PARTY-LICENSES.md
/usr/share/doc/voice-type/copyright
/usr/share/icons/hicolor/512x512/apps/io.github.eriknovikov.voice-type.png
EOF
}
expected_files | sort > "$TEMP_DIR/expected"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

BINARY=${PACKAGE_VERIFY_BINARY:-$(dirname "$PACKAGE_DIR")/voice-type}
[ -f "$BINARY" ] || fail "$BINARY is missing; run make packages first"
command -v readelf >/dev/null 2>&1 || fail 'readelf is required to determine the glibc floor'
GLIBC_MIN=$(readelf -V "$BINARY" | grep -o 'GLIBC_[0-9.]*' | sed 's/^GLIBC_//' | sort -Vu | tail -1)
[ -n "$GLIBC_MIN" ] || fail "cannot determine the glibc floor from $BINARY"

run() {
    "$RUNTIME" run --rm -v "$PACKAGE_DIR:/pkg:ro" "$@"
}

check_contents() {
    printf '%s\n' "$1" | sort > "$TEMP_DIR/actual"
    if ! diff -u "$TEMP_DIR/expected" "$TEMP_DIR/actual"; then
        fail 'package contains an unexpected file set'
    fi
    printf 'contents OK\n'
}

check_deb_depends() {
    ACTUAL=$(run ubuntu:24.04 dpkg-deb -f "/pkg/$DEB_NAME" Depends)
    EXPECTED="libc6 (>= $GLIBC_MIN), libstdc++6, libgtk-4-1, libwebkitgtk-6.0-4, libsoup-3.0-0, libvulkan1, ibus"
    [ "$ACTUAL" = "$EXPECTED" ] || fail "unexpected deb dependencies: $ACTUAL"
    printf 'dependencies OK\n'
}

check_rpm_depends() {
    ACTUAL=$(run fedora:latest sh -c \
        'rpm -qpR "/pkg/$1" | grep -v "^rpmlib(" | sort -u' sh "$RPM_NAME")
    EXPECTED=$(printf '%s\n' "glibc >= $GLIBC_MIN" libstdc++ gtk4 webkitgtk6.0 libsoup3 vulkan-loader ibus | sort -u)
    [ "$ACTUAL" = "$EXPECTED" ] || fail "unexpected rpm dependencies: $ACTUAL"
    printf 'dependencies OK\n'
}

check_arch_depends() {
    ACTUAL=$(run archlinux sh -c \
        'bsdtar -xOf "/pkg/$1" .PKGINFO | sed -n "s/^depend = //p" | sort' sh "$ARCH_NAME")
    EXPECTED=$(printf '%s\n' gtk4 webkitgtk-6.0 libsoup3 vulkan-icd-loader ibus | sort)
    [ "$ACTUAL" = "$EXPECTED" ] || fail "unexpected Arch dependencies: $ACTUAL"
    printf 'dependencies OK\n'
}

if [ "$SKIP_RUN" = 1 ]; then
    VERSION_CHECK=:
else
    VERSION_CHECK='/usr/bin/voice-type --version'
fi

install_deb() {
    IMAGE=$1
    printf '==> %s\n' "$IMAGE"
    run -e DEBIAN_FRONTEND=noninteractive -e VERSION_CHECK="$VERSION_CHECK" "$IMAGE" sh -c '
        apt-get update -qq &&
        apt-get install -y -qq "/pkg/$1" &&
        test "$(stat -c %a /usr/bin/voice-type)" = 755 &&
        sh -c "$VERSION_CHECK"
    ' sh "$DEB_NAME"
}

install_rpm() {
    IMAGE=$1
    printf '==> %s\n' "$IMAGE"
    run -e VERSION_CHECK="$VERSION_CHECK" "$IMAGE" sh -c '
        dnf install -y -q "/pkg/$1" &&
        test "$(stat -c %a /usr/bin/voice-type)" = 755 &&
        sh -c "$VERSION_CHECK"
    ' sh "$RPM_NAME"
}

install_arch() {
    IMAGE=archlinux
    printf '==> %s\n' "$IMAGE"
    run -e VERSION_CHECK="$VERSION_CHECK" "$IMAGE" sh -c '
        pacman-key --init >/dev/null 2>&1 &&
        pacman-key --populate archlinux >/dev/null 2>&1 &&
        pacman -Syu --noconfirm --quiet &&
        pacman -U --noconfirm "/pkg/$1" >/dev/null 2>&1 &&
        test "$(stat -c %a /usr/bin/voice-type)" = 755 &&
        sh -c "$VERSION_CHECK"
    ' sh "$ARCH_NAME"
}

printf 'Checking %s packages with %s\n' "$PACKAGE_DIR" "$RUNTIME"

printf '\n== deb contents ==\n'
DEB_FILES=$(run ubuntu:24.04 sh -c \
    'dpkg-deb --fsys-tarfile "/pkg/$1" | tar -tf - | sed "s|^\./|/|" | grep -v "/$"' sh "$DEB_NAME")
check_contents "$DEB_FILES"
check_deb_depends

printf '\n== rpm contents ==\n'
RPM_FILES=$(run fedora:latest rpm -qpl "/pkg/$RPM_NAME")
check_contents "$RPM_FILES"
check_rpm_depends

if [ -n "$ARCHPKG" ]; then
    printf '\n== Arch contents ==\n'
    ARCH_FILES=$(run archlinux sh -c 'pacman -Qqlp "/pkg/$1" | grep -v "/$"' sh "$ARCH_NAME")
    check_contents "$ARCH_FILES"
    check_arch_depends
fi

printf '\n== install ==\n'
install_deb ubuntu:24.04
install_deb debian:13
install_rpm fedora:latest
[ -z "$ARCHPKG" ] || install_arch

printf '\n== unsupported distribution ==\n'
if run -e DEBIAN_FRONTEND=noninteractive debian:12 sh -c '
    apt-get update -qq >/dev/null 2>&1 &&
    apt-get install -y "/pkg/$1"
' sh "$DEB_NAME" > "$TEMP_DIR/debian12.log" 2>&1; then
    fail 'Debian 12 unexpectedly installed an incompatible package'
fi
grep -q 'libc6' "$TEMP_DIR/debian12.log" || {
    cat "$TEMP_DIR/debian12.log" >&2
    fail 'Debian 12 did not fail with a glibc dependency error'
}
printf 'Debian 12 rejected the package with a glibc dependency error\n'

printf '\nAll package checks passed.\n'
