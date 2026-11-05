import { useEffect, useState, useRef, type KeyboardEvent } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { api } from "./api";
// Keep releases and captures in order when switching between the two pickers.
let captureTransition: Promise<unknown> = Promise.resolve();
function changeCapture(action: () => Promise<void>): Promise<void> {
  const next = captureTransition.then(action, action);
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
  onChange: (value: string) => void;
  onError: (message: string) => void;
}) {
  const captureRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [ready, setReady] = useState(false);
  const [pending, setPending] = useState("");
  useEffect(() => {
    if (!open) return;
    let canceled = false;
    setReady(false);
    setPending("");
    void changeCapture(api.beginShortcutCapture)
      .then(() => {
        if (!canceled) setReady(true);
      })
      .catch((error) => {
        onError(String(error));
        setOpen(false);
      });
    return () => {
      canceled = true;
      void changeCapture(api.endShortcutCapture).catch((error) =>
        onError(String(error)),
      );
    };
  }, [open]);
  useEffect(() => {
    const close = () => setOpen(false);
    window.addEventListener("voice-type-cancel-capture", close);
    return () => window.removeEventListener("voice-type-cancel-capture", close);
  }, []);
  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <div className="shortcut-row">
        <span>{name}</span>
        <Dialog.Trigger asChild>
          <button
            className="shortcut-key"
            aria-label={`Change ${name.toLowerCase()} shortcut`}
          >
            {value.split("+").join(" + ")}
          </button>
        </Dialog.Trigger>
      </div>
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="dialog shortcut-dialog"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            captureRef.current?.focus();
          }}
        >
          <Dialog.Title>{name}</Dialog.Title>
          <Dialog.Description>
            {ready
              ? "Press the keys you want to use."
              : "Preparing key capture…"}
          </Dialog.Description>
          <div
            ref={captureRef}
            tabIndex={0}
            role="group"
            aria-label="Shortcut keys"
            className="shortcut-preview"
            aria-live="polite"
            onKeyDown={(event) => {
              if (
                event.key === "Escape" ||
                (event.key === "Tab" &&
                  !event.ctrlKey &&
                  !event.altKey &&
                  !event.metaKey)
              )
                return;
              event.preventDefault();
              event.stopPropagation();
              if (ready && !event.repeat) {
                const result = shortcutFromKey(event);
                if (result) setPending(result);
              }
            }}
          >
            {pending ? pending.split("+").join(" + ") : "Press a shortcut"}
          </div>
          <div className="row">
            <Dialog.Close asChild>
              <button>Cancel</button>
            </Dialog.Close>
            <button
              className="primary"
              disabled={!pending || !ready}
              onClick={() => {
                onChange(pending);
                setOpen(false);
              }}
            >
              Set shortcut
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
