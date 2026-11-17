#!/usr/bin/env python3
"""Verify native bundles, or self-sign and verify an unnotarized macOS DMG."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import ssl
import subprocess
import tempfile


ROOT = Path(__file__).resolve().parent.parent
APP_ID = "io.github.eriknovikov.voice-type"
ENTITLEMENTS = {"com.apple.security.device.audio-input": True}
CERTIFICATE = ssl.PEM_cert_to_DER_cert((ROOT / "packaging/macos/signing-cert.pem").read_text())
SIGN_IDENTITY = hashlib.sha1(CERTIFICATE).hexdigest().upper()
APP_REQUIREMENT = f'identifier "{APP_ID}" and certificate leaf = H"{SIGN_IDENTITY}"'


def command(*args):
    result = subprocess.run([str(arg) for arg in args], check=True, capture_output=True, text=True)
    if args[0] == "codesign" and "--display" in args:
        return result.stdout + result.stderr
    return result.stdout


def require(condition, message):
    if not condition:
        raise ValueError(message)


def version_number(value):
    require(re.fullmatch(r"\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?", value), "invalid release version")
    return value.split("-", 1)[0]


def verify_signature(path, run=command, app=True):
    run("codesign", "--verify", "--deep", "--strict", path)
    info = run("codesign", "--display", "--verbose=4", path)
    require("Signature=adhoc" not in info, "ad-hoc signatures cannot be distributed")
    with tempfile.TemporaryDirectory(prefix="voice-type-certificate-") as temporary:
        prefix = Path(temporary) / "certificate"
        run("codesign", "--display", "--extract-certificates", prefix, path)
        require(Path(str(prefix) + "0").read_bytes() == CERTIFICATE, "unexpected signing certificate")
    if app:
        require(re.search(r"flags=.*\bruntime\b", info), "hardened runtime required")
        run("codesign", "--verify", "--strict", "--test-requirement", "=" + APP_REQUIREMENT, path)
        requirement = run("codesign", "--display", "--requirements", "-", path)
        expected = "designated => " + APP_REQUIREMENT
        require(expected.lower() in requirement.lower(), "unexpected designated requirement")
        raw = run("codesign", "--display", "--entitlements", ":-", "--xml", path)
        start, end = raw.find("<?xml"), raw.rfind("</plist>")
        require(start >= 0 and end >= start, "could not read signed entitlements")
        require(plistlib.loads(raw[start:end + len("</plist>")].encode()) == ENTITLEMENTS, "unexpected signed entitlements")


def verify_bundle(app, version, arch, signed=False, development=False, run=command):
    require(arch in ("arm64", "amd64"), "unsupported Mac architecture")
    require(not app.is_symlink() and app.is_dir(), "app must be a real bundle directory")
    number = version_number(version)
    with (app / "Contents/Info.plist").open("rb") as stream:
        info = plistlib.load(stream)
    require(info.get("CFBundleIdentifier") == APP_ID + (".dev" if development else ""), "unexpected bundle identity")
    for key in ("CFBundleVersion", "CFBundleShortVersionString"):
        require(info.get(key) == number, f"unexpected {key}")
    require(info.get("LSMinimumSystemVersion") == "13.0" and info.get("LSUIElement") is True, "unexpected OS/menu-bar policy")
    require(bool(info.get("NSMicrophoneUsageDescription")), "missing microphone purpose string")
    require(info.get("CFBundleExecutable") == "voice-type", "unexpected bundle executable")
    allowed = {
        "Contents/Info.plist", "Contents/MacOS/voice-type", "Contents/Resources/voice-type.icns",
        "Contents/Resources/LICENSE", "Contents/Resources/THIRD-PARTY-LICENSES.md",
        "Contents/_CodeSignature/CodeResources", "Contents/CodeResources",
    }
    for path in app.rglob("*"):
        require(not path.is_symlink(), "bundle must not contain symlinks")
        if path.is_file():
            require(path.relative_to(app).as_posix() in allowed, f"unexpected bundle file: {path.name}")
    for resource in ("LICENSE", "THIRD-PARTY-LICENSES.md", "voice-type.icns"):
        require((app / "Contents/Resources" / resource).stat().st_size > 0, f"missing resource: {resource}")
    binary = app / "Contents/MacOS/voice-type"
    require(os.access(binary, os.X_OK), "bundle executable is not executable")
    expected_arch = "x86_64" if arch == "amd64" else "arm64"
    require(run("lipo", "-archs", binary).strip() == expected_arch, "expected one native architecture, not a universal binary")
    load_commands = run("otool", "-l", binary)
    minimums = []
    for block in re.split(r"Load command \d+", load_commands):
        field = None
        if re.search(r"cmd LC_BUILD_VERSION\b", block):
            field = "minos"
        elif re.search(r"cmd LC_VERSION_MIN_MACOSX\b", block):
            field = "version"
        if field:
            minimums.extend(re.findall(rf"^\s*{field}\s+(\d+\.\d+(?:\.\d+)?)\s*$", block, re.MULTILINE))
    require(minimums and all(tuple(map(int, item.split(".")))[:2] == (13, 0) for item in minimums),
            f"Mach-O deployment target must be macOS 13.0; found {minimums}")
    for line in run("otool", "-L", binary).splitlines()[1:]:
        dependency = line.strip().split(" (", 1)[0]
        require(dependency.startswith(("/System/Library/", "/usr/lib/")), f"non-system runtime dependency: {dependency}")
    build_info = run("go", "version", "-m", binary)
    for entry in (f"GOARCH={arch}", "GOOS=darwin", "CGO_ENABLED=1", f"main.version={version}"):
        require(entry in build_info, f"missing build metadata: {entry}")
    if development:
        require("developmentBuild=true" in build_info, "missing development identity stamp")
    else:
        require("-tags=production" in build_info and "developmentBuild=true" not in build_info, "release requires a production build")
    require(run(binary, "--version").strip() == version, "executable version differs from release")
    if signed:
        require(not development, "development bundle cannot pass a release gate")
        verify_signature(app, run)


def verify_engine(prefix, arch):
    manifests = list(prefix.glob("lib*/transcribe-link.json"))
    require(len(manifests) == 1, "one native transcribe link manifest is required")
    manifest = json.loads(manifests[0].read_text())
    require(manifest.get("shared") is False and manifest.get("backend_dl") is False, "native inference must be static without backend modules")
    expected = {"cpu", "metal"} if arch == "arm64" else {"cpu"}
    require(set(manifest.get("backends", [])) == expected, "unexpected native inference backends")
    require(manifest.get("metal_embed") is (arch == "arm64"), "Metal shaders must be embedded on Apple Silicon only")
    require("Accelerate" in manifest.get("frameworks", []), "native inference must link system Accelerate")
    libraries = manifest.get("libraries", [])
    require(libraries and all((manifests[0].parent / ("lib" + name + ".a")).is_file() for name in libraries), "native static archives are missing")


def package_release(app, output, version, arch, keychain, run=command):
    require(keychain, "a signing keychain is required")
    version_number(version)
    require(arch in ("arm64", "amd64"), "unsupported Mac architecture")
    require(not output.is_symlink(), "release output must not be a symlink")
    output.mkdir(parents=True, exist_ok=True)
    name = f"voice-type_{version}_darwin_{arch}.dmg"
    final = output / name
    checksum = output / (name + ".sha256")
    require(not final.exists() and not final.is_symlink() and not checksum.exists() and not checksum.is_symlink(), "release output already exists; do not overwrite verified artifacts")
    verify_bundle(app, version, arch, run=run)
    with tempfile.TemporaryDirectory(prefix=".mac-release-", dir=output) as temporary:
        stage = Path(temporary)
        payload = stage / "payload"
        payload.mkdir()
        signed_app = payload / "Voice Type.app"
        run("ditto", app, signed_app)
        run("codesign", "--force", "--sign", SIGN_IDENTITY, "--keychain", keychain, "--timestamp=none", "--options", "runtime",
            "--requirements", "=designated => " + APP_REQUIREMENT,
            "--entitlements", ROOT / "packaging/macos/entitlements.plist", signed_app)
        verify_bundle(signed_app, version, arch, signed=True, run=run)
        (payload / "Applications").symlink_to("/Applications")
        dmg = stage / name
        run("hdiutil", "create", "-volname", "Voice Type", "-srcfolder", payload, "-fs", "HFS+", "-format", "UDZO", dmg)
        run("codesign", "--sign", SIGN_IDENTITY, "--keychain", keychain, "--timestamp=none", dmg)
        verify_signature(dmg, run, app=False)
        run("hdiutil", "verify", dmg)
        mount = stage / "mount"
        mount.mkdir()
        run("hdiutil", "attach", "-readonly", "-nobrowse", "-mountpoint", mount, dmg)
        try:
            require({p.name for p in mount.iterdir()} == {"Voice Type.app", "Applications"}, "unexpected DMG contents")
            require((mount / "Applications").is_symlink() and os.readlink(mount / "Applications") == "/Applications", "missing Applications link")
            verify_bundle(mount / "Voice Type.app", version, arch, signed=True, run=run)
        finally:
            run("hdiutil", "detach", mount)
        digest = hashlib.sha256()
        with dmg.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1 << 20), b""):
                digest.update(chunk)
        os.replace(dmg, final)
        checksum.write_text(f"{digest.hexdigest()}  {name}\n")
    print(f"Verified self-signed DMG (NOT notarized; per-app security approval required): {final}")
    return final


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("verify", "package"))
    parser.add_argument("--app", type=Path, default=ROOT / "build/Voice Type.app")
    parser.add_argument("--version", default=(ROOT / "VERSION").read_text().strip())
    parser.add_argument("--arch", required=True, choices=("arm64", "amd64"))
    parser.add_argument("--output", type=Path, default=ROOT / "build/macos-release")
    parser.add_argument("--development", action="store_true")
    args = parser.parse_args()
    require(platform.system() == "Darwin", "native macOS is required; cross-platform fixtures are not release evidence")
    if args.action == "verify":
        verify_engine(ROOT / "build/transcribe/install", args.arch)
        verify_bundle(args.app, args.version, args.arch, development=args.development)
        print("Verified native bundle (not a distribution signature gate)")
    else:
        require(not args.development, "development bundles cannot be released")
        package_release(args.app, args.output, args.version, args.arch, os.environ.get("MACOS_KEYCHAIN", ""))


if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError as error:
        raise SystemExit(f"Native gate failed: {Path(str(error.cmd[0])).name}\n{error.stdout or ''}{error.stderr or ''}") from error
    except (ValueError, OSError) as error:
        raise SystemExit(str(error)) from error
