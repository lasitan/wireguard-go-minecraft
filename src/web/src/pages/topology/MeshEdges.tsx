import type { TopologyEdge } from "../../core/models";

export function MeshEdges({ edges }: { edges: TopologyEdge[] }) {
  return (
    <g className="mesh-edges">
      {edges.map((e) => {
        const flow = e.flowTowardMaster ? " is-flowing" : "";
        return (
          <path
            key={e.id}
            className={`mesh-edge kind-${e.kind} status-${e.status}${flow}`}
            data-edge={e.id}
            d={e.pathD}
          />
        );
      })}
    </g>
  );
}
