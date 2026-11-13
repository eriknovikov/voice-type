# AGENTS.md — Voice Type

## Structure and behavior

- One Linux Wails v3 app: `cmd/voice-type` is the app entry point; root Go files
  share flat `package voicetype`. React lives in `frontend/`.
- `frontend/src/api.ts` contains handwritten Wails `Call.ByName` calls and JSON
  types, not generated bindings. Keep them aligned with `bridge.go` and Go DTOs.
- Minimize hides; Quit/× exits. Preserve `RequestHide`/`RequestQuit` →
  `flush-settings` → `CompleteWindowAction`: it guards profile drafts and flushes
  settings, shortcuts and preferences before hiding/quitting.

## Build and verification

- Use Go from `go.mod` and CI's Node 24 (24.15+ for the lockfile). Linux system
  build dependencies are listed in `.github/workflows/test.yml`.
- `make dev`: Wails + Vite HMR using `build/config.yml`; backend restarts build
  `build/voice-type-dev`. Its watcher does not watch `VERSION`; restart after a bump.
- `make build`: production binary at `build/voice-type`. `app.go` embeds
  `frontend/dist`, so UI changes require rebuilding the binary outside dev mode.
- `make check`: frontend typecheck/build/tests, gofmt verification, Go vet/race
  tests, Python syntax and shell checks.
- Focused UI test: `npm --prefix frontend test -- src/App.test.tsx -t 'pattern'`.
  `npm --prefix frontend run build` runs TypeScript checking before Vite.
- Before Go tests on a fresh checkout, run `make frontend native-engine`.
  Go needs both `frontend/dist` and the transcribe.cpp static libraries.
  Outside Make recipes, supply the native pkg-config path explicitly:

  ```sh
  CGO_ENABLED=1 PKG_CONFIG_PATH="$PWD/build/transcribe/pkgconfig" go test -race -run '^TestName$' .
  ```

- Native sources are pinned by `scripts/build-transcribe.sh`; do not edit generated
  `build/transcribe/` files. `VOICE_TYPE_VULKAN=0` matches CI's CPU-only builds.
- `make native-check`: isolated native tray + smoke checks; needs a graphical
  display. CI uses Xvfb with the environment in `.github/workflows/test.yml`.
- Tests must isolate HOME/XDG, D-Bus and desktop settings. Reuse `useCredentialStore`
  with `memoryCredentials` for keyring-dependent tests (`credentials_test.go`).
  Real keyring/audio/provider/model tests are opt-in; enable only when requested.

## Installation and releases

- `make install` / `./install.sh` downloads a published release, **not** the local
  build. Installation is system-wide only and needs root/sudo.
- Keep `version` in `cmd/voice-type`; Make stamps `-X main.version` from `VERSION`.
  Stay on v6 unless a major bump is requested. Change `VERSION` only when requested:
  pushes to `main` touching it **or** `.github/workflows/release.yml` trigger releases.
- Do not commit, push or publish unless requested. Before commits/pushes, verify
  identity `eriknovikov <eriknkv24@gmail.com>` and personal GitHub authentication
  as `eriknovikov`, not the company account.

## Local-only material

- Optional agent documentation starts at `agents/general.md`; it may be absent in
  other checkouts. Never link to it from user-facing files.
- Keep `agents/` excluded via repository-local `core.excludesFile` (currently
  `.git/info/agents-exclude` containing `agents/`), not `.gitignore`, so agent file
  discovery and codebase-memory indexing can still see it.
