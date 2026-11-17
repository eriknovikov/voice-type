"""Portable orchestration tests, not native macOS build/signing evidence."""

import importlib.util
import base64
import json
import os
from pathlib import Path
import plistlib
import shutil
import struct
import subprocess
import sys
import tempfile
import textwrap
import unittest


ROOT = Path(__file__).resolve().parent.parent
REV = "2778ae4bf5ca5f394d129a39467f4ad9552dc34f"
SPEC = importlib.util.spec_from_file_location("bundle_macos", ROOT / "scripts/bundle-macos.py")
BUNDLER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUNDLER)


class BundleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "packaging/macos").mkdir(parents=True)
        (self.root / "build").mkdir()
        shutil.copyfile(ROOT / "packaging/macos/Info.plist", self.root / "packaging/macos/Info.plist")
        shutil.copyfile(ROOT / "icon.png", self.root / "icon.png")
        (self.root / "LICENSE").write_text("test license")
        (self.root / "build/THIRD-PARTY-LICENSES.md").write_text("test dependency licenses")
        self.binary = self.root / "binary"
        self.binary.write_bytes(b"synthetic binary, not Mach-O")
        old_root = BUNDLER.ROOT
        BUNDLER.ROOT = self.root
        self.addCleanup(setattr, BUNDLER, "ROOT", old_root)

    def test_bundle_metadata_resources_and_clean_rebuild(self):
        for development in (False, True):
            output = self.root / ("Voice Type Dev.app" if development else "Voice Type.app")
            BUNDLER.bundle(self.binary, output, "6.4.0-dev" if development else "6.4.0", development)
            with (output / "Contents/Info.plist").open("rb") as stream:
                info = plistlib.load(stream)
            self.assertEqual(info["CFBundleIdentifier"], "io.github.eriknovikov.voice-type" + (".dev" if development else ""))
            self.assertEqual(info["CFBundleVersion"], "6.4.0")
            self.assertEqual(info["LSMinimumSystemVersion"], "13.0")
            self.assertTrue(info["LSUIElement"])
            self.assertIn("microphone", info["NSMicrophoneUsageDescription"])
            executable = output / "Contents/MacOS/voice-type"
            self.assertEqual(executable.read_bytes(), self.binary.read_bytes())
            self.assertEqual(executable.stat().st_mode & 0o777, 0o755)
            icon = (output / "Contents/Resources/voice-type.icns").read_bytes()
            self.assertEqual(struct.unpack(">4sI4sI", icon[:16]), (b"icns", len(icon), b"ic09", len(icon) - 8))
            self.assertTrue((output / "Contents/Resources/LICENSE").is_file())
            self.assertTrue((output / "Contents/Resources/THIRD-PARTY-LICENSES.md").is_file())
            stale = output / "stale"
            stale.write_text("must not survive")
            BUNDLER.bundle(self.binary, output, "6.4.0", development)
            self.assertFalse(stale.exists())

    def test_refuses_other_identity_symlink_and_invalid_version(self):
        output = self.root / "Voice Type.app"
        BUNDLER.bundle(self.binary, output, "6.4.0")
        with self.assertRaises(ValueError):
            BUNDLER.bundle(self.binary, output, "6.4.0", development=True)
        alias = self.root / "alias.app"
        alias.symlink_to(output)
        with self.assertRaises(ValueError):
            BUNDLER.bundle(self.binary, alias, "6.4.0")
        with self.assertRaises(ValueError):
            BUNDLER.bundle(self.binary, output, "bad\nversion")
        self.assertTrue((output / "Contents/MacOS/voice-type").exists())

    def test_minimal_entitlements(self):
        with (ROOT / "packaging/macos/entitlements.plist").open("rb") as stream:
            self.assertEqual(plistlib.load(stream), {"com.apple.security.device.audio-input": True})


class NativeBuildDispatchTests(unittest.TestCase):
    def run_build(self, platform, architecture, checksum="shasum", vulkan="0"):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        (root / "scripts").mkdir()
        shutil.copyfile(ROOT / "scripts/build-transcribe.sh", root / "scripts/build-transcribe.sh")
        native = root / "build/transcribe"
        source = native / ("source-" + REV)
        source.mkdir(parents=True)
        (source / ".voice-type-revision").write_text(REV)
        (source / "LICENSE").write_text("test")
        (source / "THIRD-PARTY-LICENSES.md").write_text("test")
        (native / ("transcribe.cpp-" + REV + ".tar.gz")).write_bytes(b"mock archive")
        (root / "LICENSE").write_text("test")
        tools = root / "tools"
        tools.mkdir()
        dispatcher = tools / "dispatch"
        dispatcher.write_text(f"#!{sys.executable}\n" + '''
import json, os, pathlib, sys
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ["TEST_LOG"], "a") as log:
    log.write(json.dumps([name, args]) + "\\n")
if name == "uname":
    print(os.environ["TEST_PLATFORM"])
elif name == "go" and args == ["env", "GOARCH"]:
    print(os.environ["TEST_ARCH"])
elif name == "go" and args[0] == "run":
    pathlib.Path(args[-1]).mkdir(parents=True, exist_ok=True)
elif name == "cmake" and "-S" in args:
    prefix = pathlib.Path(next(a.split("=", 1)[1] for a in args if a.startswith("-DCMAKE_INSTALL_PREFIX=")))
    (prefix / "lib").mkdir(parents=True)
    (prefix / "lib/transcribe-link.json").write_text("{}")
elif name in ("sha256sum", "shasum"):
    with open(os.environ["TEST_LOG"], "a") as log:
        log.write(json.dumps(["checksum-input", sys.stdin.read()]) + "\\n")
''')
        dispatcher.chmod(0o755)
        for name in ("go", "uname", "cmake", "curl", "pkg-config", checksum):
            (tools / name).symlink_to(dispatcher)
        for name in ("dirname", "mkdir", "cp", "cat", "tar"):
            (tools / name).symlink_to(shutil.which(name))
        log = root / "commands.jsonl"
        env = dict(os.environ, PATH=str(tools), TEST_LOG=str(log), TEST_PLATFORM=platform,
                   TEST_ARCH=architecture, VOICE_TYPE_VULKAN=vulkan)
        result = subprocess.run(["/bin/sh", str(root / "scripts/build-transcribe.sh")], env=env,
                                text=True, capture_output=True, check=False)
        calls = [json.loads(line) for line in log.read_text().splitlines()]
        return result, calls

    def test_native_platform_flags_and_checksum_tools(self):
        for platform, arch, metal, checksum in (("Darwin", "arm64", "ON", "shasum"),
                                               ("Darwin", "amd64", "OFF", "shasum"),
                                               ("Linux", "arm64", "OFF", "sha256sum")):
            with self.subTest(platform=platform, architecture=arch):
                result, calls = self.run_build(platform, arch, checksum)
                self.assertEqual(result.returncode, 0, result.stderr)
                configure = next(args for name, args in calls if name == "cmake" and "-S" in args)
                self.assertIn("-DTRANSCRIBE_METAL=" + metal, configure)
                self.assertIn("-DTRANSCRIBE_VULKAN=OFF", configure)
                self.assertIn("-DTRANSCRIBE_USE_OPENMP=OFF", configure)
                self.assertIn("-DTRANSCRIBE_GGML_BACKEND_DL=OFF", configure)
                self.assertIn("-DGGML_NATIVE=OFF", configure)
                if platform == "Darwin":
                    self.assertIn("-DCMAKE_OSX_DEPLOYMENT_TARGET=13.0", configure)
                    self.assertIn("-DCMAKE_OSX_ARCHITECTURES=" + ("arm64" if arch == "arm64" else "x86_64"), configure)
                    self.assertIn("-DGGML_METAL_EMBED_LIBRARY=ON", configure)
                    self.assertIn("-DGGML_ACCELERATE=ON", configure)
                digest_args = next(args for name, args in calls if name == checksum)
                self.assertEqual(digest_args, ["-a", "256", "-c", "-"] if checksum == "shasum" else ["-c", "-"])
                digest = next(args for name, args in calls if name == "checksum-input")
                self.assertTrue(digest.startswith("1ae437641c38126998bb133d0b9f6a315c0409293a62511372f6e4eb27a5e626  "))

    def test_mac_refuses_vulkan(self):
        result, calls = self.run_build("Darwin", "arm64", vulkan="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Vulkan is not supported", result.stderr)
        self.assertFalse(any(name == "cmake" for name, _ in calls))


class SigningSetupTests(unittest.TestCase):
    def test_import_registers_keychain_for_private_key_lookup_and_removes_p12(self):
        workflow = (ROOT / ".github/workflows/macos.yml").read_text()
        section = workflow.split("- name: Import temporary release credentials\n", 1)[1]
        script = textwrap.dedent(section.split("        run: |\n", 1)[1].split("      - name:", 1)[0])
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            tools = root / "tools"
            tools.mkdir()
            security = tools / "security"
            security.write_text(f"#!{sys.executable}\n" + '''
import json, os, pathlib, sys
with open(os.environ["TEST_LOG"], "a") as log:
    log.write(json.dumps(sys.argv[1:]) + "\\n")
if sys.argv[1] == "create-keychain":
    pathlib.Path(sys.argv[-1]).touch()
''')
            security.chmod(0o755)
            log = root / "commands.jsonl"
            keychain = str(root / "signing.keychain-db")
            result = subprocess.run(["bash", "-e", "-c", script], capture_output=True, text=True,
                                    env=dict(os.environ, PATH=str(tools) + os.pathsep + os.environ["PATH"],
                                             TEST_LOG=str(log), RUNNER_TEMP=temporary, MACOS_KEYCHAIN=keychain,
                                             CERTIFICATE=base64.b64encode(b"fixture, not a certificate").decode(),
                                             CERTIFICATE_PASSWORD="fixture-password"))
            self.assertEqual(result.returncode, 0, result.stderr)
            calls = [json.loads(line) for line in log.read_text().splitlines()]
            search = calls.index(["list-keychains", "-d", "user", "-s", keychain])
            default = calls.index(["default-keychain", "-d", "user", "-s", keychain])
            imported = next(i for i, call in enumerate(calls) if call[0] == "import")
            self.assertLess(search, imported)
            self.assertLess(default, imported)
            self.assertFalse((root / "voice-type-signing.p12").exists())


if __name__ == "__main__":
    unittest.main()
