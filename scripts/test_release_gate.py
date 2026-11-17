"""Execute the release workflow's local asset gate without contacting GitHub."""

import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import textwrap
import unittest


ROOT = Path(__file__).resolve().parent.parent


@unittest.skipUnless(os.uname().sysname == "Linux", "publisher uses GNU tools on Linux")
class ReleaseAssetGateTests(unittest.TestCase):
    def test_preview_requires_both_intact_dmgs_and_never_publishes_latest(self):
        workflow = (ROOT / ".github/workflows/mac-preview.yml").read_text()
        publication = workflow.split("- name: Publish verified preview, never Latest\n", 1)[1]
        script = textwrap.dedent(publication.split("        run: |\n", 1)[1]).split('TAG="v$VERSION"', 1)[0]
        self.assertIn('--prerelease --latest=false', publication)
        version = "6.4.0-mac.preview.1"
        for scenario in ("complete", "missing", "wrong-arch", "corrupt", "no-checksum", "extra"):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as temporary:
                root = Path(temporary)
                directory = root / "release"
                directory.mkdir()
                for arch in ("arm64", "amd64"):
                    name = f"voice-type_{version}_darwin_{arch}.dmg"
                    artifact = directory / name
                    artifact.write_bytes(b"fixture DMG, not signed")
                    (directory / (name + ".sha256")).write_text(hashlib.sha256(artifact.read_bytes()).hexdigest() + "  " + name + "\n")
                arm = directory / f"voice-type_{version}_darwin_arm64.dmg"
                if scenario == "missing":
                    arm.unlink()
                elif scenario == "wrong-arch":
                    arm.rename(directory / "wrong.dmg")
                elif scenario == "corrupt":
                    arm.write_bytes(b"corrupt")
                elif scenario == "no-checksum":
                    arm.with_suffix(".dmg.sha256").unlink()
                elif scenario == "extra":
                    (directory / "extra.dmg").write_bytes(b"unexpected")
                result = subprocess.run(["bash", "-eu", "-o", "pipefail", "-c", script], cwd=directory,
                                        env=dict(os.environ, VERSION=version, RUNNER_TEMP=temporary,
                                                 SOURCE_SHA="native-fixture", GITHUB_SHA="packaging-fixture"), capture_output=True, text=True)
                self.assertEqual(result.returncode == 0, scenario == "complete", result.stderr)
                self.assertEqual((directory / "checksums.txt").exists(), scenario == "complete")

    def test_requires_two_matching_intact_dmgs_before_any_publication(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text()
        publication = workflow.split("- name: Publish verified assets\n", 1)[1]
        script = textwrap.dedent(publication.split("        run: |\n", 1)[1]).split('TAG="v$VERSION"', 1)[0]
        for scenario in ("complete", "missing", "wrong-arch", "corrupt", "no-checksum", "extra"):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                for name in ("amd64.deb", "arm64.deb", "amd64.rpm", "arm64.rpm", "amd64.pkg.tar.zst"):
                    (directory / name).write_bytes(b"fixture package")
                for arch in ("arm64", "amd64"):
                    name = f"voice-type_6.4.0_darwin_{arch}.dmg"
                    artifact = directory / name
                    artifact.write_bytes(b"fixture DMG, not signed or notarized")
                    (directory / (name + ".sha256")).write_text(hashlib.sha256(artifact.read_bytes()).hexdigest() + "  " + name + "\n")
                arm = directory / "voice-type_6.4.0_darwin_arm64.dmg"
                if scenario == "missing":
                    arm.unlink()
                elif scenario == "wrong-arch":
                    arm.rename(directory / "voice-type_6.4.0_darwin_386.dmg")
                elif scenario == "corrupt":
                    arm.write_bytes(b"changed after verification")
                elif scenario == "no-checksum":
                    arm.with_suffix(".dmg.sha256").unlink()
                elif scenario == "extra":
                    (directory / "unexpected.dmg").write_bytes(b"extra")
                result = subprocess.run(["bash", "-eu", "-o", "pipefail", "-c", script], cwd=directory,
                                        env=dict(os.environ, VERSION="6.4.0"), capture_output=True, text=True)
                if scenario == "complete":
                    self.assertEqual(result.returncode, 0, result.stderr)
                    lines = (directory / "checksums.txt").read_text().splitlines()
                    self.assertEqual(len(lines), 7)
                    self.assertEqual(sum(line.endswith(".dmg") for line in lines), 2)
                else:
                    self.assertNotEqual(result.returncode, 0)
                    self.assertFalse((directory / "checksums.txt").exists())


if __name__ == "__main__":
    unittest.main()
