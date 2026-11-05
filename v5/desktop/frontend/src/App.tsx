import React, { useEffect, useState } from "react";
import {
  Activity,
  AudioLines,
  BookOpen,
  Check,
  Circle,
  FileText,
  Mic,
  Keyboard,
  Plus,
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

type Tab =
  "overview" | "settings" | "audio" | "shortcuts" | "vocabulary" | "logs";
const tabs = [
  { id: "overview", label: "Overview", icon: Activity },
  { id: "settings", label: "General", icon: Settings2 },
  { id: "audio", label: "Audio", icon: Mic },
  { id: "shortcuts", label: "Keyboard Shortcuts", icon: Keyboard },
  { id: "vocabulary", label: "Vocabulary", icon: BookOpen },
  { id: "logs", label: "Logs", icon: FileText },
] as const;
const errorText = (error: unknown) =>
  error instanceof Error ? error.message : String(error);
export function App() {
  const [tab, setTab] = useState<Tab>("overview");
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
  const [newList, setNewList] = useState("");
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
    setInfo(i);
    setKeys({});
    setDirty(false);
  };
  useEffect(() => {
    void run(async () => {
      await api.connect();
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
  const saveShortcut = async (
    field: "shortcut" | "daemon_shortcut",
    value: string,
  ) => {
    const current = await api.info();
    await api.preferences({
      ...current.preferences,
      [field]: value,
      setup_complete: true,
    });
    setInfo((state) =>
      state
        ? {
            ...state,
            shortcut_error: "",
            preferences: {
              ...state.preferences,
              [field]: value,
              setup_complete: true,
            },
          }
        : state,
    );
  };
  const credential = settings?.credentials[settings.provider];
  return (
    <div className="app">
      <aside>
        <div className="brand">
          <img src="/icon.png" alt="" className="brand-icon" />
          <span>Voice Type</span>
        </div>
        <nav aria-label="Main navigation">
          {tabs.map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              className={tab === id ? "active" : ""}
              onClick={() => navigate(id)}
            >
              <Icon size={18} />
              {label}
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <span className={`dot ${status.state}`} />
          <span>
            {status.state === "disconnected"
              ? "Daemon offline"
              : status.state === "idle"
                ? status.error
                  ? "Setup needed"
                  : "Ready"
                : status.state}
          </span>
          <small>v{info?.version ?? "—"}</small>
        </div>
      </aside>
      <main>
        <header>
          <div>
            <h1>{tabs.find((t) => t.id === tab)?.label}</h1>
          </div>
          <div className="window-actions">
            <span className={`status ${status.state}`}>
              <span className={`dot ${status.state}`} />
              {status.state}
            </span>
            <button
              className="window-close"
              aria-label="Close window"
              title="Hide to background"
              onClick={() => void Window.Close()}
            >
              <X size={18} />
            </button>
          </div>
        </header>
        {error && (
          <div className="alert error" role="alert">
            {error}
            <button aria-label="Dismiss error" onClick={() => setError("")}>
              <X size={16} />
            </button>
          </div>
        )}
        {notice && (
          <div className="alert success" role="status">
            <Check size={16} />
            {notice}
          </div>
        )}
        {status.state === "disconnected" && (
          <div className="alert">
            Voice Type is not connected.
            <button
              disabled={busy}
              onClick={() =>
                void run(async () => {
                  await api.connect();
                  await load();
                })
              }
            >
              Reconnect
            </button>
          </div>
        )}
        {!settings ? (
          <div className="empty">
            <AudioLines size={40} />
            <h2>Connecting</h2>
            <p>Waiting for the daemon.</p>
            {error && status.state !== "disconnected" && (
              <button
                onClick={() =>
                  void run(async () => {
                    const revision = await api.recoveryRevision();
                    setConfirm({
                      title: "Reset unreadable configuration?",
                      description:
                        "Your original file will be preserved in a private backup beside it. Provider keys and vocabulary must be set up again. A valid configuration will never be reset.",
                      run: async () => {
                        await api.resetBrokenSettings(revision);
                        await load();
                        setNotice(
                          "Configuration reset. Your original file is preserved in a private recovery backup.",
                        );
                      },
                    });
                  })
                }
              >
                Back up and reset broken settings
              </button>
            )}
          </div>
        ) : (
          <>
            {tab === "overview" && (
              <>
                <section className="hero">
                  <h2>
                    {status.state === "recording"
                      ? "Recording"
                      : status.state === "transcribing"
                        ? "Transcribing"
                        : "Dictation"}
                  </h2>
                  <p>Press your shortcut to start or stop recording.</p>
                  <button
                    className="primary large"
                    disabled={
                      busy ||
                      !["idle", "recording"].includes(status.state) ||
                      (status.state === "idle" && !!status.error)
                    }
                    onClick={() =>
                      void run(async () => {
                        await api.toggle();
                      })
                    }
                  >
                    <Mic size={17} />
                    {status.state === "recording"
                      ? "Stop dictation"
                      : "Start dictation"}
                  </button>
                  <small>
                    For insertion into another app, use your global shortcut.
                  </small>
                </section>
                <div className="grid">
                  <section className="card">
                    <span className="eyebrow">PROVIDER</span>
                    <h3>
                      {settings.provider === "openai" ? "OpenAI" : "OpenRouter"}
                    </h3>
                    <p>
                      {credential?.environment
                        ? "API key from environment"
                        : credential?.saved
                          ? "API key configured"
                          : "API key missing"}
                    </p>
                    <button onClick={() => navigate("settings")}>
                      Provider settings →
                    </button>
                  </section>
                  <section className="card">
                    <span className="eyebrow">VOCABULARY</span>
                    <h3>{settings.workspace || "General only"}</h3>
                    <p>General terms and the selected workspace.</p>
                    <button onClick={() => navigate("vocabulary")}>
                      Manage vocabulary →
                    </button>
                  </section>
                </div>
                {!info?.preferences.setup_complete && (
                  <section className="card setup">
                    <h3>Setup</h3>
                    <p>
                      1. Add your API key in General.
                      <br />
                      2. Choose and test your microphone.
                      <br />
                      3. Set your keyboard shortcuts.
                    </p>
                    <button
                      className="primary"
                      onClick={() =>
                        navigate(
                          credential?.saved || credential?.environment
                            ? "audio"
                            : "settings",
                        )
                      }
                    >
                      Continue setup
                    </button>
                  </section>
                )}
                {status.error && <div className="alert">{status.error}</div>}
              </>
            )}
            {tab === "settings" && (
              <>
                <p className="intro">Select a provider and add your API key.</p>
                <section className="card">
                  <h3>Transcription provider</h3>
                  <div className="provider-options">
                    {["openai", "openrouter"].map((p) => (
                      <button
                        className={settings.provider === p ? "selected" : ""}
                        key={p}
                        onClick={() => patch({ provider: p })}
                      >
                        <Circle size={16} />
                        <strong>
                          {p === "openai" ? "OpenAI" : "OpenRouter"}
                        </strong>
                        <span>
                          {p === "openai" ? "OpenAI API" : "OpenRouter API"}
                        </span>
                      </button>
                    ))}
                  </div>
                  <label htmlFor="key">API key</label>
                  <input
                    id="key"
                    type="password"
                    autoComplete="off"
                    spellCheck={false}
                    value={keys[settings.provider] ?? ""}
                    placeholder={
                      credential?.saved
                        ? "Enter a new key to replace the saved key."
                        : "Paste your API key"
                    }
                    onChange={(e) => {
                      setKeys({ ...keys, [settings.provider]: e.target.value });
                      setDirty(true);
                    }}
                  />
                  <p className="hint">
                    {credential?.environment
                      ? "An environment variable currently overrides the saved key."
                      : credential?.saved
                        ? "API key saved."
                        : "A key is required to transcribe audio."}
                  </p>
                  {credential?.saved && (
                    <button
                      className="danger-text"
                      onClick={() => {
                        setKeys({ ...keys, [settings.provider]: "" });
                        setDirty(true);
                        setNotice("Key removal will apply when you save.");
                      }}
                    >
                      Remove saved key
                    </button>
                  )}
                </section>
                <section className="card">
                  <h3>Daemon</h3>
                  <label htmlFor="port">Local control port</label>
                  <input
                    id="port"
                    type="number"
                    min="1024"
                    max="65535"
                    value={settings.port}
                    onChange={(e) => patch({ port: Number(e.target.value) })}
                  />
                  <p className="hint">
                    Existing desktop shortcuts use this port. Port changes
                    require a restart.
                  </p>
                  <button
                    disabled={busy || status.state !== "idle" || dirty}
                    onClick={() =>
                      void run(async () => {
                        await api.restart();
                        setNotice("Daemon restarted.");
                      })
                    }
                  >
                    <RefreshCw size={15} />
                    Restart daemon
                  </button>
                </section>
              </>
            )}
            {tab === "settings" && info && (
              <section className="card">
                <h3>Desktop</h3>
                <label className="checkbox">
                  <input
                    type="checkbox"
                    checked={info.preferences.autostart}
                    onChange={(event) =>
                      setInfo({
                        ...info,
                        preferences: {
                          ...info.preferences,
                          autostart: event.target.checked,
                        },
                      })
                    }
                  />
                  Start at login
                </label>
                <button
                  disabled={busy}
                  onClick={() =>
                    void run(async () => {
                      const saved = await api.info();
                      await api.preferences({
                        ...saved.preferences,
                        autostart: info.preferences.autostart,
                      });
                      setInfo((current) =>
                        current
                          ? {
                              ...current,
                              preferences: {
                                ...current.preferences,
                                autostart: info.preferences.autostart,
                              },
                            }
                          : current,
                      );
                      setNotice("Startup preference saved.");
                    })
                  }
                >
                  Save startup preference
                </button>
                {!info.tray_available && (
                  <p className="hint">
                    To show the tray icon on GNOME, enable the AppIndicator
                    extension.
                  </p>
                )}
              </section>
            )}
            {tab === "audio" && (
              <>
                <section className="card">
                  <h3>Microphone</h3>
                  <label htmlFor="microphone">Input device</label>
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

                  <MicrophoneMeter active={status.state === "testing"} />
                  <div className="row">
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
                    <button
                      disabled={busy}
                      onClick={() =>
                        void run(async () =>
                          setSources(await api.microphones()),
                        )
                      }
                    >
                      <RefreshCw size={15} />
                      Refresh devices
                    </button>
                  </div>
                  <p className="hint">
                    Audio is not uploaded. The test stops after 60 seconds.
                  </p>
                </section>
              </>
            )}
            {tab === "shortcuts" && info && (
              <section className="card">
                <ShortcutPicker
                  name="Transcription toggle"
                  value={info.preferences.shortcut}
                  onError={setError}
                  onChange={(value) => saveShortcut("shortcut", value)}
                />
                <ShortcutPicker
                  name="Daemon toggle"
                  value={info.preferences.daemon_shortcut}
                  onError={setError}
                  onChange={(value) => saveShortcut("daemon_shortcut", value)}
                />
                <p className="hint">
                  Click a shortcut and press the keys. Click elsewhere to save.
                  Escape disables it.
                </p>
                {info.shortcut_error && (
                  <p className="alert error">{info.shortcut_error}</p>
                )}
              </section>
            )}
            {tab === "vocabulary" && (
              <>
                <p className="intro">
                  Add names and terms to improve transcription.
                </p>
                <section className="card">
                  <label htmlFor="workspace">Active workspace</label>
                  <Dropdown
                    id="workspace"
                    label="Active workspace"
                    value={settings.workspace}
                    disabled={dirty || busy}
                    onChange={(value) =>
                      void run(async () => {
                        await api.workspace(value);
                        setSettings({ ...settings, workspace: value });
                      })
                    }
                    options={[
                      { value: "", label: "General only" },
                      ...settings.vocabulary
                        .filter((list) => list.name !== "general")
                        .map((list) => ({
                          value: list.name,
                          label: list.name,
                        })),
                    ]}
                  />

                  <p className="hint">
                    General terms are always included. Save edits before
                    switching workspace.
                  </p>
                </section>
                <div className="vocab-editor">
                  <section className="card lists">
                    {settings.vocabulary.map((v) => (
                      <button
                        key={v.name}
                        className={selected === v.name ? "active" : ""}
                        onClick={() => setSelected(v.name)}
                      >
                        <BookOpen size={15} />
                        {v.name}
                        <small>{v.terms.filter(Boolean).length}</small>
                      </button>
                    ))}
                    <form
                      onSubmit={(e) => {
                        e.preventDefault();
                        const name = newList.trim();
                        if (
                          !name ||
                          settings.vocabulary.some((v) => v.name === name)
                        ) {
                          setError("Choose a unique list name.");
                          return;
                        }
                        patch({
                          vocabulary: [
                            ...settings.vocabulary,
                            { name, terms: [] },
                          ],
                        });
                        setSelected(name);
                        setNewList("");
                      }}
                    >
                      <input
                        aria-label="New vocabulary list name"
                        placeholder="New workspace…"
                        value={newList}
                        onChange={(e) => setNewList(e.target.value)}
                      />
                      <button aria-label="Add vocabulary list" type="submit">
                        <Plus size={16} />
                      </button>
                    </form>
                  </section>
                  <section className="card terms">
                    {list ? (
                      <>
                        <div className="row">
                          <h3>
                            {selected === "general"
                              ? "General vocabulary"
                              : selected}
                          </h3>
                          {selected !== "general" && (
                            <button
                              className="icon danger-text"
                              aria-label="Delete vocabulary list"
                              onClick={() =>
                                setConfirm({
                                  title: `Delete ${selected}?`,
                                  description:
                                    "The list and its terms will be removed when you save.",
                                  run: async () => {
                                    patch({
                                      vocabulary: settings.vocabulary.filter(
                                        (v) => v.name !== selected,
                                      ),
                                    });
                                    setSelected("general");
                                  },
                                })
                              }
                            >
                              <Trash2 size={16} />
                            </button>
                          )}
                        </div>
                        {selected !== "general" && (
                          <>
                            <label htmlFor="list-name">Workspace name</label>
                            <input
                              id="list-name"
                              value={selected}
                              onChange={(e) =>
                                updateList(e.target.value, list.terms)
                              }
                            />
                          </>
                        )}
                        <label htmlFor="terms">Terms — one per line</label>
                        <textarea
                          id="terms"
                          rows={12}
                          value={list.terms.join("\n")}
                          placeholder={"Wails\nOpenRouter\nYour project name"}
                          onChange={(e) =>
                            updateList(selected, e.target.value.split(/\r?\n/))
                          }
                        />
                        <p className="hint">
                          Short, specific terms work best. Duplicates are
                          removed when saved.
                        </p>
                      </>
                    ) : (
                      <p>Select a vocabulary list.</p>
                    )}
                  </section>
                </div>
              </>
            )}
            {tab === "logs" && (
              <>
                <p className="intro">
                  Dictation logs. Transcribed text is not stored here.
                </p>
                <div className="log-tools">
                  <input
                    aria-label="Search logs"
                    placeholder="Search diagnostics…"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
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
                    className="icon"
                    aria-label="Clear logs"
                    disabled={!logs.length}
                    onClick={() =>
                      setConfirm({
                        title: "Clear diagnostic logs?",
                        description:
                          "This removes the current and rotated logs from this device.",
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
                <section className="card log-list">
                  {displayed.length ? (
                    displayed.map((row, i) => (
                      <details key={`${row.timestamp}-${i}`}>
                        <summary>
                          <span
                            className={`log-icon ${row.outcome === "ok" ? "ok" : "failed"}`}
                          >
                            {row.outcome === "ok" ? (
                              <Check size={15} />
                            ) : (
                              <X size={15} />
                            )}
                          </span>
                          <span>
                            <strong>{row.outcome.replaceAll("_", " ")}</strong>
                            <small>
                              {row.timestamp
                                ? new Date(row.timestamp).toLocaleString()
                                : "Older entry · time unavailable"}{" "}
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
                      <FileText size={32} />
                      <h3>
                        {logs.length ? "No matching entries" : "No logs yet"}
                      </h3>
                      <p>Diagnostics from your dictations will appear here.</p>
                    </div>
                  )}
                </section>
              </>
            )}
          </>
        )}
        {dirty && (
          <footer>
            <span>Unsaved changes</span>
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
            <div className="row">
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
