import { MASTER_ID } from "../../core/constants";
import { state } from "../../core/state";
import { focusTarget } from "../../app/FocusNav";

/** Floating "new version" hint; opens the Master drawer where the commands live. */
export function UpdatePill() {
  const v = state.version;
  const show = !!v?.hasUpdate && !state.drawerOpen;
  return (
    <button
      type="button"
      className={`update-pill${show ? " show" : ""}`}
      tabIndex={show ? 0 : -1}
      aria-hidden={!show}
      onClick={() => void focusTarget(MASTER_ID)}
    >
      <span className="update-pill-dot" aria-hidden />
      新版本 v{v?.latest || ""} 可用
      <span className="update-pill-go" aria-hidden>
        →
      </span>
    </button>
  );
}
