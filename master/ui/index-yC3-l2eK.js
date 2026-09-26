(function(){const o=document.createElement("link").relList;if(o&&o.supports&&o.supports("modulepreload"))return;for(const a of document.querySelectorAll('link[rel="modulepreload"]'))s(a);new MutationObserver(a=>{for(const r of a)if(r.type==="childList")for(const m of r.addedNodes)m.tagName==="LINK"&&m.rel==="modulepreload"&&s(m)}).observe(document,{childList:!0,subtree:!0});function t(a){const r={};return a.integrity&&(r.integrity=a.integrity),a.referrerPolicy&&(r.referrerPolicy=a.referrerPolicy),a.crossOrigin==="use-credentials"?r.credentials="include":a.crossOrigin==="anonymous"?r.credentials="omit":r.credentials="same-origin",r}function s(a){if(a.ep)return;a.ep=!0;const r=t(a);fetch(a.href,r)}})();const u=document.getElementById("app"),e={token:localStorage.getItem("wgmc_admin_token")||"",mesh:null,tab:"nodes",msg:"",err:""};async function l(n,o={}){const t={"Content-Type":"application/json",...o.headers};e.token&&(t.Authorization="Bearer "+e.token);const s=await fetch(n,{...o,headers:t}),a=await s.text();let r=null;try{r=a?JSON.parse(a):null}catch{r={error:a}}if(!s.ok)throw new Error(r&&r.error||s.statusText);return r}function d(n,o){e.msg=n?o:"",e.err=n?"":o,i()}async function h(n){const o=await l("/api/login",{method:"POST",body:JSON.stringify({password:n})});e.token=o.token,localStorage.setItem("wgmc_admin_token",e.token),await c()}async function c(){e.mesh=await l("/api/mesh"),i()}async function b(){const n=document.getElementById("mesh-json"),o=JSON.parse(n.value);e.mesh=await l("/api/mesh",{method:"PUT",body:JSON.stringify(o)}),d(!0,"已保存 revision="+e.mesh.revision)}async function p(n){n.preventDefault();const o=new FormData(n.target),t={name:String(o.get("name")||"")||void 0,role:String(o.get("role")),address:String(o.get("address")),listenPort:Number(o.get("listenPort")||0)||void 0,endpoint:String(o.get("endpoint")||"")||void 0},s=await l("/api/nodes",{method:"POST",body:JSON.stringify(t)});await c(),d(!0,"节点已创建 UUID="+s.id)}function f(){e.token="",localStorage.removeItem("wgmc_admin_token"),e.mesh=null,i()}function g(){u.innerHTML=`
    <main>
      <div class="card" style="max-width:380px;margin:4rem auto;">
        <h2>Master 登录</h2>
        <p class="muted">使用 wireguard-go-master.json 中的 adminPassword</p>
        <form id="login-form">
          <label>密码</label>
          <input type="password" name="password" required autofocus />
          <button type="submit">登录</button>
        </form>
        ${e.err?`<p class="error">${e.err}</p>`:""}
      </div>
    </main>`,document.getElementById("login-form").onsubmit=async n=>{n.preventDefault();try{await h(String(new FormData(n.target).get("password")))}catch(o){d(!1,o.message)}}}function y(){const n=e.mesh||{revision:0,nodes:[],links:[],forwards:[]};u.innerHTML=`
    <header>
      <h1>wireguard-mc Master <span class="muted">rev ${n.revision}</span></h1>
      <div class="row">
        <button class="secondary" id="btn-reload">刷新</button>
        <button class="secondary" id="btn-logout">退出</button>
      </div>
    </header>
    <main>
      ${e.msg?`<p class="ok">${e.msg}</p>`:""}
      ${e.err?`<p class="error">${e.err}</p>`:""}
      <div class="tabs">
        <button data-tab="nodes" class="${e.tab==="nodes"?"active":""}">节点</button>
        <button data-tab="links" class="${e.tab==="links"?"active":""}">链接</button>
        <button data-tab="forwards" class="${e.tab==="forwards"?"active":""}">转发</button>
        <button data-tab="raw" class="${e.tab==="raw"?"active":""}">原始 JSON</button>
      </div>
      <div id="tab-body"></div>
    </main>`,document.getElementById("btn-reload").onclick=()=>c().catch(t=>d(!1,t.message)),document.getElementById("btn-logout").onclick=f,document.querySelectorAll(".tabs button").forEach(t=>{t.onclick=()=>{e.tab=t.dataset.tab,i()}});const o=document.getElementById("tab-body");e.tab==="nodes"?(o.innerHTML=`
      <div class="card">
        <h2>节点列表</h2>
        <table>
          <thead><tr><th>名称</th><th>UUID</th><th>角色</th><th>Address</th><th>Endpoint</th><th>Token</th></tr></thead>
          <tbody>
            ${(n.nodes||[]).map(t=>`<tr>
              <td>${t.name||"—"}</td><td><code>${t.id}</code></td><td>${t.role}</td><td>${t.address}</td>
              <td>${t.endpoint||""}</td><td><code>${t.token||""}</code></td>
            </tr>`).join("")||'<tr><td colspan="6" class="muted">暂无节点</td></tr>'}
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
      </div>`,document.getElementById("node-form").onsubmit=t=>p(t).catch(s=>d(!1,s.message))):e.tab==="links"?o.innerHTML=`
      <div class="card">
        <h2>链接（from 拨号 to）</h2>
        <table>
          <thead><tr><th>From</th><th>To</th><th>AllowedIPs</th><th>Keepalive</th></tr></thead>
          <tbody>
            ${(n.links||[]).map(t=>`<tr>
              <td>${t.fromNodeId}</td><td>${t.toNodeId}</td>
              <td>${(t.allowedIPs||[]).join(", ")||"(默认 /32)"}</td>
              <td>${t.keepalive||""}</td>
            </tr>`).join("")||'<tr><td colspan="4" class="muted">暂无链接 — 请在「原始 JSON」中编辑</td></tr>'}
          </tbody>
        </table>
        <p class="muted">在「原始 JSON」中编辑 links / forwards 后保存。</p>
      </div>`:e.tab==="forwards"?o.innerHTML=`
      <div class="card">
        <h2>端口转发任务</h2>
        <table>
          <thead><tr><th>Node</th><th>Proto</th><th>Listen</th><th>Dest</th></tr></thead>
          <tbody>
            ${(n.forwards||[]).map(t=>`<tr>
              <td>${t.nodeId}</td><td>${t.protocol}</td><td>${t.listen}</td>
              <td>${t.destNodeId}:${t.destPort}</td>
            </tr>`).join("")||'<tr><td colspan="4" class="muted">暂无转发</td></tr>'}
          </tbody>
        </table>
      </div>`:(o.innerHTML=`
      <div class="card">
        <h2>Mesh JSON</h2>
        <textarea id="mesh-json">${JSON.stringify(n,null,2)}</textarea>
        <div class="row">
          <button id="btn-save">保存（revision+1）</button>
        </div>
      </div>`,document.getElementById("btn-save").onclick=()=>b().catch(t=>d(!1,t.message)))}function i(){e.token?e.mesh?y():(u.innerHTML='<main><p class="muted">加载中…</p></main>',c().catch(n=>{e.token="",localStorage.removeItem("wgmc_admin_token"),d(!1,n.message)})):g()}i();
