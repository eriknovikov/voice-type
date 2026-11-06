import { useId, useRef, useState, useEffect, type ReactNode } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { MoreHorizontal } from "lucide-react";

export function SegmentedControl({
  label,
  value,
  options,
  onChange,
  disabled = false,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  const name = useId();
  return (
    <fieldset className="segmented" disabled={disabled}>
      <legend className="sr-only">{label}</legend>
      {options.map((option) => (
        <label key={option.value}>
          <input
            type="radio"
            name={name}
            value={option.value}
            checked={value === option.value}
            onChange={() => onChange(option.value)}
          />
          <span>{option.label}</span>
        </label>
      ))}
    </fieldset>
  );
}

export function ConfirmDialog({
  title,
  description,
  confirmLabel,
  cancelLabel = "Cancel",
  onConfirm,
  onCancel,
}: {
  title: string;
  description: string;
  confirmLabel: string;
  cancelLabel?: string;
  onConfirm: () => Promise<void> | void;
  onCancel: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const previousFocus = useRef(document.activeElement);
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open && !busy) onCancel();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="dialog"
          onEscapeKeyDown={(event) => {
            if (busy) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (busy) event.preventDefault();
          }}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (previousFocus.current instanceof HTMLElement)
              previousFocus.current.focus();
          }}
        >
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>{description}</Dialog.Description>
          {error && (
            <p className="field-error" role="alert">
              {error}
            </p>
          )}
          <div className="dialog-actions">
            <button disabled={busy} onClick={onCancel}>
              {cancelLabel}
            </button>
            <button
              className="danger"
              disabled={busy}
              onClick={async () => {
                setBusy(true);
                setError("");
                try {
                  await onConfirm();
                  onCancel();
                } catch (e) {
                  setError(e instanceof Error ? e.message : String(e));
                } finally {
                  setBusy(false);
                }
              }}
            >
              {busy ? "Working…" : confirmLabel}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export function ActionsMenu({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  const menu = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const close = (event: PointerEvent) => {
      if (
        event.target instanceof Node &&
        !menu.current?.contains(event.target) &&
        menu.current
      )
        menu.current.open = false;
    };
    document.addEventListener("pointerdown", close);
    return () => document.removeEventListener("pointerdown", close);
  }, []);
  return (
    <details
      ref={menu}
      className="actions-menu"
      onKeyDown={(event) => {
        if (event.key === "Escape" && menu.current?.open) {
          event.preventDefault();
          event.stopPropagation();
          menu.current.open = false;
          menu.current.querySelector("summary")?.focus();
        }
      }}
    >
      <summary className="icon-button" aria-label={label} title={label}>
        <MoreHorizontal size={18} aria-hidden="true" />
      </summary>
      <div
        className="actions-popover"
        onClick={(event) => {
          if ((event.target as HTMLElement).closest("button") && menu.current) {
            menu.current.open = false;
            menu.current.querySelector("summary")?.focus();
          }
        }}
      >
        {children}
      </div>
    </details>
  );
}
