# Voice Type

System-wide speech-to-text for Linux. Press a key, speak, press again — the text
appears wherever your cursor is.

> **v4 is deprecated.** The Chrome + Web Speech API version has been replaced by
> v5, a single 6 MB static Go binary: no browser, no 400 MB Chromium, ~4 MB idle
> instead of ~440 MB. v5 needs an [OpenRouter](https://openrouter.ai) API key
> (~$0.0015/min). It still lives in [`v4/`](./v4) and installs with:
>
> ```bash
> curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v4/install.sh | sh
> ```

## Install

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
```

Fetches the static binary for your architecture, verifies its checksum, installs
it to `/usr/local/bin/voice-type`, and writes a config. Pin a release with
`--version v5.0.0`, install elsewhere with `--prefix ~/.local`.

**Requirements:** Linux, a microphone, PulseAudio or pipewire-pulse,
`wl-clipboard` (Wayland) or `xclip` (X11), membership of the `input` group, and
an OpenRouter API key. The installer handles all but the key.

The `input` group is a hard requirement — it is how the paste keystroke is sent —
and takes effect only after you log out and back in, or run `newgrp input`.

## Keyboard shortcuts

Bind these in your desktop's keyboard settings (GNOME Settings → Keyboard, KDE
System Settings → Shortcuts):

| Action            | Command                                                      |
| ----------------- | ------------------------------------------------------------ |
| Dictate           | `curl -s http://localhost:3232/toggle`                       |
| Start/stop daemon | `sh -c "curl -s http://localhost:3232/exit \|\| voice-type"` |

Press the daemon key once, then dictate as often as you like: toggle on, speak,
toggle off, and the transcript is pasted a second or two later.

## Config

`~/.config/voice-type.jsonc` — JSON with `//` comments. The installer writes it
only when absent; an existing file is **never** modified, by the installer or the
daemon. Restart the daemon after editing.

```jsonc
{
    "api_key": "sk-or-...", // or OPENROUTER_API_KEY, which wins
    "port": 3232,
    "model": "nvidia/parakeet-tdt-0.6b-v3", // any OpenRouter STT slug
    "max_duration": 600, // seconds; hard cap on one dictation
    "paste_key": "ctrl+v", // terminals usually need "ctrl+shift+v"
    "paste_delay_ms": 300, // wait before restoring the previous clipboard
    "trim_silence": true // cut leading/trailing silence before upload
}
```

Leftover v4 fields (`lang`, `browser_path`, `stream`, `punctuation`, …) are
ignored, so an old config still loads.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `/dev/uinput` permission denied | `sudo usermod -aG input $USER`, then `newgrp input` |
| Nothing pastes into a terminal | set `"paste_key": "ctrl+shift+v"` |
| Nothing happens on the hotkey | the daemon is not running — press the daemon shortcut |
| `no audio captured` | the press-to-press gap was under ~100 ms |
| Transcription failed | audio is kept in `$TMPDIR/voice-type/`, never discarded |

Logs go to stderr. Run `voice-type` in a terminal to watch them.

## Uninstall

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/uninstall.sh | sh
```

## Build from source

```bash
make build     # ./voice-type
make check     # fmt + vet + test
make dist      # static linux/amd64 + linux/arm64
make install   # build from this tree and install it
```

Go 1.24+, `CGO_ENABLED=0`, two pure-Go dependencies (PulseAudio protocol,
uinput). `go test ./...` is hermetic — no audio hardware, no network, no
`/dev/uinput`.

## More

- [AGENTS.md](./AGENTS.md) — architecture, HTTP API, and the design invariants
- [How I built Voice Type](https://dev.to/eriknovikov/how-i-built-voice-type-3i2p) (v4-era)
- [v4/](./v4) — the deprecated Chrome version
