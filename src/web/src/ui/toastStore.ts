export type ToastKind = "error" | "info" | "success";

export type ToastItem = {
  id: number;
  message: string;
  kind: ToastKind;
  leaving?: boolean;
};

type Listener = (items: ToastItem[]) => void;

let seq = 1;
let items: ToastItem[] = [];
const listeners = new Set<Listener>();
const timers = new Map<number, number>();

function emit() {
  for (const l of listeners) l(items);
}

export function subscribeToasts(listener: Listener): () => void {
  listeners.add(listener);
  listener(items);
  return () => listeners.delete(listener);
}

export function showToast(message: string, kind: ToastKind = "error", ms = 4000) {
  const id = seq++;
  items = [...items, { id, message, kind }];
  emit();
  timers.set(
    id,
    window.setTimeout(() => dismissToast(id), ms),
  );
}

export function dismissToast(id: number) {
  const t = timers.get(id);
  if (t != null) {
    window.clearTimeout(t);
    timers.delete(id);
  }
  const cur = items.find((x) => x.id === id);
  if (!cur || cur.leaving) return;
  items = items.map((x) => (x.id === id ? { ...x, leaving: true } : x));
  emit();
  window.setTimeout(() => {
    items = items.filter((x) => x.id !== id);
    emit();
  }, 280);
}
