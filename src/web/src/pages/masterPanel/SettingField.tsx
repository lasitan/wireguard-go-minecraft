import { useEffect, useRef, useState } from "react";

type Props = {
  label: string;
  hint: string;
  value: string;
  validate: (draft: string) => string;
  onSave: (draft: string) => Promise<void>;
  /** Fills the draft with a generated value (shown as a button while editing). */
  generate?: () => string;
  copyable?: boolean;
  mono?: boolean;
};

export function SettingField({ label, hint, value, validate, onSave, generate, copyable, mono }: Props) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [busy, setBusy] = useState(false);
  const [saveErr, setSaveErr] = useState("");
  const [copied, setCopied] = useState(false);
  const [flash, setFlash] = useState(false);
  const timers = useRef<number[]>([]);
  const err = editing ? validate(draft) || saveErr : "";

  useEffect(() => () => timers.current.forEach((t) => window.clearTimeout(t)), []);
  useEffect(() => {
    if (!editing) setDraft(value);
  }, [value, editing]);

  const later = (fn: () => void, ms: number) => timers.current.push(window.setTimeout(fn, ms));

  const cancel = () => {
    setEditing(false);
    setDraft(value);
    setSaveErr("");
  };

  const save = async () => {
    if (busy || validate(draft)) return;
    if (draft.trim() === value) {
      cancel();
      return;
    }
    setBusy(true);
    try {
      await onSave(draft.trim());
      setEditing(false);
      setSaveErr("");
      setFlash(true);
      later(() => setFlash(false), 900);
    } catch (e) {
      setSaveErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

  const copy = async () => {
    await navigator.clipboard.writeText(value);
    setCopied(true);
    later(() => setCopied(false), 1600);
  };

  return (
    <div className={`setting${editing ? " editing" : ""}${flash ? " saved" : ""}`}>
      <div className="setting-head">
        <span className="setting-label">{label}</span>
        {!editing ? (
          <span className="setting-tools">
            {copyable ? (
              <button type="button" className={`setting-btn${copied ? " done" : ""}`} onClick={() => void copy()}>
                {copied ? "已复制" : "复制"}
              </button>
            ) : null}
            <button type="button" className="setting-btn" onClick={() => setEditing(true)}>
              编辑
            </button>
          </span>
        ) : null}
      </div>
      {editing ? (
        <div className="setting-edit">
          <input
            autoFocus
            spellCheck={false}
            className={mono ? "mono" : ""}
            value={draft}
            aria-invalid={!!err}
            onChange={(e) => {
              setDraft(e.target.value);
              setSaveErr("");
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") void save();
              if (e.key === "Escape") cancel();
            }}
          />
          <div className="setting-actions">
            <button type="button" className="setting-save" disabled={busy || !!err} onClick={() => void save()}>
              {busy ? "保存中…" : "保存"}
            </button>
            <button type="button" className="setting-ghost" onClick={cancel}>
              取消
            </button>
            {generate ? (
              <button
                type="button"
                className="setting-ghost push"
                onClick={() => {
                  setDraft(generate());
                  setSaveErr("");
                }}
              >
                随机生成
              </button>
            ) : null}
          </div>
          <div className={`setting-err${err ? " show" : ""}`}>{err || " "}</div>
        </div>
      ) : (
        <code className={`setting-value${mono ? " mono" : ""}`} key={value}>
          {value || "—"}
        </code>
      )}
      <p className="tiny muted setting-hint">{hint}</p>
    </div>
  );
}
