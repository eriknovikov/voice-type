"""Fail-closed provenance checks for packaging-only retries, not native evidence."""

import copy
import importlib.util
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location("preview_source", ROOT / "scripts/macos-preview-source.py")
SOURCE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SOURCE)


class PreviewSourceTests(unittest.TestCase):
    def setUp(self):
        self.repository = "eriknovikov/voice-type"
        self.run = dict(repository={"full_name": self.repository}, head_repository={"full_name": self.repository},
                        head_branch="feat/mac", event="push", path=".github/workflows/mac-preview.yml",
                        status="completed", head_sha="a" * 40)
        self.jobs = [dict(name=f"mac / native (fixture, {arch}, 16.4)", conclusion="success")
                     for arch in ("arm64", "amd64")]

    def test_packaging_only_changes_can_reuse_both_tested_binaries(self):
        SOURCE.validate(self.run, self.jobs, [".github/workflows/macos.yml", "scripts/macos-release.py",
                                            "scripts/test_macos_preview_source.py", "memory.md"], self.repository)

    def test_any_app_or_native_build_change_requires_rebuild(self):
        for path in ("app.go", "audio_darwin.go", "local_native.cpp", "frontend/src/App.tsx", "go.sum",
                     "VERSION", "Makefile", "packaging/macos/Info.plist", "scripts/build-transcribe.sh", "LICENSE"):
            with self.subTest(path=path), self.assertRaisesRegex(ValueError, "source changed"):
                SOURCE.validate(self.run, self.jobs, [path], self.repository)

    def test_foreign_untrusted_incomplete_or_invalid_source_refused(self):
        for key, value in (("repository", {"full_name": "foreign/repo"}), ("head_repository", {"full_name": "fork/repo"}),
                           ("head_branch", "other"), ("event", "pull_request"), ("path", ".github/workflows/other.yml"),
                           ("status", "in_progress"), ("head_sha", "not-a-sha")):
            with self.subTest(key=key), self.assertRaises(ValueError):
                SOURCE.validate({**self.run, key: value}, self.jobs, [], self.repository)

    def test_both_successful_architecture_jobs_required(self):
        for jobs in (self.jobs[:1], [self.jobs[0], self.jobs[0]], self.jobs + self.jobs):
            with self.assertRaises(ValueError):
                SOURCE.validate(self.run, jobs, [], self.repository)
        for outcome in ("failure", "cancelled", "skipped"):
            jobs = copy.deepcopy(self.jobs)
            jobs[0]["conclusion"] = outcome
            with self.subTest(outcome=outcome), self.assertRaises(ValueError):
                SOURCE.validate(self.run, jobs, [], self.repository)


if __name__ == "__main__":
    unittest.main()
