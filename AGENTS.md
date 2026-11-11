# AGENTS.md — Voice Type

Voice Type is one Linux Wails v3 application, written in Go with a React frontend.
The root Go module uses flat `package voicetype`; `cmd/voice-type` is the only
entry point.

`frontend/` contains the UI

- `make build`: frontend and native app in `build/voice-type`.
- `make check`: frontend tests/build, formatting, Go vet/race tests, shell checks.
- `make native-check`: isolated native tray and application smoke checks.
- `./install.sh --local`: build and install the application for the current user.
- `wails3 dev`: run dev environment with builtin HMR. 

Keep `version` in `cmd/voice-type`: releases stamp `-X main.version` from the root
`VERSION`. Changing `VERSION` on `main` triggers a release. 

Do not push or publish unless the user requests it.

