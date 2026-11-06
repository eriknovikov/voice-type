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
}
export interface Settings {
  revision: string;
  profiles: ProfileSummary[];
  active_profile: string;
  port: number;
  microphone: string;
  vocabulary: VocabularyList[];
  workspace: string;
  notice?: string;
}
export interface SettingsUpdate {
  revision: string;
  port?: number;
  microphone?: string;
  vocabulary?: VocabularyList[];
}
export interface ProfileUpdate {
  revision: string;
  id?: string;
  name: string;
  provider: string;
  api_key?: string;
}
export interface Status {
  state: string;
  error: string;
  port: number;
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
    `github.com/eriknovikov/voice-type/desktop.Bridge.${method}`,
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
