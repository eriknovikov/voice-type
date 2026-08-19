# Security

## Permissions

### Microphone Access

- **Purpose**: Capture speech for OpenRouter transcription.
- **When active**: The microphone is read only during an explicit recording
  session started by the user.
- **Data flow**: Captured PCM audio is encoded as WAV and sent to OpenRouter.
- **Local retention**: Audio from failed transcription attempts is retained
  privately under `$TMPDIR/voice-type/` so it is not silently lost.

### `/dev/uinput` Access

- **Purpose**: Emit virtual keyboard events for the finalized transcript.
- **How it works**: The Go `uinput` library creates one persistent virtual
  keyboard; voice-type sends events directly to `/dev/uinput`, not through
  dotool.
- **Scope**: Events go to whichever application has focus, including terminals,
  native Wayland applications, and XWayland applications.
- **Permission**: The process needs write access to `/dev/uinput`, normally via
  membership in the `input` group.

### Network Access

- **Purpose**: Upload WAV audio for speech-to-text transcription.
- **Connection**: OpenRouter, using the configured API key.
- **No telemetry**: Voice-type does not collect usage analytics or send unrelated
  data.

### Filesystem Access

- **Configuration**: The daemon reads `~/.config/voice-type.jsonc` or the path
  selected by `XDG_CONFIG_HOME` and never writes it. The installer creates or
  explicitly replaces the config.
- **Failed audio**: May write private WAV files under `$TMPDIR/voice-type/` after
  transcription failures.
- **Logs**: Status and error logs are written to stderr only.

## Security Model

- **Localhost only**: The HTTP server binds to `127.0.0.1` and is not exposed on
  the network.
- **No authentication**: The API is designed for a single-user desktop and must
  remain localhost-only.
- **User control**: Recording starts and stops only from the user's hotkey/API
  action, subject to the hard duration cap.
- **Focused target**: Direct typing can insert text, activate shortcuts, or act
  on commands in any application that gains focus during output.
- **Clipboard borrowing**: Output pastes rather than types, so voice-type reads
  the CLIPBOARD selection, replaces it with the transcript, sends Ctrl+Shift+V,
  and writes the previous contents back. While that is in flight the previous
  clipboard contents — which may be a password or other secret — are held in the
  daemon's memory, and the transcript is briefly readable by any application
  that can read the clipboard. PRIMARY is never touched. When the previous
  contents cannot be restored faithfully, the clipboard is left alone and the
  transcript is typed through `/dev/uinput` instead.
- **Unicode input**: Non-ASCII text uses Linux `Ctrl+Shift+U` composition and
  ends with Enter; behavior depends on the focused application's input-method
  support.

## What Voice Type Does Not Do

- Does not record audio when no session is active.
- Does not run Chrome or a browser speech-recognition pipeline.
- Does not invoke dotool. It does invoke `wl-copy` and `wl-paste` for the
  clipboard step described above.
- Does not add desktop notifications, cache browser data, or maintain a file
  logger.
- Does not collect telemetry or usage statistics.
