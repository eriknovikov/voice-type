# AGENTS.md — repository root

Two projects sit side by side here. Neither one's rules apply to the other, and
a task naming one should leave the other alone.

| Tree | What it is |
| --- | --- |
| `v5/` | the current project: a single static Go binary, `package main`, flat, no `internal/`. Build and test from inside `v5/` (`make check`). `CGO_ENABLED=0` everywhere. |
| `v4/` | deprecated TypeScript/headless-Chrome version, kept installable but no longer developed. Bun, Prettier, dotool. |

Only `README.md`, `LICENSE`, `SECURITY.md`, `assets/`, `.github/workflows/`, and
the two compatibility installer shims belong at the root.

Each tree installs the same `voice-type` command, `~/.config/voice-type.jsonc`,
and port 3232, so only one can be installed at a time — installing one replaces
the other. Their install/uninstall scripts are deliberately unaware of each
other; do not reintroduce cross-references between them.

`v5/VERSION` is the only place the v5 version number lives. A push to `main`
that changes it publishes a release, so treat it as a deploy trigger.
