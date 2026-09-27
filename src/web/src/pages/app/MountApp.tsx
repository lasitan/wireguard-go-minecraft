import { createRoot } from "react-dom/client";
import { AppRoot } from "./AppRoot";

export function MountApp() {
  const el = document.getElementById("app");
  if (!el) throw new Error("#app missing");
  createRoot(el).render(<AppRoot />);
}
