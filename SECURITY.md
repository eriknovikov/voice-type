# Security

## Permissions

### Microphone Access

- **Purpose**: Capture speech for transcription by the selected provider.
- **When active**: The microphone is read only during an explicit recording
  session started by the user.
- **Data flow**: Captured PCM audio is encoded as WAV and sent to OpenAI or
  OpenRouter, as selected in the config. Active vocabulary accompanies the audio.
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
- **Connection**: HTTPS to OpenAI or OpenRouter, using the selected provider's
  API key. No automatic provider fallback sends audio to another service.
- **Credentials**: `OPENAI_API_KEY` applies only to OpenAI. `OPENROUTER_API_KEY`
  applies only to OpenRouter. A nonblank matching environment key replaces the
  config's `api_key`. Keys are never used to infer the provider.
- **No telemetry**: Voice-type does not collect usage analytics or send unrelated
  data.

### Filesystem Access

- **Configuration**: The daemon reads `~/.voice-type-config.jsonc` and never
  writes it. The installer creates or explicitly replaces the config with mode
  `600`. Interactive credential entry disables terminal echo.
- **Failed audio**: May write private WAV files under `$TMPDIR/voice-type/` after
  transcription failures.
- **Logs**: Status and error logs go to stderr and may include transcript
  previews. Protect redirected stderr as speech content. Without a terminal,
  request metrics also go to `~/.voice-type-logs.jsonl` with mode `600`.
  Metrics contain provider/model IDs, attempts, timing, and status, but no keys,
  audio, transcripts, vocabulary, or provider error bodies. They rotate at 5 MiB.

## Security Model

- **Localhost only**: The HTTP server binds to `127.0.0.1` and is not exposed on
  the network. Requests whose `Host` header is not `127.0.0.1`, `localhost`, or
  `::1` are rejected, so a web page cannot reach the daemon through DNS
  rebinding.
- **No authentication**: The API is designed for a single-user desktop and must
  remain localhost-only.
- **User control**: Recording starts and stops only from the user's hotkey/API
  action, subject to the hard duration cap.
- **Focused target**: Direct typing can insert text, activate shortcuts, or act
  on commands in any application that gains focus during output.
- **Clipboard borrowing**: Output pastes rather than types, so voice-type reads
  the CLIPBOARD selection, replaces it with the transcript, sends Ctrl+Shift+V,
  and writes the previous contents back. While that is in flight the previous
  clipboard contents (which may be a password or other secret) are held in the
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
- Does not add desktop notifications or cache browser data.
- Does not send telemetry or usage statistics to a remote collector. Request
  metrics remain local.
