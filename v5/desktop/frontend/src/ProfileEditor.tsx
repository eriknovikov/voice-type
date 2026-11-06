import { useEffect, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Copy, Eye, EyeOff } from "lucide-react";
import { api, type ProfileSummary, type ProfileUpdate } from "./api";
import { Dropdown } from "./Dropdown";

export const providerName = (provider: string) =>
  provider === "openai" ? "OpenAI" : "OpenRouter";
const nextName = (provider: string, profiles: ProfileSummary[]) => {
  let index = 1;
  while (
    profiles.some(
      (profile) =>
        profile.name.toLowerCase() ===
        `${providerName(provider)} ${index}`.toLowerCase(),
    )
  )
    index++;
  return `${providerName(provider)} ${index}`;
};
// Each provider owns its form. Keyless providers can supply a different editor
// without changing the profile list or making credentials a profile invariant.
const providerForms = {
  openai: { label: "OpenAI", requiresKey: true },
  openrouter: { label: "OpenRouter", requiresKey: true },
};
export function ProfileEditor({
  profile,
  profiles,
  onSave,
  onClose,
}: {
  profile: ProfileSummary | null;
  profiles: ProfileSummary[];
  onSave: (update: Omit<ProfileUpdate, "revision">) => Promise<void>;
  onClose: () => void;
}) {
  const [provider, setProvider] = useState(profile?.provider ?? "openai");
  const [name, setName] = useState(
    profile?.name ?? nextName("openai", profiles),
  );
  const [nameEdited, setNameEdited] = useState(false);
  const [key, setKey] = useState("");
  const [keyEdited, setKeyEdited] = useState(false);
  const [revealed, setRevealed] = useState<string>();
  const [visible, setVisible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copied, setCopied] = useState(false);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const definition = providerForms[provider as keyof typeof providerForms];
  const validateName = () => {
    if (
      !name.trim() ||
      profiles.some(
        (item) =>
          item.id !== profile?.id &&
          item.name.toLowerCase() === name.trim().toLowerCase(),
      )
    ) {
      setError("Choose a unique profile name.");
      return false;
    }
    return true;
  };
  const toggleReveal = async () => {
    if (visible) {
      setVisible(false);
      setRevealed(undefined);
      return;
    }
    setError("");
    if (!profile || keyEdited) {
      setVisible(true);
      return;
    }
    setBusy(true);
    try {
      const value = await api.revealProfileKey(profile.id);
      if (alive.current) {
        setRevealed(value);
        setVisible(true);
      }
    } catch (e) {
      if (alive.current) setError(String(e instanceof Error ? e.message : e));
    } finally {
      if (alive.current) setBusy(false);
    }
  };
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="dialog profile-dialog"
          aria-describedby={undefined}
          onEscapeKeyDown={(event) => {
            if (busy) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (busy) event.preventDefault();
          }}
        >
          <Dialog.Title>
            {profile ? "Edit profile" : "New profile"}
          </Dialog.Title>
          <form
            onSubmit={async (event) => {
              event.preventDefault();
              if (!validateName()) return;
              if (
                definition.requiresKey &&
                (!profile || keyEdited) &&
                !key.trim()
              ) {
                setError("Enter an API key.");
                return;
              }
              setBusy(true);
              setError("");
              try {
                await onSave({
                  id: profile?.id,
                  name: name.trim(),
                  provider,
                  ...(!profile || keyEdited ? { api_key: key.trim() } : {}),
                });
                if (alive.current) onClose();
              } catch (e) {
                if (alive.current)
                  setError(e instanceof Error ? e.message : String(e));
              } finally {
                if (alive.current) setBusy(false);
              }
            }}
          >
            <div className="profile-form-field">
              <label htmlFor="profile-provider">Provider</label>
              {profile ? (
                <div className="read-only-field">{providerName(provider)}</div>
              ) : (
                <Dropdown
                  id="profile-provider"
                  label="Provider"
                  value={provider}
                  disabled={busy}
                  options={Object.entries(providerForms).map(
                    ([value, form]) => ({ value, label: form.label }),
                  )}
                  onChange={(value) => {
                    setProvider(value);
                    if (!nameEdited) setName(nextName(value, profiles));
                  }}
                />
              )}
            </div>
            <div className="profile-form-field">
              <label htmlFor="profile-name">Name</label>
              <input
                id="profile-name"
                value={name}
                autoComplete="off"
                disabled={busy}
                onChange={(event) => {
                  setName(event.target.value);
                  setNameEdited(true);
                  setError("");
                }}
                onBlur={validateName}
              />
            </div>
            {definition.requiresKey && (
              <div className="profile-form-field">
                <label htmlFor="profile-key">API key</label>
                <div className="key-editor">
                  <input
                    id="profile-key"
                    type={visible ? "text" : "password"}
                    value={keyEdited ? key : (revealed ?? key)}
                    placeholder={profile?.masked_key ?? "Paste API key"}
                    autoComplete="off"
                    spellCheck={false}
                    disabled={busy}
                    onChange={(event) => {
                      setKey(event.target.value);
                      setKeyEdited(true);
                      setRevealed(undefined);
                      setError("");
                    }}
                  />
                  <button
                    type="button"
                    className="icon-button"
                    aria-label={visible ? "Hide API key" : "Reveal API key"}
                    title={visible ? "Hide key" : "Reveal key"}
                    disabled={busy}
                    onClick={() => void toggleReveal()}
                  >
                    {visible ? <EyeOff size={16} /> : <Eye size={16} />}
                  </button>
                  {profile && (
                    <button
                      type="button"
                      className="icon-button"
                      aria-label="Copy saved API key"
                      title={copied ? "Copied" : "Copy saved key"}
                      disabled={busy || keyEdited}
                      onClick={async () => {
                        setError("");
                        try {
                          await api.copyProfileKey(profile.id);
                          if (alive.current) setCopied(true);
                        } catch (e) {
                          if (alive.current)
                            setError(
                              e instanceof Error ? e.message : String(e),
                            );
                        }
                      }}
                    >
                      <Copy size={16} />
                    </button>
                  )}
                </div>
                {copied && (
                  <span className="quiet" role="status">
                    Copied
                  </span>
                )}
              </div>
            )}
            {error && (
              <p className="field-error" role="alert">
                {error}
              </p>
            )}
            <div className="dialog-actions">
              <button type="button" disabled={busy} onClick={onClose}>
                Cancel
              </button>
              <button type="submit" className="primary" disabled={busy}>
                {busy ? "Saving…" : "Save profile"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
