import { Call } from "@wailsio/runtime";
export interface VocabularyList {
  original_name?: string;
  name: string;
  terms: string[];
}
export interface Settings {
  revision: string;
  provider: string;
  port: number;
  microphone: string;
  vocabulary: VocabularyList[];
  workspace: string;
  credentials: Record<string, { saved: boolean; environment: boolean }>;
}
export interface SettingsUpdate {
  revision: string;
  provider: string;
  port: number;
  microphone: string;
  vocabulary: VocabularyList[];
  api_keys: Record<string, string>;
}
export interface Status {
  state: string;
  error: string;
  port: number;
  level: number;
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
}
export interface AudioSource {
  id: string;
  name: string;
}
export interface LogRecord {
  timestamp?: string;
  event: string;
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
  recoveryRevision: () => call<string>("RecoveryRevision"),
  resetBrokenSettings: (revision: string) =>
    call<Settings>("ResetBrokenSettings", revision),
  settings: () => call<Settings>("Settings"),
  save: (update: SettingsUpdate) => call<Settings>("SaveSettings", update),
  workspace: (name: string) => call<void>("SelectWorkspace", name),
  status: () => call<Status>("Status"),
  microphones: () => call<AudioSource[]>("Microphones"),
  startTest: (id: string) => call<void>("StartMicrophoneTest", id),
  stopTest: () => call<void>("StopMicrophoneTest"),
  toggle: () => call<void>("Toggle"),
  logs: () => call<LogRecord[]>("Logs"),
  exportLogs: (query: string, outcome: string) =>
    call<void>("ExportLogs", query, outcome),
  clearLogs: () => call<void>("ClearLogs"),
  connect: () => call<void>("Connect"),
  restart: () => call<void>("Restart"),
  quit: () => call<void>("Quit"),
  info: () => call<DesktopInfo>("Info"),
  preferences: (prefs: Preferences) => call<void>("SavePreferences", prefs),
};
