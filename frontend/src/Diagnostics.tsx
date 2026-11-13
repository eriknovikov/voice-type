import { useEffect, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import {
  AlertTriangle,
  CheckCircle2,
  Copy,
  Info,
  RefreshCw,
  X,
  XCircle,
} from "lucide-react";
import { api, type DiagnosticsReport } from "./api";

const icons = {
  ok: CheckCircle2,
  warn: AlertTriangle,
  fail: XCircle,
  info: Info,
} as const;
const errorText = (error: unknown) =>
  error instanceof Error ? error.message : String(error);

export function Diagnostics({ onClose }: { onClose: () => void }) {
  const [report, setReport] = useState<DiagnosticsReport | null>(null);
  const [running, setRunning] = useState(true);
  const [error, setError] = useState("");
  const [runRequest, setRunRequest] = useState(0);
  const [copied, setCopied] = useState(false);
  const [copying, setCopying] = useState(false);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const previousFocus = useRef(document.activeElement);

  useEffect(
    () => () => {
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
    },
    [],
  );

  useEffect(() => {
    let active = true;
    setRunning(true);
    setError("");
    setCopied(false);
    if (copiedTimer.current) clearTimeout(copiedTimer.current);
    void api.diagnostics().then(
      (value) => {
        if (!active) return;
        setReport(value);
        setRunning(false);
      },
      (error) => {
        if (!active) return;
        setReport(null);
        setError(errorText(error));
        setRunning(false);
      },
    );
    return () => {
      active = false;
    };
  }, [runRequest]);

  const copy = async () => {
    setCopying(true);
    setError("");
    try {
      await api.copyDiagnostics();
      setCopied(true);
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
      copiedTimer.current = setTimeout(() => setCopied(false), 1800);
    } catch (error) {
      setError(errorText(error));
    } finally {
      setCopying(false);
    }
  };

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="dialog diagnostics-dialog"
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (previousFocus.current instanceof HTMLElement)
              previousFocus.current.focus();
          }}
        >
          <div className="diagnostics-header">
            <Dialog.Title>Diagnostics</Dialog.Title>
            <Dialog.Close asChild>
              <button
                className="icon-button"
                aria-label="Close diagnostics"
                title="Back to Voice Type"
              >
                <X size={18} aria-hidden="true" />
              </button>
            </Dialog.Close>
          </div>
          <Dialog.Description className="quiet diagnostics-description">
            Diagnostics about your system stack relevant to Voice Type, useful for troubleshooting.
          </Dialog.Description>
          <div className="diagnostics-body" aria-busy={running}>
            {error && (
              <p className="field-error" role="alert">
                {error}
              </p>
            )}
            {running && (
              <p className="quiet diagnostic-empty" role="status">
                Checking your system…
              </p>
            )}
            {report && (
              <div
                className="settings-group diagnostic-list"
                role="list"
                aria-label="System checks"
              >
                {report.checks.map((check) => {
                  const Icon = icons[check.status] ?? Info;
                  return (
                    <div
                      className="diagnostic-row"
                      role="listitem"
                      key={`${check.label}-${check.detail}`}
                    >
                      <Icon
                        className={`diagnostic-icon ${check.status}`}
                        size={18}
                        aria-hidden="true"
                      />
                      <strong>
                        {check.label}
                        <span className="sr-only"> ({check.status})</span>
                      </strong>
                      <span className="quiet">{check.detail}</span>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
          <div className="diagnostics-footer">
            <button
              disabled={running || copying}
              onClick={() => setRunRequest((value) => value + 1)}
            >
              <RefreshCw size={16} aria-hidden="true" />
              {running ? "Checking…" : "Run again"}
            </button>
            <button
              className="primary"
              disabled={!report || running || copying}
              onClick={() => void copy()}
            >
              <Copy size={16} aria-hidden="true" />
              <span role="status">
                {copying ? "Copying…" : copied ? "Copied" : "Copy to clipboard"}
              </span>
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
