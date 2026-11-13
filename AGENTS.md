# AGENTS.md — Voice Type

Voice Type is one Linux Wails v3 application, written in Go with a React frontend.
The root Go module uses flat `package voicetype`; `cmd/voice-type` is the only
entry point.

`frontend/` contains the UI

- `make build`: frontend and native app in `build/voice-type`.
- `make check`: frontend tests/build, formatting, Go vet/race tests, shell checks.
- `make native-check`: isolated native tray and application smoke checks.
- `./install.sh`: install the latest release system-wide (needs root or sudo).
- `wails3 dev`: run dev environment with builtin HMR. 

Keep `version` in `cmd/voice-type`: releases stamp `-X main.version` from the root
`VERSION`. Changing `VERSION` on `main` triggers a release. 

Do not push or publish unless the user requests it.

Internal, local-only documentation starts at `agents/general.md`. It is
kept out of git through the repository-local exclude file configured as
`core.excludesFile` (for example `.git/info/agents-exclude`), not `.gitignore`,
so codebase-memory-mcp and OpenCode file discovery still index and show it; it
may not exist in other checkouts, so never link to it from user-facing files.
Set it up with `printf 'agents/\n' > .git/info/agents-exclude && git config core.excludesFile "$PWD/.git/info/agents-exclude"`.

