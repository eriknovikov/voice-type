import React, { useEffect, useState } from "react";
import {
  AudioLines,
  BookOpen,
  Check,
  FileText,
  Mic,
  Keyboard,
  RefreshCw,
  Settings2,
  Trash2,
  X,
} from "lucide-react";
import * as Dialog from "@radix-ui/react-dialog";
import { Events, Window } from "@wailsio/runtime";
import {
  api,
  type Settings,
  type Status,
  type DesktopInfo,
  type AudioSource,
  type LogRecord,
} from "./api";
import "./style.css";
import { MicrophoneMeter } from "./MicrophoneMeter";
import { Dropdown } from "./Dropdown";
import { ShortcutPicker } from "./ShortcutPicker";
import { VocabularyEditor } from "./VocabularyEditor";

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
export function App() {
  const [tab, setTab] = useState<Tab>("settings");
  const [settings, setSettings] = useState<Settings | null>(null);
  const [info, setInfo] = useState<DesktopInfo | null>(null);
  const [status, setStatus] = useState<Status>({
    state: "disconnected",
    error: "",
    port: 3232,
    level: 0,
  });
  const [sources, setSources] = useState<AudioSource[]>([]);
  const [keys, setKeys] = useState<Record<string, string>>({});
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState("general");
  const [logs, setLogs] = useState<LogRecord[]>([]);
  const [query, setQuery] = useState("");
  const [outcome, setOutcome] = useState("all");
  const [confirm, setConfirm] = useState<{
    title: string;
    description: string;
    run: () => Promise<void>;
  } | null>(null);
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await fn();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  const load = async () => {
    const [s, i] = await Promise.all([api.settings(), api.info()]);
    if (!s.vocabulary.some((v) => v.name === "general"))
      s.vocabulary.unshift({ name: "general", terms: [] });
    setSettings(s);
    setSelected((current) =>
      s.vocabulary.some((item) => item.name === current) ? current : "general",
    );
    setInfo(i);
    setKeys({});
    setDirty(false);
  };
  useEffect(() => {
    void run(async () => {
      await load();
    });
    const off = Events.On("operation-error", (e) => setError(String(e.data)));
    const offWorkspace = Events.On("workspace-changed", (e) =>
      setSettings((current) =>
        current ? { ...current, workspace: String(e.data) } : current,
      ),
    );
    const offCapture = Events.On("cancel-shortcut-capture", () =>
      window.dispatchEvent(new Event("voice-type-cancel-capture")),
    );
    const offNavigation = Events.On("navigate", (e) => {
      if (String(e.data) === "settings") setTab("settings");
    });
    return () => {
      off();
      offWorkspace();
      offCapture();
      offNavigation();
    };
  }, []);
  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const next = await api.status();
        if (alive)
          setStatus((previous) =>
            previous.state === next.state &&
            previous.error === next.error &&
            previous.port === next.port
              ? previous
              : next,
          );
        const freshInfo = await api.info();
        if (alive)
          setInfo((current) =>
            current
              ? current.shortcut_error === freshInfo.shortcut_error &&
                current.last_shortcut_at === freshInfo.last_shortcut_at &&
                current.tray_available === freshInfo.tray_available
                ? current
                : {
                    ...current,
                    shortcut_error: freshInfo.shortcut_error,
                    last_shortcut_at: freshInfo.last_shortcut_at,
                    tray_available: freshInfo.tray_available,
                  }
              : freshInfo,
          );
      } catch {
        if (alive) setStatus((s) => ({ ...s, state: "disconnected" }));
      }
      if (alive) timer = setTimeout(poll, 500);
    };
    void poll();
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, []);
  useEffect(() => {
    if (tab === "audio")
      void run(async () => setSources(await api.microphones()));
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
  const patch = (value: Partial<Settings>) => {
    setSettings((s) => (s ? { ...s, ...value } : s));
    setDirty(true);
  };
  const save = async () => {
    if (!settings) return;
    const saved = await api.save({
      revision: settings.revision,
      provider: settings.provider,
      port: settings.port,
      microphone: settings.microphone,
      vocabulary: settings.vocabulary,
      api_keys: keys,
    });
    setSettings(saved);
    setSelected((current) => current.trim());
    setKeys({});
    setDirty(false);
    setNotice("Saved. Changes apply to the next dictation.");
  };
  const navigate = (next: Tab) => {
    setTab(next);
    setNotice("");
  };
  const list = settings?.vocabulary.find((v) => v.name === selected);
  const updateList = (name: string, terms: string[]) => {
    if (!settings) return;
    patch({
      vocabulary: settings.vocabulary.map((v) =>
        v.name === selected
          ? { ...v, original_name: v.original_name ?? v.name, name, terms }
          : v,
      ),
    });
    setSelected(name);
  };
  const displayed = logs.filter(
    (row) =>
      (outcome === "all" ||
        (outcome === "errors" ? row.outcome !== "ok" : row.outcome === "ok")) &&
      JSON.stringify(row).toLowerCase().includes(query.toLowerCase()),
  );
  const exportLogs = () =>
    void run(async () => {
      await api.exportLogs(query, outcome);
    });
  const saveShortcut = async (value: string) => {
    const current = await api.info();
    await api.preferences({
      ...current.preferences,
      shortcut: value,
      setup_complete: true,
    });
    setInfo((state) =>
      state
        ? {
            ...state,
            shortcut_error: "",
            preferences: {
              ...state.preferences,
              shortcut: value,
              setup_complete: true,
            },
          }
        : state,
    );
  };
  const credential = settings?.credentials[settings.provider];
  const descriptions: Record<Tab, string> = {
    settings: "Provider, API key, and desktop preferences.",
    audio: "Choose your microphone and check the input level.",
    shortcuts: "Set the keys to start and stop recording.",
    vocabulary: "Add names and terms to improve transcription.",
    logs: "Review dictation activity and errors. Transcribed text is not stored.",
  };
  const activateVocabulary = () =>
    void run(async () => {
      if (dirty) await save();
      const name = selected.trim() === "general" ? "" : selected.trim();
      await api.workspace(name);
      setSettings((current) =>
        current ? { ...current, workspace: name } : current,
      );
      setNotice("Vocabulary updated.");
    });
  return (
    <div className="app">
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
        </div>
      </aside>
      <main aria-busy={busy}>
        <header>
          <div>
            <h1>{tabs.find((item) => item.id === tab)?.label}</h1>
            <p>{descriptions[tab]}</p>
          </div>
          <span className={`status ${status.state}`}>
            <span className={`dot ${status.state}`} />
            {status.state === "disconnected" ? "Offline" : status.state}
          </span>
        </header>
        <button
          className="window-close icon-button"
          aria-label="Close window"
          title="Hide to background"
          onClick={() => void Window.Close()}
        >
          <X size={17} />
        </button>
        {error && (
          <div className="alert error" role="alert">
            <span>{error}</span>
            <button
              className="icon-button"
              aria-label="Dismiss error"
              onClick={() => setError("")}
            >
              <X size={15} />
            </button>
          </div>
        )}
        {notice && (
          <div className="alert success" role="status">
            <Check size={15} />
            {notice}
          </div>
        )}
        {!settings ? (
          <div className="empty">
            <AudioLines size={28} />
            <h2>Connecting</h2>
            <p>Loading settings.</p>
            {error && status.state !== "disconnected" && (
              <button
                onClick={() =>
                  void run(async () => {
                    const revision = await api.recoveryRevision();
                    setConfirm({
                      title: "Reset unreadable configuration?",
                      description:
                        "A private backup will preserve your original file. You will need to enter your keys and vocabulary again.",
                      run: async () => {
                        await api.resetBrokenSettings(revision);
                        await load();
                        setNotice(
                          "Configuration reset. A backup of the original file was saved.",
                        );
                      },
                    });
                  })
                }
              >
                Back up and reset settings
              </button>
            )}
          </div>
        ) : (
          <div className="page-content">
            {tab === "settings" && (
              <>
                <section className="settings-section">
                  <div className="section-heading">
                    <h2>Transcription</h2>
                    <p>Use your OpenAI or OpenRouter account.</p>
                  </div>
                  <div className="settings-group">
                    <div className="setting-row">
                      <div className="setting-label">
                        <label htmlFor="provider">Provider</label>
                        <p>Used for speech-to-text requests.</p>
                      </div>
                      <div className="setting-control">
                        <Dropdown
                          id="provider"
                          label="Provider"
                          value={settings.provider}
                          disabled={busy}
                          onChange={(value) => patch({ provider: value })}
                          options={[
                            { value: "openai", label: "OpenAI" },
                            { value: "openrouter", label: "OpenRouter" },
                          ]}
                        />
                      </div>
                    </div>
                    <div className="setting-row">
                      <div className="setting-label">
                        <label htmlFor="key">API key</label>
                        <p>
                          {credential?.environment
                            ? "An environment variable overrides this key."
                            : credential?.saved
                              ? "A key is saved for this provider."
                              : "Required to transcribe audio."}
                        </p>
                      </div>
                      <div className="setting-control">
                        <input
                          id="key"
                          type="password"
                          autoComplete="off"
                          spellCheck={false}
                          disabled={busy}
                          value={keys[settings.provider] ?? ""}
                          placeholder={
                            credential?.saved
                              ? "Replace saved key"
                              : "Paste your API key"
                          }
                          onChange={(event) => {
                            setKeys({
                              ...keys,
                              [settings.provider]: event.target.value,
                            });
                            setDirty(true);
                          }}
                        />
                        {credential?.saved && (
                          <button
                            className="text-button danger-text"
                            disabled={busy}
                            onClick={() => {
                              setKeys({ ...keys, [settings.provider]: "" });
                              setDirty(true);
                              setNotice("Key will be removed when you save.");
                            }}
                          >
                            Remove saved key
                          </button>
                        )}
                      </div>
                    </div>
                  </div>
                </section>
                {info && (
                  <section className="settings-section">
                    <div className="section-heading">
                      <h2>Desktop</h2>
                    </div>
                    <div className="settings-group">
                      <div className="setting-row">
                        <div className="setting-label">
                          <label htmlFor="autostart">Start at login</label>
                          <p>Run Voice Type in the background.</p>
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
                              void run(async () => {
                                const saved = await api.info();
                                await api.preferences({
                                  ...saved.preferences,
                                  autostart: enabled,
                                  setup_complete: true,
                                });
                                setInfo((current) =>
                                  current
                                    ? {
                                        ...current,
                                        preferences: {
                                          ...current.preferences,
                                          autostart: enabled,
                                        },
                                      }
                                    : current,
                                );
                              });
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
                        <p>
                          Save, then quit and reopen Voice Type to change the
                          port.
                        </p>
                      </div>
                      <div className="setting-control">
                        <input
                          id="port"
                          type="number"
                          min="1024"
                          max="65535"
                          value={settings.port}
                          disabled={busy}
                          onChange={(event) =>
                            patch({ port: Number(event.target.value) })
                          }
                        />
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
                      <p>Choose a microphone or use the system default.</p>
                    </div>
                    <div className="setting-control">
                      <Dropdown
                        id="microphone"
                        label="Input device"
                        value={settings.microphone}
                        disabled={busy || status.state === "testing"}
                        onChange={(value) => patch({ microphone: value })}
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
                        onClick={() =>
                          void run(async () =>
                            setSources(await api.microphones()),
                          )
                        }
                      >
                        <RefreshCw size={13} />
                        Refresh devices
                      </button>
                    </div>
                  </div>
                  <div className="setting-row">
                    <div className="setting-label">
                      <h3>Input level</h3>
                      <p>The test stays on this device.</p>
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
                              if (status.state === "testing") {
                                await api.stopTest();
                                setStatus((current) => ({
                                  ...current,
                                  state: "idle",
                                  level: 0,
                                }));
                              } else {
                                await api.startTest(settings.microphone);
                                setStatus((current) => ({
                                  ...current,
                                  state: "testing",
                                  level: 0,
                                }));
                              }
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
                    onChange={saveShortcut}
                  />
                </div>
                <p className="quiet section-note">
                  Click a shortcut, press the keys, then click elsewhere to
                  save. Escape disables it.
                </p>
                {info.shortcut_error && (
                  <div className="alert error" role="alert">
                    {info.shortcut_error}
                  </div>
                )}
              </section>
            )}
            {tab === "vocabulary" && (
              <VocabularyEditor
                lists={settings.vocabulary}
                selected={selected}
                active={settings.workspace}
                busy={busy}
                onSelect={setSelected}
                onCreate={(name) => {
                  patch({
                    vocabulary: [...settings.vocabulary, { name, terms: [] }],
                  });
                  setSelected(name);
                }}
                onRename={(name) => updateList(name, list?.terms ?? [])}
                onTerms={(terms) => updateList(selected, terms)}
                onActivate={activateVocabulary}
                onDelete={() =>
                  setConfirm({
                    title: `Delete ${selected}?`,
                    description:
                      "This vocabulary and its terms will be removed when you save.",
                    run: async () => {
                      patch({
                        vocabulary: settings.vocabulary.filter(
                          (item) => item.name !== selected,
                        ),
                      });
                      setSelected("general");
                    },
                  })
                }
              />
            )}
            {tab === "logs" && (
              <>
                <div className="log-tools">
                  <input
                    aria-label="Search logs"
                    placeholder="Search logs"
                    value={query}
                    onChange={(event) => setQuery(event.target.value)}
                  />
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
                  <button disabled={!displayed.length} onClick={exportLogs}>
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
                          "Current and rotated logs will be removed from this device.",
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
                    displayed.map((row, index) => (
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
                            <strong>{row.outcome.replaceAll("_", " ")}</strong>
                            <small>
                              {row.timestamp
                                ? new Date(row.timestamp).toLocaleString()
                                : "Time unavailable"}{" "}
                              · {row.provider}
                            </small>
                          </span>
                          <span className="duration">
                            {(row.total_ms / 1000).toFixed(1)}s
                          </span>
                        </summary>
                        <dl>
                          <dt>Model</dt>
                          <dd>{row.configured_model || "—"}</dd>
                          <dt>Audio</dt>
                          <dd>{(row.audio_ms / 1000).toFixed(1)} seconds</dd>
                          <dt>Attempts</dt>
                          <dd>{row.attempt_count}</dd>
                          {row.error && (
                            <>
                              <dt>Error</dt>
                              <dd>{row.error}</dd>
                            </>
                          )}
                        </dl>
                        <pre>{JSON.stringify(row.attempts, null, 2)}</pre>
                      </details>
                    ))
                  ) : (
                    <div className="empty">
                      <FileText size={26} />
                      <h2>
                        {logs.length ? "No matching logs" : "No logs yet"}
                      </h2>
                      <p>
                        {logs.length
                          ? "Try another search or filter."
                          : "Dictation activity will appear here."}
                      </p>
                    </div>
                  )}
                </section>
              </>
            )}
          </div>
        )}
        {dirty && (
          <footer>
            <span>Unsaved changes</span>
            <div className="actions">
              <button disabled={busy} onClick={() => void run(load)}>
                Discard
              </button>
              <button
                className="primary"
                disabled={busy}
                onClick={() => void run(save)}
              >
                {busy ? "Saving…" : "Save changes"}
              </button>
            </div>
          </footer>
        )}
      </main>
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
