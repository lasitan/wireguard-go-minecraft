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
};

type Tab = "topology" | "forwards" | "raw";

const ONLINE_MS = 45_000;
/** Vite `npm run dev` — skip login and render demo mesh. */
const isDevPreview = import.meta.env.DEV;

function demoMesh(): Mesh {
  const now = new Date().toISOString();
  const ago = new Date(Date.now() - 120_000).toISOString();
  return {
    revision: 7,
    nodes: [
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001",
        name: "web-server",
        role: "server",
        address: "100.96.0.1/24",
        endpoint: "1.2.3.4:25590",
        listenPort: 25590,
        token: "demo-token-web",
        lastSeen: now,
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002",
        name: "db-replica",
        role: "client",
        address: "100.96.0.2/24",
        token: "demo-token-db",
        lastSeen: now,
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003",
        name: "MacBook Pro",
        role: "client",
        address: "100.96.0.10/24",
        token: "demo-token-mac",
        lastSeen: now,
      },
      {
        id: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0004",
        name: "dev-vm",
        role: "client",
        address: "",
        token: "demo-token-vm",
        lastSeen: ago,
      },
    ],
    links: [
      {
        fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002",
        toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001",
        keepalive: 5,
      },
      {
        fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003",
        toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001",
        keepalive: 5,
      },
      {
        fromNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0004",
        toNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001",
        keepalive: 5,
      },
    ],
    forwards: [
      {
        nodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001",
        protocol: "tcp",
        listen: "3389",
        destNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0003",
        destPort: 3389,
      },
      {
        nodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0001",
        protocol: "tcp",
        listen: "5432",
        destNodeId: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeee0002",
        destPort: 5432,
      },
    ],
  };
}

function demoMeta(): Meta {
  return {
    enrollToken: "dev-preview-enroll-token",
    vpnSubnet: "100.96.0.0/24",
    listen: ":8443",
  };
}

const app = document.getElementById("app")!;
const state: {
  token: string;
  mesh: Mesh | null;
  meta: Meta | null;
  tab: Tab;
  selectedId: string | null;
  msg: string;
  err: string;
  pollTimer: number | null;
  demo: boolean;
} = {
  token: isDevPreview ? "dev-preview" : localStorage.getItem("wgmc_admin_token") || "",
  mesh: isDevPreview ? demoMesh() : null,
  meta: isDevPreview ? demoMeta() : null,
  tab: "topology",
  selectedId: null,
  msg: isDevPreview ? "开发预览：假数据（npm run dev）" : "",
  err: "",
  pollTimer: null,
  demo: isDevPreview,
};

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

function setMsg(ok: boolean, text: string) {
  state.msg = ok ? text : "";
  state.err = ok ? "" : text;
  render();
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
    render();
    return;
  }
  const [mesh, meta] = await Promise.all([api<Mesh>("/api/mesh"), api<Meta>("/api/meta")]);
  state.mesh = mesh;
  state.meta = meta;
  if (state.selectedId && !(mesh.nodes || []).some((n) => n.id === state.selectedId)) {
    state.selectedId = null;
  }
  render();
}

async function saveMesh() {
  if (state.demo) {
    setMsg(true, "开发预览：已忽略保存");
    return;
  }
  const ta = document.getElementById("mesh-json") as HTMLTextAreaElement;
  const parsed = JSON.parse(ta.value) as Mesh;
  state.mesh = await api<Mesh>("/api/mesh", { method: "PUT", body: JSON.stringify(parsed) });
  setMsg(true, "已保存 revision=" + state.mesh.revision);
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
    state.selectedId = null;
    setMsg(true, "开发预览：已从假数据中移除（不持久）");
    return;
  }
  if (!confirm("删除该节点？相关链接与转发也会移除。")) return;
  state.mesh = await api<Mesh>("/api/nodes?id=" + encodeURIComponent(id), { method: "DELETE" });
  state.selectedId = null;
  setMsg(true, "节点已删除");
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
  render();
}

function startPoll() {
  if (state.demo) return;
  if (state.pollTimer != null) clearInterval(state.pollTimer);
  state.pollTimer = window.setInterval(() => {
    if (!state.token) return;
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
      setMsg(false, (err as Error).message);
    }
  };
}

function layoutNodes(nodes: Node[], cx: number, cy: number, radius: number) {
  const n = nodes.length;
  return nodes.map((node, i) => {
    const angle = -Math.PI / 2 + (2 * Math.PI * i) / Math.max(n, 1);
    return {
      node,
      x: cx + Math.cos(angle) * radius,
      y: cy + Math.sin(angle) * radius,
    };
  });
}

function renderTopology(m: Mesh) {
  const nodes = m.nodes || [];
  const links = m.links || [];
  const W = 920;
  const H = 560;
  const cx = W / 2;
  const cy = H / 2;
  const radius = Math.min(W, H) * 0.34;
  const placed = layoutNodes(nodes, cx, cy, radius);
  const byId = new Map(placed.map((p) => [p.node.id, p]));

  const linkLines = links
    .map((l) => {
      const a = byId.get(l.fromNodeId);
      const b = byId.get(l.toNodeId);
      if (!a || !b) return "";
      return `<line class="mesh-link" x1="${a.x}" y1="${a.y}" x2="${b.x}" y2="${b.y}" />`;
    })
    .join("");

  const spokes = placed
    .map(
      (p) =>
        `<line class="mesh-spoke" x1="${cx}" y1="${cy}" x2="${p.x}" y2="${p.y}" />`
    )
    .join("");

  const cards = placed
    .map((p) => {
      const online = isOnline(p.node);
      const selected = state.selectedId === p.node.id;
      return `
        <g class="node-wrap ${selected ? "selected" : ""}" data-id="${p.node.id}" transform="translate(${p.x}, ${p.y})">
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

  const selected = nodes.find((n) => n.id === state.selectedId) || null;

  return `
    <div class="topo-layout">
      <div class="topo-stage">
        <div class="topo-hint muted">节点由 Agent 持 enrollToken 自动加入 · 仅可删除 · 绿点=近期在线</div>
        <svg class="mesh-svg" viewBox="0 0 ${W} ${H}" role="img" aria-label="mesh topology">
          ${spokes}
          ${linkLines}
          <g class="hub" transform="translate(${cx}, ${cy})">
            <circle class="hub-ring" r="36" />
            <circle class="hub-core" r="28" />
            <text class="hub-label" text-anchor="middle" dy="5">Master</text>
          </g>
          ${cards}
        </svg>
        ${
          nodes.length === 0
            ? `<div class="topo-empty">尚无节点。在 Agent 的 wireguard-go-agent.json 填入 masterUrl + enrollToken 后启动即可入网。</div>`
            : ""
        }
      </div>
      <aside class="topo-side">
        <h3>入网凭证</h3>
        <p class="muted">Agent 配置 enrollToken（与 Master 相同）即可自动注册。</p>
        <code class="token-box" id="enroll-token">${escapeHtml(state.meta?.enrollToken || "")}</code>
        <button class="secondary" id="btn-copy-enroll" type="button">复制 enrollToken</button>
        <p class="muted" style="margin-top:0.75rem">VPN 网段 ${escapeHtml(state.meta?.vpnSubnet || "")}</p>
        <hr />
        ${
          selected
            ? `<h3>节点详情</h3>
               <div class="detail"><span>名称</span><b>${escapeHtml(selected.name || "—")}</b></div>
               <div class="detail"><span>角色</span><b>${escapeHtml(selected.role)}</b></div>
               <div class="detail"><span>地址</span><b>${escapeHtml(selected.address || "—")}</b></div>
               <div class="detail"><span>状态</span><b class="${isOnline(selected) ? "ok" : "muted"}">${isOnline(selected) ? "在线" : "离线"}</b></div>
               <div class="detail"><span>UUID</span><code class="tiny">${escapeHtml(selected.id)}</code></div>
               <div class="detail"><span>Token</span><code class="tiny">${escapeHtml(selected.token || "")}</code></div>
               <button class="danger" id="btn-del-node" type="button">删除节点</button>`
            : `<p class="muted">点击图中节点查看详情 / 删除</p>`
        }
      </aside>
    </div>`;
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function renderApp() {
  const m = state.mesh || { revision: 0, nodes: [], links: [], forwards: [] };
  const online = (m.nodes || []).filter(isOnline).length;
  app.innerHTML = `
    <header>
      <div>
        <h1>wireguard-mc <span class="muted">mesh</span>${state.demo ? ` <span class="demo-badge">DEV</span>` : ""}</h1>
        <div class="sub muted">rev ${m.revision} · ${online}/${(m.nodes || []).length} 在线${state.demo ? " · 假数据预览" : ""}</div>
      </div>
      <div class="row">
        ${state.demo ? "" : `<button class="secondary" id="btn-reload" type="button">刷新</button>
        <button class="secondary" id="btn-logout" type="button">退出</button>`}
        ${state.demo ? `<button class="secondary" id="btn-reload" type="button">重置假数据</button>` : ""}
      </div>
    </header>
    <main class="wide">
      ${state.msg ? `<p class="ok banner">${state.msg}</p>` : ""}
      ${state.err ? `<p class="error banner">${state.err}</p>` : ""}
      <div class="tabs">
        <button data-tab="topology" class="${state.tab === "topology" ? "active" : ""}" type="button">拓扑</button>
        <button data-tab="forwards" class="${state.tab === "forwards" ? "active" : ""}" type="button">转发</button>
        <button data-tab="raw" class="${state.tab === "raw" ? "active" : ""}" type="button">高级 JSON</button>
      </div>
      <div id="tab-body"></div>
    </main>`;
  document.getElementById("btn-reload")!.onclick = () => {
    if (state.demo) {
      state.mesh = demoMesh();
      state.meta = demoMeta();
      state.selectedId = null;
      setMsg(true, "开发预览：假数据已重置");
      return;
    }
    refresh().catch((e) => setMsg(false, e.message));
  };
  const logoutBtn = document.getElementById("btn-logout");
  if (logoutBtn) logoutBtn.onclick = logout;
  document.querySelectorAll(".tabs button").forEach((b) => {
    (b as HTMLButtonElement).onclick = () => {
      state.tab = (b as HTMLElement).dataset.tab as Tab;
      render();
    };
  });
  const body = document.getElementById("tab-body")!;
  if (state.tab === "topology") {
    body.innerHTML = renderTopology(m);
    body.querySelectorAll(".node-wrap").forEach((g) => {
      (g as SVGGElement).onclick = () => {
        state.selectedId = (g as HTMLElement).dataset.id || null;
        render();
      };
    });
    const copyBtn = document.getElementById("btn-copy-enroll");
    if (copyBtn) {
      copyBtn.onclick = async () => {
        try {
          await navigator.clipboard.writeText(state.meta?.enrollToken || "");
          setMsg(true, "enrollToken 已复制");
        } catch {
          setMsg(false, "复制失败");
        }
      };
    }
    const del = document.getElementById("btn-del-node");
    if (del && state.selectedId) {
      del.onclick = () => deleteNode(state.selectedId!).catch((e) => setMsg(false, e.message));
    }
  } else if (state.tab === "forwards") {
    body.innerHTML = `
      <div class="panel">
        <h2>端口转发</h2>
        <p class="muted">在「高级 JSON」中编辑 forwards；拓扑页只负责观察与删除节点。</p>
        <div class="fwd-grid">
          ${(m.forwards || [])
            .map(
              (f) => `<div class="fwd-card">
                <div class="fwd-title">${escapeHtml(f.protocol.toUpperCase())} :${escapeHtml(f.listen)}</div>
                <div class="muted">${escapeHtml(f.nodeId.slice(0, 8))}… → ${escapeHtml(f.destNodeId.slice(0, 8))}…:${f.destPort}</div>
              </div>`
            )
            .join("") || `<p class="muted">暂无转发</p>`}
        </div>
      </div>`;
  } else {
    body.innerHTML = `
      <div class="panel">
        <h2>Mesh JSON</h2>
        <p class="muted">可改链接/转发/已有节点字段；<b>不能通过 JSON 新增节点</b>（须 Agent enroll）。</p>
        <textarea id="mesh-json"></textarea>
        <div class="row"><button id="btn-save" type="button">保存</button></div>
      </div>`;
    (document.getElementById("mesh-json") as HTMLTextAreaElement).value = JSON.stringify(m, null, 2);
    document.getElementById("btn-save")!.onclick = () => saveMesh().catch((e) => setMsg(false, e.message));
  }
}

function render() {
  if (state.demo) {
    if (!state.mesh) state.mesh = demoMesh();
    if (!state.meta) state.meta = demoMeta();
    renderApp();
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
        setMsg(false, e.message);
      });
  } else renderApp();
}

render();
