#!/usr/bin/env python3
"""Assemble a bundle. Signing and native verification are separate build gates."""

import argparse
import os
from pathlib import Path
import plistlib
import re
import shutil
import struct
import tempfile


ROOT = Path(__file__).resolve().parent.parent


def bundle(binary, output, version, development=False):
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?", version):
        raise ValueError("version must be a numeric release version with an optional suffix")
    if output.suffix != ".app" or output.is_symlink():
        raise ValueError("output must be a real .app directory")
    binary = binary.resolve(strict=True)
    with (ROOT / "packaging/macos/Info.plist").open("rb") as stream:
        info = plistlib.load(stream)
    if development:
        info["CFBundleIdentifier"] += ".dev"
        info["CFBundleName"] += " Dev"
        info["CFBundleDisplayName"] += " Dev"
    info["CFBundleVersion"] = version.split("-", 1)[0]
    info["CFBundleShortVersionString"] = info["CFBundleVersion"]
    if output.exists():
        with (output / "Contents/Info.plist").open("rb") as stream:
            previous = plistlib.load(stream)
        if previous.get("CFBundleIdentifier") != info["CFBundleIdentifier"]:
            raise ValueError("refusing to replace a bundle with a different identity")
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix=".voice-type-bundle-", dir=output.parent) as temporary:
        stage = Path(temporary) / "new.app"
        contents = stage / "Contents"
        (contents / "MacOS").mkdir(parents=True)
        resources = contents / "Resources"
        resources.mkdir()
        shutil.copyfile(binary, contents / "MacOS/voice-type")
        (contents / "MacOS/voice-type").chmod(0o755)
        with (contents / "Info.plist").open("wb") as stream:
            plistlib.dump(info, stream, sort_keys=False)
        # Modern ICNS accepts a PNG-backed 512px icon. Reuse the tracked logo.
        icon = (ROOT / "icon.png").read_bytes()
        if icon[:8] != b"\x89PNG\r\n\x1a\n" or struct.unpack(">II", icon[16:24]) != (512, 512):
            raise ValueError("icon.png must be a 512x512 PNG")
        (resources / "voice-type.icns").write_bytes(
            struct.pack(">4sI4sI", b"icns", len(icon) + 16, b"ic09", len(icon) + 8) + icon
        )
        shutil.copyfile(ROOT / "LICENSE", resources / "LICENSE")
        shutil.copyfile(ROOT / "build/THIRD-PARTY-LICENSES.md", resources / "THIRD-PARTY-LICENSES.md")
        previous_path = Path(temporary) / "previous.app"
        if output.exists():
            output.rename(previous_path)
        try:
            os.replace(stage, output)
        except OSError:
            if previous_path.exists():
                previous_path.rename(output)
            raise
    print(f"Assembled {output} ({info['CFBundleIdentifier']}, {version}); not release-signed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--version", required=True)
    parser.add_argument("--development", action="store_true")
    args = parser.parse_args()
    bundle(args.binary, args.output, args.version, args.development)


if __name__ == "__main__":
    main()
