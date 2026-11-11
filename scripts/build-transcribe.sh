#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
REV=2778ae4bf5ca5f394d129a39467f4ad9552dc34f
ARCHIVE_SHA256=1ae437641c38126998bb133d0b9f6a315c0409293a62511372f6e4eb27a5e626
NATIVE_DIR="$ROOT/build/transcribe"
SOURCE_DIR="$NATIVE_DIR/source-$REV"
ARCHIVE="$NATIVE_DIR/transcribe.cpp-$REV.tar.gz"
BUILD_DIR="$NATIVE_DIR/build"
PREFIX="$NATIVE_DIR/install"

for tool in cmake curl sha256sum tar pkg-config go; do
    command -v "$tool" >/dev/null 2>&1 || {
        printf 'Missing build dependency: %s\n' "$tool" >&2
        exit 1
    }
done

mkdir -p "$NATIVE_DIR"
if [ ! -f "$ARCHIVE" ]; then
    curl -fLsS --retry 3 \
        "https://github.com/anomalyco/transcribe.cpp/archive/$REV.tar.gz" \
        -o "$ARCHIVE.tmp"
    printf '%s  %s\n' "$ARCHIVE_SHA256" "$ARCHIVE.tmp" | sha256sum -c -
    mv "$ARCHIVE.tmp" "$ARCHIVE"
fi
printf '%s  %s\n' "$ARCHIVE_SHA256" "$ARCHIVE" | sha256sum -c -

if [ ! -f "$SOURCE_DIR/.voice-type-revision" ]; then
    mkdir -p "$SOURCE_DIR"
    tar -xzf "$ARCHIVE" --strip-components=1 -C "$SOURCE_DIR"
    printf '%s\n' "$REV" > "$SOURCE_DIR/.voice-type-revision"
fi

VULKAN=OFF
case "${VOICE_TYPE_VULKAN:-auto}" in
    0|off|OFF) ;;
    1|on|ON)
        command -v glslc >/dev/null 2>&1 || {
            printf '%s\n' 'VOICE_TYPE_VULKAN=1 requires glslc.' >&2
            exit 1
        }
        pkg-config --exists vulkan || {
            printf '%s\n' 'VOICE_TYPE_VULKAN=1 requires Vulkan development files.' >&2
            exit 1
        }
        VULKAN=ON
        ;;
    auto)
        if command -v glslc >/dev/null 2>&1 && pkg-config --exists vulkan; then
            VULKAN=ON
        fi
        ;;
    *)
        printf 'Invalid VOICE_TYPE_VULKAN value: %s\n' "$VOICE_TYPE_VULKAN" >&2
        exit 2
        ;;
esac

cmake -S "$SOURCE_DIR" -B "$BUILD_DIR" \
    -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_INSTALL_PREFIX="$PREFIX" \
    -DTRANSCRIBE_BUILD_SHARED=OFF \
    -DTRANSCRIBE_BUILD_TESTS=OFF \
    -DTRANSCRIBE_BUILD_EXAMPLES=OFF \
    -DTRANSCRIBE_BUILD_TOOLS=OFF \
    -DTRANSCRIBE_BUILD_REAL_MODEL_TESTS=OFF \
    -DTRANSCRIBE_USE_OPENMP=OFF \
    -DTRANSCRIBE_USE_SYSTEM_BLAS=OFF \
    -DTRANSCRIBE_VULKAN="$VULKAN" \
    -DTRANSCRIBE_X86_CONSERVATIVE=ON \
    -DGGML_NATIVE=OFF
cmake --build "$BUILD_DIR" --parallel "${VOICE_TYPE_BUILD_JOBS:-4}"
cmake --install "$BUILD_DIR"

MANIFEST=
for candidate in "$PREFIX/lib/transcribe-link.json" "$PREFIX/lib64/transcribe-link.json"; do
    if [ -f "$candidate" ]; then
        MANIFEST=$candidate
        break
    fi
done
[ -n "$MANIFEST" ] || {
    printf '%s\n' 'transcribe.cpp did not install its native link manifest.' >&2
    exit 1
}
go run "$ROOT/scripts/generate-transcribe-pkgconfig.go" \
    "$PREFIX" "$MANIFEST" "$NATIVE_DIR/pkgconfig"
cp "$ROOT/LICENSE" "$ROOT/build/LICENSE"
{
    printf '# transcribe.cpp license\n\n'
    cat "$SOURCE_DIR/LICENSE"
    printf '\n---\n\n'
    cat "$SOURCE_DIR/THIRD-PARTY-LICENSES.md"
} > "$ROOT/build/THIRD-PARTY-LICENSES.md"
printf 'Built transcribe.cpp %s (%s backend build)\n' "$REV" "$VULKAN"
