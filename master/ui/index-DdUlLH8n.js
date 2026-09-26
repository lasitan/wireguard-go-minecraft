(function(){const n=document.createElement("link").relList;if(n&&n.supports&&n.supports("modulepreload"))return;for(const a of document.querySelectorAll('link[rel="modulepreload"]'))o(a);new MutationObserver(a=>{for(const s of a)if(s.type==="childList")for(const d of s.addedNodes)d.tagName==="LINK"&&d.rel==="modulepreload"&&o(d)}).observe(document,{childList:!0,subtree:!0});function r(a){const s={};return a.integrity&&(s.integrity=a.integrity),a.referrerPolicy&&(s.referrerPolicy=a.referrerPolicy),a.crossOrigin==="use-credentials"?s.credentials="include":a.crossOrigin==="anonymous"?s.credentials="omit":s.credentials="same-origin",s}function o(a){if(a.ep)return;a.ep=!0;const s=r(a);fetch(a.href,s)}})();const S=45e3,w=document.getElementById("app"),e={token:localStorage.getItem("wgmc_admin_token")||"",mesh:null,meta:null,tab:"topology",selectedId:null,msg:"",err:"",pollTimer:null};async function f(t,n={}){const r={"Content-Type":"application/json",...n.headers};e.token&&(r.Authorization="Bearer "+e.token);const o=await fetch(t,{...n,headers:r}),a=await o.text();let s=null;try{s=a?JSON.parse(a):null}catch{s={error:a}}if(!o.ok)throw new Error(s&&s.error||o.statusText);return s}function c(t,n){e.msg=t?n:"",e.err=t?"":n,p()}function h(t){if(!t.lastSeen)return!1;const n=Date.parse(t.lastSeen);return Number.isNaN(n)?!1:Date.now()-n<S}function M(t){if(!t)return"—";const n=t.indexOf("/");return n>=0?t.slice(0,n):t}function E(t){const n=(t.name||t.role||t.id).trim();return n.length>14?n.slice(0,12)+"…":n}async function L(t){const n=await f("/api/login",{method:"POST",body:JSON.stringify({password:t})});e.token=n.token,localStorage.setItem("wgmc_admin_token",e.token),await y(),T()}async function y(){const[t,n]=await Promise.all([f("/api/mesh"),f("/api/meta")]);e.mesh=t,e.meta=n,e.selectedId&&!(t.nodes||[]).some(r=>r.id===e.selectedId)&&(e.selectedId=null),p()}async function B(){const t=document.getElementById("mesh-json"),n=JSON.parse(t.value);e.mesh=await f("/api/mesh",{method:"PUT",body:JSON.stringify(n)}),c(!0,"已保存 revision="+e.mesh.revision)}async function P(t){confirm("删除该节点？相关链接与转发也会移除。")&&(e.mesh=await f("/api/nodes?id="+encodeURIComponent(t),{method:"DELETE"}),e.selectedId=null,c(!0,"节点已删除"))}function j(){e.pollTimer!=null&&(clearInterval(e.pollTimer),e.pollTimer=null),e.token="",localStorage.removeItem("wgmc_admin_token"),e.mesh=null,e.meta=null,e.selectedId=null,p()}function T(){e.pollTimer!=null&&clearInterval(e.pollTimer),e.pollTimer=window.setInterval(()=>{e.token&&y().catch(()=>{})},8e3)}function A(){w.innerHTML=`
    <main class="login-wrap">
      <div class="login-card">
        <h2>Master 控制台</h2>
        <p class="muted">使用 wireguard-go-master.json 中的 adminPassword</p>
        <form id="login-form">
          <label>密码</label>
          <input type="password" name="password" required autofocus />
          <button type="submit">登录</button>
        </form>
        ${e.err?`<p class="error">${e.err}</p>`:""}
      </div>
    </main>`,document.getElementById("login-form").onsubmit=async t=>{t.preventDefault();try{await L(String(new FormData(t.target).get("password")))}catch(n){c(!1,n.message)}}}function J(t,n,r,o){const a=t.length;return t.map((s,d)=>{const b=-Math.PI/2+2*Math.PI*d/Math.max(a,1);return{node:s,x:n+Math.cos(b)*o,y:r+Math.sin(b)*o}})}function _(t){var I,k;const n=t.nodes||[],r=t.links||[],o=920,a=560,s=o/2,d=a/2,b=Math.min(o,a)*.34,v=J(n,s,d,b),$=new Map(v.map(l=>[l.node.id,l])),x=r.map(l=>{const g=$.get(l.fromNodeId),m=$.get(l.toNodeId);return!g||!m?"":`<line class="mesh-link" x1="${g.x}" y1="${g.y}" x2="${m.x}" y2="${m.y}" />`}).join(""),N=v.map(l=>`<line class="mesh-spoke" x1="${s}" y1="${d}" x2="${l.x}" y2="${l.y}" />`).join(""),O=v.map(l=>{const g=h(l.node),m=e.selectedId===l.node.id;return`
        <g class="node-wrap ${m?"selected":""}" data-id="${l.node.id}" transform="translate(${l.x}, ${l.y})">
          <foreignObject x="-78" y="-34" width="156" height="68">
            <div xmlns="http://www.w3.org/1999/xhtml" class="node-card ${g?"online":"offline"} ${m?"selected":""}">
              <span class="dot"></span>
              <div class="node-text">
                <div class="node-name">${i(E(l.node))}</div>
                <div class="node-ip">${i(M(l.node.address))}</div>
              </div>
            </div>
          </foreignObject>
        </g>`}).join(""),u=n.find(l=>l.id===e.selectedId)||null;return`
    <div class="topo-layout">
      <div class="topo-stage">
        <div class="topo-hint muted">节点由 Agent 持 enrollToken 自动加入 · 仅可删除 · 绿点=近期在线</div>
        <svg class="mesh-svg" viewBox="0 0 ${o} ${a}" role="img" aria-label="mesh topology">
          ${N}
          ${x}
          <g class="hub" transform="translate(${s}, ${d})">
            <circle class="hub-ring" r="36" />
            <circle class="hub-core" r="28" />
            <text class="hub-label" text-anchor="middle" dy="5">Master</text>
          </g>
          ${O}
        </svg>
        ${n.length===0?'<div class="topo-empty">尚无节点。在 Agent 的 wireguard-go-agent.json 填入 masterUrl + enrollToken 后启动即可入网。</div>':""}
      </div>
      <aside class="topo-side">
        <h3>入网凭证</h3>
        <p class="muted">Agent 配置 enrollToken（与 Master 相同）即可自动注册。</p>
        <code class="token-box" id="enroll-token">${i(((I=e.meta)==null?void 0:I.enrollToken)||"")}</code>
        <button class="secondary" id="btn-copy-enroll" type="button">复制 enrollToken</button>
        <p class="muted" style="margin-top:0.75rem">VPN 网段 ${i(((k=e.meta)==null?void 0:k.vpnSubnet)||"")}</p>
        <hr />
        ${u?`<h3>节点详情</h3>
               <div class="detail"><span>名称</span><b>${i(u.name||"—")}</b></div>
               <div class="detail"><span>角色</span><b>${i(u.role)}</b></div>
               <div class="detail"><span>地址</span><b>${i(u.address||"—")}</b></div>
               <div class="detail"><span>状态</span><b class="${h(u)?"ok":"muted"}">${h(u)?"在线":"离线"}</b></div>
               <div class="detail"><span>UUID</span><code class="tiny">${i(u.id)}</code></div>
               <div class="detail"><span>Token</span><code class="tiny">${i(u.token||"")}</code></div>
               <button class="danger" id="btn-del-node" type="button">删除节点</button>`:'<p class="muted">点击图中节点查看详情 / 删除</p>'}
      </aside>
    </div>`}function i(t){return t.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;")}function H(){const t=e.mesh||{revision:0,nodes:[],links:[],forwards:[]},n=(t.nodes||[]).filter(h).length;w.innerHTML=`
    <header>
      <div>
        <h1>wireguard-mc <span class="muted">mesh</span></h1>
        <div class="sub muted">rev ${t.revision} · ${n}/${(t.nodes||[]).length} 在线</div>
      </div>
      <div class="row">
        <button class="secondary" id="btn-reload" type="button">刷新</button>
        <button class="secondary" id="btn-logout" type="button">退出</button>
      </div>
    </header>
    <main class="wide">
      ${e.msg?`<p class="ok banner">${e.msg}</p>`:""}
      ${e.err?`<p class="error banner">${e.err}</p>`:""}
      <div class="tabs">
        <button data-tab="topology" class="${e.tab==="topology"?"active":""}" type="button">拓扑</button>
        <button data-tab="forwards" class="${e.tab==="forwards"?"active":""}" type="button">转发</button>
        <button data-tab="raw" class="${e.tab==="raw"?"active":""}" type="button">高级 JSON</button>
      </div>
      <div id="tab-body"></div>
    </main>`,document.getElementById("btn-reload").onclick=()=>y().catch(o=>c(!1,o.message)),document.getElementById("btn-logout").onclick=j,document.querySelectorAll(".tabs button").forEach(o=>{o.onclick=()=>{e.tab=o.dataset.tab,p()}});const r=document.getElementById("tab-body");if(e.tab==="topology"){r.innerHTML=_(t),r.querySelectorAll(".node-wrap").forEach(s=>{s.onclick=()=>{e.selectedId=s.dataset.id||null,p()}});const o=document.getElementById("btn-copy-enroll");o&&(o.onclick=async()=>{var s;try{await navigator.clipboard.writeText(((s=e.meta)==null?void 0:s.enrollToken)||""),c(!0,"enrollToken 已复制")}catch{c(!1,"复制失败")}});const a=document.getElementById("btn-del-node");a&&e.selectedId&&(a.onclick=()=>P(e.selectedId).catch(s=>c(!1,s.message)))}else e.tab==="forwards"?r.innerHTML=`
      <div class="panel">
        <h2>端口转发</h2>
        <p class="muted">在「高级 JSON」中编辑 forwards；拓扑页只负责观察与删除节点。</p>
        <div class="fwd-grid">
          ${(t.forwards||[]).map(o=>`<div class="fwd-card">
                <div class="fwd-title">${i(o.protocol.toUpperCase())} :${i(o.listen)}</div>
                <div class="muted">${i(o.nodeId.slice(0,8))}… → ${i(o.destNodeId.slice(0,8))}…:${o.destPort}</div>
              </div>`).join("")||'<p class="muted">暂无转发</p>'}
        </div>
      </div>`:(r.innerHTML=`
      <div class="panel">
        <h2>Mesh JSON</h2>
        <p class="muted">可改链接/转发/已有节点字段；<b>不能通过 JSON 新增节点</b>（须 Agent enroll）。</p>
        <textarea id="mesh-json"></textarea>
        <div class="row"><button id="btn-save" type="button">保存</button></div>
      </div>`,document.getElementById("mesh-json").value=JSON.stringify(t,null,2),document.getElementById("btn-save").onclick=()=>B().catch(o=>c(!1,o.message)))}function p(){e.token?e.mesh?H():(w.innerHTML='<main class="login-wrap"><p class="muted">加载中…</p></main>',y().then(()=>T()).catch(t=>{e.token="",localStorage.removeItem("wgmc_admin_token"),c(!1,t.message)})):A()}p();
