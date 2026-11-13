import { Call } from "@wailsio/runtime";
export interface VocabularyList {
  original_name?: string;
  name: string;
  terms: string[];
}
export interface ProfileSummary {
  id: string;
  name: string;
  provider: string;
  model_id?: string;
  streaming_mode?: string;
  languages?: string[];
  unload_after_dictation?: boolean;
}
export interface APIKeySummary {
  masked_key?: string;
  saved: boolean;
  error?: string;
}
export interface Settings {
  revision: string;
  profiles: ProfileSummary[];
  active_profile: string;
  microphone: string;
  vocabulary: VocabularyList[];
  workspace: string;
  api_keys: Record<string, APIKeySummary>;
  credential_storage: string;
  notice?: string;
}
export interface SettingsUpdate {
  revision: string;
  microphone?: string;
  vocabulary?: VocabularyList[];
}
export interface ProfileUpdate {
  revision: string;
  id?: string;
  name: string;
  provider: string;
  model_id?: string;
  streaming_mode?: string;
  languages?: string[];
  unload_after_dictation?: boolean;
}
export interface LocalModel {
  id: string;
  name: string;
  bytes: number;
  ram_bytes: number;
  downloaded_bytes: number;
  progress: number;
  languages: string;
  supports_streaming: boolean;
  license: string;
  license_url: string;
  model_card_url: string;
  state:
    | "not_installed"
    | "downloading"
    | "pausing"
    | "paused"
    | "verifying"
    | "ready"
    | "failed"
    | "cancelling";
  error: string;
}
export interface Status {
  state: string;
  error: string;
  level: number;
  notice?: string;
  notice_version?: number;
  warning?: string;
  warning_version?: number;
}
export interface Preferences {
  shortcut: string;
  autostart: boolean;
  setup_complete: boolean;
}
export interface DesktopInfo {
  last_shortcut_at?: string;
  preferences: Preferences;
  tray_available: boolean;
  shortcut_error: string;
  version: string;
  preferences_error?: string;
  notice?: string;
}
export interface AudioSource {
  id: string;
  name: string;
}
export interface DiagnosticCheck {
  label: string;
  status: "ok" | "warn" | "fail" | "info";
  detail: string;
}
export interface DiagnosticsReport {
  version: string;
  generated_at: string;
  checks: DiagnosticCheck[];
}
export interface LogRecord {
  timestamp?: string;
  event: string;
  message?: string;
  provider: string;
  configured_model: string;
  outcome: string;
  level?: "info" | "warn" | "error";
  error?: string;
  total_ms: number;
  audio_ms: number;
  attempt_count: number;
  attempts: unknown[];
  timing?: {
    capture_requested?: string;
    capture_started?: string;
    capture_startup_ms?: number;
    startup_backlog_drained?: string;
    connection_started?: string;
    connection_ready?: string;
    connection_ms?: number;
    stop_requested?: string;
    capture_stopped?: string;
    final_audio_sent?: string;
    commit_sent?: string;
    transcript_received?: string;
    insertion_submitted?: string;
    insertion_cleanup_completed?: string;
    stop_to_transcript_ms?: number;
    stop_to_submission_ms?: number;
  };
}
export interface LogPage {
  records: LogRecord[];
  revision: string;
  outcome: string;
  offset: number;
  limit: number;
  total: number;
  retained: number;
  unchanged?: boolean;
}
const call = <T>(method: string, ...args: unknown[]): Promise<T> =>
  Call.ByName(`github.com/eriknovikov/voice-type.Bridge.${method}`, ...args);
export const api = {
  beginShortcutCapture: () => call<void>("BeginShortcutCapture"),
  endShortcutCapture: () => call<void>("EndShortcutCapture"),
  recoveryRevision: () => call<string>("RecoveryRevision"),
  resetBrokenSettings: (revision: string) =>
    call<Settings>("ResetBrokenSettings", revision),
  settings: () => call<Settings>("Settings"),
  save: (update: SettingsUpdate) => call<Settings>("SaveSettings", update),
  upsertProfile: (update: ProfileUpdate) =>
    call<Settings>("UpsertProfile", update),
  deleteProfile: (revision: string, id: string) =>
    call<Settings>("DeleteProfile", revision, id),
  selectProfile: (revision: string, id: string) =>
    call<Settings>("SelectProfile", revision, id),
  saveAPIKey: (provider: string, key: string) =>
    call<Settings>("SaveAPIKey", provider, key),
  deleteAPIKey: (provider: string) => call<Settings>("DeleteAPIKey", provider),
  setCredentialStorage: (mode: string) =>
    call<Settings>("SetCredentialStorage", mode),
  copyAPIKey: (provider: string) => call<void>("CopyAPIKey", provider),
  diagnostics: () => call<DiagnosticsReport>("Diagnostics"),
  copyDiagnostics: () => call<void>("CopyDiagnostics"),
  workspace: (name: string) => call<void>("SelectWorkspace", name),
  status: () => call<Status>("Status"),
  microphones: () => call<AudioSource[]>("Microphones"),
  localModels: () => call<LocalModel[]>("LocalModels"),
  installLocalModel: (id: string) => call<void>("InstallLocalModel", id),
  resumeLocalModel: (id: string) => call<void>("ResumeLocalModel", id),
  retryLocalModel: (id: string) => call<void>("RetryLocalModel", id),
  pauseLocalModel: (id: string) => call<void>("PauseLocalModel", id),
  cancelLocalModel: (id: string) => call<void>("CancelLocalModel", id),
  removeLocalModel: (id: string) => call<void>("RemoveLocalModel", id),
  startTest: (id: string) => call<void>("StartMicrophoneTest", id),
  stopTest: () => call<void>("StopMicrophoneTest"),
  toggle: () => call<void>("Toggle"),
  logs: (outcome: string, offset: number) =>
    call<LogPage>("Logs", outcome, offset),
  exportLogs: (outcome: string) => call<void>("ExportLogs", outcome),
  clearLogs: () => call<void>("ClearLogs"),
  frontendReady: () => call<boolean>("FrontendReady"),
  requestHide: () => call<void>("RequestHide"),
  quit: () => call<void>("RequestQuit"),
  completeWindowAction: (action: "hide" | "quit") =>
    call<void>("CompleteWindowAction", action),
  info: () => call<DesktopInfo>("Info"),
  preferences: (prefs: Preferences) => call<void>("SavePreferences", prefs),
};
