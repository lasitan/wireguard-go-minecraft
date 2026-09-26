import { VIEW } from "../../core/constants";
import { state } from "../../core/state";
import { camToAttr } from "../../camera/CameraMath";
import { BuildEdgeGraph } from "../../topology/BuildEdgeGraph";
import { layoutNodes } from "../../topology/layoutNodes";
import { ResolveIpConflicts } from "../../topology/ResolveIpConflicts";
import { goHome } from "../../app/FocusNav";
import { AgentNodes } from "./AgentNodes";
import { EdgeLegend } from "./EdgeLegend";
import { HubNode } from "./HubNode";
import { MeshEdges } from "./MeshEdges";
import { SettingsDrawer } from "./SettingsDrawer";
import "./topology.css";
import "./drawer.css";

export function TopologyPage() {
  const m = state.mesh || { revision: 0, nodes: [], links: [], forwards: [] };
  const nodes = m.nodes || [];
  state.placed = layoutNodes(nodes);

  const conflicts = ResolveIpConflicts(m);
  const edges = BuildEdgeGraph(m, state.placed, conflicts);
  const vb = camToAttr(state.camera);

  const stageClass = [
    "stage",
    state.selectedId ? "has-selection" : "",
    state.drawerOpen ? "drawer-open" : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={stageClass}>
      <svg className="mesh-svg" viewBox={vb} preserveAspectRatio="xMidYMid meet" role="img" aria-label="mesh">
        <rect
          className="stage-hit"
          x={VIEW.cx - VIEW.w}
          y={VIEW.cy - VIEW.h}
          width={VIEW.w * 2}
          height={VIEW.h * 2}
          fill="transparent"
          onClick={() => void goHome()}
        />
        <MeshEdges edges={edges} />
        <HubNode />
        <AgentNodes placed={state.placed} conflicts={conflicts} />
      </svg>

      <EdgeLegend />

      {nodes.length === 0 ? <div className="topo-empty">等待 Agent 持 key 入网…</div> : null}
      {state.demo ? <div className="dev-chip">DEV</div> : null}
      {state.err ? <div className="toast error">{state.err}</div> : null}

      <SettingsDrawer />
    </div>
  );
}
