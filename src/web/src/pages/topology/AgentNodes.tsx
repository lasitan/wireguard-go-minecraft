import type { IpConflictMap, PlacedNode } from "../../core/models";
import { state } from "../../core/state";
import { hostOf } from "../../utils/hostOf";
import { isOnline } from "../../utils/isOnline";
import { shortName } from "../../utils/shortName";
import { useAgentDrag } from "./useAgentDrag";

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
  const drag = useAgentDrag();

  return (
    <>
      {placed.map((p, i) => {
        const online = isOnline(p.node);
        const selected = state.selectedId === p.node.id;
        const conflict = conflicts.has(p.node.id);
        const disabled = !!p.node.disabled;
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
          conflict && !disabled ? "conflict" : "",
          disabled ? "disabled" : "",
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
            style={{ ["--jitter-delay" as string]: `${delay}s`, cursor: "grab" }}
            onPointerDown={(e) => drag.onPointerDown(e, p.node.id, p.x, p.y)}
            onPointerMove={drag.onPointerMove}
            onPointerUp={drag.onPointerUp}
            onPointerCancel={drag.onPointerUp}
          >
            <foreignObject x={-78} y={-34} width={156} height={68}>
              <div className={cardClass}>
                <span className="dot" />
                <div className="node-text">
                  <div className="node-name">{shortName(p.node)}</div>
                  <div className="node-ip">
                    {hostOf(p.node.address)}
                    {disabled ? (
                      <span className="disabled-tag"> 已停用</span>
                    ) : conflict ? (
                      <span className="conflict-tag"> IP冲突</span>
                    ) : null}
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
