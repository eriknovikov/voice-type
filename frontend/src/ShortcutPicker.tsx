import { useEffect, useState, useRef, type KeyboardEvent } from "react";
import { api } from "./api";
// Release the global binding before capturing and persist only after release.
let captureTransition: Promise<unknown> = Promise.resolve();
let captureResult: Promise<unknown> = Promise.resolve();
export const flushShortcutCapture = () => captureResult.then(() => {});
function changeCapture(action: () => Promise<void>): Promise<void> {
  const next = captureTransition.then(action, action);
  captureResult = next;
  captureTransition = next.catch(() => {});
  return next;
}
export function shortcutFromKey(
  event: Pick<
    KeyboardEvent,
    "key" | "ctrlKey" | "altKey" | "shiftKey" | "metaKey"
  >,
): string | null {
  if (
    [
      "Control",
      "Alt",
      "Shift",
      "Meta",
      "Escape",
      "Dead",
      "Unidentified",
    ].includes(event.key)
  )
    return null;
  const names: Record<string, string> = {
    " ": "Space",
    ArrowUp: "Up",
    ArrowDown: "Down",
    ArrowLeft: "Left",
    ArrowRight: "Right",
    "+": "Plus",
  };
  const key =
    names[event.key] ??
    (event.key.length === 1 ? event.key.toUpperCase() : event.key);
  return [
    ...(event.ctrlKey ? ["Ctrl"] : []),
    ...(event.altKey ? ["Alt"] : []),
    ...(event.shiftKey ? ["Shift"] : []),
    ...(event.metaKey ? ["Super"] : []),
    key,
  ].join("+");
}
export function ShortcutPicker({
  name,
  value,
  onChange,
  onError,
}: {
  name: string;
  value: string;
  onChange: (value: string) => Promise<void>;
  onError: (message: string) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [ready, setReady] = useState(false);
  const [pending, setPending] = useState<string | null>(null);
  const active = useRef(false);
  const draft = useRef<string | null>(null);
  const change = useRef(onChange);
  change.current = onChange;
  const finish = (next: string | null) => {
    if (!active.current) return;
    active.current = false;
    setEditing(false);
    setReady(false);
    void changeCapture(async () => {
      await api.endShortcutCapture();
      if (next !== null) await change.current(next);
    }).catch((error) => onError(String(error)));
  };
  useEffect(
    () => () => {
      if (active.current) {
        active.current = false;
        const next = draft.current;
        void changeCapture(async () => {
          await api.endShortcutCapture();
          if (next !== null) await change.current(next);
        }).catch((error) => onError(String(error)));
      }
    },
    [],
  );
  useEffect(() => {
    const close = () => finish(draft.current);
    window.addEventListener("voice-type-cancel-capture", close);
    return () => window.removeEventListener("voice-type-cancel-capture", close);
  }, []);
  return (
    <div className="shortcut-row">
      <div className="shortcut-description">
        <span>{name}</span>
      </div>
      <div className="shortcut-control">
        <button
          className="shortcut-key"
          aria-label={`Change ${name.toLowerCase()} shortcut`}
          aria-pressed={editing}
          onClick={() => {
            if (active.current) return;
            active.current = true;
            draft.current = null;
            setPending(null);
            setEditing(true);
            setReady(false);
            void changeCapture(api.beginShortcutCapture)
              .then(() => {
                if (active.current) setReady(true);
              })
              .catch((error) => {
                active.current = false;
                setEditing(false);
                onError(String(error));
              });
          }}
          onBlur={() => finish(draft.current)}
          onKeyDown={(event) => {
            if (!active.current) return;
            if (event.key === "Tab") {
              finish(draft.current);
              return;
            }
            event.preventDefault();
            event.stopPropagation();
            if (event.key === "Escape") {
              finish("");
              return;
            }
            if (!ready || event.repeat) return;
            if (event.key === "Enter" && draft.current !== null) {
              finish(draft.current);
              return;
            }
            const shortcut = shortcutFromKey(event);
            if (shortcut) {
              draft.current = shortcut;
              setPending(shortcut);
            }
          }}
        >
          {(editing ? pending : value)
            ? (editing ? pending! : value).split("+").map((key, index) => (
                <span className="key-part" key={`${key}-${index}`}>
                  {index > 0 && (
                    <span className="key-plus" aria-hidden="true">
                      +
                    </span>
                  )}
                  <kbd>{key}</kbd>
                </span>
              ))
            : editing
              ? ready
                ? "Press keys…"
                : "Preparing…"
              : "Disabled"}
        </button>
        {!editing && value !== "Ctrl+Space" && (
          <button
            className="text-button"
            onClick={() =>
              void changeCapture(() => change.current("Ctrl+Space")).catch(
                (error) => onError(String(error)),
              )
            }
          >
            Reset
          </button>
        )}
      </div>
    </div>
  );
}
