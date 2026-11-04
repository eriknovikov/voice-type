import React, { useEffect, useState } from "react";
import {
  Activity,
  AudioLines,
  BookOpen,
  Check,
  Circle,
  FileText,
  Mic,
  Plus,
  RefreshCw,
  Settings2,
  Trash2,
  X,
} from "lucide-react";
import * as Dialog from "@radix-ui/react-dialog";
import { Events } from "@wailsio/runtime";
import {
  api,
  type Settings,
  type Status,
  type DesktopInfo,
  type AudioSource,
  type LogRecord,
} from "./api";
import "./style.css";

type Tab = "overview" | "settings" | "audio" | "vocabulary" | "logs";
const tabs = [
  { id: "overview", label: "Overview", icon: Activity },
  { id: "settings", label: "Settings", icon: Settings2 },
  { id: "audio", label: "Audio & shortcuts", icon: Mic },
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
    return () => off();
  }, []);
  useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      try {
        const next = await api.status();
        if (alive) setStatus(next);
        const freshInfo = await api.info();
        if (alive)
          setInfo((current) =>
            current
              ? {
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
    setNotice("Settings saved. Changes apply to your next dictation.");
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
  const credential = settings?.credentials[settings.provider];
  return (
    <div className="app">
      <aside>
        <div className="brand">
          <AudioLines size={27} />
          <span>
            voice type<small>YOUR WORDS, EVERYWHERE</small>
          </span>
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
                  : "Ready when you are"
                : status.state}
          </span>
          <small>v{info?.version ?? "—"}</small>
        </div>
      </aside>
      <main>
        <header>
          <div>
            <div className="eyebrow">VOICE TYPE</div>
            <h1>{tabs.find((t) => t.id === tab)?.label}</h1>
          </div>
          <span className={`status ${status.state}`}>
            <span className={`dot ${status.state}`} />
            {status.state}
          </span>
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
            <h2>Connecting to Voice Type</h2>
            <p>Your settings will appear here once the daemon is ready.</p>
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
                  <div className="hero-icon">
                    <AudioLines size={48} />
                  </div>
                  <div className="eyebrow">LESS TYPING. MORE FLOW.</div>
                  <h2>
                    {status.state === "recording"
                      ? "Listening to you"
                      : status.state === "transcribing"
                        ? "Turning speech into text"
                        : "Speak naturally."}
                  </h2>
                  <p>
                    Your words appear wherever your cursor is.
                    <br />
                    Keep this window tucked away and use your shortcut.
                  </p>
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
                          : "Add an API key to get started"}
                    </p>
                    <button onClick={() => navigate("settings")}>
                      Manage provider →
                    </button>
                  </section>
                  <section className="card">
                    <span className="eyebrow">VOCABULARY</span>
                    <h3>{settings.workspace || "General only"}</h3>
                    <p>Names and terminology, just as you spell them.</p>
                    <button onClick={() => navigate("vocabulary")}>
                      Manage vocabulary →
                    </button>
                  </section>
                </div>
                {!info?.preferences.setup_complete && (
                  <section className="card setup">
                    <h3>Make it yours</h3>
                    <p>
                      1. Add a provider key in Settings.
                      <br />
                      2. Choose and test your microphone.
                      <br />
                      3. Configure your shortcut and startup preference.
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
                <p className="intro">
                  Choose how your speech is transcribed. Your keys stay on this
                  device.
                </p>
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
                          {p === "openai"
                            ? "Direct transcription API"
                            : "Use your OpenRouter credits"}
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
                        ? "A key is saved. Enter a new key to replace it."
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
                        ? "The saved key is never sent back to this window."
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
            {tab === "audio" && (
              <>
                <p className="intro">
                  A good microphone and one shortcut. That’s all you need.
                </p>
                <section className="card">
                  <h3>Microphone</h3>
                  <label htmlFor="microphone">Input device</label>
                  <select
                    id="microphone"
                    value={settings.microphone}
                    onChange={(e) => patch({ microphone: e.target.value })}
                  >
                    <option value="">System default</option>
                    {settings.microphone &&
                      !sources.some((s) => s.id === settings.microphone) && (
                        <option value={settings.microphone}>
                          Unavailable: {settings.microphone}
                        </option>
                      )}
                    {sources.map((s) => (
                      <option value={s.id} key={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                  <div
                    className="meter"
                    role="meter"
                    aria-label="Microphone level"
                    aria-valuemin={0}
                    aria-valuemax={100}
                    aria-valuenow={Math.round(status.level * 100)}
                  >
                    <div
                      style={{
                        width: `${Math.max(0, Math.min(100, status.level * 100))}%`,
                      }}
                    />
                  </div>
                  <div className="row">
                    <button
                      disabled={
                        busy || !["idle", "testing"].includes(status.state)
                      }
                      onClick={() =>
                        void run(async () => {
                          if (status.state === "testing") await api.stopTest();
                          else await api.startTest(settings.microphone);
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
                    The test stays local and stops after 60 seconds or when you
                    leave this page.
                  </p>
                </section>
                {info && (
                  <section className="card">
                    <h3>Shortcut & startup</h3>
                    <label htmlFor="shortcut">Toggle dictation</label>
                    <input
                      id="shortcut"
                      value={info.preferences.shortcut}
                      onChange={(e) =>
                        setInfo({
                          ...info,
                          preferences: {
                            ...info.preferences,
                            shortcut: e.target.value,
                          },
                        })
                      }
                    />
                    <p className="hint">
                      For example Ctrl+Alt+Space. On Wayland, your desktop asks
                      you to approve or choose the final shortcut.
                    </p>
                    <label className="checkbox">
                      <input
                        type="checkbox"
                        checked={info.preferences.autostart}
                        onChange={(e) =>
                          setInfo({
                            ...info,
                            preferences: {
                              ...info.preferences,
                              autostart: e.target.checked,
                            },
                          })
                        }
                      />
                      Start quietly when I log in
                    </label>
                    <button
                      className="primary"
                      disabled={busy}
                      onClick={() =>
                        void run(async () => {
                          if (dirty) await save();
                          await api.preferences({
                            ...info.preferences,
                            setup_complete: true,
                          });
                          setInfo(await api.info());
                          setNotice(
                            "Desktop preferences saved. Complete any shortcut approval shown by your desktop.",
                          );
                        })
                      }
                    >
                      Save desktop preferences
                    </button>
                    {info.last_shortcut_at && (
                      <p className="hint">
                        Shortcut last activated at{" "}
                        {new Date(info.last_shortcut_at).toLocaleTimeString()}.
                      </p>
                    )}
                    {info.shortcut_error && (
                      <p className="alert error">{info.shortcut_error}</p>
                    )}
                  </section>
                )}
                {info && !info.tray_available && (
                  <section className="card">
                    <h3>Enable a tray icon on GNOME</h3>
                    <p>
                      Your desktop is not providing a system tray. Enable the
                      AppIndicator and KStatusNotifierItem extension to show
                      Voice Type in the top panel. You can always reopen this
                      window from your application launcher.
                    </p>
                  </section>
                )}
              </>
            )}
            {tab === "vocabulary" && (
              <>
                <p className="intro">
                  Teach Voice Type your names, projects and specialist terms.
                </p>
                <section className="card">
                  <label htmlFor="workspace">Active workspace</label>
                  <select
                    id="workspace"
                    disabled={dirty || busy}
                    value={settings.workspace}
                    onChange={(e) =>
                      void run(async () => {
                        await api.workspace(e.target.value);
                        setSettings({ ...settings, workspace: e.target.value });
                      })
                    }
                  >
                    <option value="">General only</option>
                    {settings.vocabulary
                      .filter((v) => v.name !== "general")
                      .map((v) => (
                        <option key={v.name}>{v.name}</option>
                      ))}
                  </select>
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
                  A quiet place for diagnostics. Dictated text is never stored
                  here.
                </p>
                <div className="log-tools">
                  <input
                    aria-label="Search logs"
                    placeholder="Search diagnostics…"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                  />
                  <select
                    aria-label="Filter logs"
                    value={outcome}
                    onChange={(e) => setOutcome(e.target.value)}
                  >
                    <option value="all">All outcomes</option>
                    <option value="errors">Errors</option>
                    <option value="ok">Successful</option>
                  </select>
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
                        {logs.length
                          ? "No matching entries"
                          : "Nothing to troubleshoot"}
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
