import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { MASTER_ID } from "../../core/constants";
import { state } from "../../core/state";
import { ResolveIpConflicts } from "../../topology/ResolveIpConflicts";
import { goHome } from "../../app/FocusNav";
import { AgentPanel } from "../agentPanel/AgentPanel";
import { MasterPanel } from "../masterPanel/MasterPanel";
import "./drawer.css";

const SIZE_KEY = "lasitan_drawer_size";
const MIN_W = 300;
const MIN_H = 280;

type DrawerSize = { w: number; h: number };

function loadSize(): DrawerSize {
  try {
    const raw = localStorage.getItem(SIZE_KEY);
    if (raw) {
      const v = JSON.parse(raw) as DrawerSize;
      if (typeof v.w === "number" && typeof v.h === "number") return clampSize(v.w, v.h);
    }
  } catch {
    /* ignore */
  }
  return clampSize(380, typeof window !== "undefined" ? window.innerHeight : 720);
}

function clampSize(w: number, h: number): DrawerSize {
  const maxW = typeof window !== "undefined" ? Math.floor(window.innerWidth * 0.92) : 900;
  const maxH = typeof window !== "undefined" ? Math.floor(window.innerHeight * 0.96) : 900;
  return {
    w: Math.max(MIN_W, Math.min(maxW, Math.round(w))),
    h: Math.max(MIN_H, Math.min(maxH, Math.round(h))),
  };
}

type Edge = "w" | "s" | "sw";

export function SettingsDrawer() {
  const open = state.drawerOpen;
  const swap = state.contentSwap;
  const id = state.selectedId;
  const [size, setSize] = useState<DrawerSize>(loadSize);
  const [resizing, setResizing] = useState(false);
  const sizeRef = useRef(size);
  sizeRef.current = size;
  const drag = useRef<{ edge: Edge; startX: number; startY: number; startW: number; startH: number } | null>(null);

  useEffect(() => {
    const onResize = () => {
      const next = clampSize(sizeRef.current.w, sizeRef.current.h);
      sizeRef.current = next;
      setSize(next);
    };
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const persist = useCallback((next: DrawerSize) => {
    sizeRef.current = next;
    setSize(next);
    localStorage.setItem(SIZE_KEY, JSON.stringify(next));
  }, []);

  const onEdgeDown = (edge: Edge) => (e: ReactPointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    const cur = sizeRef.current;
    drag.current = { edge, startX: e.clientX, startY: e.clientY, startW: cur.w, startH: cur.h };
    setResizing(true);
    e.currentTarget.setPointerCapture(e.pointerId);
  };

  const onEdgeMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    const d = drag.current;
    if (!d) return;
    const dx = e.clientX - d.startX;
    const dy = e.clientY - d.startY;
    let w = d.startW;
    let h = d.startH;
    if (d.edge === "w" || d.edge === "sw") w = d.startW - dx;
    if (d.edge === "s" || d.edge === "sw") h = d.startH + dy;
    const next = clampSize(w, h);
    sizeRef.current = next;
    setSize(next);
  };

  const onEdgeUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (!drag.current) return;
    drag.current = null;
    setResizing(false);
    persist(sizeRef.current);
    try {
      e.currentTarget.releasePointerCapture(e.pointerId);
    } catch {
      /* ignore */
    }
  };

  return (
    <>
      <div className={`drawer-scrim${open ? " show" : ""}`} id="drawer-scrim" onClick={() => void goHome()} />
      <aside
        id="settings-drawer"
        className={`settings-drawer${open ? " open" : ""}${swap ? " content-swap" : ""}${resizing ? " resizing" : ""}`}
        style={{ width: size.w, height: size.h, ["--drawer-w" as string]: `${size.w}px` }}
      >
        <div className="drawer-resize drawer-resize-w" onPointerDown={onEdgeDown("w")} onPointerMove={onEdgeMove} onPointerUp={onEdgeUp} />
        <div className="drawer-resize drawer-resize-s" onPointerDown={onEdgeDown("s")} onPointerMove={onEdgeMove} onPointerUp={onEdgeUp} />
        <div className="drawer-resize drawer-resize-sw" onPointerDown={onEdgeDown("sw")} onPointerMove={onEdgeMove} onPointerUp={onEdgeUp} />
        <DrawerBody id={open ? id : null} />
      </aside>
    </>
  );
}

function DrawerBody({ id }: { id: string | null }) {
  if (!id) {
    return <div className="drawer-body muted" style={{ paddingTop: "2rem" }}>选择 Master 或 Agent</div>;
  }
  if (id === MASTER_ID) return <MasterPanel />;

  const n = (state.mesh?.nodes || []).find((x) => x.id === id);
  if (!n) return null;
  const conflicts = state.mesh ? ResolveIpConflicts(state.mesh) : new Map();
  return <AgentPanel node={n} conflict={conflicts.has(n.id)} />;
}
