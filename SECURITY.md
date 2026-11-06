# Security

Report vulnerabilities privately through the repository's GitHub security
advisory flow. Do not include API keys, recorded speech or transcripts in issues.

Voice Type runs as your desktop user. It records from PulseAudio/PipeWire and
sends each completed recording over HTTPS to the selected OpenAI or OpenRouter
profile. There is no offline transcription or silent provider fallback.

Text insertion uses IBus, restores the previous input engine, and rejects
protected/password fields. It does not use clipboard pasting, simulated typing,
`/dev/uinput`, or a privileged helper. Avoid changing focused applications during
transcription when you do not want text delivered to the new focus.

There is no TCP control listener or management socket. Wails handles application
commands over the session bus; settings calls remain inside the app. The command
set is a fixed whitelist, never shell code. A private OS-held engine lock prevents
concurrent engines, including across separate session buses sharing a runtime
directory. GNOME integration changes only Voice Type's owned shortcut entries.

Profile API keys are stored in `~/.voice-type-config.jsonc` and private recovery
backups, with file mode 0600 and application directories 0700. Keys are masked in
routine UI responses and exposed only by explicit Reveal or Copy actions. Copying
places the key on your system clipboard. Environment keys are not imported.
Configuration writes are atomic and synced; revisions prevent stale UI writes.
These files are private to your OS user, not encrypted against that user or root.

Diagnostic logs are bounded and exclude transcripts, audio, API keys and raw
provider responses. Normal audio is held in memory. Failed transcription or
rejected vocabulary echoes can retain WAV files in a private `voice-type`
directory under the system temporary directory; remove these yourself when no
longer needed. Uninstall preserves user data and retained audio.

Release archives contain a native CGO application and require GTK4/WebKitGTK
runtime libraries. The installer verifies SHA-256 checksums from the selected
GitHub release, the archive layout, and the executable version before replacing
an installation. Checksums provide integrity relative to the release; they are
not an independent publisher signature. Upgrades use guarded shutdown and refuse
to overwrite an engine that is still running.
