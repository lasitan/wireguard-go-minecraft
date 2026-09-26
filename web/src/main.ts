import "./styles.css";

type Node = {
  id: string;
  name?: string;
  role: string;
  address: string;
  endpoint?: string;
  token?: string;
  listenPort?: number;
  lastSeen?: string;
};

type Link = {
  fromNodeId: string;
  toNodeId: string;
  allowedIPs?: string[];
  keepalive?: number;
};

type Forward = {
  nodeId: string;
  protocol: string;
  listen: string;
  destNodeId: string;
  destPort: number;
};

type Mesh = {
  revision: number;
  nodes: Node[];
  links: Link[];
  forwards: Forward[];
};

type Meta = {
  enrollToken: string;
  vpnSubnet: string;
  listen: string;
  defaultIface?: string;
  defaultPoll?: string;
};

type Cam = { x: number; y: number; w: number; h: number };
type Focus = { x: number; y: number; scale: number };

const ONLINE_MS = 45_000;
const MASTER_ID = "__master__";
const isDevPreview = import.meta.env.DEV;
const VIEW = { w: 1200, h: 720, cx: 600, cy: 360, radius: 240 };
const FOCUS_SCALE = 1.38;
const CAM_MS = 380;

function demoMesh(): Mesh {
  const now = new Date().toISOString();
  const ago = new Date(Date.now() - 120_000).toISOString();
  return {
    revision: 7,
    nodes: [
      { id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", name: "web-server", role: "server", address: "100.96.0.1/24", endpoint: "1.2.3.4:25590", listenPort: 25590, token: "demo-token-web", lastSeen: now },
      { id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002", name: "db-replica", role: "client", address: "100.96.0.2/24", token: "demo-token-db", lastSeen: now },
      { id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003", name: "MacBook Pro", role: "client", address: "100.96.0.10/24", token: "demo-token-mac", lastSeen: now },
      { id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0004", name: "dev-vm", role: "client", address: "", token: "demo-token-vm", lastSeen: ago },
    ],
    links: [
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", keepalive: 5 },
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", keepalive: 5 },
      { fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0004", toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", keepalive: 5 },
    ],
    forwards: [
      { nodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", protocol: "tcp", listen: "3389", destNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003", destPort: 3389 },
      { nodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001", protocol: "tcp", listen: "5432", destNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002", destPort: 5432 },
    ],
  };
}

function demoMeta(): Meta {
  return { enrollToken: "dev-preview-enroll-token", vpnSubnet: "100.96.0.0/24", listen: ":8443", defaultIface: "wg0", defaultPoll: "10s" };
}

function homeCam(): Cam {
  return { x: 0, y: 0, w: VIEW.w, h: VIEW.h };
}

function focusToCam(f: Focus): Cam {
  const w = VIEW.w / f.scale;
  const h = VIEW.h / f.scale;
  return { x: f.x - w / 2, y: f.y - h / 2, w, h };
}

function camToAttr(c: Cam): string {
  return `${c.x} ${c.y} ${c.w} ${c.h}`;
}

function easeInOutCubic(t: number): number {
  return t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2;
}

function camsNear(a: Cam, b: Cam, eps = 0.5): boolean {
  return Math.abs(a.x - b.x) < eps && Math.abs(a.y - b.y) < eps && Math.abs(a.w - b.w) < eps && Math.abs(a.h - b.h) < eps;
}

const app = document.getElementById("app")!;
const state: {
  token: string;
  mesh: Mesh | null;
  meta: Meta | null;
  selectedId: string | null;
  drawerOpen: boolean;
  err: string;
  pollTimer: number | null;
  demo: boolean;
  camera: Cam;
  placed: { node: Node; x: number; y: number }[];
  busy: boolean;
} = {
  token: isDevPreview ? "dev-preview" : localStorage.getItem("wgmc_admin_token") || "",
  mesh: isDevPreview ? demoMesh() : null,
  meta: isDevPreview ? demoMeta() : null,
  selectedId: null,
  drawerOpen: false,
  err: "",
  pollTimer: null,
  demo: isDevPreview,
  camera: homeCam(),
  placed: [],
  busy: false,
};

let cameraRaf = 0;
let cameraDone: (() => void) | null = null;
let drawerTimer = 0;
let focusGen = 0;

async function api<T = unknown>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(opts.headers as Record<string, string> | undefined),
  };
  if (state.token) headers.Authorization = "Bearer " + state.token;
  const res = await fetch(path, { ...opts, headers });
  const text = await res.text();
  let body: { error?: string } | null = null;
  try {
    body = text ? JSON.parse(text) : null;
  } catch {
    body = { error: text };
  }
  if (!res.ok) throw new Error((body && body.error) || res.statusText);
  return body as T;
}

function isOnline(n: Node): boolean {
  if (!n.lastSeen) return false;
  const t = Date.parse(n.lastSeen);
  if (Number.isNaN(t)) return false;
  return Date.now() - t < ONLINE_MS;
}

function hostOf(addr: string): string {
  if (!addr) return "—";
  const i = addr.indexOf("/");
  return i >= 0 ? addr.slice(0, i) : addr;
}

function shortName(n: Node): string {
  const raw = (n.name || n.role || n.id).trim();
  return raw.length > 14 ? raw.slice(0, 12) + "…" : raw;
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

function layoutNodes(nodes: Node[]) {
  const n = nodes.length;
  return nodes.map((node, i) => {
    const angle = -Math.PI / 2 + (2 * Math.PI * i) / Math.max(n, 1);
    return {
      node,
      x: VIEW.cx + Math.cos(angle) * VIEW.radius,
      y: VIEW.cy + Math.sin(angle) * VIEW.radius,
    };
  });
}

function pointForSelection(id: string): { x: number; y: number } {
  if (!id || id === MASTER_ID) return { x: VIEW.cx, y: VIEW.cy };
  const hit = state.placed.find((p) => p.node.id === id);
  return hit ? { x: hit.x, y: hit.y } : { x: VIEW.cx, y: VIEW.cy };
}

function readSvgCamera(): Cam {
  const svg = document.querySelector(".mesh-svg") as SVGSVGElement | null;
  if (!svg) return { ...state.camera };
  const raw = svg.getAttribute("viewBox");
  if (!raw) return { ...state.camera };
  const [x, y, w, h] = raw.split(/\s+/).map(Number);
  if ([x, y, w, h].some((n) => Number.isNaN(n))) return { ...state.camera };
  return { x, y, w, h };
}

function applyCamera(c: Cam) {
  state.camera = c;
  const svg = document.querySelector(".mesh-svg") as SVGSVGElement | null;
  if (svg) svg.setAttribute("viewBox", camToAttr(c));
}

/** Cancel in-flight camera tween; next animate resumes from current viewBox. */
function stopCameraTween() {
  if (cameraRaf) {
    cancelAnimationFrame(cameraRaf);
    cameraRaf = 0;
  }
  if (cameraDone) {
    const done = cameraDone;
    cameraDone = null;
    done();
  }
}

/** Smoothly fly camera from the current on-screen position to target. */
function animateCameraTo(target: Cam, ms = CAM_MS): Promise<void> {
  return new Promise((resolve) => {
    stopCameraTween();
    const from = readSvgCamera();
    if (camsNear(from, target)) {
      applyCamera(target);
      resolve();
      return;
    }
    cameraDone = resolve;
    const t0 = performance.now();
    const tick = (now: number) => {
      const p = Math.min(1, (now - t0) / ms);
      const e = easeInOutCubic(p);
      applyCamera({
        x: from.x + (target.x - from.x) * e,
        y: from.y + (target.y - from.y) * e,
        w: from.w + (target.w - from.w) * e,
        h: from.h + (target.h - from.h) * e,
      });
      if (p < 1) {
        cameraRaf = requestAnimationFrame(tick);
      } else {
        cameraRaf = 0;
        const done = cameraDone;
        cameraDone = null;
        done?.();
      }
    };
    cameraRaf = requestAnimationFrame(tick);
  });
}

function setDrawerOpen(open: boolean) {
  state.drawerOpen = open;
  document.getElementById("settings-drawer")?.classList.toggle("open", open);
  document.getElementById("drawer-scrim")?.classList.toggle("show", open);
}

function bindDrawerActions() {
  document.getElementById("btn-close-drawer")?.addEventListener("click", () => void goHome());
  document.getElementById("btn-copy-enroll")?.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(state.meta?.enrollToken || "");
    } catch {
      /* ignore */
    }
  });
  document.getElementById("btn-del-node")?.addEventListener("click", () => {
    if (state.selectedId && state.selectedId !== MASTER_ID) {
      deleteNode(state.selectedId).catch((e) => {
        state.err = e.message;
        mountStage(true);
      });
    }
  });
  document.getElementById("btn-logout")?.addEventListener("click", logout);
}

function syncSelectionChrome() {
  const stage = document.querySelector(".stage");
  if (!stage) return;
  stage.classList.toggle("has-selection", !!state.selectedId);
  stage.classList.toggle("drawer-open", state.drawerOpen);

  document.querySelectorAll(".node-wrap").forEach((g) => {
    const id = (g as HTMLElement).dataset.id;
    const on = id === state.selectedId;
    g.classList.toggle("is-selected", on);
    g.querySelector(".node-card")?.classList.toggle("selected", on);
  });
  document.querySelector(".hub")?.classList.toggle("is-selected", state.selectedId === MASTER_ID);

  const drawer = document.getElementById("settings-drawer");
  if (drawer) {
    drawer.innerHTML = drawerContent();
    bindDrawerActions();
  }
  setDrawerOpen(state.drawerOpen);
}

async function focusTarget(id: string) {
  if (state.selectedId === id && state.drawerOpen && !cameraRaf) return;
  const gen = ++focusGen;
  if (drawerTimer) {
    window.clearTimeout(drawerTimer);
    drawerTimer = 0;
  }
  state.busy = true;

  const switching = !!state.selectedId && state.selectedId !== id;
  if (switching && state.drawerOpen) {
    document.getElementById("settings-drawer")?.classList.add("content-swap");
  }

  // Keep drawer open while flying agent→agent; only open after first focus lands.
  state.selectedId = id;
  syncSelectionChrome();

  const pt = pointForSelection(id);
  const target = focusToCam({ x: pt.x, y: pt.y, scale: FOCUS_SCALE });
  // Interrupt any in-flight home/focus tween and continue from current camera.
  await animateCameraTo(target, CAM_MS);
  if (gen !== focusGen) return;

  state.drawerOpen = true;
  syncSelectionChrome();
  document.getElementById("settings-drawer")?.classList.remove("content-swap");
  state.busy = false;
}

async function goHome() {
  const home = homeCam();
  if (!state.selectedId && !state.drawerOpen && camsNear(readSvgCamera(), home) && !cameraRaf) {
    return;
  }
  const gen = ++focusGen;
  if (drawerTimer) {
    window.clearTimeout(drawerTimer);
    drawerTimer = 0;
  }
  state.busy = true;
  setDrawerOpen(false);

  // Reverse-pan from wherever the camera currently is (interruptible mid-focus).
  await animateCameraTo(home, CAM_MS);
  if (gen !== focusGen) return;

  state.selectedId = null;
  state.drawerOpen = false;
  syncSelectionChrome();
  state.busy = false;
}

async function login(password: string) {
  const body = await api<{ token: string }>("/api/login", {
    method: "POST",
    body: JSON.stringify({ password }),
  });
  state.token = body.token;
  localStorage.setItem("wgmc_admin_token", state.token);
  await refresh();
  startPoll();
}

async function refresh() {
  if (state.demo) {
    state.mesh = demoMesh();
    state.meta = demoMeta();
    mountStage(true);
    return;
  }
  const [mesh, meta] = await Promise.all([api<Mesh>("/api/mesh"), api<Meta>("/api/meta")]);
  state.mesh = mesh;
  state.meta = meta;
  const lost =
    state.selectedId &&
    state.selectedId !== MASTER_ID &&
    !(mesh.nodes || []).some((n) => n.id === state.selectedId);
  mountStage(true);
  if (lost) void goHome();
}

async function deleteNode(id: string) {
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
    mountStage(true);
    return;
  }
  if (!confirm("删除该节点？相关链接与转发也会移除。")) return;
  state.mesh = await api<Mesh>("/api/nodes?id=" + encodeURIComponent(id), { method: "DELETE" });
  await goHome();
  mountStage(true);
}

function logout() {
  if (state.pollTimer != null) {
    clearInterval(state.pollTimer);
    state.pollTimer = null;
  }
  state.token = "";
  localStorage.removeItem("wgmc_admin_token");
  state.mesh = null;
  state.meta = null;
  state.selectedId = null;
  state.drawerOpen = false;
  state.camera = homeCam();
  render();
}

function startPoll() {
  if (state.demo) return;
  if (state.pollTimer != null) clearInterval(state.pollTimer);
  state.pollTimer = window.setInterval(() => {
    if (!state.token || state.busy || cameraRaf) return;
    refresh().catch(() => {});
  }, 8000);
}

function renderLogin() {
  app.innerHTML = `
    <main class="login-wrap">
      <div class="login-card">
        <h2>Master 控制台</h2>
        <p class="muted">使用 wireguard-go-master.json 中的 adminPassword</p>
        <form id="login-form">
          <label>密码</label>
          <input type="password" name="password" required autofocus />
          <button type="submit">登录</button>
        </form>
        ${state.err ? `<p class="error">${state.err}</p>` : ""}
      </div>
    </main>`;
  document.getElementById("login-form")!.onsubmit = async (e) => {
    e.preventDefault();
    try {
      await login(String(new FormData(e.target as HTMLFormElement).get("password")));
    } catch (err) {
      state.err = (err as Error).message;
      renderLogin();
    }
  };
}

function drawerContent(): string {
  if (!state.selectedId) {
    return `<div class="drawer-body muted" style="padding-top:2rem">选择 Master 或 Agent</div>`;
  }
  if (state.selectedId === MASTER_ID) {
    return `
      <div class="drawer-head">
        <div>
          <div class="drawer-kicker">Control plane</div>
          <h2>Master</h2>
        </div>
        <button type="button" class="icon-btn" id="btn-close-drawer" aria-label="关闭">✕</button>
      </div>
      <div class="drawer-body">
        <div class="detail"><span>Listen</span><b>${escapeHtml(state.meta?.listen || "—")}</b></div>
        <div class="detail"><span>网段</span><b>${escapeHtml(state.meta?.vpnSubnet || "—")}</b></div>
        <div class="detail"><span>默认网卡</span><b>${escapeHtml(state.meta?.defaultIface || "wg0")}</b></div>
        <div class="detail"><span>轮询</span><b>${escapeHtml(state.meta?.defaultPoll || "10s")}</b></div>
        <label>enrollToken（Agent 入网密钥）</label>
        <code class="token-box">${escapeHtml(state.meta?.enrollToken || "")}</code>
        <button type="button" class="secondary" id="btn-copy-enroll">复制 enrollToken</button>
        ${state.demo ? "" : `<button type="button" class="secondary" id="btn-logout" style="margin-top:0.75rem">退出登录</button>`}
      </div>`;
  }
  const n = (state.mesh?.nodes || []).find((x) => x.id === state.selectedId);
  if (!n) return "";
  return `
    <div class="drawer-head">
      <div>
        <div class="drawer-kicker">${escapeHtml(n.role)}</div>
        <h2>${escapeHtml(n.name || shortName(n))}</h2>
      </div>
      <button type="button" class="icon-btn" id="btn-close-drawer" aria-label="关闭">✕</button>
    </div>
    <div class="drawer-body">
      <div class="detail"><span>状态</span><b class="${isOnline(n) ? "ok" : "muted"}">${isOnline(n) ? "在线" : "离线"}</b></div>
      <div class="detail"><span>地址</span><b>${escapeHtml(n.address || "—")}</b></div>
      <div class="detail"><span>Endpoint</span><b>${escapeHtml(n.endpoint || "—")}</b></div>
      <div class="detail"><span>UUID</span><code class="tiny">${escapeHtml(n.id)}</code></div>
      <div class="detail"><span>Token</span><code class="tiny">${escapeHtml(n.token || "")}</code></div>
      <button type="button" class="danger" id="btn-del-node">删除节点</button>
    </div>`;
}

/** Full stage mount. Preserves current camera so remounts don't jump. */
function mountStage(preserveCamera: boolean) {
  const m = state.mesh || { revision: 0, nodes: [], links: [], forwards: [] };
  const nodes = m.nodes || [];
  const links = m.links || [];
  state.placed = layoutNodes(nodes);
  const byId = new Map(state.placed.map((p) => [p.node.id, p]));
  if (!preserveCamera) state.camera = homeCam();
  const vb = camToAttr(state.camera);

  const linkLines = links
    .map((l) => {
      const a = byId.get(l.fromNodeId);
      const b = byId.get(l.toNodeId);
      if (!a || !b) return "";
      return `<line class="mesh-link" x1="${a.x}" y1="${a.y}" x2="${b.x}" y2="${b.y}" />`;
    })
    .join("");

  const spokes = state.placed
    .map((p) => `<line class="mesh-spoke" x1="${VIEW.cx}" y1="${VIEW.cy}" x2="${p.x}" y2="${p.y}" />`)
    .join("");

  const cards = state.placed
    .map((p) => {
      const online = isOnline(p.node);
      const selected = state.selectedId === p.node.id;
      return `
        <g class="node-wrap ${selected ? "is-selected" : ""}" data-id="${p.node.id}" transform="translate(${p.x}, ${p.y})">
          <foreignObject x="-78" y="-34" width="156" height="68">
            <div xmlns="http://www.w3.org/1999/xhtml" class="node-card ${online ? "online" : "offline"} ${selected ? "selected" : ""}">
              <span class="dot"></span>
              <div class="node-text">
                <div class="node-name">${escapeHtml(shortName(p.node))}</div>
                <div class="node-ip">${escapeHtml(hostOf(p.node.address))}</div>
              </div>
            </div>
          </foreignObject>
        </g>`;
    })
    .join("");

  app.innerHTML = `
    <div class="stage ${state.selectedId ? "has-selection" : ""} ${state.drawerOpen ? "drawer-open" : ""}">
      <svg class="mesh-svg" viewBox="${vb}" preserveAspectRatio="xMidYMid meet" role="img" aria-label="mesh">
        <rect class="stage-hit" x="${VIEW.cx - VIEW.w}" y="${VIEW.cy - VIEW.h}" width="${VIEW.w * 2}" height="${VIEW.h * 2}" fill="transparent" />
        ${spokes}
        ${linkLines}
        <g class="hub ${state.selectedId === MASTER_ID ? "is-selected" : ""}" data-id="${MASTER_ID}" transform="translate(${VIEW.cx}, ${VIEW.cy})" style="cursor:pointer">
          <circle class="hub-ring" r="42" />
          <circle class="hub-core" r="30" />
          <text class="hub-label" text-anchor="middle" dy="5">Master</text>
        </g>
        ${cards}
      </svg>
      ${nodes.length === 0 ? `<div class="topo-empty">等待 Agent 持 key 入网…</div>` : ""}
      ${state.demo ? `<div class="dev-chip">DEV</div>` : ""}
      ${state.err ? `<div class="toast error">${escapeHtml(state.err)}</div>` : ""}
      <div class="drawer-scrim ${state.drawerOpen ? "show" : ""}" id="drawer-scrim"></div>
      <aside id="settings-drawer" class="settings-drawer ${state.drawerOpen ? "open" : ""}">
        ${drawerContent()}
      </aside>
    </div>`;

  app.querySelector(".stage-hit")?.addEventListener("click", () => void goHome());
  app.querySelector(".hub")?.addEventListener("click", (e) => {
    e.stopPropagation();
    void focusTarget(MASTER_ID);
  });
  app.querySelectorAll(".node-wrap").forEach((g) => {
    g.addEventListener("click", (e) => {
      e.stopPropagation();
      const id = (g as HTMLElement).dataset.id;
      if (id) void focusTarget(id);
    });
  });
  document.getElementById("drawer-scrim")?.addEventListener("click", () => void goHome());
  bindDrawerActions();
}

function render() {
  if (state.demo) {
    if (!state.mesh) state.mesh = demoMesh();
    if (!state.meta) state.meta = demoMeta();
    mountStage(false);
    return;
  }
  if (!state.token) renderLogin();
  else if (!state.mesh) {
    app.innerHTML = `<main class="login-wrap"><p class="muted">加载中…</p></main>`;
    refresh()
      .then(() => startPoll())
      .catch((e) => {
        state.token = "";
        localStorage.removeItem("wgmc_admin_token");
        state.err = e.message;
        renderLogin();
      });
  } else mountStage(true);
}

render();
