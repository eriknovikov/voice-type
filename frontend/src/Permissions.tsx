import type { PermissionState, PermissionStatus } from "./api";

type Permission = "microphone" | "accessibility";
const label = (state: PermissionState) => {
  switch (state) {
    case "granted": return "Allowed";
    case "denied": return "Not allowed";
    case "restricted": return "Restricted by system policy";
    default: return "Not requested";
  }
};

export function Permissions({ status, busy, request, openSettings }: {
  status: PermissionStatus;
  busy: boolean;
  request: (permission: Permission) => void;
  openSettings: (permission: Permission) => void;
}) {
  return (
    <section className="settings-section" aria-label="macOS permissions">
      <div className="section-heading"><h2>macOS permissions</h2></div>
      <p className="quiet section-note">Allow microphone access for recording and Accessibility access for inserting text. Voice Type only requests access when you choose Allow.</p>
      <div className="settings-group">
      {(["microphone", "accessibility"] as const).map((permission) => {
        const name = permission === "microphone" ? "Microphone" : "Accessibility";
        const state = status[permission];
        const canRequest = state === "unknown" || (permission === "accessibility" && state === "denied");
        return (
          <div key={permission} className="setting-row">
            <div className="setting-label">
              <strong>{name}: {label(state)}</strong>
              {state === "restricted" && <p className="quiet">Contact your administrator to change this permission.</p>}
            </div>
            <div className="setting-control align-end">
              {canRequest && <button disabled={busy} onClick={() => request(permission)}>Allow {name.toLowerCase()}</button>}
              <button disabled={busy} onClick={() => openSettings(permission)}>Open {name} settings</button>
            </div>
          </div>
        );
      })}
      </div>
      <p className="quiet section-note">After changing access in System Settings, return here. If macOS asks you to quit and reopen Voice Type, do so. No Screen Recording or Input Monitoring access is needed.</p>
    </section>
  );
}
