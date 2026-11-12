import { Check, Copy, Eye, EyeOff, KeyRound, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, type APIKeySummary, type Settings } from "./api";
import { Dropdown } from "./Dropdown";
import { providerName } from "./ProfileEditor";
import { ConfirmDialog } from "./ui";

const providers = ["openai", "openrouter"] as const;
export function APIKeys({
  keys,
  storage,
  busy,
  mutate,
  onError,
}: {
  keys: Record<string, APIKeySummary>;
  storage: string;
  busy: boolean;
  mutate: (operation: () => Promise<Settings>) => Promise<void>;
  onError: (message: string) => void;
}) {
  const changeStorage = (mode: string) => {
    void mutate(() => api.setCredentialStorage(mode)).catch((cause) =>
      onError(cause instanceof Error ? cause.message : String(cause)),
    );
  };
  return (
    <section className="settings-section">
      <div className="settings-group api-key-list">
        {providers.map((provider) => (
          <APIKeyRow
            key={provider}
            provider={provider}
            summary={keys[provider]}
            busy={busy}
            mutate={mutate}
            onError={onError}
          />
        ))}
      </div>
      <div className="settings-group">
        <label htmlFor="credential-storage">Storage</label>
        <Dropdown
          id="credential-storage"
          label="Credential storage"
          value={storage}
          options={[
            { value: "keyring", label: "System keyring" },
            { value: "file", label: "Local file" },
          ]}
          onChange={changeStorage}
        />
      </div>
      <p className="quiet section-note">
        {storage === "file"
          ? "Saved keys are copied here; new keys are stored unencrypted, base64-encoded, in ~/.local/share/voice-type."
          : "Saved keys are copied to the system keyring; a keyring failure falls back to a private local file."}
      </p>
    </section>
  );
}

function APIKeyRow({
  provider,
  summary,
  busy,
  mutate,
  onError,
}: {
  provider: (typeof providers)[number];
  summary?: APIKeySummary;
  busy: boolean;
  mutate: (operation: () => Promise<Settings>) => Promise<void>;
  onError: (message: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState("");
  const [visible, setVisible] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(summary?.error ?? "");
  const [copied, setCopied] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const copiedTimer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => () => clearTimeout(copiedTimer.current), []);
  useEffect(() => setError(summary?.error ?? ""), [summary?.error]);

  const save = async () => {
    if (!value.trim()) {
      setError("Enter an API key.");
      input.current?.focus();
      return;
    }
    setSaving(true);
    setError("");
    try {
      await mutate(() => api.saveAPIKey(provider, value));
      setEditing(false);
      setValue("");
      setVisible(false);
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : String(cause);
      setError(message);
      onError(message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="api-key-row">
      <div className="api-key-heading">
        <span className="api-key-provider">
          <KeyRound size={16} aria-hidden="true" />
          <strong>{providerName(provider)}</strong>
        </span>
        {summary?.saved && !editing && (
          <span className="profile-key">{summary.masked_key}</span>
        )}
      </div>
      {editing ? (
        <>
          <div className="key-editor">
            <input
              ref={input}
              autoFocus
              aria-label={`${providerName(provider)} API key`}
              type={visible ? "text" : "password"}
              value={value}
              autoComplete="off"
              spellCheck={false}
              disabled={saving}
              placeholder="Paste API key…"
              aria-invalid={!!error}
              onChange={(event) => {
                setValue(event.target.value);
                setError("");
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  event.preventDefault();
                  void save();
                }
                if (event.key === "Escape") {
                  event.preventDefault();
                  setEditing(false);
                  setValue("");
                  setVisible(false);
                  setError(summary?.error ?? "");
                }
              }}
            />
            <button
              type="button"
              className="icon-button"
              aria-label={visible ? "Hide API key" : "Reveal API key"}
              onClick={() => setVisible(!visible)}
              disabled={saving}
            >
              {visible ? (
                <EyeOff size={16} aria-hidden="true" />
              ) : (
                <Eye size={16} aria-hidden="true" />
              )}
            </button>
          </div>
          {error && (
            <p className="field-error" role="alert">
              {error}
            </p>
          )}
          <div className="api-key-actions">
            <button
              type="button"
              onClick={() => {
                setEditing(false);
                setValue("");
                setVisible(false);
                setError(summary?.error ?? "");
              }}
              disabled={saving}
            >
              Cancel
            </button>
            <button
              type="button"
              className="primary"
              onClick={() => void save()}
              disabled={saving || busy}
            >
              {saving ? "Saving…" : "Save"}
            </button>
          </div>
        </>
      ) : (
        <div className="api-key-actions">
          {summary?.saved && (
            <>
              <button
                type="button"
                className="icon-button"
                aria-label={`Copy ${providerName(provider)} API key`}
                title={copied ? "Copied" : "Copy key"}
                disabled={busy}
                onClick={async () => {
                  try {
                    await api.copyAPIKey(provider);
                    setCopied(true);
                    clearTimeout(copiedTimer.current);
                    copiedTimer.current = setTimeout(() => setCopied(false), 1600);
                  } catch (cause) {
                    onError(cause instanceof Error ? cause.message : String(cause));
                  }
                }}
              >
                {copied ? (
                  <Check size={16} aria-hidden="true" />
                ) : (
                  <Copy size={16} aria-hidden="true" />
                )}
              </button>
              <button
                type="button"
                className="icon-button danger-text"
                aria-label={`Delete ${providerName(provider)} API key`}
                title="Delete key"
                disabled={busy}
                onClick={() => setDeleting(true)}
              >
                <Trash2 size={16} aria-hidden="true" />
              </button>
            </>
          )}
          <button
            type="button"
            onClick={() => setEditing(true)}
            disabled={busy}
          >
            {summary?.saved ? "Replace" : "Add"}
          </button>
        </div>
      )}
      {error && !editing && (
        <p className="field-error" role="alert">
          {error}
        </p>
      )}
      {deleting && (
        <ConfirmDialog
          title={`Delete ${providerName(provider)} API key?`}
          description="Existing profiles will need a new key."
          confirmLabel="Delete key"
          onCancel={() => setDeleting(false)}
          onConfirm={async () => {
            await mutate(() => api.deleteAPIKey(provider));
            setDeleting(false);
          }}
        />
      )}
    </div>
  );
}
