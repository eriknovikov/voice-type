# AGENTS.md — repository root

Two projects sit side by side here. Neither one's rules apply to the other, and
a task naming one should leave the other alone.

| Tree | What it is |
| --- | --- |
| `v5/` | the current project: a single static Go binary. Library code is `package voicetype`, flat at `v5/` with no `internal/`; entry points are `package main` stubs under `v5/cmd/`. Build and test from inside `v5/` (`make check`). `CGO_ENABLED=0` everywhere. |
| `v4/` | deprecated TypeScript/headless-Chrome version, kept installable but no longer developed. Bun, Prettier, dotool. |

Only `README.md`, `LICENSE`, `SECURITY.md`, `assets/`, `.github/workflows/`, and
the two compatibility installer shims belong at the root.

Each tree installs the same `voice-type` command, `~/.config/voice-type.jsonc`,
and port 3232, so only one can be installed at a time — installing one replaces
the other. Their install/uninstall scripts are deliberately unaware of each
other; do not reintroduce cross-references between them.

Binaries live under `v5/cmd/`: `cmd/voice-type` is the shipped daemon, and
anything else there is a diagnostic that exercises one stage of the pipeline
without a provider round trip. Keep `version` in `cmd/voice-type` -- the linker
stamps `-X main.version`, and release CI verifies that exact string in the built
binary.

`v5/VERSION` is the only place the v5 version number lives. A push to `main`
that changes it publishes a release, so treat it as a deploy trigger.
