import { useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Plus, Trash2, Check, X, BookOpen } from "lucide-react";
import type { VocabularyList } from "./api";
export function VocabularyEditor({
  lists,
  selected,
  active,
  busy,
  onSelect,
  onCreate,
  onRename,
  onTerms,
  onDelete,
  onActivate,
}: {
  lists: VocabularyList[];
  selected: string;
  active: string;
  busy: boolean;
  onSelect: (name: string) => void;
  onCreate: (name: string) => void;
  onRename: (name: string) => void;
  onTerms: (terms: string[]) => void;
  onDelete: () => void;
  onActivate: () => void;
}) {
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [term, setTerm] = useState("");
  const list = lists.find((item) => item.name === selected);
  const addTerms = (values: string[]) => {
    if (!list) return;
    const seen = new Set(list.terms.map((value) => value.trim().toLowerCase()));
    const added = values
      .map((value) => value.trim())
      .filter((value) => {
        if (!value || seen.has(value.toLowerCase())) return false;
        seen.add(value.toLowerCase());
        return true;
      });
    if (added.length) onTerms([...list.terms, ...added]);
    setTerm("");
  };
  const inUse = selected === "general" ? active === "" : selected === active;
  return (
    <div className="vocabulary-layout">
      <div className="vocabulary-nav">
        <div className="vocabulary-nav-heading">
          <span>Vocabularies</span>
          <button
            className="icon-button"
            aria-label="New vocabulary"
            title="New vocabulary"
            disabled={busy}
            onClick={() => {
              setName("");
              setError("");
              setCreating(true);
            }}
          >
            <Plus size={16} />
          </button>
        </div>
        <div
          className="vocabulary-list"
          role="navigation"
          aria-label="Vocabularies"
        >
          {lists.map((item) => (
            <button
              key={item.original_name ?? item.name}
              className={selected === item.name ? "selected" : ""}
              aria-current={selected === item.name ? "true" : undefined}
              onClick={() => {
                onSelect(item.name);
                setTerm("");
              }}
            >
              <BookOpen size={15} />
              <span>{item.name === "general" ? "General" : item.name}</span>
              {item.name === active || (item.name === "general" && !active) ? (
                <Check size={14} aria-label="In use" />
              ) : (
                <small>{item.terms.filter(Boolean).length}</small>
              )}
            </button>
          ))}
        </div>
        <p className="quiet">General is always included.</p>
      </div>
      <section className="vocabulary-detail">
        {list ? (
          <>
            <div className="vocabulary-heading">
              <div>
                {selected === "general" ? (
                  <h2>General</h2>
                ) : (
                  <input
                    className="name-input"
                    disabled={busy}
                    aria-label="Vocabulary name"
                    value={list.name}
                    onChange={(event) => onRename(event.target.value)}
                  />
                )}
                <p>
                  {list.terms.filter(Boolean).length} terms
                  {selected === "general"
                    ? " · Included in every dictation"
                    : ""}
                </p>
              </div>
              <div className="actions">
                {inUse ? (
                  <span className="badge">
                    <Check size={13} />
                    In use
                  </span>
                ) : (
                  <button disabled={busy} onClick={onActivate}>
                    {selected === "general"
                      ? "Use general only"
                      : "Use this vocabulary"}
                  </button>
                )}
                {selected !== "general" && (
                  <button
                    className="icon-button danger-text"
                    disabled={busy}
                    aria-label="Delete vocabulary"
                    title="Delete vocabulary"
                    onClick={onDelete}
                  >
                    <Trash2 size={16} />
                  </button>
                )}
              </div>
            </div>
            <form
              className="add-term"
              onSubmit={(event) => {
                event.preventDefault();
                addTerms([term]);
              }}
            >
              <input
                disabled={busy}
                aria-label="New term"
                placeholder="Add a name or term"
                value={term}
                onChange={(event) => setTerm(event.target.value)}
                onPaste={(event) => {
                  const pasted = event.clipboardData.getData("text");
                  if (pasted.includes("\n")) {
                    event.preventDefault();
                    addTerms(pasted.split(/\r?\n/));
                  }
                }}
              />
              <button disabled={busy || !term.trim()} type="submit">
                <Plus size={15} />
                Add
              </button>
            </form>
            <div className="term-list">
              {list.terms.length ? (
                list.terms.map((value, index) => (
                  <div className="term-row" key={index}>
                    <input
                      disabled={busy}
                      aria-label={`Term ${index + 1}`}
                      value={value}
                      onChange={(event) =>
                        onTerms(
                          list.terms.map((previous, i) =>
                            i === index ? event.target.value : previous,
                          ),
                        )
                      }
                    />
                    <button
                      className="icon-button"
                      disabled={busy}
                      title="Remove term"
                      aria-label={`Remove term ${index + 1}`}
                      onClick={() =>
                        onTerms(list.terms.filter((_, i) => i !== index))
                      }
                    >
                      <X size={14} />
                    </button>
                  </div>
                ))
              ) : (
                <div className="empty-terms">
                  <BookOpen size={24} />
                  <h3>No terms yet</h3>
                  <p>
                    Add names or terminology you use.
                    <br />
                    You can also paste several lines at once.
                  </p>
                </div>
              )}
            </div>
            <p className="quiet term-help">
              Changes apply to the next dictation after saving.
            </p>
          </>
        ) : (
          <div className="empty">
            <p>Select a vocabulary to edit its terms.</p>
          </div>
        )}
      </section>
      <Dialog.Root open={creating} onOpenChange={setCreating}>
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content className="dialog">
            <Dialog.Title>New vocabulary</Dialog.Title>
            <Dialog.Description>
              Give this vocabulary a name.
            </Dialog.Description>
            <form
              onSubmit={(event) => {
                event.preventDefault();
                const value = name.trim();
                if (
                  !value ||
                  lists.some(
                    (item) => item.name.toLowerCase() === value.toLowerCase(),
                  )
                ) {
                  setError("Choose a unique name.");
                  return;
                }
                onCreate(value);
                setCreating(false);
              }}
            >
              <label htmlFor="new-vocabulary-name">Name</label>
              <input
                id="new-vocabulary-name"
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="For example, medical terms"
              />
              {error && (
                <p className="field-error" role="alert">
                  {error}
                </p>
              )}
              <div className="dialog-actions">
                <Dialog.Close asChild>
                  <button type="button">Cancel</button>
                </Dialog.Close>
                <button className="primary" type="submit">
                  Create vocabulary
                </button>
              </div>
            </form>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
