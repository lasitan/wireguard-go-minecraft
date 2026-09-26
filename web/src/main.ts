import "./styles.css";

type Node = {
  id: string;
  name?: string;
  role: string;
  address: string;
  endpoint?: string;
  token?: string;
  listenPort?: number;
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

type Tab = "nodes" | "links" | "forwards" | "raw";

const app = document.getElementById("app")!;
const state: {
  token: string;
  mesh: Mesh | null;
  tab: Tab;
  msg: string;
  err: string;
} = {
  token: localStorage.getItem("wgmc_admin_token") || "",
  mesh: null,
  tab: "nodes",
  msg: "",
  err: "",
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

async function login(password: string) {
  const body = await api<{ token: string }>("/api/login", {
    method: "POST",
    body: JSON.stringify({ password }),
  });
  state.token = body.token;
  localStorage.setItem("wgmc_admin_token", state.token);
  await loadMesh();
}

async function loadMesh() {
  state.mesh = await api<Mesh>("/api/mesh");
  render();
}

async function saveMesh() {
  const ta = document.getElementById("mesh-json") as HTMLTextAreaElement;
  const parsed = JSON.parse(ta.value) as Mesh;
  state.mesh = await api<Mesh>("/api/mesh", { method: "PUT", body: JSON.stringify(parsed) });
  setMsg(true, "已保存 revision=" + state.mesh.revision);
}

async function createNode(ev: Event) {
  ev.preventDefault();
  const fd = new FormData(ev.target as HTMLFormElement);
  const node = {
    name: String(fd.get("name") || "") || undefined,
    role: String(fd.get("role")),
    address: String(fd.get("address")),
    listenPort: Number(fd.get("listenPort") || 0) || undefined,
    endpoint: String(fd.get("endpoint") || "") || undefined,
  };
  const created = await api<Node>("/api/nodes", { method: "POST", body: JSON.stringify(node) });
  await loadMesh();
  setMsg(true, "节点已创建 UUID=" + created.id);
}

function logout() {
  state.token = "";
  localStorage.removeItem("wgmc_admin_token");
  state.mesh = null;
  render();
}

function renderLogin() {
  app.innerHTML = `
    <main>
      <div class="card" style="max-width:380px;margin:4rem auto;">
        <h2>Master 登录</h2>
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

function renderApp() {
  const m = state.mesh || { revision: 0, nodes: [], links: [], forwards: [] };
  app.innerHTML = `
    <header>
      <h1>wireguard-mc Master <span class="muted">rev ${m.revision}</span></h1>
      <div class="row">
        <button class="secondary" id="btn-reload">刷新</button>
        <button class="secondary" id="btn-logout">退出</button>
      </div>
    </header>
    <main>
      ${state.msg ? `<p class="ok">${state.msg}</p>` : ""}
      ${state.err ? `<p class="error">${state.err}</p>` : ""}
      <div class="tabs">
        <button data-tab="nodes" class="${state.tab === "nodes" ? "active" : ""}">节点</button>
        <button data-tab="links" class="${state.tab === "links" ? "active" : ""}">链接</button>
        <button data-tab="forwards" class="${state.tab === "forwards" ? "active" : ""}">转发</button>
        <button data-tab="raw" class="${state.tab === "raw" ? "active" : ""}">原始 JSON</button>
      </div>
      <div id="tab-body"></div>
    </main>`;
  document.getElementById("btn-reload")!.onclick = () => loadMesh().catch((e) => setMsg(false, e.message));
  document.getElementById("btn-logout")!.onclick = logout;
  document.querySelectorAll(".tabs button").forEach((b) => {
    (b as HTMLButtonElement).onclick = () => {
      state.tab = (b as HTMLElement).dataset.tab as Tab;
      render();
    };
  });
  const body = document.getElementById("tab-body")!;
  if (state.tab === "nodes") {
    body.innerHTML = `
      <div class="card">
        <h2>节点列表</h2>
        <table>
          <thead><tr><th>名称</th><th>UUID</th><th>角色</th><th>Address</th><th>Endpoint</th><th>Token</th></tr></thead>
          <tbody>
            ${(m.nodes || [])
              .map(
                (n) => `<tr>
              <td>${n.name || "—"}</td><td><code>${n.id}</code></td><td>${n.role}</td><td>${n.address}</td>
              <td>${n.endpoint || ""}</td><td><code>${n.token || ""}</code></td>
            </tr>`
              )
              .join("") || `<tr><td colspan="6" class="muted">暂无节点</td></tr>`}
          </tbody>
        </table>
      </div>
      <div class="card">
        <h2>添加节点（Master 分配 UUID，自动生成密钥与 token）</h2>
        <form id="node-form">
          <label>名称</label><input name="name" placeholder="显示名（可选）" />
          <label>角色</label>
          <select name="role"><option value="client">client</option><option value="server">server</option></select>
          <label>Address</label><input name="address" required placeholder="10.10.0.7/24" />
          <label>ListenPort（server）</label><input name="listenPort" placeholder="25590" />
          <label>Endpoint（server 公网）</label><input name="endpoint" placeholder="1.2.3.4:25590" />
          <button type="submit">创建</button>
        </form>
      </div>`;
    document.getElementById("node-form")!.onsubmit = (e) => createNode(e).catch((err) => setMsg(false, err.message));
  } else if (state.tab === "links") {
    body.innerHTML = `
      <div class="card">
        <h2>链接（from 拨号 to）</h2>
        <table>
          <thead><tr><th>From</th><th>To</th><th>AllowedIPs</th><th>Keepalive</th></tr></thead>
          <tbody>
            ${(m.links || [])
              .map(
                (l) => `<tr>
              <td>${l.fromNodeId}</td><td>${l.toNodeId}</td>
              <td>${(l.allowedIPs || []).join(", ") || "(默认 /32)"}</td>
              <td>${l.keepalive || ""}</td>
            </tr>`
              )
              .join("") || `<tr><td colspan="4" class="muted">暂无链接 — 请在「原始 JSON」中编辑</td></tr>`}
          </tbody>
        </table>
        <p class="muted">在「原始 JSON」中编辑 links / forwards 后保存。</p>
      </div>`;
  } else if (state.tab === "forwards") {
    body.innerHTML = `
      <div class="card">
        <h2>端口转发任务</h2>
        <table>
          <thead><tr><th>Node</th><th>Proto</th><th>Listen</th><th>Dest</th></tr></thead>
          <tbody>
            ${(m.forwards || [])
              .map(
                (f) => `<tr>
              <td>${f.nodeId}</td><td>${f.protocol}</td><td>${f.listen}</td>
              <td>${f.destNodeId}:${f.destPort}</td>
            </tr>`
              )
              .join("") || `<tr><td colspan="4" class="muted">暂无转发</td></tr>`}
          </tbody>
        </table>
      </div>`;
  } else {
    body.innerHTML = `
      <div class="card">
        <h2>Mesh JSON</h2>
        <textarea id="mesh-json">${JSON.stringify(m, null, 2)}</textarea>
        <div class="row">
          <button id="btn-save">保存（revision+1）</button>
        </div>
      </div>`;
    document.getElementById("btn-save")!.onclick = () => saveMesh().catch((e) => setMsg(false, e.message));
  }
}

function render() {
  if (!state.token) renderLogin();
  else if (!state.mesh) {
    app.innerHTML = `<main><p class="muted">加载中…</p></main>`;
    loadMesh().catch((e) => {
      state.token = "";
      localStorage.removeItem("wgmc_admin_token");
      setMsg(false, e.message);
    });
  } else renderApp();
}

render();
