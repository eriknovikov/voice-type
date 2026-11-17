package voicetype

func startupPresentation(platform string, command Command, ready bool) (hidden bool, initial Command) {
	initial = command
	if command == CommandToggle && !ready {
		initial = CommandShow
	}
	if platform == "darwin" && ready && command == CommandShow {
		return true, CommandBackground
	}
	return ready && (command == CommandBackground || command == CommandToggle), initial
}

func (b *Bridge) allowNativeQuit(platform string) bool {
	if platform != "darwin" || b.finalQuit.Load() || b.engine == nil {
		return true
	}
	go b.RequestQuit()
	return false
}

func (b *Bridge) quitApplication() {
	b.finalQuit.Store(true)
	if b.app != nil {
		b.app.Quit()
	}
}

func (d *engine) setSuspended(suspended bool) {
	d.mu.Lock()
	d.suspended = suspended
	state, session := d.state, d.sessionID
	if suspended && (state == stateChecking || state == stateRecording || state == stateTranscribing) {
		d.sessionInterrupted = true
		if d.sessionCancel != nil {
			d.sessionCancel()
		}
	}
	d.mu.Unlock()
	if suspended {
		go d.StopMicrophoneTest()
		if state == stateRecording {
			go func() { _ = d.stopSessionMatching(session, false) }()
		}
	}
}
