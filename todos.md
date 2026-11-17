# macOS feature tickets

Source of scope and decisions: [end_goal.md](end_goal.md).
Findings and session handoff: [memory.md](memory.md).

## Workflow

- Statuses: `TODO`, `DOING`, `BLOCKED`, `DONE`. Start with the lowest unblocked ID.
- Keep one primary ticket `DOING`; split oversized work into ID-suffixed subtickets.
- Update status, evidence and `memory.md` before compaction or ending a work session.
- `DONE` requires the ticket's acceptance checks and definition of done, not just code.
- Record commands, outcomes, tested OS/architecture and commit or working-tree state.
  Keep evidence concise; do not paste secrets, transcripts or giant logs here.
- Record external blockers with the exact user action needed. Continue independent
  work first. Never replace missing runtime evidence with a mock-test claim.
- Dependencies gate completion. A blocked native/signing check does not prevent
  implementing and locally testing independent portions of later tickets; leave
  those tickets open with the remaining gate explicit.
- Tests may invalidate an implementation assumption. Record the evidence and the
  minimal correction in memory; ask before changing an approved product decision.

## MAC-000: Durable feature handoff

- Status: DONE
- Depends: none
- Decisions: D01-D12
- Scope: Create `feat/mac`, scope/decision document, ticket backlog and durable memory.
- Acceptance: The branch is active, links/IDs are consistent, and resumption does
  not depend on conversation history.
- Done when: All three documents exist and the repository instructions point to them.
- Evidence: Created from clean `dev` at `37a00bad072c807dd3fd56f276f092127554c986`.
  Documentation-only validation is recorded in `memory.md`; nothing is committed.

## MAC-010: Platform seams and portable shared tests

- Status: DONE
- Depends: MAC-000
- Decisions: D01, D05, D12
- Scope: Separate Linux tray/capture concerns; share the secure Unix engine-lock
  implementation so Darwin can compile; add default insertion selection at every
  call site and platform-neutral output messages. Constrain Linux integration
  tests and remove Linux assumptions from shared test fixtures.
- Acceptance: Linux default insertion remains IBus; shared factories are replaceable
  in tests; Darwin test sources no longer reference excluded IBus helpers.
- Done when: `make check` passes on Linux and platform selection has focused tests.
  Complete native Darwin compilation in MAC-020, not by disabling Cgo/features.
- Evidence: Linux arm64, uncommitted working tree: `make check` PASS (93 UI tests,
  Go vet/race, Python/shell checks). Focused race tests for default final/live
  insertion, capture lifecycle/buffering and Unix lock/process ownership PASS.
  Pulse capture and D-Bus tray are Linux-only; shared tests replace the platform
  default. IBus desktop/tray integration is Linux-only; opt-in providers stay shared.
  Darwin capture itself remains MAC-050; no native compilation is claimed here.

## MAC-020: Native Mac build and development bundle

- Status: DONE
- Depends: MAC-010
- Decisions: D02, D09, D10
- Scope: Make build/dev recipes platform-aware; consume Apple link-manifest
  frameworks; support Mac checksum tools; build pinned static inference with
  explicit backend flags, embedded Metal source and a 13.0 deployment target.
  Assemble minimal production/dev bundles from tracked `packaging/macos/` assets.
- Acceptance: Native arm64 and amd64 builds embed the UI; arm64 has Metal and CPU,
  Intel has CPU/Accelerate; runtime dependencies contain no accidental Homebrew
  libraries or shader sidecars. Linux still builds. Native tests compile.
- Done when: A native build on the available Mac passes architecture/version/
  deployment/dependency checks and manifest-generator tests pass. MAC-120 must
  supply both-architecture native evidence before completion. Cross-target
  `go list` is not build evidence. With no Mac access, keep this ticket open and
  continue independent implementation using clearly recorded local test evidence.
- Evidence: Implemented Apple framework manifests with Linux rejection retained,
  checksum-tool fallback, explicit native CPU/Metal flags, 13.0 target, build/dev
  recipes, minimal bundle/entitlements/icon/license assembly and dev build stamp.
  Linux `make check` PASS, including 11 Go manifest cases and 5 Python tests
  (synthetic bundles and platform dispatch, not native compile/sign evidence).
  MAC-050 capture source is now present; native compilation awaits Mac access. User action:
  supply spare Mac OS/architecture and user-run or explicitly approved remote access.
  Superseding native evidence: run 44141118702 PASS on arm64 macOS 26/Xcode 26.6
  and amd64 macOS 15/Xcode 16.4: check/build/mac-verify, static backends, system-only
  dependencies, 13.0 deployment target, bundle metadata/version and source stamp.

## MAC-030: Private paths, engine lock and dev isolation

- Status: DONE
- Depends: MAC-020
- Decisions: D08
- Scope: Add Mac Application Support/log paths; preserve Linux paths; complete
  Mac runtime-directory wiring for the shared Unix lock. Reuse Keychain behavior.
  Make dev identity work without inherited shell environment and isolate dev data,
  credentials and locks. Copy legacy config only into an absent destination.
- Acceptance: Path, migration, permissions and process-contention tests pass;
  symlinks/unsafe ownership are rejected; quitting/crashing releases the engine
  lock; dev and production do not overwrite each other's data or credentials.
- Done when: Focused path/lock tests and shared tests pass on Linux and native Mac.
  Keychain integration is opt-in and must not touch real user credentials in CI.
- Evidence: Implemented/tested on Linux arm64: centralized native/private paths,
  unchanged Linux production paths, isolated dev storage/Keychain service and
  environment-independent build identity. Atomic create-only legacy migration,
  symlink/regular-file/ownership checks and private writes are in place.
  `go test -race ./...` PASS, including migration concurrency and secure Unix
  lock process/crash tests. Native Mac runtime/Keychain verification is still open;
  needs Mac access, not production credentials. No real Keychain test was run.
  Native evidence: full path/migration/lock race suite PASS on both architectures
  in run 44141118702. Real Keychain remains opt-in and untested, not a CI claim.

## MAC-040: Permissions and recording preflight

- Status: DONE
- Depends: MAC-020, MAC-030
- Decisions: D04
- Scope: Public Objective-C/Cgo permission bridge, microphone purpose string/audio
  entitlement, typed bridge endpoints and onboarding state. Preflight before
  provider setup/model loading/capture; keep enumeration and diagnostics read-only.
- Acceptance: Mock tests cover unknown/granted/denied/restricted/revoked states,
  explicit requests and failure guidance; missing access cannot become silent
  capture; callbacks do not block the main thread or hold engine locks.
- Done when: Native build and unit/UI tests pass; no extra entitlements/permissions
  are added. Actual allow/deny/relaunch flows are verified in MAC-130.
- Evidence: Implemented AVFoundation/AX read-only status and explicit async request/
  Settings actions, purpose string and minimal audio entitlement. Final/live
  recording and microphone meter preflight before setup/capture; ready hidden
  startup also requires permission. Go tests cover unknown/denied/restricted/
  granted/revoked states, no implicit requests and unlocked explicit actions.
  Three new frontend tests PASS; typecheck/build PASS. Chromium flow PASS with
  mocked native responses: no initial request, visible failure/retry, granted and
  revoked status, explicit Settings action, and no Mac panel on Linux.
  Objective-C compilation, real TCC prompts and native bundle flows need Mac access.
  Native evidence: Objective-C compilation and unit/UI tests PASS on both
  architectures in run 44141118702. Actual TCC prompts remain MAC-130.

## MAC-050: Core Audio recorder

- Status: DONE
- Depends: MAC-010, MAC-040
- Decisions: D03
- Scope: Add malgo v0.11.28 Core Audio backend while retaining shared buffering,
  metering, chunk/session sinks and the existing recorder contract.
- Acceptance: Tests cover 16 kHz mono PCM16, stable IDs/default selection,
  successful-enumeration-only fallback, owned samples, repeated start/stop,
  cancellation, capture errors and device/context cleanup. No network, settings
  writes or device teardown inside the real-time callback.
- Done when: Synthetic/null-backend and recorder contract tests pass with race
  testing; Linux capture tests pass. Hardware checks remain in MAC-130.
- Evidence: Implemented malgo v0.11.28 Core Audio adapter and shared buffering.
  `go test -race -tags malgo_test -run '^TestMalgo' -count=1 -v .` PASS (5 tests):
  real null-backend 320-sample/20ms capture, stable-ID round-trip, repeated start/
  stop/close, owned PCM16, enumeration-error/no-fallback, missing-device fallback,
  callback failures/device-stop state, permission refusal before capture and
  retained recovery audio when permission is revoked before Stop.
  Added this null-backend gate to `make check`; Linux keeps PulseAudio only in
  production. Darwin source selection includes the adapter and excludes Pulse.
  Core Audio compilation/execution, device loss and hardware remain unverified.
  Native completion requires Mac access and MAC-040; hardware acceptance is MAC-130.
  Native evidence: Core Audio adapter compilation, recorder contracts and malgo
  null-backend race tests PASS on both architectures in run 44141118702.

## MAC-060: Clipboard-free direct insertion

- Status: DONE
- Depends: MAC-010, MAC-040
- Decisions: D05
- Scope: Mac TextInserter using writable AX selected text, otherwise Unicode
  events only when no AX submission occurred. Pin target at first insertion and
  for live sessions; recheck focus and safety before submission/chunks.
- Acceptance: Tests cover selections/caret, Unicode including surrogate pairs,
  multiline text, cancellation, focus change, secure/read-only/non-text/unknown
  targets, partial/ambiguous failure, at-most-once submission and idempotent Close.
  No whole-document rewrite, clipboard use or fallback after ambiguous delivery.
- Done when: Adapter tests and existing final/live insertion tests pass; timing
  remains accurate. Actual application compatibility is recorded in MAC-130.
- Evidence: Policy implemented in insert_mac.go with a fake retained-target adapter.
  Linux arm64: focused race suite PASS, including 25 repeated runs; 3-second UTF-16
  fuzz run PASS. Tests cover fail-closed safety/capability checks, first-target
  pinning, cancellation, partial/ambiguous delivery, no replay, cleanup and shared
  timing. Engine live-failure test retains private recovery without final replay.
  `make check` PASS (96 UI tests, Go vet/race, null capture and script gates).
  Native adapter now registered in insert_darwin.go/.c: read-only AX capability
  inspection, exact element/PID/process-birth checks, selected-text setter only,
  private-source process-targeted Unicode events, secure-input refusal, bounded
  AX messaging and C cancellation guards. Production C control-flow fixture PASS
  under strict C11 warnings and AddressSanitizer/UBSan (19 refusal scenarios plus
  Unicode/fallback/resource checks). Linux Cgo generation PASS; a Darwin probe
  cannot compile here (`gcc` rejects `-arch`). These are NOT Apple SDK/native app
  evidence. Mac build/runtime verification remains blocked on user-supplied access.
  Native evidence: real Apple SDK adapter build and native/shared unit/race tests
  PASS on both architectures in run 44141118702. Application acceptance is MAC-130.

## MAC-070: Explicit paste compatibility

- Status: DONE
- Depends: MAC-060
- Decisions: D06
- Scope: Mac desktop preference and serialized pasteboard transaction: snapshot
  items/formats, transient/concealed markers, one Command-V, bounded read-request
  coordination and ownership-checked restoration. Never retry delivery.
- Acceptance: Tests cover text/rich/image/empty clipboard, user copy during paste,
  repeated reads, timeout/cancellation, focus changes and failed submission.
  Paste plus live mode is rejected clearly before recording; no silent downgrade.
- Done when: Unit/UI tests pass, clipboard privacy limitations are visible, and
  real clipboard restoration checks are scheduled in MAC-130.
- Evidence: Implemented serialized transaction policy, native NSPasteboard adapter,
  one process-targeted Command-V, output preference/UI and per-recording backend
  snapshot. Paste+live is rejected before credential/provider/model/capture setup.
  Linux focused race tests PASS: all-format/item/empty snapshots, concurrent copies,
  bounded repeated/no reads, timeout/cancel/focus/failure restoration, no replay.
  3 frontend tests and Chromium mock success/failure/privacy/live-warning flows PASS;
  `make check` PASS with 99 UI tests, Go vet/race and 6 Python tests. Native event
  fixture (including ownership check before Command-V) also PASS under ASan/UBSan.
  Native pasteboard/Objective-C compilation and restoration need Mac access.
  Public pasteboard APIs have no cross-process atomic compare-and-write; ownership
  checks reduce but cannot eliminate concurrent-copy races. Data-provider requests
  are advisory and may be cached, not proof the target consumed every read.
  Native evidence: Objective-C adapter build and shared paste/unit/UI tests PASS
  on both architectures in run 44141118702. Real clipboard checks remain MAC-130.

## MAC-080: Menu bar, shortcuts and safe lifecycle

- Status: DONE
- Depends: MAC-030, MAC-040
- Decisions: D07
- Scope: Accessory/menu-bar configuration, native menu clicks without focus theft,
  reopen/lazy-window handling, state indicators, Control-Option-Space default and
  platform-specific reset/migration. Use SMAppService with menu-bar-first cold
  launch, effective login state, guarded native Quit/Command-Q and sleep handling.
- Acceptance: Tests cover first-run/recovery versus ready cold starts, shortcut
  capture/conflict/rollback, external login disable, no webview on ready background
  start, unsaved draft flush, no recursive quit and no stale insertion after sleep.
- Done when: Lifecycle/preference tests and Linux regression tests pass; installed
  login/reopen/focus/quit behavior is explicitly covered by MAC-130.
- Evidence: Implemented accessory activation, ready cold-launch menu-bar policy,
  lazy reopen, no Mac tray-click focus theft, asynchronous guarded native Quit,
  platform shortcut default/reset/labels, effective SMAppService status and
  explicit-only Mac login registration. Unrelated saves never override OS denial.
  Sleep cancels active contexts, stops capture/meter, retains recovery audio and
  never resumes stale output on wake. Focused race tests PASS, including 5 repeats
  of sleep/quit/login cases. Chromium mock login failure/retry and shortcut reset
  flow PASS. `make check` PASS (101 UI tests, Go vet/race and 6 script tests).
  Native menu/reopen/Command-Q/login/sleep execution remains blocked on Mac access.
  Native evidence: lifecycle/preference unit/race tests and native production
  builds PASS on both architectures in run 44141118702. Installed flows are MAC-130.

## MAC-090: Platform-aware frontend and help

- Status: DONE
- Depends: MAC-040, MAC-050, MAC-060, MAC-070, MAC-080
- Decisions: D04-D07
- Scope: Align Go DTOs and handwritten `frontend/src/api.ts`; render permission
  onboarding, effective startup status, Mac key labels/default and output choice.
  Replace Linux-only help on Mac while retaining it on Linux. Keep current window.
- Acceptance: UI tests cover permission failures/actions, shortcut reset, output
  privacy warning and live/paste incompatibility; bridge payloads stay aligned.
- Done when: Typecheck/build and all frontend tests pass; no Linux guidance appears
  as Mac setup instructions; autosave/profile draft protection remains intact.
- Evidence: Implemented platform-aware FAQ, saved Mac shortcut labels, permissions,
  clipboard/live limits, effective login, data/update/removal help; retained Linux
  IBus/GNOME/Wayland guidance and hid its AppIndicator note on Mac. Linux arm64:
  frontend build and all 103 tests PASS. Chromium mock FAQ and diagnostics retry/
  copy-failure/retry flows PASS; own harness/tab/server removed. UI/bridge tests
  from MAC-040/070/080 remain passing. Native prerequisite tickets remain open;
  this is not native Mac UI or installed-app acceptance evidence.
  Native evidence: prerequisite native suites and all 103 frontend tests PASS
  on both architectures in run 44141118702. Installed Mac UI remains MAC-130.

## MAC-100: Mac diagnostics, updates and uninstall

- Status: DONE
- Depends: MAC-030, MAC-080, MAC-090
- Decisions: D08, D11
- Scope: Read-only Mac doctor/report for OS/arch, bundle, permissions, audio devices,
  profile/model/storage health and last failure. Shared diagnostic logic must not
  remain trapped in doctor_linux.go. Add matching-release download opening, safe
  Linux-installer refusal and explicit login/data/Keychain removal instructions.
- Acceptance: Diagnostics cause no prompt/capture/secret lookup; update tests select
  correct platform/arch, handle network errors, and never execute install.sh on Mac.
  Uninstall does not silently delete user data. Logs contain no sensitive content.
- Done when: Native/shared diagnostic and update tests pass and README/help explain
  install, update, data locations, limits and removal accurately.
- Evidence: Shared read-only doctor checks extracted; Mac report adds bundle,
  permission/login/audio/output/storage health without prompting, capture or
  Keychain lookup. Diagnostics no longer migrate/repair/create configuration.
  Linux installer/apply embedding is Linux-only; Mac selects an exact stable DMG
  URL by architecture and opens it, with a manual fallback on failure. README/FAQ
  explain retained data/keys and removal; shell scripts refuse Mac before changes.
  Linux arm64 focused race tests, full make check/build and native Linux smoke PASS.
  Both Darwin architectures select the correct sources and omit install.sh embedding
  (go list only). Native Objective-C/doctor/update execution still requires Mac access.
  Native evidence: doctor/update compilation and full native/shared test suites
  PASS on both architectures in run 44141118702. Real installed flow is MAC-130.

## MAC-110: Final signed DMG packaging

- Status: DOING
- Depends: MAC-020, MAC-080, MAC-100
- Decisions: D02, D10
- Scope: Final bundle metadata/icon/licenses, production/dev identities and minimal
  entitlements; version stamping from VERSION; separate arm64/amd64 DMGs with an
  Applications link. Self-sign the app and DMG with the persistent identity (D10).
- Acceptance: Verify `codesign --verify --deep --strict`, pinned certificate and
  stable designated requirement, bundle/version/architecture/deployment and checksums
  after final packaging. No mutable user files in bundle; no blanket security
  exceptions or Gatekeeper-bypass installation instructions.
- Done when: Both final DMGs have pinned verifiable self-signatures and installable
  contents. Ad-hoc builds cannot pass distribution gates. No notarization claim.
- Evidence: Implemented mac-verify/mac-release and scripts/macos-release.py: static
  inference manifest/backend checks, bundle/version/architecture/13.0/dependency
  checks, Developer ID/hardened runtime/minimal entitlements, app then DMG notary/
  staple, Gatekeeper/ticket checks, readonly mounted-content validation and checksum.
  No unsigned fallback; existing outputs are not overwritten. Portable fixtures
  PASS for both architecture sequences and fail-closed signature/notary/content/
  credential/output cases; these produce no real signed artifacts. BLOCKED on native
  Mac execution and securely supplied Developer ID/private key, Team ID and notary
  credentials. No actual signing/notarization or final DMG is claimed.
  Superseding 2026-11-17: persistent self-signed identity created outside git,
  encrypted environment secrets stored with main/feat/mac branch restrictions.
  Removed Apple gates in favor of exact certificate/DR/runtime/entitlement checks.
  All 16 portable Python tests and actionlint v1.7.12 PASS; native execution pending.
  First hosted run 44131304495: Intel static inference and 103 UI tests PASS;
  Cgo compile found VERSION/<version> collision on case-insensitive APFS. Fix is a
  case-sensitive CI source volume. Apple Silicon had no available hosted runner.
  Signing/DMGs remain unverified; no public preview exists yet.
  Retry 44134615417: Intel native Cgo compile/vet and 103 UI tests PASS. Correcting
  test fixture config/log directories and Linux-only Pulse test scope. macos-15
  arm64 runner unavailable twice; use macos-26/Xcode 26.6 (still target macOS 13).
  Run 44138018299: BOTH native check/build suites PASS (Go race, malgo, 103 UI
  tests, production bundles). mac-verify parser confuses linker-tool version with
  minimum OS; realistic fixture reproduces it. Fix keeps strict 13.0 validation.
  Run 44141118702: native bundle gates PASS on both architectures. Signing imports
  identity, then hangs in user-domain trust setup. Cancelled and corrected to
  noninteractive administrative trust on disposable runners, with bounded steps.
  Run 44154713968: import/trust PASS on both runners with correct valid identity;
  codesign cannot locate private key. Add temporary keychain to search/default
  lists before import/signing; no signed DMG/publication evidence yet.
  Run 44158542323: app signing and initial strict verification PASS on both; fix
  codesign optional extraction prefix to `--extract-certificates=prefix`.
  Packaging-only reuse validates previous trusted native jobs, unchanged app/build
  source and clean binary revision. Local 23 Python tests + actionlint PASS.

## MAC-120: Native CI and release gates

- Status: BLOCKED
- Depends: MAC-020, MAC-090, MAC-100
- Decisions: D02, D10, D12
- Scope: Native `macos-15` arm64 and `macos-15-intel` amd64 jobs, Xcode 16.4,
  Go from go.mod and Node 24. Add adapter/build/bundle checks and trusted-context
  signing jobs. Retain Linux gates; require two Mac DMGs plus five Linux packages
  in stable release publishing. Separate Mac Preview publishes both DMGs as a
  prerelease, never Latest. Cache native outputs by OS/arch/revision/build flags.
- Acceptance: No signing secrets for untrusted PRs; tests isolate HOME/XDG,
  credentials and desktop services; unsigned test builds cannot pass release gates.
  An unsigned synthetic bundle may test packaging without pretending to be a DMG
  accepted by Gatekeeper. Both architectures run native tests, not only compile.
- Done when: Relevant CI runs pass. Running remote workflows needs user-approved
  push/dispatch access; editing release.yml does not authorize a main-branch push.
- Evidence: Implemented reusable macos.yml with native arm64/Intel test/build/
  bundle checks, Xcode 16.4, pinned actions, Go/Node setup and native cache keys.
  Test calls do not pass signing credentials. Main-only signing waits for both
  native jobs, uses the macos-release environment and temporary keychain cleanup.
  Release publish requires the signed Mac job plus five Linux packages and two
  exact DMGs with verified sidecar checksums. actionlint v1.7.12 PASS; executable
  publisher fixture rejects missing/wrong/corrupt/extra DMGs before publication.
  Full Python suite: 16 PASS on Linux arm64. Hosted runs remain BLOCKED on an
  explicitly authorized push/dispatch; configure macos-release reviewer protection
   and signing secrets before a release. No workflow was run remotely.

## MAC-125: Early device smoke without release credentials

- Status: BLOCKED
- Depends: MAC-090, MAC-100
- Decisions: D03-D08, D12
- Scope: Arrange user-run or explicitly approved remote testing of a locally built
  development bundle on the spare Mac. Start hardware validation without waiting
  for Developer ID or public release access. Record device/OS/arch and artifact ID.
- Acceptance: With explicit test consent, verify microphone permissions/capture,
  one local-model dictation, direct/paste output in TextEdit, shortcut/menu/quit
  and dev isolation. Use the MAC-130 checklist to record any additional coverage.
- Done when: These core flows have actual device evidence and failures are fixed
  or have linked follow-up tickets. No Gatekeeper/TCC bypass or production-key use.
  Development smoke is not evidence of final notarized-install acceptance.
- Evidence: Native-check entry point is make check build mac-verify; device flows
  remain the acceptance list above. User action: supply spare Mac OS/version and
  architecture, plus either explicitly authorized SSH host/user access or a user-run
  check arrangement. All source/docs are uncommitted and must be transferred with
  the working tree; cloning feat/mac alone is insufficient. No Apple signing
  credentials are needed for this first native gate.

## MAC-130: Installed acceptance on the spare Mac

- Status: TODO
- Depends: MAC-110, MAC-125
- Decisions: D03-D12
- Scope: Install the actual signed app through Finder/DMG. Record Mac model,
  OS/architecture, app version and tested artifact hashes before testing.
- Acceptance: Record pass/fail/not-tested for permissions allow/deny/revoke/relaunch;
  built-in/USB/Bluetooth capture and device loss; sleep/wake; both local models
  and supported modes; Metal/CPU behavior; mock cloud pipeline; direct/paste in
  TextEdit, Safari, Chrome, VS Code and Terminal; Unicode/multiline/selection,
  protected targets and focus changes; clipboard restoration/concurrent copy;
  login/reopen/minimize/Command-Q/unsaved drafts; upgrade and removal.
- Done when: Installed core flows pass with evidence, failures are fixed/retested,
  and unsupported apps/OS/hardware are accurately documented. Native CI covers
  both architectures; never claim full hardware coverage from one laptop. Record
  minimum-OS coverage honestly. Real provider/model/audio/Keychain tests need
  explicit opt-in; paid provider smoke is optional unless separately authorized.
- Evidence: pending

## MAC-140: Completion and release handoff

- Status: TODO
- Depends: MAC-110, MAC-120, MAC-130
- Decisions: D10-D12
- Scope: Re-run Linux checks/native smoke and Mac gates; close tickets with evidence,
  summarize exact artifacts/coverage/known limits, update README and durable state.
- Acceptance: No unresolved release-critical failures; no fabricated hardware or
  signing evidence; no unrelated changes or accidental VERSION bump. User can
  install the named artifact and follow concise setup/update/removal instructions.
- Done when: Implementation and public-release readiness are reported separately,
  user handoff is self-contained, and any publish action remains explicitly gated.
  Commits/pushes require personal identity/auth verification and user authorization.
- Evidence: pending
