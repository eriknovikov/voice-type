# macOS feature memory

## Resume here

- Goal and decisions: [end_goal.md](end_goal.md).
- Work queue and definitions of done: [todos.md](todos.md).
- Branch: `feat/mac`, created from clean `dev` at
  `37a00bad072c807dd3fd56f276f092127554c986`.
- State: public self-signed/unnotarized Mac preview.6 is downloadable and verified.
  Both native check/build/verify suites and both signing jobs PASS. VERSION is 6.4.0.
- Current policy: user rejected Apple fees and chose persistent self-signing.
  D10 now supersedes all historical Developer ID/notarization requirements below.
  The user approved end-to-end implementation, feat/mac commit/push, hosted CI and
  public preview downloads. No main merge, stable publication or VERSION bump.
- Ready for user: reboot this same machine into macOS, download the matching DMG,
  drag to Applications, use per-app Open Anyway if blocked, grant Microphone and
  Accessibility, then test one dictation in TextEdit. No Git/build tools/SSH needed.
- Public release: https://github.com/eriknovikov/voice-type/releases/tag/v6.4.0-mac.preview.6.
  Assets: voice-type_6.4.0-mac.preview.6_darwin_arm64.dmg (Apple Silicon),
  voice-type_6.4.0-mac.preview.6_darwin_amd64.dmg (Intel), checksums.txt.
  Both anonymous HTTP downloads returned 200; actual downloaded bytes match the
  public checksums and native signed artifacts. Not a device-installation claim.
- App source: 002d590; packaging source: 07c5157; native/source run 44158542323;
  final signing run 44161860130. That run's publisher remains red: its integration
  token got HTTP403 despite contents:write, also after precreating the source tag.
  Exact asset-gated publisher was therefore executed locally with explicit verified
  eriknovikov auth, successfully publishing the SAME checked artifacts. Do not
  misreport the hosted publisher as passing. No broad personal token was stored in CI.
- MACOS_PREVIEW_NATIVE_RUN temporary repo variable is removed. Next code changes
  rebuild normally; packaging-only retries can reuse tested binaries with strict
  provenance. Resolve hosted publication permissions separately if needed; do not
  delay this already-published device test for pipeline cleanup.
  Run 44158542323 passed native gates and now signs/strict-verifies both apps.
  Certificate extraction failed because optional codesign prefix needs `=path`.
  Reuse its existing binaries (version 6.4.0-mac.preview.6, source 002d590) via
  temporary repo variable MACOS_PREVIEW_NATIVE_RUN=44158542323. Provenance validator
  checks both trusted native jobs passed and app/build sources are unchanged; signing
  checks each binary's clean source stamp. Do NOT rebuild the app for pipeline-only
  corrections. Clear the variable after publishing/verification. No DMG yet.
  Run 44141118702 passed BOTH
  native jobs, including minimum-OS/runtime-dependency/source-stamp checks, but
  signing stalled in non-sudo security add-trusted-cert; cancelled at user's status
  check. Use sudo -n with administrative trust on the disposable CI runner only,
  plus 2/3/5-minute step timeouts. No user Mac root certificate install is required.
  Need signature/DMG/download evidence next; no installed/hardware/notarization claim.
  Intel native Cgo compilation/vet now pass. Apple Silicon macos-15 could not
  acquire a runner twice, so use free macos-26 with Xcode 26.6 for arm64 instead;
  Intel keeps macos-15-intel/Xcode 16.4. Both still enforce a 13.0 deployment target.
  Then the user reboots this same machine into macOS for installer acceptance.
  No local Mac build tools or remote access are requested.
- MAC-090/100 help, diagnostics and matching-DMG updates are now implemented and
  locally tested. MAC-110/120 packaging and native CI/release gates are also written.
  Do not redo these implementations or call Linux fixtures native verification.
  release.yml is modified and still triggers releases on main; stay on feat/mac.
  The Mac implementation is NOT native-verified or a demonstrated end-to-end app.
  Maintain ticket evidence and this resume section before stopping/compaction.
- Signing: encrypted persistent PKCS#12 and its password are owner-only files in
  ~/.local/share/voice-type/signing/ outside the repo, also installed as secrets in
  GitHub's macos-release environment restricted to main/feat/mac. Public certificate
  is packaging/macos/signing-cert.pem. Private material must never enter git/logs.
  User should copy the encrypted identity and password into separate secure backups.
- User approved explicit paste compatibility and its live-mode limitation. Mac
  version/chip and installed behavior remain unknown; both chips get CI/downloads.

## Operating rules

- Read global and repository AGENTS.md. Use the graph first for structural discovery;
  index project is `home-nkv-dev-f-voice-type`. Check freshness after branch edits.
- Keep the flat Go/React/Wails structure. Do not spawn subagents unless authorized
  by the user or applicable instructions. Prefer small changes and focused tests.
- Do not edit generated `build/transcribe/`. Keep tracked assets outside disposable
  `build/`. Never put secrets, personal audio or transcripts in these documents.
- No commit/push/publish or VERSION change without explicit user authorization.
  A release.yml change pushed to main can itself trigger a release.
- For authorized git/network publishing, verify personal identity
  `eriknovikov <eriknkv24@gmail.com>` and GitHub `eriknovikov`, never company `eriknkv`.
- Isolate tests from real HOME/XDG/desktop/Keychain. Real audio/keyring/provider/model
  tests remain opt-in. Do not assume spare-Mac availability grants remote access,
  Apple credentials, paid API use or permission to change personal credentials.

## Verified baseline, 2026-11-17

- VERSION is `6.4.0`; go.mod requires Go `1.26.0`; Wails/runtime are `3.0.0`.
  Current investigation host is Linux arm64 with Go `1.26.10`.
- Full `make check` passed on the baseline: frontend typecheck/build, 8 test files,
  93 tests, Go vet/race tests, Python syntax and shell syntax/shellcheck. Native
  engine archive checksum passed and CPU-only static transcribe.cpp built.
- An isolated malgo v0.11.28 null-backend probe passed `go test -mod=mod -race -v`:
  320 owned PCM16 samples per 20 ms at 16000 Hz, one channel; device-ID round trip.
  This verifies the Go capture contract, NOT real Core Audio/hardware behavior.
- A representative Apple manifest fed into the current pkg-config generator failed
  as expected: `unexpected platform frameworks in the Linux link manifest`.
- `GOOS=darwin CGO_ENABLED=1 go list -e` confirmed Linux engine-lock sources are
  excluded while desktop_pipeline_test.go is included. This was file-selection
  evidence, NOT a successful Mac compilation.
- Graph coverage checks on 35 relevant files plus cmd/frontend/scripts/workflow
  scopes reported no recorded gaps; critical source was also read directly.
- No Mac app, real-model/provider run, signed bundle or notarization was performed.

Disposable probe sources are in `/tmp/opencode/voice-type-macos-probe/` on this
host, but continuation must not depend on their survival. Baseline make-check
output was recorded by the harness; rerun gates after implementation changes.

## Baseline source map and porting facts (before implementation)

- `app.go`: Wails startup/single instance, lazy window, tray/menu/state ticker.
  `trayAvailable` directly queries D-Bus. Mac needs its own availability behavior.
- `bridge.go`: Go DTOs, shortcut registration, window lifecycle and flush handshake.
  `frontend/src/api.ts` is handwritten; change it with every DTO/API change.
- `audio.go`: Recorder currently embeds Pulse client/stream and shared buffering.
  Keep `acceptSamples`, chunk sink, session buffer, meter and stop ownership rules.
  Core Audio malgo IDs contain stable device UIDs; default selection is nil.
  Malgo callback slices are borrowed and must not escape without ownership/copy.
- `engine_settings.go`: startup uses `&Recorder{}` for lazy capture. Preserve that
  behavior. A successful enumeration is required before clearing a saved device.
- `insert.go`: existing TextInserter/constructor registry. Add Darwin registration
  and default selection; do not replace the interface with a broad abstraction.
- Hard-coded IBus factories: `output.go`, `startLocalStream`, and the nil-factory
  fallback in `newLocalStreamSessionWithContext` in `local_transcriber.go`.
  Several shared tests currently replace the IBus map entry and must use the
  platform default. `engine.go` and `runlog.go` contain IBus-specific wording.
- Live stream `commitSnapshot` creates one inserter lazily and appends committed
  word-boundary deltas. Preserve no-rewrite/no-duplicate behavior and bounded queue.
- `engine_lock_linux.go`: private openat/O_NOFOLLOW/flock ownership/mode/link-count
  protections. MAC-010 shares these for Darwin compilation; MAC-030 wires the
  native private state directory. Retain the stable lock inode.
- `preferences.go`: Ctrl+Space defaults/migration, GTK shortcut validation,
  autostart registration and rollback. Limit Linux migration to Linux. Existing
  `Super` maps to Command in Wails; UI can use Mac labels without needless format
  churn. Cold/background readiness must include Mac permissions.
- `development.go`: dev mode currently depends only on VOICE_TYPE_DEV. A dev bundle
  launched without shell environment needs a build-stamped development identity.
- Data paths currently spread across `config.go`, `workspace.go`, `preferences.go`,
  `private_document.go`, `local_models.go`, `credentials_file.go`,
  `credential_storage.go`, `runlog.go` and `diagnostics.go`. Keep Linux behavior.
  Suggested Mac layout: Application Support/Voice Type/{config.jsonc,state,models,
  backups,credentials}; Library/Logs/Voice Type/events.jsonl. Dev uses a separate
  Voice Type Dev root. Existing private temporary recovery can remain unchanged.
- `credentials.go`: reuse go-keyring v0.2.8. Darwin uses /usr/bin/security and sends
  new secret values through stdin. Preserve read-back/write/delete verification.
- `doctor_other.go` is a non-Linux placeholder. Reusable checks currently live
  inside doctor_linux.go; extract only needed shared checks for doctor_darwin.go.
- `update.go` embeds install.sh, re-execs a copy and targets /usr/bin/voice-type.
  Split Linux apply behavior. Mac opens matching release downloads, never installer.
- `desktop_pipeline_test.go` references helpers from `_linux_test.go`. Split/constrain
  its Linux integration portion without losing shared/provider tests. Tray and
  installer fixtures and parts of bridge/autostart tests also assume Linux.

## Native engine and packaging facts

- Pin: transcribe.cpp `2778ae4bf5ca5f394d129a39467f4ad9552dc34f`, version 0.4.1,
  archive SHA256 `1ae437641c38126998bb133d0b9f6a315c0409293a62511372f6e4eb27a5e626`.
- `local_native.cpp` already requests AUTO then retries CPU when model load fails.
  glibc malloc_trim is guarded. No macOS-specific rewrite is needed there.
- Native CMake supports TRANSCRIBE_METAL, static libraries, embedded Metal source,
  and Apple Accelerate. The installed link manifest emits `c++`, `m` and frameworks
  including Accelerate/Foundation/Metal/MetalKit. Consume it rather than hardcode
  Linux link dependencies. Intel GPU AUTO selection is guarded upstream because
  pre-Apple7 Metal GPUs can produce incorrect results; ship Intel CPU-only.
- Keep native OpenMP off, non-Apple system BLAS off and GGML_NATIVE off. Set Metal
  explicitly by target architecture; use embedded source to avoid shader sidecars.
- `scripts/build-transcribe.sh` currently requires sha256sum and Linux-oriented
  flags. `scripts/generate-transcribe-pkgconfig.go` explicitly rejects frameworks.
- `Makefile` and `build/config.yml` explicitly check GTK/WebKitGTK. Make those
  checks Linux-only. `app.go` embeds frontend/dist, which must exist before Go build.
- `build/*` is ignored except config.yml and `make clean` removes generated build
  entries. Put tracked Mac bundle/signing source assets under packaging/macos/.
- Current release has 2 deb, 2 rpm, 1 amd64 Arch package plus checksums. Add 2 DMGs
  to the publish allowlist/counts; preserve Linux verification and release stamping.
- App bundle ID: `io.github.eriknovikov.voice-type`; dev adds `.dev`. Credential
  service is currently `com.eriknovikov.voice-type`; keep production compatibility
  and explicitly separate the dev service. Icon source icon.png is 512x512 RGBA.
- Build on macos-15 arm64 and macos-15-intel amd64 with Xcode_16.4.app selected,
  Go from go.mod, Node 24, cmake/pkgconf/shellcheck. Set deployment target 13.0 in
  native compilation, Go external linking and Info.plist; verify resulting Mach-O.

## Wails/macOS caveats already checked in pinned source

- Wails sources are in Go module cache at
  `github.com/wailsapp/wails/v3@v3.0.0/pkg/application`.
- Global shortcuts use Carbon RegisterEventHotKey, not an event tap; no Input
  Monitoring permission is needed. Letters are layout-aware; Super maps to Cmd.
- Bundled macOS 13+ autostart uses SMAppService.mainAppService and ignores extra
  arguments. Do NOT assume EnableWithOptions(--background) hides the startup UI.
  Approved solution is menu-bar-first ready cold launches, not launch-reason guesses.
- `App.Quit` reaches NSApp terminate and ShouldQuit. Native Quit must use an
  asynchronous flush plus a final-quit guard; startup-failure shutdown must still
  work. Do not block the AppKit main thread waiting for frontend autosave.
- Wails' default reopen handler only shows existing windows. Add ensureWindow for
  a background instance that has never created a settings window.
- Mac tray's native menu path avoids activation when no custom left-click handler
  is attached. The current `tray.OnClick(show)` should not carry over unchanged.
- Wails WebKit media permission options do not replace native capture's TCC checks.
- Production needs a microphone purpose string, hardened-runtime audio-input
  entitlement and stable signing identity. Do not add private_mac_apis, app sandbox,
  JIT/unsigned-memory/library-validation exceptions without a demonstrated need.
- Direct Unicode events can be ignored by some app frameworks. AX selected-text
  support also varies. Never label event posting as verified visible delivery.
- Pasteboard read requests are not proof of insertion by the intended target;
  clipboard managers may read too. Mark only submission, guard ownership, cap
  waits, and do not retry after ambiguous delivery. Concealment markers are advisory.
  An existing reviewed design uses 200 ms read quiet time, 8 s overall timeout
  and 500 ms failed-injection timeout. These are reference starting points for
  tested constants, not claims of guaranteed delivery or mandatory user settings.

## External gates, not hidden implementation work

- Mac access: arrange a user-run install/checklist or explicit remote access when
  native/hardware work needs it. Do not assume SSH credentials or network hosts.
  MAC-125 permits early locally built dev-bundle testing without Apple release
  credentials; MAC-130 still requires final signed/notarized installation evidence.
- Apple signing: user must supply Developer ID certificate/private key, Team ID
  and notarization credentials through a secure mechanism. Never store them here.
- CI: local workflow edits are permitted, but push/dispatch and hosted execution
  require appropriate access and explicit authorization. Do not substitute Linux
  cross-compilation or mocks for native CI evidence.
- Hardware/privacy prompts require the person at the Mac. Stop at those prompts;
  do not modify TCC databases or bypass Gatekeeper. Use per-app permission resets
  only with consent, never broad resets for all applications.
- No hardware/full minimum-OS coverage is implied by the spare laptop. Record what
  was tested, what CI covers, and remaining coverage before declaring readiness.

## References

- Wails Mac build: https://v3.wails.io/guides/build/macos/
- Wails shortcuts: https://v3.wails.io/features/keyboard/global-shortcuts/
- Wails autostart: https://v3.wails.io/features/autostart/basics/
- Apple microphone authorization:
  https://developer.apple.com/documentation/avfoundation/capture_setup/requesting_authorization_for_media_capture_on_macos
- Apple Unicode event limitations:
  https://developer.apple.com/documentation/coregraphics/cgevent/keyboardsetunicodestring(stringlength:unicodestring:)
- Apple notarization:
  https://developer.apple.com/documentation/security/notarizing-macos-software-before-distribution
- Go minimum OS: https://go.dev/doc/go1.26#darwin
- Runner availability: https://github.com/actions/runner-images
- Malgo pin: https://github.com/gen2brain/malgo/tree/v0.11.28
- Paste transaction design reviewed:
  https://github.com/cjpais/Handy/tree/aca8985190c3ad7f361d8a80cb30f179513ed690/src-tauri/src/paste_tx

Use current Context7 documentation for API-specific work and the pinned dependency
source as the implementation authority. These references do not authorize repeating
the whole investigation; proceed with implementation and targeted failing-test fixes.

## Suggested continuation goal

Implement Voice Type for macOS end to end on feat/mac using end_goal.md as the
approved scope, todos.md as the testable backlog, and memory.md as the durable
handoff. Start at the next unblocked ticket and maintain these files as you work.
Complete every implementation and acceptance gate, preserving Linux behavior.
Continue independent work around blockers; stop only for a prerequisite that
actually requires me, with the smallest exact action I must take. Never claim
missing native/device/signing evidence, and do not commit, push, bump VERSION or
publish unless I explicitly authorize it.

## Progress log

- Publication complete: v6.4.0-mac.preview.6 is public, isDraft=false,
  isPrerelease=true. Both final DMGs anonymous HTTP200 and SHA256 PASS:
  arm64 9,509,924 bytes / 7e7c460707c19cc65f06b166caa1e6ae6384059a06f82560a49a49e5705126f3;
  amd64 8,040,550 bytes / 9c4cabe741907caac0f2c65c5a637bf0a4e31c5fa34a8841129a6c5154023b13.
  Stable Latest is still v6.4.3 (release 446007629). VERSION unchanged. No main merge.
  Integration-token publisher failed twice; same publisher with personal auth PASS.
  Temporary reuse variable removed. Remaining acceptance requires the user's Mac.
- Run 44161860130: BOTH signing jobs PASS. Final DMG SHA-256:
  arm64 7e7c460707c19cc65f06b166caa1e6ae6384059a06f82560a49a49e5705126f3;
  amd64 9c4cabe741907caac0f2c65c5a637bf0a4e31c5fa34a8841129a6c5154023b13.
  Publisher verifies both checksums, then release creation gets HTTP403 despite
  contents:write (older source tag did not exist). Authorized personal account
  created that exact source tag; retry only publisher, reusing finished signed DMGs.
- Run 44158542323: keychain search/default fix PASS; signing and initial strict
  verification work on both architectures. Failure is `--extract-certificates`
  optional argument parsing: use --extract-certificates=prefix, not separate argv.
  Added cross-run artifact reuse for packaging-only retries. 23 local Python tests
  and actionlint PASS; live provenance probe selects correct preview.6/source/run.
  Release notes/target now distinguish app source from packaging workflow revision.
- Run 44154713968: both native jobs PASS again. Noninteractive administrative
  trust fix PASS on both runners; 1 valid signing identity matches pinned cert.
  codesign fails `no identity found`/`item could not be found in keychain`.
  The imported keychain was not in user search/default lists; add both. No DMG or
  public preview has been produced yet. Keep all bounded signing timeouts.
- Run 44141118702: BOTH native jobs PASS, including native check/build/mac-verify,
  version/arch/13.0/system-dependency checks, clean source stamp and bundle archives.
  Signing import succeeded (1 identity imported), then trust setup hung for ~58m.
  Cancelled run, inspected its logs and matched the documented headless trust
  prompt problem (actions/runner-images#12116). Split trust from private-key import,
  use noninteractive sudo only on ephemeral CI and bound all signing steps.
  No release/DMG yet. actionlint PASS for the correction.
- Current run: https://github.com/eriknovikov/voice-type/actions/runs/44141118702,
  commit a6cb492. Watcher sh_1eaa0bf17001uwyfBRGvSDpRrj notifies on completion.
- Run 44138018299: BOTH architectures passed native compilation/vet, full Go race
  suite, malgo null-backend race tests, 103 UI tests and production bundle assembly.
  Python: 17 tests OK, 3 correctly skipped Linux-only checks. Native mac-verify
  rejected the deployment target. Realistic otool fixture with LC_BUILD_VERSION's
  linker-tool `version 1115.7.3` reproduces failure: parser wrongly treats it as an
  OS minimum. Parse only minos for LC_BUILD_VERSION and version for the legacy
  LC_VERSION_MIN_MACOSX command; keep strict macOS 13 validation. No signing yet.
- Current run: https://github.com/eriknovikov/voice-type/actions/runs/44138018299,
  commit 9d70b04. Background watcher sh_1ea916046001wHUyb65blpTpEG notifies at
  completion. Local full Go race suite PASS after test fixes. Private PKCS#12
  integrity/password check PASS; private files have 0600 permissions outside git.
- Run 44134615417: case-sensitive APFS fix PASS. Intel native compilation, vet
  and 103 UI tests PASS. Go race suite found 3 fixture groups missing Mac config/
  log parents, plus a Pulse-only test mistakenly shared on Mac. Create fixture
  parents and move that test to profile_session_linux_test.go, without skipping
  shared coverage or changing production behavior. Arm runner unavailable again.
  Official macos-26 arm64 image lists Xcode 26.6; switch to that free runner pool.
  Signing/publication have still not run. No downloaded installer exists yet.
- Retry run: https://github.com/eriknovikov/voice-type/actions/runs/44134615417,
  commit 2a192d0. Watcher is background shell sh_1ea8032c7001ZbfSrJgDUvvnTI and will
  notify on completion. Do not poll it. All 17 portable tests/actionlint still PASS.
- Native run 44131304495 failed before signing/publication. Intel Xcode 16.4
  compiled the static engine and passed all 103 UI tests, but Cgo C++ compilation
  resolved <version> to root VERSION on default case-insensitive APFS. CI now
  mounts an ephemeral case-sensitive APFS source workspace before checkout and
  verifies VERSION/version are distinct. Keep VERSION and the flat package intact.
  Save native caches before later gates so failures do not rebuild inference.
  Apple Silicon did not acquire a runner after 15m (GitHub capacity issue), not a
  source failure. Retry both architectures; no Mac build/signature claim yet.
- 2026-11-17 preview execution: committed/pushed c1d6bdb on feat/mac using verified
  personal author/committer and explicit eriknovikov HTTPS credentials. Native Mac
  Preview run: https://github.com/eriknovikov/voice-type/actions/runs/44131304495.
  Hosted completion/signing/publication is pending. No stable/main/VERSION change.
  Linux arm64 `VOICE_TYPE_VULKAN=0 VOICE_TYPE_TEST_IBUS_DAEMON=/usr/bin/ibus-daemon
  make check build` PASS: 103 UI tests, Go vet/race, malgo and 17 Python tests.
  actionlint v1.7.12 and git whitespace checks PASS. Linux native tray and full
  application smoke PASS on Wayland using its absolute socket path. X11 was
  unavailable and xvfb-run absent; neither is needed for the passing Wayland run.
  Stable Latest baseline is v6.4.3 (release 446007629); previews must not change it.
  Browser relay is offline; verify final downloads through anonymous HTTP/hash
  checks rather than claim an interactive browser installation test.
- 2026-11-17: Completed portability investigation and Linux baseline checks.
  User approved recommended decisions, requested branch and durable handoff, and
  offered a spare Mac. Created feat/mac and the three planning documents. No app
  code changed. Current phase is ready for MAC-010.
- 2026-11-17 implementation: MAC-010 complete. Shared Recorder now has a small
  captureDevice lifecycle seam; audio_linux.go retains Pulse behavior. Stop/Close
  serialize teardown so a new Start cannot race draining callbacks. Added synthetic
  lifecycle/fallback/error tests. DefaultInsertBackend selects IBus on Linux and
  mac-direct on Darwin; all final/live factories and shared test overrides use it.
  Shared output errors are platform-neutral. Unix lock/tests now select Linux and
  Darwin; D-Bus tray and desktop integration are Linux-only. Real provider opt-in
  stays shared. Linux `make check` PASS with 93 frontend tests and Go vet/race;
  focused race tests PASS. Darwin capture/compilation remains unimplemented and
  unverified until MAC-050/MAC-020. Began MAC-020 link-manifest regression tests.
- 2026-11-17: MAC-020 portable implementation/checks PASS. `make check` includes
  Go manifest tests (Linux/Apple dependencies and unsafe inputs) and five Python
  orchestration tests (bundle metadata/resources/rebuild/refusal/minimal entitlements,
  Darwin/Linux flags, shasum/sha256sum dispatch and Vulkan refusal). Synthetic
  bundles deliberately contain non-Mach-O fixtures and prove no native behavior.
  Mac `make build` and dev watcher now assemble/ad-hoc-sign separate app bundles;
  no Developer ID/notarization claim. Native Go capture is still MAC-050. Full Linux
  check PASS after build changes (93 UI tests, Go vet/race, scripts). Mac native gate
  is BLOCKED on access; continue independent implementation.
- 2026-11-17: MAC-030 implementation underway. paths.go centralizes existing Linux
  production paths and Mac Application Support/Logs, with dev roots separated on
  both platforms. Build-stamped dev identity also selects a separate Keychain
  service. Mac-only legacy-config copy leaves source intact and atomically creates
  destination only if absent. Unix private reads reject symlinks/foreign ownership;
  native app directory creation checks ownership and rejects symlinked app roots.
  Shared config test helpers create parent directories for Mac fixtures. All Go
  packages passed race tests after initial path changes; final checks pending.
- 2026-11-17: MAC-030 portable implementation/checks complete. Secure Unix reads
  also reject hard links; private writes replace files atomically. Log files refuse
  symlinks and chmod only the opened descriptor. Explicitly read-only directories
  remain errors, not silently repaired. Runtime lock preparation does not relax its
  existing permission checks. No real Keychain/audio/provider calls were performed.
- 2026-11-17: MAC-040/050 portable implementation/checks complete. permissions.go
  holds typed status/preflight and explicit bridge actions. permissions_darwin.m
  uses public AVFoundation and AX APIs; prompts/Settings actions dispatch to main
  without retaining Go pointers. Unbundled mic requests fail rather than raising
  a missing-purpose-string exception. No Input Monitoring or Screen Recording.
  engine preflight precedes recording configuration/provider/model/capture work;
  meter needs mic only. frontend/src/Permissions.tsx renders Mac onboarding in
  General; App compares permission changes during polling. Three focused UI tests
  and frontend typecheck/build PASS.
- 2026-11-17: Chromium browser smoke PASS against temporary Vite mock harness:
  initial action list empty; explicit mic request failure visible; retry updates
  Allowed state; explicit Accessibility Settings action recorded; simulated mic
  revocation updates UI; switching platform to Linux removes panel. Screenshot
  inspected. This is frontend-flow evidence, NOT actual macOS permission evidence.
  Own browser tab/dev server were closed and temporary harness deleted.
- 2026-11-17: Core Audio adapter in audio_darwin.go uses the malgoCapture backend
  in audio_malgo.go. Backend compiles on Darwin, or on Linux only with malgo_test
  tag; it is not linked into production Linux builds. DeviceID.Pointer allocations
  are freed after InitDevice. Capture callbacks decode owned PCM16 only; no network,
  settings writes, device lifecycle or logging from real-time callbacks. Explicit
  stop drains callbacks and reports unexpected stop/permission failure for recovery.
  Five null-backend tests PASS under race (320 mono samples per 20ms @16kHz,
  stable IDs, lifecycle, ownership, fallback rules, callback errors and permission
  refusal/revocation with recovery audio). make check now runs that tag gate as
  well as the normal Linux suite.
- 2026-11-17: Test safety/portability: main_test.go isolates default suite HOME/XDG
  and mocks the keyring plus permission driver. Child test processes keep their
  inherited contention fixture. Explicit real-provider/keyring opt-ins remain
  separate and must run focused. Linux installer/tray/IBus integration tests are
  Linux-only; pure GNOME shortcut tests stay shared, native GNOME tests skip on Mac.
  `GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go list -e` selected Core Audio/ObjC/Unix
  files and omitted Pulse/IBus/Linux integration sources with no selection error;
  this is NOT native compilation evidence. All graph evidence paths checked with
  check_index_coverage; no recorded issues on the changed source paths.

## Earlier implementation verification, Linux arm64

- MAC-060 policy: focused race tests PASS, including 25 repetitions and a 3-second
  UTF-16 round-trip fuzz run. Pins the first target, rechecks safety/focus before
  native handoffs and Unicode chunks, stops after any failure, and permits Unicode
  only after certified no-submission AX unsupported. No clipboard/document rewrite.
  Existing engine live recovery retains audio/private transcript after ambiguous
  delivery without final replay. Native mutators must also recheck immediately
  before mutation. Native adapter is now registered in insert_darwin.go/.c/.h.
- MAC-060 native code pins AX element, PID and process start time (rejects PID
  reuse/self-targets), inspects only role/subrole/enabled/settable metadata, never
  surrounding text or AXValue contents. Unicode requires affirmative editability
  when selected-text replacement is unavailable. Unknown text-field subroles fail
  closed; text areas can omit the optional subrole. Secure input rejects all output.
  Events use a private source, zero modifiers and the pinned PID; key-up is balanced
  even if cancellation races key-down. AX setter failure is always unknown, never
  permission to fall back. AX queries have a 250ms per-message limit. C cancellation
  guards are signalled by context.AfterFunc and freed only after callback completion.
- Native C fixture compiles the production implementation against fake platform
  APIs, asserting no content reads/whole-value writes and resource release. PASS
  with strict C11 warnings and a separate ASan/UBSan build/run. Covers 19 refusal
  scenarios, PID reuse, late cancellation/focus loss, before/after-setter unsupported,
  balanced Unicode events and read-only event-post preflight. This is control-flow
  evidence, NOT Apple SDK ABI or Mac runtime evidence. Linux `go tool cgo` generation
  succeeds. Darwin Cgo generation is unavailable with Linux GCC (`-arch` unsupported).
  Safe Darwin-only tests were added for lazy registration and cancellation cleanup;
  they still need native execution. Real events/AX are not invoked by default tests.
- MAC-070 implemented in insert_mac_paste.go and paste_darwin.go/.m/.h. One global
  coordinator serializes snapshot/publication/post/read-coordination/restoration.
  200ms read quiet, 8s normal bound, 500ms ambiguous-failure bound. Never replays.
  Restoration always rechecks ownership and closes partial transactions on errors.
  Native snapshots materialize every item/format before publishing, refuse missing
  data or >32MiB/128 items/types, and keep snapshots in memory only. Uses advisory
  transient/concealed markers plus a private UUID ownership token/change count.
  Newer clipboard data is not intentionally overwritten; public APIs have no atomic
  cross-process compare-and-write, leaving an unavoidable narrow race. Provider
  callbacks can be cached, so quiet/read counts do not prove target consumption.
  ObjC exception boundaries return generic errors without clipboard contents.
- Output mode is an optional desktop preference (missing/empty = direct). It is
  snapshotted into sttSettings before provider/credential setup in reloadConfig;
  final/live factories use the snapshot, not a later preference read. Paste+live
  errors clearly before model/capture work and never changes the profile. Linux
  keeps IBus regardless of this Mac-only preference. Native Mac constructors are
  lazy, so default test construction/Close does not query focus or alter clipboard.
- MAC-070 focused race tests and 3 UI tests PASS. Chromium mock browser flow PASS:
  save failure leaves Direct selected; retry shows paste privacy/live warning;
  profile remains live; selecting Direct hides warning; Linux hides output choices.
  Temporary harness/tab/server removed. `make check` PASS (99 UI tests, Go vet/race,
  6 Python tests including C fixture). Updated C fixture and ASan/UBSan PASS for
  single Command-V plus last ownership/focus/access checks. Linux Cgo generation
  for both native Go wrappers PASS. Native AppKit/AX compilation remains unverified.
- MAC-080 implemented: accessory Mac activation, ready cold launches stay menu-bar
  only, first-run/recovery stays visible, shortcut registration failures affect
  readiness, native tray menu avoids OnClick activation, reopen lazily creates and
  shows settings. Native Quit asynchronously requests the existing flush-settings
  handshake; only the final-quit guard permits subsequent NSApp termination.
  Startup failure can still exit. Linux quit and cold-show behavior stay unchanged.
- Mac shortcut default/reset is Ctrl+Alt+Space with Control/Option/Command labels;
  Linux-only legacy migration cannot remap the Mac default. Saved custom bindings
  are preserved. Wails still owns Carbon global shortcut registration.
- Login registration uses Wails SMAppService requests; new native read-only status
  preserves requires-approval instead of Wails' legacy-LaunchAgent fallback. Mac
  startup never registers automatically, unrelated saves never change login state,
  explicit SetLoginStartup can retry the same stored intent, and rollback restores
  effective OS state. UI switch reflects effective status, not saved intent. Raw
  unbundled Mac login registration is refused. Development startup remains disabled.
- Mac sleep events cancel active contexts and stop capture/meter. Recovery audio
  is retained privately on interruption; wake never resumes the old context.
  Tests cover both recording and a blocked finalization completing after wake with
  zero insertion. Busy native Quit cannot bypass the flush/idle guard. Focused
  sleep/quit/login race tests PASS 5 repeats, plus startup/default/migration tests.
- Chromium mock lifecycle UI PASS: startup performs no login writes, OS-disabled
  state remains unchecked despite stored true intent, explicit failure stays
  visible and unchecked, retry checks it, Command labels display correctly, Reset
  saves Ctrl+Alt+Space and shows Control+Option+Space without another login write.
  Own harness/tab/server removed. Latest `make check` PASS: 101 UI tests, Go
  vet/race, malgo gate, 6 Python tests. None is native AppKit/login/tray evidence.
- `make check`: PASS on the current implementation: 8 frontend files/101 tests,
  TypeScript/Vite build, Go vet/race (root/cmd/scripts), malgo null-backend race
  gate, 6 Python/native-control-flow tests, Python syntax and shell syntax/shellcheck.
- Explicit fresh malgo run: all 5 tests PASS, including permission revocation at
  Stop retaining recorded audio and native device cleanup. No real audio captured.
- `make build` and `make dev-backend`: PASS. CLI version checks print `6.4.0` and
  `6.4.0-dev`. Dev build metadata has the linker-stamped developmentBuild=true;
  malgo is absent from production Linux dependency metadata. Final production
  binary rebuilt after logging hardening with production tag and 6.4.0 stamp.
- Private logging symlink test PASS as part of the Go race suite: no writer or
  reader follows the link, and its target stays unchanged. Rotation uses the same
  secure open helper.
- Docs validator PASS: 16 tickets, complete fields, valid local links, no dependency
  cycles, clean whitespace. `git diff --check` PASS. VERSION/release.yml unchanged.
- Not tested: native Darwin compilation, real TCC/Core Audio/AX, hardware,
  Developer ID/notarization, hosted Mac CI or installed release behavior.
- Required user action at this checkpoint: provide the spare Mac's OS/version and
  architecture, and either explicitly approved SSH host/user access or a user-run
  native-check arrangement. No Apple signing secrets are needed for this gate.
- Nothing committed/pushed/published. All new source/docs must accompany any
  transfer to the Mac; they are not available by cloning the uncommitted branch.

## Latest continuation checkpoint, Linux arm64, 2026-11-17

- MAC-090: FAQ is platform-aware, uses the saved shortcut, retains Linux-specific
  help only on Linux and adds Mac permissions/insertion/privacy/login/data/update/
  removal guidance. Mac no longer gets the AppIndicator recommendation.
- MAC-100: RunDoctor is shared in diagnostics_report.go; doctor_checks.go has bounded
  private config reads without migration/repair/creation or credential lookup.
  Model/log checks reject symlinks; recent failures include rotated logs and do not
  copy arbitrary error text. doctor_mac.go is the portable policy; doctor_darwin.go/.m
  supply Foundation metadata and read-only permission/login/Core Audio state.
- Updates are split: update_linux.go alone embeds/executes install.sh. Darwin calls
  runMacUpdate, validates stable GitHub metadata and an exact architecture-specific
  release URL, and opens the DMG download with /usr/bin/open. It never replaces the
  app or consumes VOICE_TYPE_UPDATE_APPLY. HTTP, cancellation, unsafe metadata,
  no-new-version and browser-failure/manual-link paths have tests. Linux scripts
  refuse Mac without deleting data. README covers unpublished status and removal.
- MAC-110: scripts/macos-release.py verifies native bundle/inference metadata and
  provides app then DMG signing/notary/staple, signatures/team/runtime/entitlements,
  Gatekeeper/tickets, readonly mounted contents and checksums. make mac-verify and
  make mac-release are native-only. Tests use fake tools/files, not real DMGs.
  Native source flags now explicitly disable backend modules as well as shared libs.
- MAC-120: reusable .github/workflows/macos.yml tests macos-15 arm64 and macos-15-intel
  amd64 with Xcode 16.4, Node 24, Go from go.mod and OS/arch/flags/revision-sensitive
  inference caches. PR/test calls have no signing secrets. Release signing is a
  separate main-only macos-release environment job after both native builds, with
  temporary certificate/keychain cleanup. Release publication requires five Linux
  packages and two exact, sidecar-checksummed Mac DMGs. Configure required reviewers
  and the declared secrets before authorized signing. No hosted workflow was run.
- Verification: make check build PASS; final
  VOICE_TYPE_TEST_IBUS_DAEMON=/usr/bin/ibus-daemon make check PASS (103 UI tests,
  Go vet/race including isolated IBus/mock providers, malgo gate, all 16 Python
  tests and shell checks). Final focused diagnostic/update race tests PASS.
  actionlint v1.7.12 PASS; document links/ticket dependency/whitespace validator PASS.
  GDK_BACKEND=x11 WAYLAND_DISPLAY= make native-check PASS on the real Linux display:
  private-bus native tray, startup/setup/single-instance/Quit/SIGTERM/conflict smoke.
- Chromium mock FAQ/diagnostics PASS: Mac saved shortcut/help/privacy, report error
  then retry, copy error then retry, and Linux-only instructions after platform
  switch. Temporary harness, server and own tab removed. Native Mac UI is untested.
- Both Darwin architectures pass go-list source selection with the new doctor/
  updater and no install.sh embedding. This is NOT compilation/SDK/runtime evidence.
  Graph coverage reported no recorded issues for the inspected implementation paths.
- Remaining user prerequisites: Mac OS/version/architecture and explicitly approved
  SSH or a user-run arrangement first; signing credentials and authorized CI later.
  No Mac build, hardware/provider/model/real-Keychain test, Developer ID signature,
  notarization or published artifact exists. No commit/push/VERSION change.

## Planning handoff verification (historical)

- Documentation validator: PASS, 12 unique decisions, 16 unique tickets, every
  ticket has status/dependencies/scope/acceptance/definition of done/evidence,
  no broken local links, no dependency cycles, next ready ticket MAC-010.
- `git diff --check`: PASS. All four handoff/instruction files also passed explicit
  final-newline/trailing-whitespace checks, including the new untracked documents.
- Scope of this handoff: three new Markdown files and an AGENTS.md discovery
  pointer. No application behavior changed, so the app suite was not rerun for
  these documentation edits. The earlier baseline results above are not claims
  about future implementation changes.
- Nothing committed or published. Keep these files with the branch when moving
  the work to another checkout; untracked files do not travel with git by themselves.
