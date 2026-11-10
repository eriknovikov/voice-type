import { useEffect, useRef, useState, type RefObject } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Trash2 } from "lucide-react";
import type { APIKeySummary, ProfileSummary, ProfileUpdate } from "./api";
import { Dropdown } from "./Dropdown";
import { ConfirmDialog, SegmentedControl } from "./ui";

export const providerName = (provider: string) =>
  provider === "openai"
    ? "OpenAI"
    : provider === "openrouter"
      ? "OpenRouter"
      : provider === "local"
        ? "On-device"
      : provider;
const unifiedModel = "parakeet-unified-en";
const modelSupportsStreaming = (modelID: string) => modelID === unifiedModel;
const defaultMode = (modelID: string) =>
  modelSupportsStreaming(modelID) ? "process" : "none";
const localModelOptions = [
  {
    value: unifiedModel,
    label: "Parakeet Unified English · 731 MB",
  },
  { value: "parakeet-tdt-v3", label: "Parakeet TDT v3 · 740 MB" },
];
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
  local: { label: "On-device", requiresKey: false },
};
type ProviderDraft = {
  modelID: string;
  streamingMode: string;
  languages: string;
};
export function ProfileEditor({
  profile,
  profiles,
  apiKeys,
  onSave,
  onDelete,
  onClose,
  closeGuard,
}: {
  profile: ProfileSummary | null;
  profiles: ProfileSummary[];
  apiKeys: Record<string, APIKeySummary>;
  onSave: (update: Omit<ProfileUpdate, "revision">) => Promise<void>;
  onDelete: () => Promise<void>;
  onClose: () => void;
  closeGuard: RefObject<(() => Promise<boolean>) | null>;
}) {
  const [provider, setProvider] = useState(profile?.provider ?? "openai");
  const initialName = useRef(profile?.name ?? nextName("openai", profiles));
  const [name, setName] = useState(initialName.current);
  const autoName = useRef(profile ? null : initialName.current);
  const initialLanguages = (profile?.languages ?? []).join(", ");
  const localModelID =
    profile?.provider === "local"
      ? (profile.model_id ?? unifiedModel)
      : unifiedModel;
  const localMode =
    profile?.provider === "local"
      ? (profile.streaming_mode ?? defaultMode(localModelID))
      : defaultMode(localModelID);
  const initialDrafts = useRef<Record<string, ProviderDraft>>({
    openai: {
      modelID: unifiedModel,
      streamingMode:
        profile?.provider === "openai"
          ? (profile.streaming_mode ?? "none")
          : "process",
      languages: initialLanguages,
    },
    openrouter: {
      modelID: unifiedModel,
      streamingMode: "none",
      languages: "",
    },
    local: {
      modelID: localModelID,
      streamingMode: localMode,
      languages: "",
    },
  });
  const providerDrafts = useRef(structuredClone(initialDrafts.current));
  const startingDraft = initialDrafts.current[profile?.provider ?? "openai"];
  const [modelID, setModelID] = useState(startingDraft.modelID);
  const [streamingMode, setStreamingMode] = useState(
    startingDraft.streamingMode,
  );
  const [languages, setLanguages] = useState(startingDraft.languages);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [nameError, setNameError] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [discarding, setDiscarding] = useState(false);
  const nameInput = useRef<HTMLInputElement>(null);
  const alive = useRef(true);
  const dismiss = useRef<{
    promise: Promise<boolean>;
    resolve: (value: boolean) => void;
  } | null>(null);
  const previousFocus = useRef(document.activeElement);
  const dirty =
    (profile ? name !== initialName.current : name !== autoName.current) ||
    provider !== (profile?.provider ?? "openai") ||
    (provider === "local" &&
      (modelID !== initialDrafts.current.local.modelID ||
        streamingMode !== initialDrafts.current.local.streamingMode)) ||
    (provider === "openai" &&
      (streamingMode !== initialDrafts.current.openai.streamingMode ||
        languages !== initialDrafts.current.openai.languages));
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
      dismiss.current?.resolve(false);
    };
  }, []);
  const definition = providerForms[provider as keyof typeof providerForms];
  const keySummary = apiKeys[provider];
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
                if (!validName) {
                  nameInput.current?.focus();
                  return;
                }
                setBusy(true);
                setError("");
                try {
                  await onSave({
                    id: profile?.id,
                    name: name.trim(),
                    provider,
                    ...(provider === "local"
                      ? { model_id: modelID, streaming_mode: streamingMode }
                      : {}),
                    ...(provider === "openai"
                      ? {
                          streaming_mode: streamingMode,
                          languages: languages
                            .split(",")
                            .map((code) => code.trim().toLowerCase())
                            .filter(Boolean),
                        }
                      : {}),
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
                <SegmentedControl
                  label="Provider"
                  value={provider}
                  disabled={busy}
                  options={Object.entries(providerForms).map(
                    ([value, form]) => ({ value, label: form.label }),
                  )}
                  onChange={(value) => {
                    providerDrafts.current[provider] = {
                      modelID,
                      streamingMode,
                      languages,
                    };
                    setProvider(value);
                    const next = providerDrafts.current[value];
                    setModelID(next.modelID);
                    setStreamingMode(next.streamingMode);
                    setLanguages(next.languages);
                    if (autoName.current !== null) {
                      autoName.current = nextName(value, profiles);
                      setName(autoName.current);
                    }
                  }}
                />
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
                    autoName.current = null;
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
                  <span className="field-label">API key</span>
                  <p className="quiet key-summary" role="status">
                    {keySummary?.saved
                      ? `${keySummary.masked_key} · Saved for ${definition.label}`
                      : keySummary?.error ||
                        `No ${definition.label} API key saved`}
                  </p>
                  <p className="quiet">Manage API keys in the sidebar.</p>
                </div>
              )}
              {provider === "openai" && (
                <>
                  <div className="profile-form-field">
                    <span className="field-label">Transcription mode</span>
                    <SegmentedControl
                      label="Transcription mode"
                      value={streamingMode}
                      disabled={busy}
                      options={[
                        { value: "process", label: "Upload while recording" },
                        { value: "none", label: "Record then upload" },
                      ]}
                      onChange={setStreamingMode}
                    />
                    <p className="quiet">
                      GPT Transcribe inserts the final text once, after you stop.
                      Upload while recording starts capture immediately and
                      connects in the background; transcription begins at Stop.
                    </p>
                  </div>
                  <div className="profile-form-field">
                    <label htmlFor="profile-languages">
                      Language hints (optional)
                    </label>
                    <input
                      id="profile-languages"
                      value={languages}
                      disabled={busy}
                      autoComplete="off"
                      spellCheck={false}
                      placeholder="Automatic recognition"
                      onChange={(event) => setLanguages(event.target.value)}
                    />
                    <p className="quiet">
                      Leave blank for automatic recognition, or enter language
                      codes separated by commas, for example en, es, de.
                    </p>
                  </div>
                </>
              )}
              {provider === "local" && (
                <>
                  <div className="profile-form-field">
                    <label htmlFor="local-model">Local model</label>
                    <Dropdown
                      id="local-model"
                      label="Local model"
                      value={modelID}
                      disabled={busy}
                      options={localModelOptions}
                      onChange={(value) => {
                        setModelID(value);
                        setStreamingMode(defaultMode(value));
                      }}
                    />
                    <p className="quiet">
                      Models are downloaded and verified in Local Models. Both
                      models use saved vocabulary as decoder bias.
                    </p>
                  </div>
                  <div className="profile-form-field">
                    <span className="field-label">Dictation mode</span>
                    {modelSupportsStreaming(modelID) ? (
                      <>
                        <SegmentedControl
                          label="Dictation mode"
                          value={streamingMode}
                          disabled={busy}
                          options={[
                            {
                              value: "process",
                              label: "Process while recording",
                            },
                            { value: "live", label: "Live insertion" },
                            { value: "none", label: "No streaming" },
                          ]}
                          onChange={setStreamingMode}
                        />
                        <p className="quiet">
                          Live insertion sends only committed text. Voice Type
                          never edits or removes text already inserted.
                        </p>
                      </>
                    ) : (
                      <p className="quiet">
                        No streaming. This model transcribes after recording
                        stops.
                      </p>
                    )}
                  </div>
                </>
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
          description="This profile will be removed. Its provider API key is shared and stays available to other profiles."
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
