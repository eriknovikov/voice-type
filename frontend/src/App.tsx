import React, {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import {
  BookOpen,
  FileText,
  Mic,
  Keyboard,
  RefreshCw,
  Settings2,
  Download,
  KeyRound,
  CircleHelp,
  X,
} from "lucide-react";
import { Events } from "@wailsio/runtime";
import {
  api,
  type Status,
  type DesktopInfo,
  type AudioSource,
  type LocalModel,
  type LogPage,
  type Preferences,
  type ProfileSummary,
  type VocabularyList,
} from "./api";
import "./style.css";
import { MicrophoneMeter } from "./MicrophoneMeter";
import { Dropdown } from "./Dropdown";
import { ShortcutPicker, flushShortcutCapture } from "./ShortcutPicker";
import { VocabularyEditor } from "./VocabularyEditor";
import { ProfileEditor } from "./ProfileEditor";
import { Profiles } from "./Profiles";
import { LogViewer } from "./LogViewer";
import { ConfirmDialog } from "./ui";
import { SettingsStore } from "./settingsStore";
import { LocalModels } from "./LocalModels";
import { APIKeys } from "./APIKeys";
import { FAQ } from "./FAQ";
import { Diagnostics } from "./Diagnostics";

type Tab =
  | "settings"
  | "local-models"
  | "api-keys"
  | "audio"
  | "shortcuts"
  | "vocabulary"
  | "logs"
  | "faq";
const tabs = [
  { id: "settings", label: "General", icon: Settings2 },
  { id: "local-models", label: "Local Models", icon: Download },
  { id: "api-keys", label: "API Keys", icon: KeyRound },
  { id: "audio", label: "Audio", icon: Mic },
  { id: "shortcuts", label: "Shortcuts", icon: Keyboard },
  { id: "vocabulary", label: "Vocabulary", icon: BookOpen },
  { id: "logs", label: "Logs", icon: FileText },
  { id: "faq", label: "FAQ", icon: CircleHelp },
] as const;
const errorText = (error: unknown) =>
  error instanceof Error ? error.message : String(error);
const eventValue = (data: unknown): unknown =>
  Array.isArray(data) ? data[0] : data;
const sameInfo = (a: DesktopInfo | null, b: DesktopInfo) =>
  a !== null &&
  a.last_shortcut_at === b.last_shortcut_at &&
  a.tray_available === b.tray_available &&
  a.version === b.version &&
  a.shortcut_error === b.shortcut_error &&
  a.preferences_error === b.preferences_error &&
  a.notice === b.notice &&
  a.preferences.shortcut === b.preferences.shortcut &&
  a.preferences.autostart === b.preferences.autostart &&
  a.preferences.setup_complete === b.preferences.setup_complete;
export function App() {
  const [store] = useState(() => new SettingsStore(api.settings, api.save));
  const {
    settings,
    status: saveStatus,
    error: saveError,
  } = useSyncExternalStore(store.subscribe, store.getSnapshot);
  const [tab, setTab] = useState<Tab>("settings");
  const [info, setInfo] = useState<DesktopInfo | null>(null);
  const [status, setStatus] = useState<Status>({
    state: "disconnected",
    error: "",
    level: 0,
  });
  const [sources, setSources] = useState<AudioSource[]>([]);
  const [localModels, setLocalModels] = useState<LocalModel[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState("general");
  const [visible, setVisible] = useState(false);
  const [logPage, setLogPage] = useState<LogPage | null>(null);
  const [logOffset, setLogOffset] = useState(0);
  const [logReload, setLogReload] = useState(0);
  const [outcome, setOutcome] = useState("all");
  const [editor, setEditor] = useState<{
    profile: ProfileSummary | null;
  } | null>(null);
  const [dismissedNotice, setDismissedNotice] = useState("");
  const [diagnosticsRequest, setDiagnosticsRequest] = useState(0);
  const prefsTail = useRef<Promise<unknown>>(Promise.resolve());
  const profileGuard = useRef<(() => Promise<boolean>) | null>(null);
  const windowActionVersion = useRef(0);
  const [showSaved, setShowSaved] = useState(false);
  useEffect(() => {
    setShowSaved(saveStatus === "saved");
    if (saveStatus !== "saved") return;
    const timer = setTimeout(() => setShowSaved(false), 1800);
    return () => clearTimeout(timer);
  }, [saveStatus]);
  const [confirm, setConfirm] = useState<{
    title: string;
    description: string;
    label: string;
    run: () => Promise<void>;
  } | null>(null);
  const run = async (operation: () => Promise<void>) => {
    setBusy(true);
    setError("");
    try {
      await operation();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const savePreferences = (patch: Partial<Preferences>) => {
    const result = prefsTail.current
      .catch(() => {})
      .then(async () => {
        const current = await api.info();
        await api.preferences({
          ...current.preferences,
          ...patch,
          setup_complete: true,
        });
        setInfo(await api.info());
      });
    prefsTail.current = result;
    return result;
  };
  useEffect(() => {
    void store.load().catch((e) => setError(errorText(e)));
    void api
      .localModels()
      .then(setLocalModels)
      .catch(() => {});
    const offError = Events.On("operation-error", (event) =>
      setError(String(eventValue(event.data))),
    );
    const offWorkspace = Events.On("workspace-changed", (event) =>
      store.updateWorkspace(String(eventValue(event.data))),
    );
    const offCapture = Events.On("cancel-shortcut-capture", () =>
      window.dispatchEvent(new Event("voice-type-cancel-capture")),
    );
    const offNavigation = Events.On("navigate", (event) => {
      if (String(eventValue(event.data)) === "settings") {
        void store
          .flush()
          .then(async () => {
            await flushShortcutCapture();
            await prefsTail.current;
            setTab("settings");
          })
          .catch((e) => setError(errorText(e)));
      }
    });
    const offFlush = Events.On("flush-settings", (event) => {
      const data = eventValue(event.data) as { action?: string } | undefined;
      if (data?.action !== "hide" && data?.action !== "quit") return;
      const action = data.action;
      const version = ++windowActionVersion.current;
      void (async () => {
        try {
          if (profileGuard.current && !(await profileGuard.current())) return;
          if (version !== windowActionVersion.current) return;
          // Native tray/close actions also commit blur-validated fields.
          if (document.activeElement instanceof HTMLElement)
            document.activeElement.blur();
          await store.flush();
          await flushShortcutCapture();
          await prefsTail.current;
          if (version !== windowActionVersion.current) return;
          await api.completeWindowAction(action);
        } catch (e) {
          setError(errorText(e));
        }
      })();
    });
    const offVisibility = Events.On("window-visibility", (event) =>
      setVisible(Boolean(eventValue(event.data))),
    );
    void api
      .frontendReady()
      .then(setVisible)
      .catch((e) => {
        // Without a native answer, keep the UI observable rather than frozen.
        setVisible(true);
        setError(errorText(e));
      });
    return () => {
      offError();
      offWorkspace();
      offCapture();
      offNavigation();
      offFlush();
      offVisibility();
      store.dispose();
    };
  }, [store]);
  useEffect(() => {
    if (!visible) return;
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    let lastNotice = "";
    let lastNoticeVersion: number | undefined;
    const poll = async () => {
      try {
        const [next, freshInfo] = await Promise.all([api.status(), api.info()]);
        if (alive) {
          setStatus((previous) =>
            previous.state === next.state &&
            previous.error === next.error &&
            previous.notice === next.notice &&
            previous.notice_version === next.notice_version
              ? previous
              : next,
          );
          setInfo((previous) =>
            sameInfo(previous, freshInfo) ? previous : freshInfo,
          );
          if (
            next.notice &&
            (next.notice !== lastNotice ||
              next.notice_version !== lastNoticeVersion)
          ) {
            lastNotice = next.notice;
            lastNoticeVersion = next.notice_version;
            setDismissedNotice("");
            void store.refresh().catch((e) => setError(errorText(e)));
          }
        }
      } catch {
        if (alive)
          setStatus((current) => ({ ...current, state: "disconnected" }));
      }
      if (alive) timer = setTimeout(poll, 500);
    };
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [store, visible]);
  const refreshSources = async () => {
    setSources(await api.microphones());
    // Missing-device fallback can update the config; merge it with pending edits.
    await store.refresh();
  };
  useEffect(() => {
    if (tab === "audio") void run(refreshSources);
    return () => {
      if (tab === "audio") void api.stopTest().catch(() => {});
    };
  }, [tab]);
  useEffect(() => {
    if (tab !== "logs") {
      setLogPage(null);
      return;
    }
    if (!visible) return;
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const page = await api.logs(outcome, logOffset);
        if (alive && !page.unchanged) setLogPage(page);
      } catch (e) {
        if (alive) setError(errorText(e));
      }
      if (alive) timer = setTimeout(poll, 2000);
    };
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [tab, visible, outcome, logOffset, logReload]);
  useEffect(() => {
    if (tab !== "local-models" || !visible) return;
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const models = await api.localModels();
        if (alive)
          setLocalModels((previous) =>
            JSON.stringify(previous) === JSON.stringify(models)
              ? previous
              : models,
          );
      } catch (e) {
        if (alive) setError(errorText(e));
      }
      if (alive) timer = setTimeout(poll, 700);
    };
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [tab, visible]);
  const navigate = (next: Tab) =>
    void run(async () => {
      await store.flush();
      await flushShortcutCapture();
      await prefsTail.current;
      setTab(next);
    });
  const openProfileEditor = (profile: ProfileSummary | null) => {
    setEditor({ profile });
    void api
      .localModels()
      .then(setLocalModels)
      .catch(() => {});
  };
  const updateVocabulary = (lists: VocabularyList[], debounce = false) =>
    store.patch({ vocabulary: lists }, debounce);
  const list = settings?.vocabulary.find((item) => item.name === selected);
  const renameVocabulary = (name: string): boolean => {
    const value = name.trim();
    if (
      !settings ||
      !value ||
      settings.vocabulary.some(
        (item) =>
          item.name !== selected &&
          item.name.toLowerCase() === value.toLowerCase(),
      )
    )
      return false;
    if (value !== selected) {
      updateVocabulary(
        settings.vocabulary.map((item) =>
          item.name === selected
            ? {
                ...item,
                original_name: item.original_name ?? item.name,
                name: value,
              }
            : item,
        ),
      );
      setSelected(value);
    }
    return true;
  };
  const notice = [
    ...new Set([settings?.notice, status.notice, info?.notice].filter(Boolean)),
  ].join(" ");
  const visibleError =
    error || saveError || info?.preferences_error || status.error;
  return (
    <div className="app">
      <a className="skip-link" href="#main-content">
        Skip to settings
      </a>
      <div className="window-drag" aria-hidden="true" />
      <aside>
        <div className="brand">
          <img src="/icon.png" alt="" width="32" height="32" />
          <span>Voice Type</span>
        </div>
        <nav aria-label="Main navigation">
          {tabs.map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              className={tab === id ? "active" : ""}
              aria-current={tab === id ? "page" : undefined}
              onClick={() => navigate(id)}
            >
              <Icon size={18} aria-hidden="true" />
              {label}
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <div className={`save-status ${saveStatus}`}>
            <span role="status">
              {saveStatus === "saving"
                ? "Saving…"
                : saveStatus === "saved" && showSaved
                  ? "Saved"
                  : saveStatus === "error"
                    ? "Not saved"
                    : ""}
            </span>
            {saveStatus === "error" && (
              <button
                className="text-button"
                aria-label="Retry save"
                onClick={() => void run(store.retry)}
              >
                Retry
              </button>
            )}
          </div>
          <small>v{info?.version ?? "—"}</small>
        </div>
      </aside>
      <main id="main-content" tabIndex={-1} aria-busy={busy}>
        <header>
          <h1>{tabs.find((item) => item.id === tab)?.label}</h1>
          <div className="header-actions">
            <span className={`status ${status.state}`} role="status">
              <span className={`dot ${status.state}`} aria-hidden="true" />
              {status.state === "disconnected"
                ? "Offline"
                : status.state === "idle"
                  ? "Idle"
                  : status.state === "checking"
                    ? "Preparing"
                  : status.state === "testing"
                    ? "Testing"
                    : status.state.charAt(0).toUpperCase() +
                      status.state.slice(1)}
            </span>
            <button
              className="window-close icon-button"
              aria-label="Close window"
              title="Hide to background"
              onClick={() =>
                void api.requestHide().catch((e) => setError(errorText(e)))
              }
            >
              <X size={18} aria-hidden="true" />
            </button>
          </div>
        </header>
        {visibleError && (
          <div className="alert error" role="alert">
            <span>{visibleError}</span>
            <span className="alert-actions">
              <button
                className="text-button"
                onClick={() => {
                  setTab("settings");
                  setDiagnosticsRequest((value) => value + 1);
                }}
              >
                Diagnostics
              </button>
              {info?.preferences_error && !error && !saveError ? (
                <button onClick={() => void run(() => savePreferences({}))}>
                  Retry
                </button>
              ) : error ? (
                <button
                  className="icon-button"
                  aria-label="Dismiss error"
                  onClick={() => setError("")}
                >
                  <X size={15} aria-hidden="true" />
                </button>
              ) : null}
            </span>
          </div>
        )}
        {notice && notice !== dismissedNotice && (
          <div className="alert notice" role="status">
            <span>{notice}</span>
            <button
              className="icon-button"
              aria-label="Dismiss notice"
              onClick={() => setDismissedNotice(notice)}
            >
              <X size={15} aria-hidden="true" />
            </button>
          </div>
        )}
        {!settings ? (
          <div className="empty">
            <h2>{error ? "Settings unavailable" : "Loading…"}</h2>
            {error && (
              <button onClick={() => void run(store.load)}>Retry</button>
            )}
          </div>
        ) : (
          <div className="page-content">
            {tab === "settings" && (
              <>
                <Profiles
                  profiles={settings.profiles}
                  active={settings.active_profile}
                  busy={busy}
                  onCreate={() => openProfileEditor(null)}
                  onEdit={(profile) => openProfileEditor(profile)}
                  onSelect={(id) =>
                    void run(() =>
                      store.mutate((revision) =>
                        api.selectProfile(revision, id),
                      ),
                    )
                  }
                />
                {info && (
                  <section className="settings-section">
                    <div className="settings-group">
                      <label
                        className="setting-row switch-row"
                        htmlFor="autostart"
                      >
                        <span>Start at login</span>
                        <span className="setting-control align-end">
                          <input
                            id="autostart"
                            className="switch"
                            type="checkbox"
                            role="switch"
                            checked={info.preferences.autostart}
                            disabled={busy}
                            onChange={(event) => {
                              const enabled = event.target.checked;
                              void run(() =>
                                savePreferences({ autostart: enabled }),
                              );
                            }}
                          />
                        </span>
                      </label>
                    </div>
                    {!info.tray_available && (
                      <p className="quiet section-note">
                        Tray requires the AppIndicator extension.
                      </p>
                    )}
                  </section>
                )}
                <Diagnostics
                  onError={setError}
                  autoRun={diagnosticsRequest}
                />
              </>
            )}
            {tab === "local-models" && (
              <LocalModels
                models={localModels}
                busy={busy}
                onInstall={(id) => void run(() => api.installLocalModel(id))}
                onResume={(id) => void run(() => api.resumeLocalModel(id))}
                onRetry={(id) => void run(() => api.retryLocalModel(id))}
                onPause={(id) => void run(() => api.pauseLocalModel(id))}
                onCancel={(id) => void run(() => api.cancelLocalModel(id))}
                onRemove={(id) => void run(() => api.removeLocalModel(id))}
              />
            )}
            {tab === "api-keys" && (
              <APIKeys
                keys={settings.api_keys}
                storage={settings.credential_storage}
                busy={busy}
                mutate={store.mutateAPIKeys}
                onError={setError}
              />
            )}
            {tab === "audio" && (
              <section className="settings-section">
                <div className="settings-group">
                  <div className="audio-device">
                    <label htmlFor="microphone">Input device</label>
                    <div className="device-control">
                      <Dropdown
                        id="microphone"
                        label="Input device"
                        value={settings.microphone}
                        disabled={busy || status.state === "testing"}
                        onChange={(value) => store.patch({ microphone: value })}
                        options={[
                          { value: "", label: "System default" },
                          ...(settings.microphone &&
                          !sources.some(
                            (source) => source.id === settings.microphone,
                          )
                            ? [
                                {
                                  value: settings.microphone,
                                  label: `Unavailable: ${settings.microphone}`,
                                },
                              ]
                            : []),
                          ...sources.map((source) => ({
                            value: source.id,
                            label: source.name,
                          })),
                        ]}
                      />
                      <button
                        className="icon-button"
                        aria-label="Refresh devices"
                        title="Refresh devices"
                        disabled={busy || status.state === "testing"}
                        onClick={() => void run(refreshSources)}
                      >
                        <RefreshCw size={17} aria-hidden="true" />
                      </button>
                    </div>
                  </div>
                </div>
                <div className="settings-group microphone-test">
                  <div className="section-heading">
                    <h2>Microphone</h2>
                    <button
                      disabled={
                        busy || !["idle", "testing"].includes(status.state)
                      }
                      onClick={() =>
                        void run(async () => {
                          await store.flush();
                          if (status.state === "testing") await api.stopTest();
                          else {
                            await api.startTest(settings.microphone);
                            await store.refresh();
                          }
                          setStatus(await api.status());
                        })
                      }
                    >
                      {status.state === "testing" ? "Stop" : "Test"}
                    </button>
                  </div>
                  <MicrophoneMeter active={status.state === "testing"} />
                  {status.state === "testing" ? (
                    <p className="quiet meter-note" role="status">
                      Listening…
                    </p>
                  ) : status.state === "recording" ? (
                    <p className="quiet meter-note">Stop recording first.</p>
                  ) : status.state === "transcribing" ? (
                    <p className="quiet meter-note">Wait for transcription.</p>
                  ) : null}
                </div>
              </section>
            )}
            {tab === "shortcuts" && info && (
              <section className="settings-section">
                <div className="settings-group shortcut-settings">
                  <ShortcutPicker
                    name="Dictation"
                    value={info.preferences.shortcut}
                    onError={setError}
                    onChange={(shortcut) => savePreferences({ shortcut })}
                  />
                </div>
                {info.shortcut_error && (
                  <div className="alert error" role="alert">
                    {info.shortcut_error}
                  </div>
                )}
              </section>
            )}
            {
              <div hidden={tab !== "vocabulary"}>
                <VocabularyEditor
                  lists={settings.vocabulary}
                  selected={selected}
                  active={settings.workspace}
                  busy={busy}
                  onSelect={(name) =>
                    void run(async () => {
                      await store.flush();
                      setSelected(name);
                    })
                  }
                  onCreate={(name) => {
                    updateVocabulary([
                      ...settings.vocabulary,
                      { name, terms: [] },
                    ]);
                    setSelected(name);
                  }}
                  onRename={renameVocabulary}
                  onTerms={(terms) => {
                    if (list)
                      updateVocabulary(
                        settings.vocabulary.map((item) =>
                          item.name === selected ? { ...item, terms } : item,
                        ),
                        true,
                      );
                  }}
                  onActivate={() =>
                    void run(async () => {
                      await store.flush();
                      const name = selected === "general" ? "" : selected;
                      await api.workspace(name);
                      store.updateWorkspace(name);
                    })
                  }
                  onDelete={() =>
                    setConfirm({
                      title: `Delete ${selected}?`,
                      description: "Its terms will be deleted.",
                      label: "Delete vocabulary",
                      run: async () => {
                        updateVocabulary(
                          settings.vocabulary.filter(
                            (item) => item.name !== selected,
                          ),
                        );
                        setSelected("general");
                        await store.flush();
                      },
                    })
                  }
                />
              </div>
            }
            {tab === "logs" && (
              <LogViewer
                page={logPage}
                outcome={outcome}
                onOutcome={(value) => {
                  setOutcome(value);
                  setLogOffset(0);
                  setLogPage(null);
                }}
                onOffset={(offset) => setLogOffset(Math.max(0, offset))}
                busy={busy}
                onExport={() => void run(() => api.exportLogs(outcome))}
                onClear={() =>
                  setConfirm({
                    title: "Clear logs?",
                    description: "All logs will be deleted.",
                    label: "Clear logs",
                    run: async () => {
                      await api.clearLogs();
                      setLogOffset(0);
                      setLogPage(null);
                      setLogReload((value) => value + 1);
                    },
                  })
                }
              />
            )}
            {tab === "faq" && <FAQ />}
          </div>
        )}
      </main>
      {editor && settings && (
        <ProfileEditor
          profile={editor.profile}
          profiles={settings.profiles}
          apiKeys={settings.api_keys}
          localModels={localModels}
          onManageModels={() => {
            setEditor(null);
            void navigate("local-models");
          }}
          onSave={(update) =>
            store.mutate((revision) =>
              api.upsertProfile({ ...update, revision }),
            )
          }
          onClose={() => setEditor(null)}
          onDelete={() =>
            store.mutate((revision) =>
              api.deleteProfile(revision, editor.profile!.id),
            )
          }
          closeGuard={profileGuard}
        />
      )}
      {confirm && (
        <ConfirmDialog
          title={confirm.title}
          description={confirm.description}
          confirmLabel={confirm.label}
          onConfirm={confirm.run}
          onCancel={() => setConfirm(null)}
        />
      )}
    </div>
  );
}
