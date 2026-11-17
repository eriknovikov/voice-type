#!/usr/bin/env python3
"""Reuse native preview artifacts only when both tested app sources are unchanged."""

import json
import os
import re
import subprocess
import sys


PACKAGING_ONLY = {
    "scripts/macos-release.py", "scripts/macos-preview-source.py", "README.md",
    "memory.md", "todos.md", "end_goal.md",
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def validate(run, jobs, changed, repository):
    require(run["repository"]["full_name"] == repository and run["head_repository"]["full_name"] == repository,
            "native artifacts must belong to this repository")
    require(run["head_branch"] == "feat/mac" and run["event"] == "push" and
            run["path"] == ".github/workflows/mac-preview.yml", "native artifacts must come from a trusted preview push")
    require(run["status"] == "completed", "native source run must be complete")
    require(re.fullmatch(r"[0-9a-f]{40}", run["head_sha"]), "invalid source revision")
    native = [job for job in jobs if job["name"].startswith("mac / native (")]
    require(len(native) == 2 and all(job["conclusion"] == "success" for job in native) and
            any(", arm64," in job["name"] for job in native) and
            any(", amd64," in job["name"] for job in native), "both native architecture gates must have passed")
    for path in changed:
        allowed = path in PACKAGING_ONLY or path.startswith(".github/workflows/") or (
            path.startswith("scripts/test_") and path.endswith(".py"))
        require(allowed, f"app/build source changed; cannot reuse tested binaries: {path}")


def command(*args):
    return subprocess.check_output(args, text=True).strip()


def main():
    run_id = sys.argv[1]
    require(re.fullmatch(r"[0-9]+", run_id), "invalid native run ID")
    repository = os.environ["GITHUB_REPOSITORY"]
    endpoint = f"repos/{repository}/actions/runs/{run_id}"
    run = json.loads(command("gh", "api", endpoint))
    result = json.loads(command("gh", "api", endpoint + "/jobs?per_page=100"))
    require(result["total_count"] <= 100, "too many source jobs to validate")
    source = run["head_sha"]
    changed = command("git", "diff", "--name-only", source, "HEAD", "--").splitlines()
    validate(run, result["jobs"], changed, repository)
    version = command("git", "show", source + ":VERSION")
    require(re.fullmatch(r"\d+\.\d+\.\d+", version), "invalid native base version")
    print(f"version={version}-mac.preview.{int(run['run_number'])}")
    print(f"source-sha={source}")
    print(f"native-run-id={run_id}")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, KeyError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"Cannot reuse native artifacts: {error}") from error
