# AGENTS.md — Voice Type

Voice Type is one Linux Wails v3 application, written in Go with a React frontend.
The root Go module uses flat `package voicetype`; `cmd/voice-type` is the only
entry point. The private engine runs inside Wails. Do not add a separate daemon,
management socket, HTTP control listener, or standalone configuration CLI.

`frontend/` contains the UI; build it before compiling Go because its distribution
is embedded. Native builds require CGO, GTK4 and WebKitGTK 6.0.

- `make build`: frontend and native app in `build/voice-type`.
- `make check`: frontend tests/build, formatting, Go vet/race tests, shell checks.
- `make native-check`: isolated native tray and application smoke checks.
- `./install.sh --local`: build and install the application for the current user.

Keep `version` in `cmd/voice-type`: releases stamp `-X main.version` from the root
`VERSION`. Changing `VERSION` on `main` triggers a release. Do not push or publish
unless the user requests it.

Preserve config/profile recovery, private file permissions, credential snapshots
for in-flight sessions, cancellation and idempotent shutdown. Unknown config
fields must survive saves; legacy `port` fields are ignored.

GNOME shortcut writes must be limited to the exact owned Voice Type paths. Shared
shortcut lists preserve all foreign entries and ordering. Native tests must set
private HOME/XDG paths, keyfile GSettings and the child bus environment **before**
starting D-Bus; never exercise test shortcuts against the user's real settings.
