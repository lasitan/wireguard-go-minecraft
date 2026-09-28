import { MASTER_ID, VIEW } from "../../core/constants";
import { state } from "../../core/state";
import { focusTarget } from "../../app/FocusNav";

export function HubNode() {
  const selected = state.selectedId === MASTER_ID;
  return (
    <g className="hub-layer">
    <g
      className={`hub${selected ? " is-selected" : ""}`}
      data-id={MASTER_ID}
      transform={`translate(${VIEW.cx}, ${VIEW.cy})`}
      style={{ cursor: "pointer" }}
      onPointerDown={(e) => e.stopPropagation()}
      onClick={(e) => {
        e.stopPropagation();
        void focusTarget(MASTER_ID);
      }}
    >
      <circle className="hub-ring" r={42} />
      <circle className="hub-core" r={30} />
      <text className="hub-label" textAnchor="middle" dy={5} style={{ userSelect: "none" }}>
        Master
      </text>
    </g>
    </g>
  );
}
