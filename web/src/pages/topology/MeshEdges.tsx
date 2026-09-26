import type { TopologyEdge } from "../../core/models";

export function MeshEdges({ edges }: { edges: TopologyEdge[] }) {
  return (
    <>
      {edges.map((e) => {
        const flow = e.flowTowardMaster ? " is-flowing" : "";
        return (
          <line
            key={e.id}
            className={`mesh-edge kind-${e.kind} status-${e.status}${flow}`}
            data-edge={e.id}
            x1={e.x1}
            y1={e.y1}
            x2={e.x2}
            y2={e.y2}
          />
        );
      })}
    </>
  );
}
