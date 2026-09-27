import { useEffect, useState, type ReactNode } from "react";

/** Click-to-edit row value: Enter saves, Escape cancels. */
export function InlineField({
  label,
  value,
  display,
  placeholder,
  busy,
  validate,
  onSave,
}: {
  label: string;
  value: string;
  display: ReactNode;
  placeholder?: string;
  busy: boolean;
  validate: (draft: string) => string;
  onSave: (draft: string) => Promise<void>;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const err = editing ? validate(draft) : "";

  useEffect(() => {
    setEditing(false);
    setDraft(value);
  }, [value]);

  const cancel = () => {
    setEditing(false);
    setDraft(value);
  };
  const save = () => {
    if (err) return;
    if (draft.trim() === value) {
      setEditing(false);
      return;
    }
    void onSave(draft.trim()).then(() => setEditing(false), () => undefined);
  };

  return (
    <div className="ov-row">
      <span className="ov-label">{label}</span>
      <div className={`inline-edit${editing ? " editing" : ""}`}>
        {editing ? (
          <>
            <input
              autoFocus
              value={draft}
              placeholder={placeholder}
              aria-invalid={!!err}
              onChange={(e) => setDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") save();
                if (e.key === "Escape") cancel();
              }}
            />
            <div className="inline-actions">
              <button type="button" className="mini" disabled={busy || !!err} onClick={save}>
                保存
              </button>
              <button type="button" className="mini ghost" onClick={cancel}>
                取消
              </button>
            </div>
            <div className={`field-err${err ? " show" : ""}`}>{err || " "}</div>
          </>
        ) : (
          <button type="button" className="inline-value" onClick={() => setEditing(true)} title="点击编辑">
            {display}
            <span className="edit-hint">编辑</span>
          </button>
        )}
      </div>
    </div>
  );
}
