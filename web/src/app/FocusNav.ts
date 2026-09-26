import { MASTER_ID, VIEW } from "../core/constants";
import { notify, state } from "../core/state";
import { animateCameraTo, isCameraAnimating, readSvgCamera } from "../camera/CameraController";
import { camsNear, focusCamAt, homeCam } from "../camera/CameraMath";

let drawerTimer = 0;
let focusGen = 0;

function pointForSelection(id: string): { x: number; y: number } {
  if (!id || id === MASTER_ID) return { x: VIEW.cx, y: VIEW.cy };
  const hit = state.placed.find((p) => p.node.id === id);
  return hit ? { x: hit.x, y: hit.y } : { x: VIEW.cx, y: VIEW.cy };
}

export async function focusTarget(id: string) {
  if (state.selectedId === id && state.drawerOpen && !isCameraAnimating()) return;
  const gen = ++focusGen;
  if (drawerTimer) {
    window.clearTimeout(drawerTimer);
    drawerTimer = 0;
  }
  state.busy = true;

  const switching = !!state.selectedId && state.selectedId !== id;
  state.contentSwap = switching && state.drawerOpen;
  state.selectedId = id;
  notify();

  const pt = pointForSelection(id);
  await animateCameraTo(focusCamAt(pt.x, pt.y));
  if (gen !== focusGen) return;

  state.drawerOpen = true;
  state.contentSwap = false;
  state.busy = false;
  notify();
}

export async function goHome() {
  const home = homeCam();
  if (!state.selectedId && !state.drawerOpen && camsNear(readSvgCamera(), home) && !isCameraAnimating()) {
    return;
  }
  const gen = ++focusGen;
  if (drawerTimer) {
    window.clearTimeout(drawerTimer);
    drawerTimer = 0;
  }
  state.busy = true;
  state.drawerOpen = false;
  notify();

  await animateCameraTo(home);
  if (gen !== focusGen) return;

  state.selectedId = null;
  state.drawerOpen = false;
  state.contentSwap = false;
  state.busy = false;
  notify();
}
