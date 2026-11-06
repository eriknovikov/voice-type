import { useEffect, useRef, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { Plus, Trash2, Check, Pencil, BookOpen } from "lucide-react";
import type { VocabularyList } from "./api";
import { Dropdown } from "./Dropdown";
import { ActionsMenu } from "./ui";
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
  const [renaming, setRenaming] = useState(false);
  const [nameDrafts, setNameDrafts] = useState<Record<string, string>>({});
  const [renameError, setRenameError] = useState("");
  const renameInput = useRef<HTMLInputElement>(null);
  const renameButton = useRef<HTMLButtonElement>(null);
  const restoreRenameFocus = useRef(false);
  const createButton = useRef<HTMLButtonElement>(null);
  const skipRename = useRef(false);
  const list = lists.find((item) => item.name === selected);
  const nameDraft = nameDrafts[selected] ?? selected;
  const termCount = list?.terms.filter((term) => term.trim()).length ?? 0;
  const inUse = selected !== "general" && selected === active;
  useEffect(() => {
    setRenameError("");
    setRenaming(false);
  }, [selected]);
  useEffect(() => {
    if (renaming) {
      renameInput.current?.focus();
      renameInput.current?.select();
    }
  }, [renaming]);
  return (
    <div className="vocabulary-layout">
      <div className="vocabulary-nav">
        <div className="vocabulary-nav-heading">
          <span className="vocabulary-nav-label">Vocabularies</span>
          <div className="vocabulary-mobile-selector">
            <Dropdown
              label="Vocabulary to edit"
              value={selected}
              disabled={busy}
              onChange={onSelect}
              options={lists.map((item) => ({
                value: item.name,
                label: item.name === "general" ? "General" : item.name,
              }))}
            />
          </div>
          <button
            ref={createButton}
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
            <Plus size={16} aria-hidden="true" />
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
              title={item.name === "general" ? "General" : item.name}
              disabled={busy}
              onClick={() => onSelect(item.name)}
            >
              <BookOpen size={15} aria-hidden="true" />
              <span>{item.name === "general" ? "General" : item.name}</span>
              <small>{item.terms.filter((term) => term.trim()).length}</small>
              {item.name !== "general" && item.name === active && (
                <Check size={14} aria-label="In use" />
              )}
            </button>
          ))}
        </div>
      </div>
      <section className="vocabulary-detail" aria-label="Vocabulary editor">
        {list ? (
          <>
            <div className="vocabulary-heading">
              <div className="vocabulary-title">
                <div className="title-with-action">
                  {renaming ? (
                    <input
                      ref={renameInput}
                      className="name-input"
                      name="vocabulary-name"
                      autoComplete="off"
                      disabled={busy}
                      aria-label="Vocabulary name"
                      value={nameDraft}
                      aria-invalid={!!renameError}
                      aria-describedby={
                        renameError ? "vocabulary-name-error" : undefined
                      }
                      onChange={(event) => {
                        setNameDrafts({
                          ...nameDrafts,
                          [selected]: event.target.value,
                        });
                        setRenameError("");
                      }}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          restoreRenameFocus.current = true;
                          event.currentTarget.blur();
                        }
                        if (event.key === "Escape") {
                          event.preventDefault();
                          skipRename.current = true;
                          setNameDrafts({
                            ...nameDrafts,
                            [selected]: selected,
                          });
                          setRenameError("");
                          setRenaming(false);
                          requestAnimationFrame(() =>
                            renameButton.current?.focus(),
                          );
                        }
                      }}
                      onBlur={() => {
                        if (skipRename.current) {
                          skipRename.current = false;
                          return;
                        }
                        if (!onRename(nameDraft)) {
                          setRenameError("Choose a unique name.");
                          if (restoreRenameFocus.current)
                            renameInput.current?.focus();
                        } else {
                          setRenaming(false);
                          if (restoreRenameFocus.current)
                            requestAnimationFrame(() =>
                              renameButton.current?.focus(),
                            );
                        }
                        restoreRenameFocus.current = false;
                      }}
                    />
                  ) : (
                    <h2 title={selected === "general" ? "General" : selected}>
                      {selected === "general" ? "General" : selected}
                    </h2>
                  )}
                  {selected !== "general" && !renaming && (
                    <button
                      ref={renameButton}
                      className="icon-button"
                      aria-label="Rename vocabulary"
                      title="Rename vocabulary"
                      disabled={busy}
                      onClick={() => {
                        skipRename.current = false;
                        setRenaming(true);
                      }}
                    >
                      <Pencil size={15} aria-hidden="true" />
                    </button>
                  )}
                </div>
                {renameError && (
                  <p
                    id="vocabulary-name-error"
                    className="field-error"
                    role="alert"
                  >
                    {renameError}
                  </p>
                )}
                <p>
                  {termCount} {termCount === 1 ? "term" : "terms"}
                </p>
              </div>
              <div className="actions">
                {selected === "general" && (
                  <span className="badge neutral">Always included</span>
                )}
                {inUse ? (
                  <span className="badge">
                    <Check size={13} aria-hidden="true" />
                    In use
                  </span>
                ) : selected !== "general" || active ? (
                  <button disabled={busy} onClick={onActivate}>
                    {selected === "general"
                      ? "Use general only"
                      : "Use vocabulary"}
                  </button>
                ) : null}
                {selected !== "general" && (
                  <ActionsMenu label="Vocabulary actions">
                    <button
                      className="danger-text"
                      disabled={busy}
                      onClick={onDelete}
                    >
                      <Trash2 size={15} aria-hidden="true" />
                      Delete vocabulary
                    </button>
                  </ActionsMenu>
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
              name="vocabulary-terms"
              className="vocabulary-terms"
              aria-describedby="vocabulary-terms-hint"
              disabled={busy}
              placeholder={"Voice Type\nOpenAI\nWails"}
              value={list.terms.join("\n")}
              onChange={(event) => onTerms(event.target.value.split(/\r?\n/))}
              spellCheck={false}
              autoComplete="off"
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
          <Dialog.Content
            className="dialog"
            aria-describedby={undefined}
            onCloseAutoFocus={(event) => {
              event.preventDefault();
              createButton.current?.focus();
            }}
          >
            <Dialog.Title>New vocabulary</Dialog.Title>
            <form
              noValidate
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
                  document.getElementById("new-vocabulary-name")?.focus();
                  return;
                }
                onCreate(value);
                setCreating(false);
              }}
            >
              <div className="profile-form-field">
                <label htmlFor="new-vocabulary-name">Name</label>
                <input
                  id="new-vocabulary-name"
                  name="new-vocabulary-name"
                  autoComplete="off"
                  value={name}
                  aria-invalid={!!error}
                  aria-describedby={error ? "new-vocabulary-error" : undefined}
                  onChange={(event) => {
                    setName(event.target.value);
                    setError("");
                  }}
                  placeholder="Medical terms…"
                />
                {error && (
                  <p
                    id="new-vocabulary-error"
                    className="field-error"
                    role="alert"
                  >
                    {error}
                  </p>
                )}
              </div>
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
