# What's voice-type?

A system-wide speech-to-text app. Press a key, speak, press again — the text
appears wherever your cursor is in the system.

# Why voice-type?

- Productivity: You speak faster than what you type.
- Less typing: Avoid wrist pain and strain.
- It's cool, accurate, almost free, open-source, and near 0 RAM usage.

# Requirements

- Linux, internet connectivity.
- An openai (recommended) or openrouter api key. This is required for transcription. Cost is very low (cents per month), so don't worry about pricing. I recommend you put on it a small credit limit, like 1 or 2 USD, per month.

# Installation

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v5/install.sh | bash
```

# Desktop app (from source)

The optional Wails desktop app runs dictation and provides settings, provider keys, vocabulary,
input-device testing, shortcuts and diagnostic logs. From `v5/`, run
`make install-desktop`, then open **Voice Type** from your application launcher.
See [desktop build requirements and setup](v5/desktop/README.md).

The daemon-only installation above remains available. Its CLI supports
`start`, `version`, `--help` and the optional `--model` override. Configuration
and vocabulary management are in the desktop app.

# Troubleshooting

Logs are stored in `~/.voice-type-logs.jsonl`

They don't include your actual transcriptions, but are helpful in case you experience any issue.

# Issues & Contributing

If you experience a problem with voice-type, please let me know! Open an issue, text me directly, or feel free to fix it yourself and drop a PR. All contributions are welcome, but please, no AI slop.
