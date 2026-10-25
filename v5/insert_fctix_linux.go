package voicetype

// Reserve Fcitx for runtime selection. No Fcitx transport or insertion code is
// implemented. NewTextInserter returns ErrInsertBackendNotImplemented for it.
//
// Future Fcitx5 support would entail:
//   - A separately installed native Fcitx5 add-on exposing a D-Bus method that
//     checks focus/protected fields and calls InputContext::commitString.
//   - A Go D-Bus client implementing TextInserter here, registered below instead
//     of nil. The Go binary stays static; no clipboard or simulated typing.
//   - A CLI option such as --insert-backend=ibus|fcitx (default ibus), passed from
//     cliOptions through daemon startup to preflight and output.go, replacing
//     their hardcoded IBus selection. Give cmd/try the same option for diagnostics.
//   - A clear error when the selected Fcitx add-on is missing; no silent fallback.
func init() {
	textInserterBackends[InsertBackendFcitx] = nil
}
