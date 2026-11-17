# Bring Voice Type to macOS

## End goal

Deliver an installable, tested macOS version of Voice Type that records from a
microphone, transcribes through the existing cloud/local engines, and inserts
text into supported focused applications without regressing Linux.

Owner: the implementing agent, end to end. Branch: `feat/mac`.
The user reboots this same machine from Fedora into macOS for installer testing.
No Git, terminal or development tools are required on their Mac.

## Approved decisions

- **D01: Keep the application.** Keep Go, React, Wails v3 and the flat package.
  Add small platform-specific implementations, not a rewrite or plugin framework.
- **D02: Support macOS 13+, arm64 and amd64.** Build natively for each architecture
  and distribute separate DMGs. No universal binary or macOS 12 support initially.
- **D03: Record through malgo v0.11.28/Core Audio on Mac.** Reuse the existing
  16 kHz mono PCM16 buffering, streaming and meter. Keep Linux Pulse capture.
- **D04: Explicit permissions.** Use public AVFoundation/Accessibility APIs for
  onboarding and preflight. Request only microphone and Accessibility access.
  No hidden prompts, permission bypasses, Screen Recording or global key listener.
- **D05: Direct insertion by default.** Prefer writable AX selected text, then
  Unicode events when AX insertion is unsupported. Reject unsafe targets and
  never retry after an ambiguous submission. Universal app compatibility is not
  promised. Keep supported incremental local live insertion clipboard-free.
- **D06: Explicit paste compatibility.** Offer opt-in clipboard plus Command-V,
  serialized with guarded restoration. Warn about clipboard exposure. Reject live
  insertion with paste selected; process-while-recording and insert-at-stop work.
  Never silently switch output methods or downgrade a profile's mode.
- **D07: Menu-bar first.** Preserve lazy settings, current custom window and
  minimize/hide versus Quit/close behavior. Default Mac shortcut is
  Control-Option-Space. After setup and readiness, cold launches stay in the menu
  bar, including login startup. Reopening/menu actions show settings. Use Wails'
  SMAppService main-app startup; show effective OS login status without overriding
  user denial. No launch-reason heuristics or login helper initially.
- **D08: Native private storage.** Use Application Support and Library/Logs on Mac,
  existing Keychain library by default, and optional private file credentials.
  Preserve Linux paths. Isolate development identity, data, keys and locks.
- **D09: Keep local inference.** Keep the pinned transcribe.cpp and GGUF catalog.
  Statically link Metal with embedded shaders plus CPU fallback on Apple Silicon;
  use CPU/Accelerate on Intel. No CoreML conversion or replacement models.
- **D10: Free self-signed distribution.** Store source assets in `packaging/macos/`.
  Build native `.app` bundles and checksummed DMGs with our persistent self-signed
  certificate and hardened runtime. Back up its private key securely. No Apple
  membership, Developer ID or notarization. Label downloads unnotarized, explain
  per-app Security approval, never disable Gatekeeper or install root certificates
  on users' Macs. Test permission retention across updates rather than promise it.
  Start with public Mac prereleases separate from stable Latest, without a VERSION
  bump or main merge. This supersedes the original paid-signing requirement.
- **D11: Simple updates first.** Open the matching newer Mac release download;
  users replace the app. Never run the Linux installer on Mac. No Sparkle,
  Homebrew tap, Mac App Store or automatic replacement in this feature.
- **D12: Evidence before completion.** Keep Linux gates, add native Mac CI for both
  architectures, and test the installed signed app on the user's Mac. Record
  actual OS/architecture/application coverage rather than assuming it.

## Overall plan

1. Separate Linux assumptions and establish a native Mac build and dev bundle.
2. Implement private paths/locking, permissions and Core Audio recording.
3. Implement direct insertion and the explicit paste compatibility path.
4. Finish menu-bar lifecycle, shortcuts, onboarding and Mac diagnostics/updates.
5. Package/sign, add CI, verify installed behavior and prepare release handoff.

## Product guardrails

- Preserve per-recording settings snapshots, bounded buffers and ten-minute cap.
- Preserve `RequestHide`/`RequestQuit` -> `flush-settings` -> `CompleteWindowAction`,
  including native Mac quit paths and profile-draft protection.
- Local processing stays offline. Never silently change providers or output modes.
- Do not log transcripts, audio, API keys, clipboard data or surrounding target text.
- Keep protected-field refusal, at-most-once submission and existing local recovery.
- Keep implementation changes minimal and leave unrelated work alone.

## Completion and authority

Implementation is complete only when `todos.md` has evidence for every required
ticket, both native builds pass, Linux passes, and installed Mac acceptance passes.
Test gaps or unavailable external prerequisites must be explicit, not marked done.

Public-release readiness additionally requires pinned self-signatures and verified
final DMGs, with installed behavior and unnotarized limits stated honestly.
The user authorized end-to-end implementation and downloadable public previews on
2026-11-17, including feat/mac commits/pushes and hosted CI. No main merge, stable
release or VERSION bump is authorized. Other publishing needs a separate request.
Windows and wider Linux-distro support are follow-up work, not this feature.

## Durable handoff

Read [todos.md](todos.md) for executable tickets and [memory.md](memory.md) for
verified findings, evidence, constraints and the current resumption point.
