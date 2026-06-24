const HELP_TEXT = `
VOICE TYPE - Real-Time Dictation Daemon

Usage:
  voice-type                 Run the daemon (foreground).
  voice-type help            Show this help.
  voice-type update          Self-update the binary (ST-7).
  voice-type shortcuts --apply   Apply configured keyboard shortcuts to your desktop.

Configuration:
  All settings live in ~/.config/voice-type.jsonc (JSON with // comments).
  On first run a default file is written there automatically.

  {
    "port": 3232,
    "lang": "en-US",
    "browser_type": "chrome",        // "chrome" | "chromium"
    "browser_path": "/usr/bin/google-chrome-stable",
    "stream": true,
    "timeout": 0,                    // seconds of silence, 0 = off
    "sound": false,
    "text": false,
    "punctuation": true,             // spoken punctuation + capitalization for en-*
    "shortcuts": {
      "daemon": "F10",
      "toggle": "F9",
      "languages": { "es-ES": "F8" } // optional, BCP47 tag -> key
    }
  }

Per-request language:
  /start and /toggle accept ?lang=<bcp47> (alias ?language=) to override the
  config default for that request. /stop and /exit ignore it.

Supported languages (most common):
  en-US  es-ES  ru-RU  zh-CN  fr-FR
Full list: https://github.com/eriknovikov/voice-type/blob/main/src/constants.ts

HTTP API (GET on http://localhost:<port>):
  /health  /toggle  /start  /stop  /exit  /togglePunctuation
`

export function showHelp() {
    console.log(HELP_TEXT)
}
