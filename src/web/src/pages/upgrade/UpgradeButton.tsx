import type { UpgradeRequest } from "../../core/models";
import { isUpgrading, startUpgrade } from "../../app/UpgradeActions";
import "./upgrade.css";

/** Starts a web-triggered upgrade; `keys` are the targets whose progress disables it. */
export function UpgradeButton({
  label,
  req,
  keys,
  knownVersions,
  ghost,
}: {
  label: string;
  req: UpgradeRequest;
  keys: string[];
  knownVersions?: Record<string, string>;
  ghost?: boolean;
}) {
  const busy = keys.length > 0 && keys.every(isUpgrading);
  return (
    <button
      type="button"
      className={`mini upgrade-btn${ghost ? " ghost" : ""}${busy ? " busy" : ""}`}
      disabled={busy || keys.length === 0}
      onClick={(e) => {
        e.stopPropagation();
        void startUpgrade(req, knownVersions);
      }}
    >
      <span className={`upgrade-spin${busy ? " on" : ""}`} aria-hidden />
      {busy ? "升级中" : label}
    </button>
  );
}
