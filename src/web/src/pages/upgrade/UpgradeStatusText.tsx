import type { UpgradeStatus } from "../../core/models";
import "./upgrade.css";

const ICON: Record<UpgradeStatus["phase"], string> = {
  sending: "…",
  running: "↻",
  done: "✓",
  failed: "!",
};

/** One-line progress of a web-triggered upgrade; height-animates in and out. */
export function UpgradeStatusText({ status }: { status?: UpgradeStatus }) {
  return (
    <div className={`upgrade-status${status ? ` show ${status.phase}` : ""}`} aria-live="polite">
      <div>
        {status ? (
          <p key={status.phase + status.message}>
            <span className="upgrade-icon" aria-hidden>
              {ICON[status.phase]}
            </span>
            {status.message}
          </p>
        ) : null}
      </div>
    </div>
  );
}
