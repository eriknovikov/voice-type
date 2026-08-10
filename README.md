# Voice Type

System-wide speech-to-text for Linux. Press a key, speak, press again — the text
appears wherever your cursor is.

> **v4 is deprecated.** The Chrome + Web Speech API version has been replaced by
> v5, a single 6 MB static Go binary: no browser, no 400 MB Chromium, ~4 MB idle
> instead of ~440 MB. v5 needs an [OpenRouter](https://openrouter.ai) API key
> (~$0.0015/min). The deprecated v4 source remains in [`v4/`](./v4) and can be
> installed instead of v5 with the command below. The two versions share the
> binary name, config path, and port, so back up your config before switching.
>
> ```bash
> curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v4/install.sh | sh
> ```

## Install

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
```

Fetches the static binary for your architecture, verifies its checksum and
embedded version, installs it to `/usr/local/bin/voice-type`, and writes a
private config. Pin a release with:

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/v5.0.0/install.sh | sh -s -- --version v5.0.0
```

Build the current working tree with `./install.sh --local` from the repository
root.

**Requirements:** Linux, a microphone, PulseAudio or pipewire-pulse, membership
of the `input` group, and an OpenRouter API key. The installer handles the
binary and config; it does not install packages or change group membership.

The `input` group is a hard requirement because it permits direct virtual-keyboard
events into the focused application. Membership takes effect only after you log
out and back in, or run `newgrp input`.

## Keyboard shortcuts

Bind these in your desktop's keyboard settings (GNOME Settings → Keyboard, KDE
System Settings → Shortcuts):

| Action            | Command                                                      |
| ----------------- | ------------------------------------------------------------ |
| Dictate           | `curl -s http://localhost:3232/toggle`                       |
| Start/stop daemon | `sh -c "curl -s http://localhost:3232/exit \|\| voice-type"` |

Press the daemon key once, then dictate as often as you like: toggle on, speak,
toggle off, and the transcript is typed a second or two later. Longer transcripts
take additional time because every character is emitted as keyboard input.

## Config

`~/.config/voice-type.jsonc` — JSON with `//` comments. The installer writes it
on a fresh install. If it already exists, the installer offers to skip creation
and keeps it by default; declining replaces it without a backup. The daemon
never modifies it. Restart the daemon after editing.

```jsonc
{
    "api_key": "sk-or-...", // or OPENROUTER_API_KEY, which wins
    "port": 3232 // int 1024-65535
}
```

That is the entire config. The model, duration cap, keyboard timing, US layout,
and silence handling are tuned defaults compiled into the binary, not knobs —
every option is one more thing to get wrong, and none of these earned that.

Any other field is ignored, so both v4 configs (`lang`, `browser_path`,
`stream`, `punctuation`, …) and configs from earlier v5 installs still load.
Historical v5 fields such as `model`, `max_duration`, `paste_key`,
`paste_delay_ms`, and `trim_silence` are ignored rather than rejected.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `/dev/uinput` permission denied | `sudo usermod -aG input $USER`, then `newgrp input` |
| Nothing happens on the hotkey | the daemon is not running — press the daemon shortcut |
| `no speech detected` | the capture was under ~0.35 s, or held nothing but room tone — nothing was uploaded |
| `no audio captured` | the press-to-press gap was under ~100 ms |
| Transcription failed | audio is kept in `$TMPDIR/voice-type/`, never discarded |
| Punctuation is wrong | voice-type targets a US active keyboard layout; switch the desktop layout to US |
| Unicode is missing or literal | the focused app must support Linux `Ctrl+Shift+U` Unicode composition |

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
