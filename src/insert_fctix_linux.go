package voicetype

// Reserve Fcitx for runtime selection. No Fcitx transport or insertion code is
// implemented. NewTextInserter returns ErrInsertBackendNotImplemented for it.
func init() {
	textInserterBackends[InsertBackendFcitx] = nil
}
