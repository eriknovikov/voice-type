import { useEffect, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Plus, Trash2, Check, BookOpen } from "lucide-react";
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
  onRename: (name: string) => boolean;
  onTerms: (terms: string[]) => void;
  onDelete: () => void;
  onActivate: () => void;
}) {
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const list = lists.find((item) => item.name === selected);
  const [nameDrafts, setNameDrafts] = useState<Record<string, string>>({});
  const nameDraft = nameDrafts[selected] ?? selected;
  const [renameError, setRenameError] = useState("");
  useEffect(() => {
    setRenameError("");
  }, [selected]);
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
              onClick={() => onSelect(item.name)}
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
                    value={nameDraft}
                    aria-invalid={!!renameError}
                    onChange={(event) => {
                      setNameDrafts({
                        ...nameDrafts,
                        [selected]: event.target.value,
                      });
                      setRenameError("");
                    }}
                    onBlur={() => {
                      if (!onRename(nameDraft))
                        setRenameError("Choose a unique name.");
                    }}
                  />
                )}
                {renameError && (
                  <p className="field-error" role="alert">
                    {renameError}
                  </p>
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
            <label htmlFor="vocabulary-terms">Words and phrases</label>
            <p
              id="vocabulary-terms-hint"
              className="quiet vocabulary-terms-hint"
            >
              One word or phrase per line.
            </p>
            <textarea
              id="vocabulary-terms"
              className="vocabulary-terms"
              aria-describedby="vocabulary-terms-hint"
              disabled={busy}
              placeholder={"Voice Type\nOpenAI\nWails"}
              value={list.terms.join("\n")}
              onChange={(event) => onTerms(event.target.value.split(/\r?\n/))}
              spellCheck={false}
            />
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
