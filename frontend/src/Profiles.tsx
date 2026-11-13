import { Plus, Pencil } from "lucide-react";
import type { ProfileSummary } from "./api";
import { providerName } from "./ProfileEditor";

const modelName = (id?: string) =>
  id === "parakeet-tdt-v3" ? "Parakeet TDT v3" : "Parakeet Unified English";
const modeName = (mode?: string) =>
  mode === "live" ? "Live" : mode === "none" ? "Batch" : "Streaming";

export function Profiles({
  profiles,
  active,
  busy,
  onSelect,
  onEdit,
  onCreate,
}: {
  profiles: ProfileSummary[];
  active: string;
  busy: boolean;
  onSelect: (id: string) => void;
  onEdit: (profile: ProfileSummary) => void;
  onCreate: () => void;
}) {
  return (
    <section className="settings-section">
      <div className="section-heading">
        <h2 id="profiles-title">Profiles</h2>
        {!!profiles.length && (
          <button onClick={onCreate}>
            <Plus size={16} aria-hidden="true" />
            Add profile
          </button>
        )}
      </div>
      {profiles.length ? (
        <div
          className="settings-group profile-list"
          role="radiogroup"
          aria-labelledby="profiles-title"
        >
          {profiles.map((profile) => (
            <div
              className={`profile-row ${active === profile.id ? "is-active" : ""}`}
              key={profile.id}
            >
              <label className="profile-select">
                <input
                  type="radio"
                  name="active-profile"
                  value={profile.id}
                  aria-label={`Use ${profile.name}`}
                  checked={active === profile.id}
                  disabled={busy}
                  onChange={() => onSelect(profile.id)}
                />
                <span className="profile-summary">
                  <strong title={profile.name}>{profile.name}</strong>
                  <span className="profile-meta">
                    <span>{providerName(profile.provider)}</span>
                    {profile.provider === "local" ? (
                      <span>
                        {modelName(profile.model_id)} ·{" "}
                        {modeName(profile.streaming_mode)}
                      </span>
                    ) : profile.provider === "openai" ? (
                      <span>
                        {profile.streaming_mode === "process"
                          ? "Streaming"
                          : "Batch"}
                      </span>
                    ) : (
                      <span>Batch</span>
                    )}
                  </span>
                </span>
              </label>
              <button
                className="icon-button"
                disabled={busy}
                aria-label={`Edit ${profile.name}`}
                title="Edit profile"
                onClick={() => onEdit(profile)}
              >
                <Pencil size={16} aria-hidden="true" />
              </button>
            </div>
          ))}
        </div>
      ) : (
        <div className="settings-group profile-empty">
          <h3>Welcome to Voice Type</h3>
          <p className="quiet">
            Add your first profile to choose how you’d like to transcribe.
          </p>
          <button className="primary" onClick={onCreate}>
            <Plus size={16} aria-hidden="true" />
            Add profile
          </button>
        </div>
      )}
    </section>
  );
}
