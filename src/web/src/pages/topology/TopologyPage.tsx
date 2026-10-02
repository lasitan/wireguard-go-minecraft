import { useRef, type ReactElement } from "react";
import { VIEW } from "../../core/constants";
import { state } from "../../core/state";
import { camToAttr } from "../../camera/CameraMath";
import { isCameraAnimating } from "../../camera/CameraController";
import { BuildEdgeGraph } from "../../topology/BuildEdgeGraph";
import { isTweening } from "../../topology/PositionTween";
import { ResolveIpConflicts } from "../../topology/ResolveIpConflicts";
import { syncPlacedNodes } from "../../topology/SyncPlacedNodes";
import { AgentNodes } from "./AgentNodes";
import { EdgeLegend } from "./EdgeLegend";
import { HubNode } from "./HubNode";
import { MagnetGhost } from "./MagnetGhost";
import { MagnetRails } from "./MagnetRails";
import { MeshEdges } from "./MeshEdges";
import { SubnetZones } from "./SubnetZones";
import { SettingsDrawer } from "./SettingsDrawer";
import { UpdatePill } from "./UpdatePill";
import { useCameraGestures } from "./useCameraGestures";
import "./topology.css";
import "./drawer.css";
import "./magnet.css";

export function TopologyPage() {
  const svgRef = useRef<SVGSVGElement>(null);
  useCameraGestures(svgRef);
  const chrome = useRef<ReactElement | null>(null);
  if (!state.draggingId || !chrome.current) {
    chrome.current = (
      <>
        <EdgeLegend />
        <UpdatePill />
        <SettingsDrawer />
      </>
    );
  }

  const m = state.mesh || { revision: 0, nodes: [], links: [], forwards: [] };
  const nodes = m.nodes || [];
  const placed = syncPlacedNodes(m);

  const conflicts = ResolveIpConflicts(m);
  const edges = BuildEdgeGraph(m, placed, conflicts, state.stacks, {
    fast: !!state.draggingId || isTweening(),
  });
  const vb = camToAttr(state.camera);

  const stageClass = [
    "stage",
    state.selectedId ? "has-selection" : "",
    state.drawerOpen ? "drawer-open" : "",
    state.busy || state.draggingId || isCameraAnimating() || isTweening() ? "is-moving" : "",
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
        <defs>
          <pattern
            id="cam-grid"
            width={10}
            height={10}
            patternUnits="userSpaceOnUse"
          >
            <path
              d="M 10 0 L 0 0 0 10"
              fill="none"
              stroke="rgba(27, 36, 48, 0.08)"
              strokeWidth={1}
              vectorEffect="non-scaling-stroke"
            />
          </pattern>
        </defs>
        <rect
          className="stage-hit"
          x={VIEW.cx - VIEW.w * 2}
          y={VIEW.cy - VIEW.h * 2}
          width={VIEW.w * 4}
          height={VIEW.h * 4}
          fill="url(#cam-grid)"
        />
        <MeshEdges edges={edges} />
        <SubnetZones mesh={m} placed={placed} />
        <MagnetRails placed={placed} />
        <AgentNodes placed={placed} conflicts={conflicts} />
        <HubNode />
        <MagnetGhost />
      </svg>

      {nodes.length === 0 ? <div className="topo-empty">等待 Agent 持 key 入网…</div> : null}
      {state.demo ? <div className="dev-chip">DEV</div> : null}
      {state.err ? <div className="toast error">{state.err}</div> : null}

      {chrome.current}
    </div>
  );
}
