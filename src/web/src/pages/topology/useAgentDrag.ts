import { useRef, type PointerEvent as ReactPointerEvent } from "react";
import { DRAG_CLICK_PX } from "../../core/constants";
import { clientToSvg } from "../../camera/SvgCoords";
import { notify, state } from "../../core/state";
import { setNodePosition } from "../../topology/SyncPlacedNodes";
import { CLUSTER_PITCH, clusterSlot, findClusterTarget, findMagnetTarget } from "../../topology/MagnetLayout";
import { attachedMothers, clusterMembers, isMother } from "../../topology/MagnetStacks";
import {
  hitPlacedNode,
  nodeVpnPrefix,
  subnetZones,
  zoneAt,
} from "../../topology/SubnetGroups";
import { attachNode, reassignNodeSubnet, setNodeCluster, setNodeParents, swapNodeAddresses } from "../../app/NodeActions";
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
 * out of its stack detaches it from that mother only. Dropping a card right
 * beside another card of the same kind joins its cluster; dragging a cluster
 * member away from its siblings leaves the cluster.
 */
export function useAgentDrag() {
  const drag = useRef<DragState | null>(null);
  const moveRaf = useRef(0);
  const pendingMove = useRef<{
    pointerId: number;
    clientX: number;
    clientY: number;
    el: SVGGElement;
  } | null>(null);

  function flushMove() {
    moveRaf.current = 0;
    const e = pendingMove.current;
    pendingMove.current = null;
    if (!e) return;
    const d = drag.current;
    if (!d || e.pointerId !== d.pointerId) return;
    const dist = Math.hypot(e.clientX - d.startClientX, e.clientY - d.startClientY);
    if (!d.moved && dist >= DRAG_CLICK_PX) {
      d.moved = true;
      state.draggingId = d.id;
      e.el.classList.add("is-dragging");
    }
    if (!d.moved) return;

    const svg = e.el.ownerSVGElement;
    if (!svg) return;
    const pt = clientToSvg(svg, e.clientX, e.clientY);
    const x = pt.x - d.grabDx;
    const y = pt.y - d.grabDy;
    setNodePosition(d.id, x, y);
    state.magnetTarget = d.canSnap ? findMagnetTarget(x, y, d.id) : null;
    state.clusterTarget = state.magnetTarget ? null : findClusterTarget(x, y, d.id);
    notify();
  }

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
    pendingMove.current = {
      pointerId: e.pointerId,
      clientX: e.clientX,
      clientY: e.clientY,
      el: e.currentTarget,
    };
    if (!moveRaf.current) moveRaf.current = requestAnimationFrame(flushMove);
  }

  function onPointerUp(e: ReactPointerEvent<SVGGElement>) {
    const d = drag.current;
    if (!d || e.pointerId !== d.pointerId) return;
    e.stopPropagation();
    if (moveRaf.current) {
      cancelAnimationFrame(moveRaf.current);
      moveRaf.current = 0;
      flushMove();
    }
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
    const clusterTarget = state.clusterTarget;
    state.draggingId = null;
    state.magnetTarget = null;
    state.clusterTarget = null;

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
    const self = mesh?.nodes.find((n) => n.id === d.id);

    if (mesh && clusterTarget) {
      const anchor = state.nodePositions[clusterTarget.id];
      if (anchor) {
        const slot = clusterSlot(anchor, clusterTarget.side);
        setNodePosition(d.id, slot.x, slot.y);
      }
      notify();
      if (!clusterMembers(mesh, clusterTarget.id).includes(d.id)) {
        const target = mesh.nodes.find((n) => n.id === clusterTarget.id);
        const warn = `加入集群后，本卡的磁吸、路由与端口转发配置将改为与「${target?.name || clusterTarget.id}」一致。继续？`;
        if (window.confirm(warn)) setNodeCluster(d.id, clusterTarget.id).catch(fail);
      }
      return;
    }

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
        const gone = d.parentAtStart ? clusterMembers(mesh, d.parentAtStart) : [];
        const rest = attachedMothers(mesh, d.id).filter((m) => !gone.includes(m));
        setNodeParents(d.id, rest).catch(fail);
      }
      return;
    }

    if (mesh && self?.cluster && !magnetTarget) {
      const me = state.nodePositions[d.id];
      const nearSibling = clusterMembers(mesh, d.id).some((id) => {
        const p = id !== d.id ? state.nodePositions[id] : undefined;
        return !!p && !!me && Math.hypot(p.x - me.x, p.y - me.y) < CLUSTER_PITCH * 1.8;
      });
      if (!nearSibling) {
        notify();
        setNodeCluster(d.id, "").catch(fail);
        return;
      }
    }

    notify();
    runSubnet();
  }

  return { onPointerDown, onPointerMove, onPointerUp };
}
