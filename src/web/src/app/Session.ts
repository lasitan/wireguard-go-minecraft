import { DeleteNode } from "../api/DeleteNode";
import { GetMesh } from "../api/GetMesh";
import { GetMeta } from "../api/GetMeta";
import { Login as LoginApi } from "../api/Login";
import { LoginWithPasskey as LoginWithPasskeyApi } from "../api/WebAuthn";
import { isCameraAnimating } from "../camera/CameraController";
import { homeCam } from "../camera/CameraMath";
import { MASTER_ID, TOKEN_KEY } from "../core/constants";
import { demoMesh, demoMeta, touchDemoHeartbeats } from "../core/demoData";
import { notify, state } from "../core/state";
import type { Mesh } from "../core/models";
import { hydrateNodePositions } from "../topology/NodePositionStore";
import { goHome } from "./FocusNav";
import { isLive, startLive, stopLive } from "./LiveSocket";
import { startVersionPoll, stopVersionPoll } from "./VersionCheck";

async function acceptToken(token: string) {
  state.token = token;
  localStorage.setItem(TOKEN_KEY, state.token);
  await refresh();
  startPoll();
}

export async function login(password: string) {
  await acceptToken(await LoginApi(password));
}

export async function loginWithPasskey() {
  await acceptToken(await LoginWithPasskeyApi());
}

export async function refresh() {
  if (state.demo) {
    state.mesh = demoMesh();
    state.meta = demoMeta();
    hydrateNodePositions(state.mesh);
    notify();
    return;
  }
  const [mesh, meta] = await Promise.all([GetMesh(), GetMeta()]);
  state.meta = meta;
  applyMesh(mesh);
}

function applyMesh(mesh: Mesh) {
  state.mesh = mesh;
  hydrateNodePositions(mesh);
  const lost =
    state.selectedId &&
    state.selectedId !== MASTER_ID &&
    !(mesh.nodes || []).some((n) => n.id === state.selectedId);
  notify();
  if (lost) void goHome();
}

const PUSH_RETRY_MS = 300;
let pendingMesh: Mesh | null = null;
let pendingTimer: number | null = null;

/** Pushed meshes wait until no drag, camera move or request is in flight. */
function onPushedMesh(mesh: Mesh) {
  pendingMesh = mesh;
  if (pendingTimer != null) return;
  const attempt = () => {
    pendingTimer = null;
    const m = pendingMesh;
    if (!m || !state.token) return;
    if (state.busy || state.draggingId || isCameraAnimating()) {
      pendingTimer = window.setTimeout(attempt, PUSH_RETRY_MS);
      return;
    }
    pendingMesh = null;
    if (state.mesh && m.revision < state.mesh.revision) return;
    applyMesh(m);
  };
  attempt();
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
  stopVersionPoll();
  stopLive();
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
  startVersionPoll();
  if (state.demo) {
    // Keep demo agents online and re-evaluate edge status as time passes.
    state.pollTimer = window.setInterval(() => {
      if (!state.mesh) return;
      touchDemoHeartbeats(state.mesh);
      notify();
    }, 8000);
    return;
  }
  startLive({
    mesh: onPushedMesh,
    authFailed: (message) => {
      logout();
      state.err = message;
      notify();
    },
  });
  // Fallback while the push channel is down.
  state.pollTimer = window.setInterval(() => {
    if (!state.token || isLive() || state.busy || isCameraAnimating()) return;
    refresh().catch(() => {});
  }, 8000);
}
