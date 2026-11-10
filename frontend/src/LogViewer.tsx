import { Check, ChevronRight, FileText, X } from "lucide-react";
import type { LogPage, LogRecord } from "./api";
import { providerName } from "./ProfileEditor";
import { SegmentedControl } from "./ui";

const timestamp = new Intl.DateTimeFormat(undefined, {
  dateStyle: "medium",
  timeStyle: "medium",
});
const seconds = new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 });
const count = new Intl.NumberFormat();
const titles: Record<string, string> = {
  "desktop.started": "App started",
  "startup.failed": "Startup failed",
  "config.recovered": "Settings recovered",
  config_recovery: "Settings recovered",
  "preferences.recovered": "Preferences recovered",
  "preferences.invalid": "Preferences unavailable",
  preferences: "Preferences changed",
};
function eventTitle(row: LogRecord) {
  if (row.event.startsWith("dictation.") || row.event === "dictation")
    return row.outcome === "ok" ? "Dictation completed" : "Dictation failed";
  return (
    titles[row.event] ??
    row.event.replace(/[_.]/g, " ").replace(/^./, (char) => char.toUpperCase())
  );
}
export function LogViewer({
  page,
  outcome,
  onOutcome,
  onOffset,
  onExport,
  onClear,
  busy,
}: {
  page: LogPage | null;
  outcome: string;
  onOutcome: (value: string) => void;
  onOffset: (offset: number) => void;
  onExport: () => void;
  onClear: () => void;
  busy: boolean;
}) {
  const displayed = page?.records ?? [];
  const total = page?.total ?? 0;
  const retained = page?.retained ?? 0;
  const offset = page?.offset ?? 0;
  const limit = page?.limit ?? 200;
  const occurrences = new Map<string, number>();
  return (
    <>
      <div className="log-tools">
        <SegmentedControl
          label="Filter logs"
          value={outcome}
          onChange={onOutcome}
          options={[
            { value: "all", label: "All" },
            { value: "errors", label: "Errors" },
            { value: "ok", label: "Successful" },
          ]}
        />
        <div className="actions">
          <button disabled={busy || !total} onClick={onExport}>
            Export
          </button>
          <button disabled={busy || !retained} onClick={onClear}>
            Clear
          </button>
        </div>
      </div>
      {total > 0 && (
        <div className="log-pager">
          <button
            disabled={busy || offset <= 0}
            onClick={() => onOffset(offset - limit)}
          >
            Newer
          </button>
          <span role="status">
            {offset + 1}–{offset + displayed.length} of {total}
          </span>
          <button
            disabled={busy || offset + limit >= total}
            onClick={() => onOffset(offset + limit)}
          >
            Older
          </button>
        </div>
      )}
      <section className="log-list" aria-label="Log entries">
        {displayed.length ? (
          displayed.map((row) => {
            // Polling may insert earlier rows. Identity must not depend on array index.
            const identity = JSON.stringify([
              row.timestamp,
              row.event,
              row.provider,
              row.configured_model,
              row.outcome,
              row.total_ms,
              row.message,
              row.error,
            ]);
            const occurrence = occurrences.get(identity) ?? 0;
            occurrences.set(identity, occurrence + 1);
            const date = row.timestamp ? new Date(row.timestamp) : null;
            const dictation =
              row.event.startsWith("dictation.") || row.event === "dictation";
            return (
              <details key={`${identity}:${occurrence}`} className="log-entry">
                <summary>
                  <ChevronRight
                    size={16}
                    className="expansion-chevron"
                    aria-hidden="true"
                  />
                  <span
                    className={`log-icon ${row.outcome === "ok" ? "ok" : "failed"}`}
                    aria-hidden="true"
                  >
                    {row.outcome === "ok" ? (
                      <Check size={16} />
                    ) : (
                      <X size={16} />
                    )}
                  </span>
                  <span className="log-summary">
                    <strong>{eventTitle(row)}</strong>
                    <small>
                      {date && Number.isFinite(date.getTime()) ? (
                        <time dateTime={row.timestamp}>
                          {timestamp.format(date)}
                        </time>
                      ) : (
                        "Time unavailable"
                      )}
                      {row.provider && (
                        <span>{providerName(row.provider)}</span>
                      )}
                    </small>
                  </span>
                  {row.total_ms > 0 && (
                    <span className="duration">
                      {seconds.format(row.total_ms / 1000)} s
                    </span>
                  )}
                </summary>
                <div className="log-detail">
                  <dl>
                    {dictation && (
                      <>
                        <dt>Model</dt>
                        <dd>{row.configured_model || "—"}</dd>
                        <dt>Audio</dt>
                        <dd>{seconds.format(row.audio_ms / 1000)} s</dd>
                        <dt>Attempts</dt>
                        <dd>{count.format(row.attempt_count)}</dd>
                      </>
                    )}
                    <dt>Outcome</dt>
                    <dd>{row.outcome.replaceAll("_", " ")}</dd>
                    {row.message && (
                      <>
                        <dt>Details</dt>
                        <dd>{row.message}</dd>
                      </>
                    )}
                    {row.error && (
                      <>
                        <dt>Error</dt>
                        <dd className="field-error">{row.error}</dd>
                      </>
                    )}
                  </dl>
                  {!!row.attempts?.length && (
                    <details className="technical-details">
                      <summary>Technical details</summary>
                      <pre>{JSON.stringify(row.attempts, null, 2)}</pre>
                    </details>
                  )}
                </div>
              </details>
            );
          })
        ) : (
          <div className="empty">
            <FileText size={24} aria-hidden="true" />
            <h2>
              {total
                ? "No records on this page"
                : retained
                  ? "No matching logs"
                  : "No logs yet"}
            </h2>
          </div>
        )}
      </section>
    </>
  );
}
