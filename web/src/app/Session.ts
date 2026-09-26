import { DeleteNode } from "../api/DeleteNode";
import { GetMesh } from "../api/GetMesh";
import { GetMeta } from "../api/GetMeta";
import { Login as LoginApi } from "../api/Login";
import { isCameraAnimating } from "../camera/CameraController";
import { homeCam } from "../camera/CameraMath";
import { MASTER_ID, TOKEN_KEY } from "../core/constants";
import { demoMesh, demoMeta, touchDemoHeartbeats } from "../core/demoData";
import { notify, state } from "../core/state";
import { goHome } from "./FocusNav";

export async function login(password: string) {
  const token = await LoginApi(password);
  state.token = token;
  localStorage.setItem(TOKEN_KEY, state.token);
  await refresh();
  startPoll();
}

export async function refresh() {
  if (state.demo) {
    state.mesh = demoMesh();
    state.meta = demoMeta();
    notify();
    return;
  }
  const [mesh, meta] = await Promise.all([GetMesh(), GetMeta()]);
  state.mesh = mesh;
  state.meta = meta;
  const lost =
    state.selectedId &&
    state.selectedId !== MASTER_ID &&
    !(mesh.nodes || []).some((n) => n.id === state.selectedId);
  notify();
  if (lost) void goHome();
}

export async function removeNode(id: string) {
  if (state.demo) {
    if (!state.mesh) return;
    state.mesh = {
      ...state.mesh,
      revision: state.mesh.revision + 1,
      nodes: state.mesh.nodes.filter((n) => n.id !== id),
      links: state.mesh.links.filter((l) => l.fromNodeId !== id && l.toNodeId !== id),
      forwards: state.mesh.forwards.filter((f) => f.nodeId !== id && f.destNodeId !== id),
    };
    await goHome();
    notify();
    return;
  }
  if (!confirm("删除该节点？相关链接与转发也会移除。")) return;
  state.mesh = await DeleteNode(id);
  await goHome();
  notify();
}

export function logout() {
  if (state.pollTimer != null) {
    clearInterval(state.pollTimer);
    state.pollTimer = null;
  }
  state.token = "";
  localStorage.removeItem(TOKEN_KEY);
  state.mesh = null;
  state.meta = null;
  state.selectedId = null;
  state.drawerOpen = false;
  state.contentSwap = false;
  state.camera = homeCam();
  notify();
}

export function startPoll() {
  if (state.pollTimer != null) clearInterval(state.pollTimer);
  if (state.demo) {
    // Keep demo agents online and re-evaluate edge status as time passes.
    state.pollTimer = window.setInterval(() => {
      if (!state.mesh) return;
      touchDemoHeartbeats(state.mesh);
      notify();
    }, 8000);
    return;
  }
  state.pollTimer = window.setInterval(() => {
    if (!state.token || state.busy || isCameraAnimating()) return;
    refresh().catch(() => {});
  }, 8000);
}
