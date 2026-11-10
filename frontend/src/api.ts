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
  masked_key: string;
  model_id?: string;
  streaming_mode?: string;
}
export interface Settings {
  revision: string;
  profiles: ProfileSummary[];
  active_profile: string;
  microphone: string;
  vocabulary: VocabularyList[];
  workspace: string;
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
  api_key?: string;
  model_id?: string;
  streaming_mode?: string;
}
export interface LocalModel {
  id: string;
  name: string;
  filename: string;
  bytes: number;
  downloaded_bytes: number;
  progress: number;
  languages: string;
  supports_streaming: boolean;
  vocabulary_support: string;
  license: string;
  license_url: string;
  model_card_url: string;
  engine_revision: string;
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
export interface LogRecord {
  timestamp?: string;
  event: string;
  message?: string;
  provider: string;
  configured_model: string;
  outcome: string;
  error?: string;
  total_ms: number;
  audio_ms: number;
  attempt_count: number;
  attempts: unknown[];
}
const call = <T>(method: string, ...args: unknown[]): Promise<T> =>
  Call.ByName(
    `github.com/eriknovikov/voice-type.Bridge.${method}`,
    ...args,
  );
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
  revealProfileKey: (id: string) => call<string>("RevealProfileKey", id),
  copyProfileKey: (id: string) => call<void>("CopyProfileKey", id),
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
  logs: () => call<LogRecord[]>("Logs"),
  exportLogs: (outcome: string) => call<void>("ExportLogs", outcome),
  clearLogs: () => call<void>("ClearLogs"),
  frontendReady: () => call<void>("FrontendReady"),
  requestHide: () => call<void>("RequestHide"),
  quit: () => call<void>("RequestQuit"),
  completeWindowAction: (action: "hide" | "quit") =>
    call<void>("CompleteWindowAction", action),
  info: () => call<DesktopInfo>("Info"),
  preferences: (prefs: Preferences) => call<void>("SavePreferences", prefs),
};
