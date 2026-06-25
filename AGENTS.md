# AGENTS.md

Guidance for AI agents working in this repository.

## NOTE FROM THE USER

If you see something like 'output in response', 'output response', 'response output', 'oir', 'o i r', or any similar combination in the prompt, it means I want the output to be directly in the response, and not creating any file.

---

## What Voice Type is

Voice Type is a **Linux-only** system-wide speech-to-text daemon. It keeps a headless Chrome/Chromium running, transcribes via the **Web Speech API** (cloud, no local models or API keys), and types into the focused window through **dotool**. A localhost HTTP server (`127.0.0.1:3232`) receives hotkey requests (`/toggle`, `/start`, `/stop`, `/exit`); optional D-Bus / `paplay` notifications provide feedback.

End-to-end: **hotkey → daemon → browser (WSA) → transcript transform → prefix-diff typing → focused app.**

For install, usage, config, and HTTP routes see [`README.md`](README.md). For a deeper technical walkthrough see [`INTERNALS.md`](INTERNALS.md). **Do not duplicate those in this file** — they change more often than agent rules.

---

## Design invariants (do not fight these)

1. **Persistent browser** — Chrome stays up for the daemon lifetime. Do not tear down the browser per dictation session.
2. **HTTP hotkeys** — Localhost Express, not D-Bus or custom IPC for control plane.
3. **Interim streaming by default** — In-progress transcripts are diffed live; `--no-stream` uses final results only.
4. **dotool for input** — Wayland-friendly virtual keyboard; layout forced to US via `DOTOOL_XKB_LAYOUT=us`.
5. **Web Speech API only** — Swapping STT means replacing `browser.js` + launch config, not a small patch.
6. **Security** — Server binds localhost only, no auth. Do not expose on `0.0.0.0` without explicit review.
7. **Puppeteer injection** — Code passed to `page.evaluate` is serialized as a single function; helpers must be **nested inside** that function, not at module scope in `browser.js`. Keep the WSA→event mapper in sync with `browserRecognition.js` (`tests/browserRecognition.sync.test.ts`).
8. **Config as sole control surface** — Runtime options live in `~/.config/voice-type.jsonc` only. No CLI flags except `help`/subcommand dispatch.
9. **dotool built from source** — The installer (`install.sh`) always compiles dotool from the `1.6` tarball; never installed via a package manager.
10. **Logging adapts to environment** — TTY → stdout only. Backgrounded (non-TTY) → `~/.local/share/voice-type/logs/voice-type.log` with 1 MB rotation (5 files kept).

`src/browser.js` must remain **JavaScript** (injected into Chrome). Imports use `.js` extensions (`verbatimModuleSyntax`).

---

## How to work in this repo

### Before implementing

1. **Ask when unclear** — If requirements, scope, or trade-offs are ambiguous, ask the user before coding. Do not guess on product behavior.
2. **Plan first for non-trivial work** — Bug fixes with an obvious one-liner, typos, and single-file tweaks can go straight to implementation. Anything touching multiple components, segment/streaming behavior, or user-visible semantics needs a short plan (goal, approach, risks, verification) and user alignment before large diffs.
3. **Minimize scope** — Smallest correct change. No drive-by refactors, unrelated README edits, or new abstractions unless requested.
4. **Match existing code** — Read surrounding code first; extend patterns already there. Reuse over reimplement.

### While implementing

5. **User docs vs agent docs** — Install/usage → `README.md`. Deep dives → `INTERNALS.md`. This file is **stable agent policy**, not a changelog of every module.
6. **Automated tests** — Run `bun test` (`tests/`; no Chrome or dotool required). Verify end-to-end behavior manually with `bun run dev` when needed.
7. **Linux-only** — No macOS/Windows paths. System deps: `dotool`, Chrome/Chromium, `paplay`, D-Bus session bus.
8. **Binary releases** — Production uses `install.sh` + tagged GitHub releases (`bun build --compile`). Keep `package.json` scripts in sync if build steps change.

### Commits and docs

9. **Do not update AGENTS.md for routine feature work** — Only revise this file when agent workflow or project-wide invariants change, not for every API or module tweak.
10. **Update README** when user-facing behavior, flags, or hotkeys change.

---

## Code style

Prettier defaults: **4-space indent**, **120** print width, **no semicolons**. Strict TypeScript. Classes: `export default class Name`. Use `import type` for type-only imports.

---

## Build (requires Bun)

| Command                                                       | Purpose           |
| ------------------------------------------------------------- | ----------------- |
| `bun run dev`                                                 | Watch mode        |
| `bun run start`                                               | Run daemon        |
| `bun run build`                                               | Bundle to `dist/` |
| `bun build src/index.ts --compile --outfile build/voice-type` | Standalone binary |

Source lives under `src/` — entry `index.ts`, orchestration `daemon.ts`, WSA `browser.js` + `browserRecognition.js`, `speechPipeline.ts`, `typingController.ts`, `dotoolSink.ts`, language-specific logic under `transcriptTransformers/`.
