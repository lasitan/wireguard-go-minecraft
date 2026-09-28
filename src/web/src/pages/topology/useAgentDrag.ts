import { useRef, type PointerEvent as ReactPointerEvent } from "react";
import { DRAG_CLICK_PX } from "../../core/constants";
import { clientToSvg } from "../../camera/SvgCoords";
import { notify, state } from "../../core/state";
import { setNodePosition } from "../../topology/SyncPlacedNodes";
import { findMagnetTarget } from "../../topology/MagnetLayout";
import { attachedMothers, isMother } from "../../topology/MagnetStacks";
import {
  hitPlacedNode,
  nodeVpnPrefix,
  subnetZones,
  zoneAt,
} from "../../topology/SubnetGroups";
import { attachNode, reassignNodeSubnet, setNodeParents, swapNodeAddresses } from "../../app/NodeActions";
import { focusTarget } from "../../app/FocusNav";

type DragState = {
  id: string;
  pointerId: number;
  startClientX: number;
  startClientY: number;
  grabDx: number;
  grabDy: number;
  moved: boolean;
  parentAtStart: string | null;
  canSnap: boolean;
};

function magnetAttachChanged(target: string | null, parentAtStart: string | null): boolean {
  return !(target === parentAtStart || (!target && !parentAtStart));
}

/**
 * Pointer drag for agents. Short press → focus; drag → reposition. Plain
 * cards snap under a mother card on release (attach / switch); holding
 * Ctrl/Shift adds that mother instead of switching. Dragging an attached card
 * out of its stack detaches it from that mother only.
 */
export function useAgentDrag() {
  const drag = useRef<DragState | null>(null);

  function onPointerDown(e: ReactPointerEvent<SVGGElement>, id: string, x: number, y: number) {
    if (e.button !== 0) return;
    e.stopPropagation();
    e.preventDefault();
    const svg = e.currentTarget.ownerSVGElement;
    if (!svg) return;
    const pt = clientToSvg(svg, e.clientX, e.clientY);
    const node = state.mesh?.nodes.find((n) => n.id === id);
    drag.current = {
      id,
      pointerId: e.pointerId,
      startClientX: e.clientX,
      startClientY: e.clientY,
      grabDx: pt.x - x,
      grabDy: pt.y - y,
      moved: false,
      parentAtStart: state.stacks.parentOf.get(id) || null,
      canSnap: !isMother(node),
    };
    e.currentTarget.setPointerCapture(e.pointerId);
  }

  function onPointerMove(e: ReactPointerEvent<SVGGElement>) {
    const d = drag.current;
    if (!d || e.pointerId !== d.pointerId) return;
    e.stopPropagation();
    const dist = Math.hypot(e.clientX - d.startClientX, e.clientY - d.startClientY);
    if (!d.moved && dist >= DRAG_CLICK_PX) {
      d.moved = true;
      state.draggingId = d.id;
      e.currentTarget.classList.add("is-dragging");
    }
    if (!d.moved) return;

    const svg = e.currentTarget.ownerSVGElement;
    if (!svg) return;
    const pt = clientToSvg(svg, e.clientX, e.clientY);
    const x = pt.x - d.grabDx;
    const y = pt.y - d.grabDy;
    setNodePosition(d.id, x, y);
    if (d.canSnap) state.magnetTarget = findMagnetTarget(x, y, d.id);
    notify();
  }

  function onPointerUp(e: ReactPointerEvent<SVGGElement>) {
    const d = drag.current;
    if (!d || e.pointerId !== d.pointerId) return;
    e.stopPropagation();
    drag.current = null;
    e.currentTarget.classList.remove("is-dragging");
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* ignore */
    }
    if (!d.moved) {
      void focusTarget(d.id);
      return;
    }

    const svg = e.currentTarget.ownerSVGElement;
    const magnetTarget = d.canSnap ? state.magnetTarget : null;
    state.draggingId = null;
    state.magnetTarget = null;

    const runSubnet = () => {
      const mesh = state.mesh;
      const placed = state.placed;
      if (!mesh || !svg || placed.length === 0) return;
      const zones = subnetZones(mesh, placed);
      if (!zones) return;

      const pt = clientToSvg(svg, e.clientX, e.clientY);
      const nodeX = pt.x - d.grabDx;
      const nodeY = pt.y - d.grabDy;
      const selfPrefix = nodeVpnPrefix(d.id, mesh);
      if (!selfPrefix) return;

      const hitId = hitPlacedNode(e.clientX, e.clientY, svg, placed);
      if (hitId && hitId !== d.id) {
        const otherPrefix = nodeVpnPrefix(hitId, mesh);
        if (otherPrefix && otherPrefix !== selfPrefix) {
          void swapNodeAddresses(d.id, hitId).catch((err: Error) => {
            state.err = err.message;
            notify();
          });
          return;
        }
      }

      const zone = zoneAt(nodeX, nodeY, zones);
      if (zone && zone.prefix !== selfPrefix && (!hitId || hitId === d.id)) {
        void reassignNodeSubnet(d.id, zone.prefix).catch((err: Error) => {
          state.err = err.message;
          notify();
        });
      }
    };

    const fail = (err: Error) => {
      state.err = err.message;
      notify();
    };
    const mesh = state.mesh;
    const addMode = e.ctrlKey || e.metaKey || e.shiftKey;
    if (d.canSnap && mesh && addMode && magnetTarget) {
      const current = attachedMothers(mesh, d.id);
      notify();
      if (!current.includes(magnetTarget)) {
        setNodeParents(d.id, [...current, magnetTarget]).catch(fail);
      }
      return;
    }
    if (d.canSnap && mesh && magnetAttachChanged(magnetTarget, d.parentAtStart)) {
      notify();
      if (magnetTarget) {
        attachNode(d.id, magnetTarget).catch(fail);
      } else {
        const rest = attachedMothers(mesh, d.id).filter((m) => m !== d.parentAtStart);
        setNodeParents(d.id, rest).catch(fail);
      }
      return;
    }

    notify();
    runSubnet();
  }

  return { onPointerDown, onPointerMove, onPointerUp };
}
