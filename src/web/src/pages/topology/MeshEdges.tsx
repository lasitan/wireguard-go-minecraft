import type { TopologyEdge } from "../../core/models";

/**
 * Chrome culls perfectly vertical/horizontal lines (zero-area bbox) when the
 * layer is composited for opacity/animation, so they randomly vanish.
 */
const MIN_SPAN = 0.01;

function nudge(a: number, b: number): number {
  return Math.abs(a - b) < MIN_SPAN ? b + MIN_SPAN : b;
}

export function MeshEdges({ edges }: { edges: TopologyEdge[] }) {
  return (
    <g className="mesh-edges">
      {edges.map((e) => {
        const flow = e.flowTowardMaster ? " is-flowing" : "";
        return (
          <line
            key={e.id}
            className={`mesh-edge kind-${e.kind} status-${e.status}${flow}`}
            data-edge={e.id}
            x1={e.x1}
            y1={e.y1}
            x2={nudge(e.x1, e.x2)}
            y2={nudge(e.y1, e.y2)}
          />
        );
      })}
    </g>
  );
}
