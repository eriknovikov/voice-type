import React, {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import {
  BookOpen,
  Check,
  FileText,
  Mic,
  Keyboard,
  RefreshCw,
  Settings2,
  Trash2,
  X,
  Plus,
  Pencil,
  Circle,
  CheckCircle2,
} from "lucide-react";
import * as Dialog from "@radix-ui/react-dialog";
import { Events } from "@wailsio/runtime";
import {
  api,
  type Status,
  type DesktopInfo,
  type AudioSource,
  type LogRecord,
  type Preferences,
  type ProfileSummary,
  type VocabularyList,
} from "./api";
import "./style.css";
import { MicrophoneMeter } from "./MicrophoneMeter";
import { Dropdown } from "./Dropdown";
import { ShortcutPicker, flushShortcutCapture } from "./ShortcutPicker";
import { VocabularyEditor } from "./VocabularyEditor";
import { ProfileEditor, providerName } from "./ProfileEditor";
import { SettingsStore } from "./settingsStore";

type Tab = "settings" | "audio" | "shortcuts" | "vocabulary" | "logs";
const tabs = [
  { id: "settings", label: "General", icon: Settings2 },
  { id: "audio", label: "Audio", icon: Mic },
  { id: "shortcuts", label: "Keyboard Shortcuts", icon: Keyboard },
  { id: "vocabulary", label: "Vocabulary", icon: BookOpen },
  { id: "logs", label: "Logs", icon: FileText },
] as const;
const errorText = (error: unknown) =>
  error instanceof Error ? error.message : String(error);
const eventValue = (data: unknown): unknown =>
  Array.isArray(data) ? data[0] : data;
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
    port: 3232,
    level: 0,
  });
  const [sources, setSources] = useState<AudioSource[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState("general");
  const [logs, setLogs] = useState<LogRecord[]>([]);
  const [outcome, setOutcome] = useState("all");
  const [editor, setEditor] = useState<{
    profile: ProfileSummary | null;
  } | null>(null);
  const [portDraft, setPortDraft] = useState("3232");
  const [portDirty, setPortDirty] = useState(false);
  const [portError, setPortError] = useState("");
  const [dismissedNotice, setDismissedNotice] = useState("");
  const prefsTail = useRef<Promise<unknown>>(Promise.resolve());
  const [confirm, setConfirm] = useState<{
    title: string;
    description: string;
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
      // Native tray/close actions also commit blur-validated fields.
      if (document.activeElement instanceof HTMLElement)
        document.activeElement.blur();
      void (async () => {
        try {
          await store.flush();
          await flushShortcutCapture();
          await prefsTail.current;
          await api.completeWindowAction(action);
        } catch (e) {
          setError(errorText(e));
        }
      })();
    });
    void api.frontendReady().catch((e) => setError(errorText(e)));
    return () => {
      offError();
      offWorkspace();
      offCapture();
      offNavigation();
      offFlush();
      store.dispose();
    };
  }, [store]);
  useEffect(() => {
    if (settings && !portDirty) setPortDraft(String(settings.port));
  }, [settings?.port, portDirty]);
  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    let lastNotice = "";
    const poll = async () => {
      try {
        const [next, freshInfo] = await Promise.all([api.status(), api.info()]);
        if (alive) {
          setStatus((previous) =>
            previous.state === next.state &&
            previous.error === next.error &&
            previous.port === next.port &&
            previous.notice === next.notice
              ? previous
              : next,
          );
          setInfo(freshInfo);
          if (next.notice && next.notice !== lastNotice) {
            lastNotice = next.notice;
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
  }, [store]);
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
    if (tab !== "logs") return;
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const rows = await api.logs();
        if (alive) setLogs(rows ?? []);
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
  }, [tab]);
  const navigate = (next: Tab) =>
    void run(async () => {
      await store.flush();
      await flushShortcutCapture();
      await prefsTail.current;
      setTab(next);
    });
  const commitPort = () => {
    if (!portDirty) return;
    const value = Number(portDraft);
    if (
      !/^\d+$/.test(portDraft) ||
      !Number.isInteger(value) ||
      value < 1024 ||
      value > 65535
    ) {
      setPortError("Use a port from 1024 to 65535.");
      return;
    }
    setPortError("");
    setPortDirty(false);
    store.patch({ port: value });
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
  const displayed = logs.filter(
    (row) =>
      outcome === "all" ||
      (outcome === "errors" ? row.outcome !== "ok" : row.outcome === "ok"),
  );
  const notice = [
    ...new Set([settings?.notice, status.notice, info?.notice].filter(Boolean)),
  ].join(" ");
  const visibleError =
    error || saveError || info?.preferences_error || status.error;
  return (
    <div className="app">
      <div className="window-drag" aria-hidden="true" />
      <aside>
        <div className="brand">
          <img src="/icon.png" alt="" />
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
              <Icon size={17} />
              {label}
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <small>v{info?.version ?? "—"}</small>
          <span className={`save-status ${saveStatus}`} role="status">
            {saveStatus === "saving"
              ? "Saving…"
              : saveStatus === "saved"
                ? "Saved"
                : saveStatus === "pending"
                  ? "Unsaved"
                  : saveStatus === "error"
                    ? "Not saved"
                    : ""}
          </span>
        </div>
      </aside>
      <main aria-busy={busy}>
        <header>
          <h1>{tabs.find((item) => item.id === tab)?.label}</h1>
          <span className={`status ${status.state}`}>
            <span className={`dot ${status.state}`} />
            {status.state === "disconnected" ? "Offline" : status.state}
          </span>
        </header>
        <button
          className="window-close icon-button"
          aria-label="Close window"
          title="Hide to background"
          onClick={() =>
            void api.requestHide().catch((e) => setError(errorText(e)))
          }
        >
          <X size={17} />
        </button>
        {visibleError && (
          <div className="alert error" role="alert">
            <span>{visibleError}</span>
            {saveError ? (
              <button onClick={() => void run(store.retry)}>Retry save</button>
            ) : info?.preferences_error && !error ? (
              <button onClick={() => void run(() => savePreferences({}))}>
                Retry
              </button>
            ) : error ? (
              <button
                className="icon-button"
                aria-label="Dismiss error"
                onClick={() => setError("")}
              >
                <X size={15} />
              </button>
            ) : null}
          </div>
        )}
        {notice && notice !== dismissedNotice && (
          <div className="alert success" role="status">
            <span>{notice}</span>
            <button
              className="icon-button"
              aria-label="Dismiss notice"
              onClick={() => setDismissedNotice(notice)}
            >
              <X size={15} />
            </button>
          </div>
        )}
        {!settings ? (
          <div className="empty">
            <h2>{error ? "Settings unavailable" : "Loading settings…"}</h2>
            {error && (
              <button onClick={() => void run(store.load)}>Retry</button>
            )}
          </div>
        ) : (
          <div className="page-content">
            {tab === "settings" && (
              <>
                <section className="settings-section">
                  <div className="section-heading profile-section-heading">
                    <h2>Transcription profiles</h2>
                    <button
                      className="text-button"
                      onClick={() => setEditor({ profile: null })}
                    >
                      <Plus size={15} />
                      Add profile
                    </button>
                  </div>
                  <div className="settings-group profile-list">
                    {settings.profiles.length ? (
                      settings.profiles.map((profile) => (
                        <div
                          className={`profile-row ${settings.active_profile === profile.id ? "is-active" : ""}`}
                          key={profile.id}
                        >
                          <button
                            className="profile-select"
                            aria-pressed={
                              settings.active_profile === profile.id
                            }
                            aria-label={`Use ${profile.name}`}
                            disabled={busy}
                            onClick={() =>
                              void run(() =>
                                store.mutate((revision) =>
                                  api.selectProfile(revision, profile.id),
                                ),
                              )
                            }
                          >
                            {settings.active_profile === profile.id ? (
                              <CheckCircle2 size={18} />
                            ) : (
                              <Circle size={18} />
                            )}
                            <span>
                              <strong>{profile.name}</strong>
                              <small>
                                {providerName(profile.provider)}
                                <span className="profile-key">
                                  {profile.masked_key}
                                </span>
                              </small>
                            </span>
                          </button>
                          <div className="actions">
                            <button
                              className="icon-button"
                              aria-label={`Edit ${profile.name}`}
                              title="Edit profile"
                              onClick={() => setEditor({ profile })}
                            >
                              <Pencil size={15} />
                            </button>
                            <button
                              className="icon-button danger-text"
                              aria-label={`Delete ${profile.name}`}
                              title="Delete profile"
                              onClick={() =>
                                setConfirm({
                                  title: `Delete ${profile.name}?`,
                                  description:
                                    settings.active_profile === profile.id
                                      ? "Choose another profile before your next dictation."
                                      : "This profile and its API key will be removed.",
                                  run: () =>
                                    store.mutate((revision) =>
                                      api.deleteProfile(revision, profile.id),
                                    ),
                                })
                              }
                            >
                              <Trash2 size={15} />
                            </button>
                          </div>
                        </div>
                      ))
                    ) : (
                      <div className="profile-empty">
                        <p>Add a profile to start dictating.</p>
                        <button
                          className="primary"
                          onClick={() => setEditor({ profile: null })}
                        >
                          <Plus size={15} />
                          Add profile
                        </button>
                      </div>
                    )}
                  </div>
                  {!!settings.profiles.length && !settings.active_profile && (
                    <p className="quiet section-note">
                      Select a profile to start dictating.
                    </p>
                  )}
                </section>
                {info && (
                  <section className="settings-section">
                    <div className="settings-group">
                      <div className="setting-row">
                        <div className="setting-label">
                          <label htmlFor="autostart">Start at login</label>
                        </div>
                        <div className="setting-control align-end">
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
                        </div>
                      </div>
                    </div>
                    {!info.tray_available && (
                      <p className="quiet section-note">
                        GNOME needs the AppIndicator extension to show the tray
                        icon.
                      </p>
                    )}
                  </section>
                )}
                <details className="advanced">
                  <summary>Advanced</summary>
                  <div className="settings-group">
                    <div className="setting-row">
                      <div className="setting-label">
                        <label htmlFor="port">Local control port</label>
                        <p>Restart Voice Type to apply.</p>
                      </div>
                      <div className="setting-control">
                        <input
                          id="port"
                          type="number"
                          min="1024"
                          max="65535"
                          value={portDraft}
                          aria-invalid={!!portError}
                          onChange={(event) => {
                            setPortDraft(event.target.value);
                            setPortDirty(true);
                            setPortError("");
                          }}
                          onBlur={commitPort}
                        />
                        {portError && (
                          <span className="field-error" role="alert">
                            {portError}
                          </span>
                        )}
                      </div>
                    </div>
                  </div>
                </details>
              </>
            )}
            {tab === "audio" && (
              <section className="settings-section">
                <div className="settings-group">
                  <div className="setting-row">
                    <div className="setting-label">
                      <label htmlFor="microphone">Input device</label>
                    </div>
                    <div className="setting-control">
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
                        className="text-button"
                        disabled={busy}
                        onClick={() => void run(refreshSources)}
                      >
                        <RefreshCw size={13} />
                        Refresh devices
                      </button>
                    </div>
                  </div>
                  <div className="setting-row">
                    <div className="setting-label">
                      <h3>Input level</h3>
                    </div>
                    <div className="setting-control">
                      <MicrophoneMeter active={status.state === "testing"} />
                      <div className="meter-actions">
                        <button
                          disabled={
                            busy || !["idle", "testing"].includes(status.state)
                          }
                          onClick={() =>
                            void run(async () => {
                              await store.flush();
                              if (status.state === "testing")
                                await api.stopTest();
                              else {
                                await api.startTest(settings.microphone);
                                await store.refresh();
                              }
                              setStatus(await api.status());
                            })
                          }
                        >
                          {status.state === "testing"
                            ? "Stop test"
                            : "Test microphone"}
                        </button>
                        <span className="quiet">Stops after 60 seconds</span>
                      </div>
                    </div>
                  </div>
                </div>
              </section>
            )}
            {tab === "shortcuts" && info && (
              <section className="settings-section">
                <div className="settings-group shortcut-settings">
                  <ShortcutPicker
                    name="Transcription toggle"
                    value={info.preferences.shortcut}
                    onError={setError}
                    onChange={(shortcut) => savePreferences({ shortcut })}
                  />
                </div>
                <p className="quiet section-note">
                  Click a shortcut and press the keys. Click elsewhere to save;
                  Escape disables it.
                </p>
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
                      description:
                        "This vocabulary and its terms will be removed.",
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
              <>
                <div className="log-tools">
                  <Dropdown
                    label="Filter logs"
                    value={outcome}
                    onChange={setOutcome}
                    options={[
                      { value: "all", label: "All outcomes" },
                      { value: "errors", label: "Errors" },
                      { value: "ok", label: "Successful" },
                    ]}
                  />
                  <button
                    disabled={!displayed.length}
                    onClick={() => void run(() => api.exportLogs(outcome))}
                  >
                    Export
                  </button>
                  <button
                    className="icon-button"
                    title="Clear logs"
                    aria-label="Clear logs"
                    disabled={!logs.length}
                    onClick={() =>
                      setConfirm({
                        title: "Clear logs?",
                        description:
                          "Current and rotated logs will be removed.",
                        run: async () => {
                          await api.clearLogs();
                          setLogs([]);
                        },
                      })
                    }
                  >
                    <Trash2 size={16} />
                  </button>
                </div>
                <section className="log-list">
                  {displayed.length ? (
                    displayed.map((row, index) => {
                      const dictation =
                        row.event.startsWith("dictation.") ||
                        row.event === "dictation";
                      return (
                        <details key={`${row.timestamp}-${index}`}>
                          <summary>
                            <span
                              className={`log-icon ${row.outcome === "ok" ? "ok" : "failed"}`}
                            >
                              {row.outcome === "ok" ? (
                                <Check size={14} />
                              ) : (
                                <X size={14} />
                              )}
                            </span>
                            <span>
                              <strong>
                                {(dictation ? row.outcome : row.event).replace(
                                  /[_.]/g,
                                  " ",
                                )}
                              </strong>
                              <small>
                                {row.timestamp
                                  ? new Date(row.timestamp).toLocaleString()
                                  : "Time unavailable"}
                                {row.provider
                                  ? ` · ${providerName(row.provider)}`
                                  : ""}
                              </small>
                            </span>
                            {row.total_ms > 0 && (
                              <span className="duration">
                                {(row.total_ms / 1000).toFixed(1)}s
                              </span>
                            )}
                          </summary>
                          <dl>
                            {dictation && (
                              <>
                                <dt>Model</dt>
                                <dd>{row.configured_model || "—"}</dd>
                                <dt>Audio</dt>
                                <dd>
                                  {(row.audio_ms / 1000).toFixed(1)} seconds
                                </dd>
                                <dt>Attempts</dt>
                                <dd>{row.attempt_count}</dd>
                              </>
                            )}
                            <dt>Outcome</dt>
                            <dd>{row.outcome.replaceAll("_", " ")}</dd>
                            {row.message && (
                              <>
                                <dt>Details</dt>
                                <dd>{row.message}</dd>
                              </>
                            )}
                            {row.error && (
                              <>
                                <dt>Error</dt>
                                <dd>{row.error}</dd>
                              </>
                            )}
                          </dl>
                          {row.attempts?.length > 0 && (
                            <pre>{JSON.stringify(row.attempts, null, 2)}</pre>
                          )}
                        </details>
                      );
                    })
                  ) : (
                    <div className="empty">
                      <FileText size={26} />
                      <h2>
                        {logs.length ? "No matching logs" : "No logs yet"}
                      </h2>
                    </div>
                  )}
                </section>
              </>
            )}
          </div>
        )}
      </main>
      {editor && settings && (
        <ProfileEditor
          profile={editor.profile}
          profiles={settings.profiles}
          onSave={(update) =>
            store.mutate((revision) =>
              api.upsertProfile({ ...update, revision }),
            )
          }
          onClose={() => setEditor(null)}
        />
      )}
      <Dialog.Root
        open={!!confirm}
        onOpenChange={(open) => {
          if (!open) setConfirm(null);
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content className="dialog">
            <Dialog.Title>{confirm?.title}</Dialog.Title>
            <Dialog.Description>{confirm?.description}</Dialog.Description>
            <div className="dialog-actions">
              <Dialog.Close asChild>
                <button>Cancel</button>
              </Dialog.Close>
              <button
                className="danger"
                disabled={busy}
                onClick={() => {
                  const action = confirm?.run;
                  setConfirm(null);
                  if (action) void run(action);
                }}
              >
                Confirm
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
