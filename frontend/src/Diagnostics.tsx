import { useEffect, useRef, useState } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  Copy,
  Info,
  RefreshCw,
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

export function Diagnostics({
  onError,
  autoRun = 0,
}: {
  onError: (message: string) => void;
  autoRun?: number;
}) {
  const [report, setReport] = useState<DiagnosticsReport | null>(null);
  const [running, setRunning] = useState(false);
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const lastAutoRun = useRef(0);

  useEffect(
    () => () => {
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
    },
    [],
  );

  const run = async () => {
    setRunning(true);
    try {
      setReport(await api.diagnostics());
    } catch (error) {
      onError(errorText(error));
    } finally {
      setRunning(false);
    }
  };

  const copy = async () => {
    try {
      await api.copyDiagnostics();
      setCopied(true);
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
      copiedTimer.current = setTimeout(() => setCopied(false), 1800);
    } catch (error) {
      onError(errorText(error));
    }
  };

  useEffect(() => {
    if (autoRun && autoRun !== lastAutoRun.current) {
      lastAutoRun.current = autoRun;
      void run();
    }
    // run is recreated every render; only an explicit request should trigger.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoRun]);

  return (
    <section className="settings-section diagnostics" aria-labelledby="diagnostics-title">
      <div className="section-heading">
        <div>
          <h2 id="diagnostics-title">Diagnostics</h2>
          <p className="quiet section-note">
            Read-only checks for audio, insertion, models and storage. No keys,
            transcripts or audio are included.
          </p>
        </div>
        <div className="diagnostic-actions">
          {report && (
            <button disabled={running} onClick={() => void copy()}>
              <Copy size={16} aria-hidden="true" />
              {copied ? "Copied" : "Copy report"}
            </button>
          )}
          <button className="primary" disabled={running} onClick={() => void run()}>
            <RefreshCw size={16} aria-hidden="true" />
            {running ? "Checking…" : report ? "Run again" : "Run checks"}
          </button>
        </div>
      </div>
      {report ? (
        <div className="settings-group diagnostic-list" role="list">
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
                <strong>{check.label}</strong>
                <span className="quiet">{check.detail}</span>
              </div>
            );
          })}
        </div>
      ) : (
        <div className="settings-group diagnostic-empty quiet">
          Run the checks before opening an issue or asking for help.
        </div>
      )}
    </section>
  );
}
