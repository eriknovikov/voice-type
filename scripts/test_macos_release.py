"""Portable fail-closed packaging fixtures. These do not sign anything."""

import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location("macos_release", ROOT / "scripts/macos-release.py")
RELEASE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(RELEASE)


class FakeNative:
    def __init__(self, arch="arm64"):
        self.arch = arch
        self.minimum = "13.0"
        self.dependency = "/usr/lib/libSystem.B.dylib"
        self.signature = "Authority=Voice Type Self-Signed\nflags=0x10000(runtime)\n"
        self.certificate = RELEASE.CERTIFICATE
        self.requirement = "designated => " + RELEASE.APP_REQUIREMENT
        self.entitlements = RELEASE.ENTITLEMENTS
        self.calls = []
        self.corrupt_mount = False

    def __call__(self, *args):
        args = tuple(map(str, args))
        self.calls.append(args)
        name = args[0]
        if name == "lipo":
            return "x86_64\n" if self.arch == "amd64" else self.arch + "\n"
        if name == "otool":
            if args[1] == "-l":
                return f"Load command 1\n cmd LC_BUILD_VERSION\n platform 1\n minos {self.minimum}\n sdk 15.5\nLoad command 2\n cmd LC_SOURCE_VERSION\n version 0.0\n"
            return f"{args[-1]}:\n\t{self.dependency} (compatibility version 1.0.0)\n"
        if name == "go":
            return f'-tags=production\n-ldflags="-X main.version=6.4.0"\nGOOS=darwin\nGOARCH={self.arch}\nCGO_ENABLED=1\n'
        if args[-1] == "--version":
            return "6.4.0\n"
        if name == "codesign":
            if "--extract-certificates" in args:
                Path(args[args.index("--extract-certificates") + 1] + "0").write_bytes(self.certificate)
                return ""
            if "--requirements" in args and "--display" in args:
                return self.requirement
            if "--entitlements" in args and "--display" in args:
                return plistlib.dumps(self.entitlements).decode()
            return self.signature if "--display" in args else ""
        if name == "ditto":
            if "-c" in args:
                Path(args[-1]).write_bytes(b"fixture zip")
            else:
                shutil.copytree(args[1], args[2])
            return ""
        if name == "hdiutil":
            if args[1] == "create":
                self.payload = Path(args[args.index("-srcfolder") + 1])
                Path(args[-1]).write_bytes(b"fixture DMG, not a disk image")
            elif args[1] == "attach":
                mount = Path(args[args.index("-mountpoint") + 1])
                shutil.copytree(self.payload, mount, symlinks=True, dirs_exist_ok=True)
                if self.corrupt_mount:
                    (mount / "Voice Type.app/Contents/Resources/config.jsonc").write_text("not allowed")
            return ""
        raise AssertionError(f"unexpected native call: {args}")


class ReleaseTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.app = self.root / "Voice Type.app"
        resources = self.app / "Contents/Resources"
        resources.mkdir(parents=True)
        (self.app / "Contents/MacOS").mkdir()
        binary = self.app / "Contents/MacOS/voice-type"
        binary.write_bytes(b"fixture, not Mach-O")
        binary.chmod(0o755)
        with (ROOT / "packaging/macos/Info.plist").open("rb") as stream:
            self.info = plistlib.load(stream)
        self.info.update(CFBundleVersion="6.4.0", CFBundleShortVersionString="6.4.0")
        self.write_info()
        for name in ("LICENSE", "THIRD-PARTY-LICENSES.md", "voice-type.icns"):
            (resources / name).write_bytes(b"fixture")
        self.output = self.root / "release"
        self.native = FakeNative()

    def write_info(self):
        (self.app / "Contents/Info.plist").write_bytes(plistlib.dumps(self.info))

    def package(self, **kwargs):
        options = dict(keychain="fixture-keychain", run=self.native)
        options.update(kwargs)
        with contextlib.redirect_stdout(io.StringIO()):
            return RELEASE.package_release(self.app, self.output, "6.4.0", self.native.arch, **options)

    def test_both_architecture_sequences_and_final_checksum(self):
        for arch in ("arm64", "amd64"):
            self.native = FakeNative(arch)
            artifact = self.package()
            self.assertEqual(artifact.name, f"voice-type_6.4.0_darwin_{arch}.dmg")
            self.assertEqual(artifact.with_suffix(".dmg.sha256").read_text(), hashlib.sha256(artifact.read_bytes()).hexdigest() + "  " + artifact.name + "\n")
            calls = self.native.calls
            self.assertFalse(any(call[0] in ("xcrun", "spctl") for call in calls))
            signatures = [call for call in calls if call[0] == "codesign" and "--sign" in call]
            self.assertEqual(len(signatures), 2)
            self.assertTrue(all(call[call.index("--sign") + 1] == RELEASE.SIGN_IDENTITY for call in signatures))
            self.assertIn("=designated => " + RELEASE.APP_REQUIREMENT, signatures[0])
            self.assertTrue(any(call[:2] == ("hdiutil", "detach") for call in calls))
            self.assertFalse(any(call[0] == "codesign" and "--sign" in call and "--deep" in call for call in calls))
            self.assertEqual((self.app / "Contents/MacOS/voice-type").read_bytes(), b"fixture, not Mach-O")

    def test_machine_readable_command_output_excludes_stderr(self):
        result = RELEASE.command(sys.executable, "-c", 'import sys; print(\'{"status":"Accepted"}\'); print("progress", file=sys.stderr)')
        self.assertEqual(json.loads(result), {"status": "Accepted"})

    def test_missing_credentials_never_sign_or_publish(self):
        with self.assertRaises(ValueError):
            self.package(keychain="")
        self.assertEqual(self.native.calls, [])
        self.assertFalse(self.output.exists())

    def test_mounted_validation_failure_does_not_publish(self):
        self.native.corrupt_mount = True
        with self.assertRaisesRegex(ValueError, "unexpected bundle file"):
            self.package()
        self.assertTrue(any(call[:2] == ("hdiutil", "detach") for call in self.native.calls))
        self.assertEqual(list(self.output.iterdir()), [])

    def test_release_rejects_bad_signatures_certificates_requirements_and_entitlements(self):
        original = self.native.signature
        for signature in ("Signature=adhoc\n", original.replace("runtime", "none")):
            self.native.signature = signature
            with self.assertRaises(ValueError):
                RELEASE.verify_bundle(self.app, "6.4.0", "arm64", signed=True, run=self.native)
        self.native.signature = original
        self.native.certificate = b"different certificate"
        with self.assertRaisesRegex(ValueError, "certificate"):
            self.package()
        self.native.certificate = RELEASE.CERTIFICATE
        self.native.requirement = 'designated => cdhash H"1234"'
        with self.assertRaisesRegex(ValueError, "designated requirement"):
            self.package()
        self.native.requirement = "designated => " + RELEASE.APP_REQUIREMENT
        self.native.entitlements = {**RELEASE.ENTITLEMENTS, "com.apple.security.cs.disable-library-validation": True}
        with self.assertRaisesRegex(ValueError, "entitlements"):
            RELEASE.verify_bundle(self.app, "6.4.0", "arm64", signed=True, run=self.native)

    def test_bad_arch_deployment_dependency_identity_and_mutable_bundle_refused(self):
        for value in ("14.0", "12.0"):
            self.native.minimum = value
            with self.assertRaisesRegex(ValueError, "deployment"):
                RELEASE.verify_bundle(self.app, "6.4.0", "arm64", run=self.native)
        self.native.minimum = "13.0"
        self.native.dependency = "/opt/homebrew/lib/libfixture.dylib"
        with self.assertRaisesRegex(ValueError, "non-system"):
            RELEASE.verify_bundle(self.app, "6.4.0", "arm64", run=self.native)
        self.native.dependency = "/usr/lib/libSystem.B.dylib"
        with self.assertRaisesRegex(ValueError, "architecture"):
            RELEASE.verify_bundle(self.app, "6.4.0", "amd64", run=self.native)
        self.info["CFBundleIdentifier"] += ".dev"
        self.write_info()
        with self.assertRaisesRegex(ValueError, "identity"):
            self.package()
        self.info["CFBundleIdentifier"] = RELEASE.APP_ID
        self.write_info()
        (self.app / "Contents/Resources/model.gguf").write_bytes(b"mutable")
        with self.assertRaisesRegex(ValueError, "unexpected bundle file"):
            self.package()

    def test_existing_or_symlink_outputs_are_not_overwritten(self):
        self.output.mkdir()
        checksum = self.output / "voice-type_6.4.0_darwin_arm64.dmg.sha256"
        target = self.root / "untouched"
        checksum.symlink_to(target)
        with self.assertRaisesRegex(ValueError, "already exists"):
            self.package()
        self.assertFalse(target.exists())

    def test_native_engine_manifest_requires_static_arch_specific_backends(self):
        prefix = self.root / "native"
        libraries = prefix / "lib"
        libraries.mkdir(parents=True)
        manifest = {"shared": False, "backend_dl": False, "backends": ["cpu", "metal"], "metal_embed": True,
                    "libraries": ["transcribe", "ggml-cpu", "ggml-metal"], "frameworks": ["Accelerate", "Metal"]}
        for name in manifest["libraries"]:
            (libraries / ("lib" + name + ".a")).write_bytes(b"fixture")
        path = libraries / "transcribe-link.json"
        path.write_text(json.dumps(manifest))
        RELEASE.verify_engine(prefix, "arm64")
        for key, value in (("shared", True), ("backend_dl", True), ("backends", ["cpu"]), ("metal_embed", False), ("frameworks", ["Metal"])):
            path.write_text(json.dumps({**manifest, key: value}))
            with self.subTest(key=key), self.assertRaises(ValueError):
                RELEASE.verify_engine(prefix, "arm64")
        manifest.update(backends=["cpu"], metal_embed=False)
        path.write_text(json.dumps(manifest))
        RELEASE.verify_engine(prefix, "amd64")

    @unittest.skipIf(os.uname().sysname == "Darwin", "Linux refusal only")
    def test_cli_refuses_non_native_host(self):
        result = subprocess.run(["python3", str(ROOT / "scripts/macos-release.py"), "package", "--arch", "arm64"], capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("native macOS is required", result.stderr)


if __name__ == "__main__":
    unittest.main()
