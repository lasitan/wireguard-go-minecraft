import { useEffect, useState } from "react";
import { dismissToast, subscribeToasts, type ToastItem } from "./toastStore";
import "./toast.css";

export function ToastHost() {
  const [items, setItems] = useState<ToastItem[]>([]);
  useEffect(() => subscribeToasts(setItems), []);
  if (!items.length) return null;
  return (
    <div className="toast-host" aria-live="polite">
      {items.map((t) => (
        <div
          key={t.id}
          className={`toast-msg ${t.kind}${t.leaving ? " leaving" : ""}`}
          role="status"
          onClick={() => dismissToast(t.id)}
        >
          {t.message}
        </div>
      ))}
    </div>
  );
}
