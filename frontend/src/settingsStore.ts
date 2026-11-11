import type { Settings, SettingsUpdate } from "./api";

type Patch = Omit<SettingsUpdate, "revision">;
type Field = keyof Patch;
type Pending = { value: unknown; version: number };
export interface SaveState {
  settings: Settings | null;
  status: "idle" | "pending" | "saving" | "saved" | "error";
  error: string;
}

// One queue owns every config revision. Responses are merged with newer drafts,
// so selecting a profile never discards vocabulary still being typed.
export class SettingsStore {
  private saved: Settings | null = null;
  private pending = new Map<Field, Pending>();
  private version = 0;
  private tail: Promise<unknown> = Promise.resolve();
  private timer: ReturnType<typeof setTimeout> | undefined;
  private listeners = new Set<() => void>();
  private state: SaveState = { settings: null, status: "idle", error: "" };
  constructor(
    private read: () => Promise<Settings>,
    private save: (update: SettingsUpdate) => Promise<Settings>,
  ) {}
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };
  getSnapshot = () => this.state;
  private publish(status = this.state.status, error = this.state.error) {
    const settings = this.saved ? { ...this.saved } : null;
    if (settings) {
      for (const [field, item] of this.pending) {
        Object.assign(settings, { [field]: item.value });
      }
      if (!settings.vocabulary.some((list) => list.name === "general")) {
        settings.vocabulary = [
          { name: "general", terms: [] },
          ...settings.vocabulary,
        ];
      }
    }
    this.state = { settings, status, error };
    this.listeners.forEach((listener) => listener());
  }
  load = async () => {
    this.saved = await this.read();
    this.publish();
  };
  patch = (patch: Patch, debounce = false) => {
    for (const field of Object.keys(patch) as Field[]) {
      this.pending.set(field, { value: patch[field], version: ++this.version });
    }
    this.publish("pending", "");
    clearTimeout(this.timer);
    if (debounce)
      this.timer = setTimeout(() => void this.flush().catch(() => {}), 500);
    else void this.flush().catch(() => {});
  };
  private enqueue<T>(operation: () => Promise<T>): Promise<T> {
    const result = this.tail.then(operation);
    this.tail = result.catch(() => {});
    return result;
  }
  flush = () => {
    clearTimeout(this.timer);
    return this.enqueue(async () => {
      while (this.pending.size) {
        if (!this.saved) throw new Error("Settings are still loading.");
        const entries = new Map(this.pending);
        const patch = Object.fromEntries(
          [...entries].map(([key, entry]) => [key, entry.value]),
        );
        this.publish("saving", "");
        try {
          this.saved = await this.save({
            revision: this.saved.revision,
            ...patch,
          });
          for (const [field, entry] of entries) {
            if (this.pending.get(field)?.version === entry.version)
              this.pending.delete(field);
          }
          // A rename may have completed while the next text edit was queued.
          const vocabulary = this.pending.get("vocabulary");
          if (vocabulary) {
            vocabulary.value = (vocabulary.value as Settings["vocabulary"]).map(
              (list) =>
                this.saved!.vocabulary.some((saved) => saved.name === list.name)
                  ? { ...list, original_name: list.name }
                  : list,
            );
          }
          this.publish(this.pending.size ? "pending" : "saved", "");
        } catch (error) {
          this.publish(
            "error",
            error instanceof Error ? error.message : String(error),
          );
          throw error;
        }
      }
    });
  };
  // Retrying is explicit after conflicts: refresh the revision while retaining
  // all failed edits, then submit those edits with the user's retry action.
  retry = () =>
    this.enqueue(async () => {
      this.saved = await this.read();
      this.publish("pending", "");
    }).then(this.flush);
  mutate = (operation: (revision: string) => Promise<Settings>) =>
    this.enqueue(async () => {
      if (!this.saved) throw new Error("Settings are still loading.");
      try {
        this.saved = await operation(this.saved.revision);
      } catch (error) {
        // Keep the dialog/draft and make its next explicit attempt usable after
        // a hand edit or automatic device fallback changed the revision.
        if (String(error).includes("settings changed elsewhere")) {
          this.saved = await this.read();
          this.publish();
          throw new Error("Settings changed elsewhere. Try again.");
        }
        throw error;
      }
      this.publish(this.pending.size ? "pending" : this.state.status);
    });
  refresh = () =>
    this.enqueue(async () => {
      this.saved = await this.read();
      this.publish();
    });
  updateWorkspace = (workspace: string) => {
    if (this.saved) this.saved = { ...this.saved, workspace };
    this.publish();
  };
  mutateAPIKeys = (operation: () => Promise<Settings>) =>
    this.enqueue(async () => {
      const result = await operation();
      if (this.saved) this.saved = { ...this.saved, api_keys: result.api_keys };
      this.publish();
    });
  dispose = () => clearTimeout(this.timer);
}
