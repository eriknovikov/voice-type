import { LocalModelDownload, formatModelSize } from "./LocalModelDownload";
import type { LocalModelDownloads } from "./useLocalModels";
const formatRAM = (bytes: number) =>
  `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GiB`;

export function LocalModels({ downloads }: { downloads: LocalModelDownloads }) {
  const { models, busy, errors, loadError, actions } = downloads;
  return (
    <section className="settings-section local-models">
      {loadError && (
        <p className="field-error" role="alert">
          {loadError}
        </p>
      )}
      {models.map((model) => (
        <article className="settings-group local-model-card" key={model.id}>
          <div className="local-model-heading">
            <div>
              <h2>{model.name}</h2>
              <p className="quiet">
                {model.languages} · {formatModelSize(model.bytes)}
              </p>
              <p
                className="quiet"
                title="Approximate resident memory while the model is loaded"
              >
                ~{formatRAM(model.ram_bytes)} RAM when loaded
              </p>
            </div>
          </div>
          <LocalModelDownload
            model={model}
            busy={busy}
            pendingAction={
              downloads.pendingID === model.id ? downloads.pendingAction : null
            }
            error={errors[model.id]}
            {...actions}
          />
        </article>
      ))}
      {!models.length && <p className="quiet">Loading…</p>}
    </section>
  );
}
