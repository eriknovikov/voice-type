# Security

Report vulnerabilities privately through the repository's GitHub security
advisory flow. Do not include API keys, recorded speech or transcripts in issues.

Voice Type runs as your desktop user. It records from PulseAudio/PipeWire.
Cloud profiles send audio to the selected OpenAI or OpenRouter provider over an
encrypted connection; Local profiles process audio locally. There is no
silent fallback from local processing to a cloud provider.

Text insertion uses IBus, restores the previous input engine, and rejects
protected/password fields. It does not use clipboard pasting, simulated typing,
`/dev/uinput`, or a privileged helper. Avoid changing focused applications during
transcription when you do not want text delivered to the new focus.

There is no TCP control listener or management socket. Wails handles application
commands over the session bus; settings calls remain inside the app. The command
set is a fixed whitelist, never shell code. A private OS-held engine lock prevents
concurrent engines, including across separate session buses sharing a runtime
directory. GNOME integration changes only Voice Type's owned shortcut entries.

OpenAI and OpenRouter API keys are stored in the Linux Secret Service, one per
provider. The active config and current recovery backups contain profile settings
but not migrated API keys. Existing inline keys migrate after successful
credential-store writes. Preserved damaged files and other older copies can still
contain plaintext keys. Keys are masked in routine UI responses and exposed only
by explicit Copy actions. Environment keys are not imported. Configuration writes
are atomic and synced; revisions prevent stale UI writes. If no compatible
credential store is available, cloud profiles fail explicitly instead of falling
back to plaintext.

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
