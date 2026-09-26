import type { IpConflictMap, PlacedNode } from "../../core/models";
import { state } from "../../core/state";
import { hostOf } from "../../utils/hostOf";
import { isOnline } from "../../utils/isOnline";
import { shortName } from "../../utils/shortName";
import { focusTarget } from "../../app/FocusNav";

function jitterDelay(id: string): number {
  let h = 0;
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) | 0;
  return Math.abs(h % 2800) / 1000;
}

export function AgentNodes({
  placed,
  conflicts,
}: {
  placed: PlacedNode[];
  conflicts: IpConflictMap;
}) {
  return (
    <>
      {placed.map((p, i) => {
        const online = isOnline(p.node);
        const selected = state.selectedId === p.node.id;
        const conflict = conflicts.has(p.node.id);
        const delay = jitterDelay(p.node.id);
        const wrapClass = [
          "node-wrap",
          "is-jitter",
          selected ? "is-selected" : "",
          conflict ? "is-conflict" : "",
        ]
          .filter(Boolean)
          .join(" ");
        const cardClass = [
          "node-card",
          online ? "online" : "offline",
          selected ? "selected" : "",
          conflict ? "conflict" : "",
        ]
          .filter(Boolean)
          .join(" ");

        return (
          <g
            key={p.node.id}
            className={wrapClass}
            data-id={p.node.id}
            data-jitter={(i % 5) + 1}
            transform={`translate(${p.x}, ${p.y})`}
            style={{ ["--jitter-delay" as string]: `${delay}s` }}
            onClick={(e) => {
              e.stopPropagation();
              void focusTarget(p.node.id);
            }}
          >
            <foreignObject x={-78} y={-34} width={156} height={68}>
              <div className={cardClass}>
                <span className="dot" />
                <div className="node-text">
                  <div className="node-name">{shortName(p.node)}</div>
                  <div className="node-ip">
                    {hostOf(p.node.address)}
                    {conflict ? <span className="conflict-tag"> IP冲突</span> : null}
                  </div>
                </div>
              </div>
            </foreignObject>
          </g>
        );
      })}
    </>
  );
}
