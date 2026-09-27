import { useRef } from "react";
import { VIEW } from "../../core/constants";
import { state } from "../../core/state";
import { camToAttr } from "../../camera/CameraMath";
import { BuildEdgeGraph } from "../../topology/BuildEdgeGraph";
import { ResolveIpConflicts } from "../../topology/ResolveIpConflicts";
import { syncPlacedNodes } from "../../topology/SyncPlacedNodes";
import { AgentNodes } from "./AgentNodes";
import { EdgeLegend } from "./EdgeLegend";
import { HubNode } from "./HubNode";
import { MeshEdges } from "./MeshEdges";
import { SettingsDrawer } from "./SettingsDrawer";
import { UpdatePill } from "./UpdatePill";
import { useCameraGestures } from "./useCameraGestures";
import "./topology.css";
import "./drawer.css";

export function TopologyPage() {
  const svgRef = useRef<SVGSVGElement>(null);
  useCameraGestures(svgRef);

  const m = state.mesh || { revision: 0, nodes: [], links: [], forwards: [] };
  const nodes = m.nodes || [];
  const placed = syncPlacedNodes(nodes);

  const conflicts = ResolveIpConflicts(m);
  const edges = BuildEdgeGraph(m, placed, conflicts);
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
      <svg
        ref={svgRef}
        className="mesh-svg"
        viewBox={vb}
        preserveAspectRatio="xMidYMid meet"
        role="img"
        aria-label="mesh"
      >
        <rect
          className="stage-hit"
          x={VIEW.cx - VIEW.w * 4}
          y={VIEW.cy - VIEW.h * 4}
          width={VIEW.w * 8}
          height={VIEW.h * 8}
          fill="transparent"
        />
        <MeshEdges edges={edges} />
        <HubNode />
        <AgentNodes placed={placed} conflicts={conflicts} />
      </svg>

      <EdgeLegend />
      <UpdatePill />

      {nodes.length === 0 ? <div className="topo-empty">等待 Agent 持 key 入网…</div> : null}
      {state.demo ? <div className="dev-chip">DEV</div> : null}
      {state.err ? <div className="toast error">{state.err}</div> : null}

      <SettingsDrawer />
    </div>
  );
}
