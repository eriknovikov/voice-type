import { useEffect, useRef, useState, type RefObject } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Check, Copy, Eye, EyeOff, Trash2 } from "lucide-react";
import { api, type ProfileSummary, type ProfileUpdate } from "./api";
import { ConfirmDialog, SegmentedControl } from "./ui";

export const providerName = (provider: string) =>
  provider === "openai"
    ? "OpenAI"
    : provider === "openrouter"
      ? "OpenRouter"
      : provider;
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
// Providers own their fields; credentials are not an invariant of a profile.
const providerForms = {
  openai: { label: "OpenAI", requiresKey: true },
  openrouter: { label: "OpenRouter", requiresKey: true },
};
export function ProfileEditor({
  profile,
  profiles,
  onSave,
  onDelete,
  onClose,
  closeGuard,
}: {
  profile: ProfileSummary | null;
  profiles: ProfileSummary[];
  onSave: (update: Omit<ProfileUpdate, "revision">) => Promise<void>;
  onDelete: () => Promise<void>;
  onClose: () => void;
  closeGuard: RefObject<(() => Promise<boolean>) | null>;
}) {
  const [provider, setProvider] = useState(profile?.provider ?? "openai");
  const initialName = useRef(profile?.name ?? nextName("openai", profiles));
  const [name, setName] = useState(initialName.current);
  const [nameEdited, setNameEdited] = useState(false);
  const [key, setKey] = useState("");
  const [keyEdited, setKeyEdited] = useState(false);
  const [revealed, setRevealed] = useState<string>();
  const [visible, setVisible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [nameError, setNameError] = useState("");
  const [keyError, setKeyError] = useState("");
  const [copied, setCopied] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [discarding, setDiscarding] = useState(false);
  const nameInput = useRef<HTMLInputElement>(null);
  const keyInput = useRef<HTMLInputElement>(null);
  const alive = useRef(true);
  const copiedTimer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const dismiss = useRef<{
    promise: Promise<boolean>;
    resolve: (value: boolean) => void;
  } | null>(null);
  const previousFocus = useRef(document.activeElement);
  const dirty =
    name !== initialName.current ||
    provider !== (profile?.provider ?? "openai") ||
    keyEdited;
  const requestClose = (): Promise<boolean> => {
    if (busy || deleting) return Promise.resolve(false);
    if (!dirty) {
      onClose();
      return Promise.resolve(true);
    }
    if (dismiss.current) return dismiss.current.promise;
    let resolve!: (value: boolean) => void;
    const promise = new Promise<boolean>((done) => {
      resolve = done;
    });
    dismiss.current = { promise, resolve };
    setDiscarding(true);
    return promise;
  };
  useEffect(() => {
    closeGuard.current = requestClose;
    return () => {
      closeGuard.current = null;
    };
  });
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      clearTimeout(copiedTimer.current);
      dismiss.current?.resolve(false);
    };
  }, []);
  const definition = providerForms[provider as keyof typeof providerForms];
  const validateName = () => {
    const message = !name.trim()
      ? "Enter a profile name."
      : profiles.some(
            (item) =>
              item.id !== profile?.id &&
              item.name.toLowerCase() === name.trim().toLowerCase(),
          )
        ? "Choose a unique profile name."
        : "";
    setNameError(message);
    return !message;
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
      if (alive.current) setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (alive.current) setBusy(false);
    }
  };
  const settleDismiss = (discard: boolean) => {
    const pending = dismiss.current;
    dismiss.current = null;
    setDiscarding(false);
    if (discard) onClose();
    pending?.resolve(discard);
  };
  return (
    <>
      <Dialog.Root
        open
        onOpenChange={(open) => {
          if (!open) void requestClose();
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content
            className="dialog profile-dialog"
            aria-describedby={undefined}
            onOpenAutoFocus={(event) => {
              if (profile) {
                event.preventDefault();
                nameInput.current?.focus();
              }
            }}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              if (previousFocus.current instanceof HTMLElement)
                previousFocus.current.focus();
            }}
            onEscapeKeyDown={(event) => {
              event.preventDefault();
              if (!discarding && !deleting) void requestClose();
            }}
            onInteractOutside={(event) => {
              event.preventDefault();
              if (!discarding && !deleting) void requestClose();
            }}
          >
            <Dialog.Title>
              {profile ? "Edit profile" : "New profile"}
            </Dialog.Title>
            <form
              noValidate
              onSubmit={async (event) => {
                event.preventDefault();
                const validName = validateName();
                const missingKey =
                  definition.requiresKey &&
                  (!profile || keyEdited) &&
                  !key.trim();
                setKeyError(missingKey ? "Enter an API key." : "");
                if (!validName) {
                  nameInput.current?.focus();
                  return;
                }
                if (missingKey) {
                  keyInput.current?.focus();
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
                {profile ? (
                  <>
                    <span className="field-label">Provider</span>
                    <div className="read-only-field">
                      {providerName(provider)}
                    </div>
                  </>
                ) : (
                  <SegmentedControl
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
                  ref={nameInput}
                  id="profile-name"
                  name="profile-name"
                  value={name}
                  autoComplete="off"
                  disabled={busy}
                  aria-invalid={!!nameError}
                  aria-describedby={
                    nameError ? "profile-name-error" : undefined
                  }
                  onChange={(event) => {
                    setName(event.target.value);
                    setNameEdited(true);
                    setNameError("");
                  }}
                  onBlur={validateName}
                />
                {nameError && (
                  <p
                    id="profile-name-error"
                    className="field-error"
                    role="alert"
                  >
                    {nameError}
                  </p>
                )}
              </div>
              {definition.requiresKey && (
                <div className="profile-form-field">
                  <label htmlFor="profile-key">API key</label>
                  <div className="key-editor">
                    <input
                      ref={keyInput}
                      id="profile-key"
                      name="profile-key"
                      type={visible ? "text" : "password"}
                      value={keyEdited ? key : (revealed ?? key)}
                      placeholder={profile?.masked_key ?? "Paste API key…"}
                      autoComplete="off"
                      spellCheck={false}
                      disabled={busy}
                      aria-invalid={!!keyError}
                      aria-describedby={
                        keyError ? "profile-key-error" : undefined
                      }
                      onChange={(event) => {
                        setKey(event.target.value);
                        setKeyEdited(true);
                        setRevealed(undefined);
                        setKeyError("");
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
                      {visible ? (
                        <EyeOff size={16} aria-hidden="true" />
                      ) : (
                        <Eye size={16} aria-hidden="true" />
                      )}
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
                            if (alive.current) {
                              setCopied(true);
                              clearTimeout(copiedTimer.current);
                              copiedTimer.current = setTimeout(
                                () => setCopied(false),
                                1600,
                              );
                            }
                          } catch (e) {
                            if (alive.current)
                              setError(
                                e instanceof Error ? e.message : String(e),
                              );
                          }
                        }}
                      >
                        {copied ? (
                          <Check size={16} aria-hidden="true" />
                        ) : (
                          <Copy size={16} aria-hidden="true" />
                        )}
                      </button>
                    )}
                  </div>
                  <span className="sr-only" role="status">
                    {copied ? "API key copied" : ""}
                  </span>
                  {keyError && (
                    <p
                      id="profile-key-error"
                      className="field-error"
                      role="alert"
                    >
                      {keyError}
                    </p>
                  )}
                </div>
              )}
              {error && (
                <p className="field-error" role="alert">
                  {error}
                </p>
              )}
              <div className="dialog-actions">
                {profile && (
                  <button
                    type="button"
                    className="text-button danger-text delete-profile"
                    disabled={busy}
                    onClick={() => setDeleting(true)}
                  >
                    <Trash2 size={15} aria-hidden="true" />
                    Delete profile
                  </button>
                )}
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => void requestClose()}
                >
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
      {discarding && (
        <ConfirmDialog
          title="Discard profile changes?"
          description="Your changes to this profile have not been saved."
          confirmLabel="Discard changes"
          cancelLabel="Keep editing"
          onConfirm={() => settleDismiss(true)}
          onCancel={() => settleDismiss(false)}
        />
      )}
      {deleting && (
        <ConfirmDialog
          title={`Delete ${profile?.name}?`}
          description="This profile and its API key will be removed. If it is selected, choose another profile before your next dictation."
          confirmLabel="Delete profile"
          onConfirm={async () => {
            await onDelete();
            onClose();
          }}
          onCancel={() => setDeleting(false)}
        />
      )}
    </>
  );
}
