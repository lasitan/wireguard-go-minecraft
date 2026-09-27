import { MASTER_ID } from "../../core/constants";
import { state } from "../../core/state";
import { ResolveIpConflicts } from "../../topology/ResolveIpConflicts";
import { goHome } from "../../app/FocusNav";
import { AgentPanel } from "../agentPanel/AgentPanel";
import { MasterPanel } from "../masterPanel/MasterPanel";
import "./drawer.css";

export function SettingsDrawer() {
  const open = state.drawerOpen;
  const swap = state.contentSwap;
  const id = state.selectedId;

  return (
    <>
      <div
        className={`drawer-scrim${open ? " show" : ""}`}
        id="drawer-scrim"
        onClick={() => void goHome()}
      />
      <aside
        id="settings-drawer"
        className={`settings-drawer${open ? " open" : ""}${swap ? " content-swap" : ""}${id ? " wide" : ""}`}
      >
        <DrawerBody id={id} />
      </aside>
    </>
  );
}

function DrawerBody({ id }: { id: string | null }) {
  if (!id) {
    return <div className="drawer-body muted" style={{ paddingTop: "2rem" }}>选择 Master 或 Agent</div>;
  }
  if (id === MASTER_ID) return <MasterPanel />;

  const n = (state.mesh?.nodes || []).find((x) => x.id === id);
  if (!n) return null;
  const conflicts = state.mesh ? ResolveIpConflicts(state.mesh) : new Map();
  return <AgentPanel node={n} conflict={conflicts.has(n.id)} />;
}
