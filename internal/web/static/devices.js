/* G7 设备：设置浮层的设备页，列出这台电脑和配对的设备。
   只经 /api/devices* 和本机 serve 通信。配对码的弹窗在 pairing.js，通过 window.xbotDevices 挂进来。 */
(function () {
  "use strict";
  const I = {
    laptop: '<path d="M18 5a2 2 0 0 1 2 2v8.5a2 2 0 0 0 .2.9l1.1 2.1a1 1 0 0 1-.9 1.5H3.6a1 1 0 0 1-.9-1.5l1.1-2.1a2 2 0 0 0 .2-.9V7a2 2 0 0 1 2-2z"/><path d="M20 16H4"/>',
    devices: '<path d="M18 8V6a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h8"/><path d="M10 19v-4"/><path d="M7 19h5"/><rect width="6" height="10" x="16" y="11" rx="2"/>',
    copy: '<rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
    check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
    refresh: '<path d="M21 12a9 9 0 0 1-15.5 6.2L3 16"/><path d="M3 21v-5h5"/><path d="M3 12a9 9 0 0 1 15.5-6.2L21 8"/><path d="M21 3v5h-5"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
    ellipsis: '<circle cx="5" cy="12" r="1.5" fill="currentColor"/><circle cx="12" cy="12" r="1.5" fill="currentColor"/><circle cx="19" cy="12" r="1.5" fill="currentColor"/>',
    pencil: '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/>',
    unlink: '<path d="m18.84 12.25 1.72-1.71a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="m5.17 11.75-1.71 1.71a5 5 0 0 0 7.07 7.07l1.71-1.71"/><path d="M8 2v3"/><path d="M2 8h3"/><path d="M16 22v-3"/><path d="M22 16h-3"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    keyboard: '<rect width="20" height="14" x="2" y="5" rx="2"/><path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M6 13h.01M18 13h.01M9 15.5h6"/>',
    spin: '<circle cx="12" cy="12" r="8.5" opacity=".22"/><path d="M12 3.5a8.5 8.5 0 0 1 8.5 8.5"/>',
    bot: '<rect x="4" y="8" width="16" height="12" rx="3"/><path d="M12 4v4"/><path d="M9 13h.01M15 13h.01"/>',
    swap: '<path d="M16 3l4 4-4 4"/><path d="M20 7H4"/><path d="M8 21l-4-4 4-4"/><path d="M4 17h16"/>',
    shield: '<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/>',
    server: '<rect width="20" height="8" x="2" y="2" rx="2"/><rect width="20" height="8" x="2" y="14" rx="2"/><path d="M6 6h.01M6 18h.01"/>',
    wifi: '<path d="M12 20h.01"/><path d="M2 8.82a15 15 0 0 1 20 0"/><path d="M5 12.86a10 10 0 0 1 14 0"/><path d="M8.5 16.43a5 5 0 0 1 7 0"/>',
    cloudOff: '<path d="m2 2 20 20"/><path d="M5.78 5.78A7 7 0 0 0 9 19h8.5a4.5 4.5 0 0 0 1.31-.2"/><path d="M21.53 16.9A4.5 4.5 0 0 0 17.5 10h-1.79A7 7 0 0 0 9.69 5.8"/>',
  };
  const svg = (k, size) => `<svg viewBox="0 0 24 24" aria-hidden="true" class="icon"${size ? ` style="width:${size}px;height:${size}px"` : ""}>${I[k]}</svg>`;
  const esc = (s) => String(s == null ? "" : s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  const host = (u) => { try { return new URL(u).host || u; } catch (e) { return u || ""; } };
  // Same rule as the server (remote.NormalizeGateway): trim, https:// for a bare host, drop the trailing slash.
  const normGw = (u) => { const s = String(u || "").trim().replace(/\/+$/, ""); return s && s.indexOf("://") < 0 ? "https://" + s : s; };

  async function api(method, path, body) {
    const res = await fetch(path, {
      method, credentials: "same-origin",
      headers: body === undefined ? {} : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (res.status === 401) { sessionExpired(); }
    authed = true;
    if (res.status === 204) return null;
    let data = null;
    try { data = await res.json(); } catch (e) { data = null; }
    if (!res.ok) { const err = new Error((data && data.error) || "请求失败"); err.status = res.status; err.data = data; throw err; } // 远端消息的 502 还带 {message}
    return data;
  }
  // 登录过期：收起设备抽屉和弹窗、清掉状态和挂到 body 上的层，重新加载让 app.js 走登录流程
  let authed = false; // Before login (the login page) a 401 is normal: never reload then.
  function sessionExpired() {
    if (!authed) { const err = new Error("未登录"); err.status = 401; throw err; }
    S.embedded = false; S.view = null; S.menuFor = ""; S.target = null; S.fieldErr = ""; S.busy = false;
    if (devicesEl) devicesEl.replaceChildren();
    if (modalEl) { try { modalEl.remove(); } catch (e) { modalEl.innerHTML = ""; } }
    devicesEl = modalEl = null;
    location.reload();
    const err = new Error("登录已过期");
    err.status = 401;
    throw err;
  }

  function left(expires, minus) {
    const t = Math.max(0, Math.round((new Date(expires).getTime() - (minus || 0) - Date.now()) / 1000));
    return Math.floor(t / 60) + ":" + String(t % 60).padStart(2, "0");
  }
  function fmtSeen(iso) {
    if (!iso) return "";
    const d = new Date(iso), now = new Date();
    if (isNaN(d.getTime()) || d.getFullYear() < 2000) return "";
    const hm = String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
    const day = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
    const diff = Math.round((day(now) - day(d)) / 86400000);
    if (diff === 0) return "今天 " + hm;
    if (diff === 1) return "昨天 " + hm;
    return (d.getMonth() + 1) + "月" + d.getDate() + "日";
  }

  // ---- 状态 ----
  const S = {
    data: null,            // GET /api/devices
    pairing: { a: {}, b: {} },
    view: null,            // drawer 之上的弹窗
    embedded: false,
    code: "",              // A 最近一次显示的码，用于确认框副行
    peer: null,            // B 端最近看到的对方 {name, os, bots}
    joinVal: "", gwOpen: false, gwVal: "", fieldErr: "", checking: false,
    menuFor: "", target: null, selfGwEdit: false, selfGwErr: "", busy: false,
  };
  let devicesEl = null, modalEl = null, lastView = "";
  // 挂给 pairing.js 的句柄（最后填上 views/acts 等）。
  const D = window.xbotDevices = {};

  function ensureHosts() {
    devicesEl = document.getElementById("settings-devices");
    if (modalEl) return;
    modalEl = document.createElement("div"); modalEl.className = "g7-layer"; modalEl.id = "devices-modal";
    document.body.append(modalEl);
  }

  async function refresh() {
    try { S.data = await api("GET", "/api/devices"); S.pairing = S.data.pairing; window.dispatchEvent(new Event("xbot:devices")); } catch (e) { /* 离线时保留旧数据 */ }
    paintComputerName();
    paintSidebar();
  }
  function paintComputerName() {
    // This computer's name under 我 at the bottom of the sidebar.
    const me = document.getElementById("me-device");
    const self = (S.data && S.data.self) || {};
    if (me) { me.textContent = self.name || self.label || ""; me.hidden = !me.textContent; }
  }

  // ---- 打开、关闭 ----
  function openDevices() { return window.XBotSettings.open("devices"); }
  async function mount() {
    ensureHosts();
    S.embedded = true; S.menuFor = ""; S.selfGwEdit = false; S.selfGwErr = "";
    await refresh();
    render();
  }
  function unmount() {
    S.embedded = false;
    if (["add", "join", "confirm", "waiting"].includes(S.view) && D.closePair) D.closePair();
    else { S.view = null; render(); }
  }
  function closeModal() { S.view = null; S.fieldErr = ""; render(); }

  const FOCUSABLE = 'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';
  // 配对弹窗管理自己的焦点与 Escape；设备内容由设置浮层承载。
  function trap(ev) {
    const top = modalEl && modalEl.firstChild ? "modal" : "";
    if (!top) return false;
    if (ev.key === "Escape") {
      if (top === "modal" && S.view !== "rename" && S.view !== "unpair" && S.view !== "gw-confirm") return false; // pairing.js owns its modals' Escape (it cancels codes)
      closeModal();
      return true;
    }
    if (ev.key !== "Tab") return false;
    const layer = modalEl;
    const items = Array.from(layer.querySelectorAll(FOCUSABLE));
    if (!items.length) return false;
    const i = items.indexOf(ev.target);
    ev.preventDefault();
    const next = ev.shiftKey ? (i <= 0 ? items.length - 1 : i - 1) : (i < 0 || i === items.length - 1 ? 0 : i + 1);
    items[next].focus();
    return true;
  }

  function modal(inner, cls) { return `<div class="modal${cls ? " " + cls : ""}" data-g7-modal><div class="modal-card g7-card" role="dialog" aria-modal="true">${inner}</div></div>`; }

  // 中文与西文、数字之间加空格（原型里是脚本统一加的，这里写进文案）。
  const sp = (s) => String(s).replace(/([\u4e00-\u9fff])([A-Za-z0-9])/g, "$1 $2").replace(/([A-Za-z0-9%])([\u4e00-\u9fff])/g, "$1 $2");
  // 改名弹窗里的地址举例：拉丁名字的小写，中文名字用「名字」（和原型一致）。
  const addrName = (label, name) => (/^[A-Za-z]/.test(label || "") && name && name !== "device" ? name : "名字");
  // 相对时间只用于「3 分钟前联系过」，一天以内；更早显示日期。
  function ago(iso) {
    const t = new Date(iso).getTime();
    if (!iso || isNaN(t) || new Date(iso).getFullYear() < 2000) return "";
    const m = Math.round((Date.now() - t) / 60000);
    if (m < 1) return "刚刚联系过";
    if (m < 60) return m + " 分钟前联系过";
    if (m < 24 * 60) return Math.round(m / 60) + " 小时前联系过";
    return "";
  }

  function viewRename() {
    const t = S.target || {};
    const self = t.self;
    if (self) {
      const others = ((S.data && S.data.devices) || []).filter((d) => !d.unpaired);
      const paired = others.length ? `已经配对的${others.map((d) => " " + esc(d.label)).join("、")} 上，还显示原来的名字。` : "";
      return modal(`<h2>这台电脑的名字</h2>
      <label class="field"><span>名字</span><input id="g7-name" type="text" maxlength="40" placeholder="这台电脑的名字" value="${esc(t.label)}"></label>
      <p class="g7-fine">只对以后配对的设备生效。${paired}最多 40 个字。</p>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close">取消</button><button class="primary" type="button" data-act="save-name">保存</button></div>`, "g7-over");
    }
    return modal(`<h2>重命名 ${esc(t.label)}</h2>
      <label class="field"><span>名字</span><input id="g7-name" type="text" maxlength="40" value="${esc(t.label)}"></label>
      <p class="g7-fine">只改这台电脑上的叫法，${esc(t.label)} 那边不变。Bot 的地址也不变，比如 <code class="g7-addr">${esc(addrName(t.label, t.name))}@${esc(t.name)}</code>。</p>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close">取消</button><button class="primary" type="button" data-act="save-name">保存</button></div>`, "g7-over");
  }
  function viewUnpair() {
    const t = S.target || {};
    const n = (t.bots || []).length;
    return modal(`<h2>解除和 ${esc(t.label)} 的配对？</h2>
      <p class="g7-lead">侧栏里「${esc(t.label)}」会消失，这里不能用它的 ${n} 个 Bot，两边的 Bot 也不能互发消息。</p>
      <p class="g7-lead">会尽量通知 ${esc(t.label)}，它那边会显示「已解除配对」。通知没送到，这边也照样解除。想再连，重新配对就行。</p>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close">取消</button><button class="primary" type="button" data-act="unpair">解除配对</button></div>`, "g7-over");
  }
  function viewGwConfirm() {
    const devs = ((S.data && S.data.devices) || []).filter((d) => !d.unpaired);
    const next = host(normGw(S.gwVal)) || "新网关";
    return modal(`<h2>换网关后要重新配对</h2>
      <p class="g7-lead">一台电脑只用一个网关。换成 ${esc(next)} 以后，下面这 ${devs.length} 台设备都要在新网关上重新配对，才能继续使用。</p>
      <ul class="g7-plain">${devs.map((d) => `<li>${esc(d.label)} <span class="g7-muted">· ${(d.bots || []).length} 个 Bot · ${d.online ? "在线" : "离线"}</span></li>`).join("")}</ul>
      <p class="g7-fine">保存前会先检查新地址，连不上就还用原来的网关。</p>
      <div class="modal-actions"><button class="secondary" type="button" data-act="gw-confirm-cancel">取消</button><span class="spacer"></span><button class="primary" type="button" data-act="gw-confirm-save">换网关</button></div>`, "g7-over");
  }

  // ---- 抽屉 ----
  // 「在线」是网关按对方刚发的 Bot 列表判的，最多晚十几分钟。
  const fineOnline = "「在线」指 16 分钟内收到过对方的 Bot 列表，所以状态可能晚十几分钟才变。关掉 XBot 的电脑会显示离线，发给它的消息在网关里最多等 7 天。";
  const official = "xbot.babelgo.cn";
  // 网关地址只填官方或自建，三种错误：格式不对（400）、连不上新地址（502）、没填。
  function gwCheck(v, cur) {
    if (!v) return "请填网关地址";
    const s = normGw(v), m = /^(https?:\/\/)?([^\/:?#\s]+)(:\d+)?(\/[^\s]*)?$/.exec(s);
    if (!m || (s.indexOf("://") >= 0 && !/^https?:\/\//.test(s))) return "网关地址不对：要用 http:// 或 https:// 开头，后面跟主机名。";
    const hostname = m[2].toLowerCase();
    if (!/^([a-z0-9-]+\.)*[a-z0-9-]+$/.test(hostname) || !/[a-z0-9]$/.test(hostname)) return "网关地址不对：要用 http:// 或 https:// 开头，后面跟主机名。";
    if (cur && s.replace(/^http:/, "https:") === normGw(cur).replace(/^http:/, "https:")) return "现在用的就是这个网关。";
    return "";
  }
  function gwSection(gw) {
    const v = S.gwVal;
    const cur = gw ? (host(gw) === official ? "官方 · " + official : "自建 · " + host(gw)) : "";
    if (!S.selfGwEdit) return `<div class="g7-set" id="gw-row"><span class="g7-set-ico">${svg("server", 20)}</span><div class="g7-set-main"><b>网关</b>
      <small>${gw ? sp(cur) : "还没设网关"}</small><small>消息端到端加密，网关只转交，看不到内容。</small></div>
      <button class="text-btn g7-set-act" type="button" data-act="self-gw-edit">${gw ? "改" : "设置"}</button></div>`;
    const bad = !!S.selfGwErr;
    return `<div class="g7-set g7-set-edit" id="gw-row"><div class="g7-set-head"><span class="g7-set-ico">${svg("server", 20)}</span><div class="g7-set-main"><b>网关</b>
        <small>现在用：${gw ? sp(cur) : "还没设网关"}</small></div></div>
      <label class="field${bad ? " is-bad" : ""}"><span class="g7-sr">新的网关地址</span><input id="g7-self-gw" type="text" spellcheck="false" placeholder="网关地址，例如 gw.example.com" value="${esc(v)}" ${S.checking ? "disabled" : ""}></label>
      ${S.checking ? `<p class="g7-fine g7-wait">${svg("spin")}<span>正在检查 ${esc(host(normGw(v)))}</span></p>`
        : bad ? `<p class="error g7-inline-err">${esc(S.selfGwErr)}</p>`
        : `<p class="g7-fine">填主机名就行，会自动补上 https://。自建网关的口令在启动 XBot 前用环境变量设置，这里不填。</p>`}
      <div class="g7-gw-acts"><button class="text-btn" type="button" data-act="self-gw-official">用官方网关</button><span class="spacer"></span>
        <button class="secondary g7-sm" type="button" data-act="self-gw-cancel">取消</button>
        <button class="primary g7-sm" type="button" data-act="self-gw-save" ${S.checking || !v.trim() ? "disabled" : ""}>${S.checking ? "检查中" : "保存"}</button></div></div>`;
  }
  function lanSection() {
    return `<div class="g7-set" id="lan-row"><span class="g7-set-ico">${svg("wifi", 20)}</span><div class="g7-set-main"><b>局域网直连</b>
      <small>两台电脑在同一个网络里时直接传消息，更快；连不上就自动走网关。打开后这台电脑会多开一个端口，只接受已配对的设备。公司网络可能不允许。</small></div>
      <span class="g7-switch" role="switch" aria-checked="false" aria-disabled="true" aria-label="局域网直连"></span></div>`;
  }

  // 和侧栏同一个头像：灰阶圆 + 首字，拉丁名两个字母，中文单字（导出的 D.sideFaces，所以下面不改侧栏的 face）。
  const faceX = (n, id) => (window.XBotFaces ? D.sideFaces(n, id, "xs") : `<span class="avatar xs">${D.sideFaces(n, id)}</span>`);
  // 在线时写多久前联系过（「3 分钟前联系过」）；隔天或对方一整天都显得在线时，写日期。
  const statusLine = (d, up) => {
    if (d.unpaired) return `<span class="g7-status">${svg("unlink", 13)}已解除配对 · 对方解除的</span>`;
    const seen = fmtSeen(d.seen);
    if (!seen) return `<span class="g7-status"><i class="status-dot off-ring"></i>还没收到它的 Bot 列表 · 几秒后会更新</span>`;
    if (!up) return `<span class="g7-status"><i class="status-dot off-ring"></i>连不上网关 · 最后联系 ${esc(seen)}</span>`;
    if (d.online) return `<span class="g7-status"><i class="status-dot ok"></i>在线${ago(d.seen) ? " · " + ago(d.seen) : " · " + esc(seen) + "联系过"}</span>`;
    return `<span class="g7-status"><i class="status-dot off-ring"></i>离线 · 最后联系 ${esc(seen)}</span>`;
  };
  function capability(d) {
    const on = d.bot_messaging !== false, err = S.bmErrMsg && S.bmErr === d.id ? S.bmErrMsg : ""; // 关掉时行也变淡，错误照常显示
    return `<div class="g7-caps${on ? "" : " is-off"}"><div class="g7-cap"><span class="g7-cap-t"><b>Bot 互发消息</b></span>
      <button class="g7-switch" id="g7-bm-${esc(d.id)}" type="button" role="switch" aria-checked="${on}" aria-label="Bot 互发消息" data-act="bm-toggle" data-id="${esc(d.id)}"></button></div>
      ${err ? `<p class="error g7-inline-err">${esc(err)}</p>` : ""}</div>`;
  }
  function devCard(d, up) {
    const open = S.menuFor === d.id;
    if (d.unpaired) {
      // 对方解除了：不显示 Bot，也不显示能力；「移除」直接删本机记录。
      return `<div class="g7-dev-card is-gone">
        <div class="g7-device g7-flat"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
          <div class="g7-dev-main"><div class="g7-dev-name">${esc(d.label)}</div>${statusLine(d, up)}</div>
          <button class="secondary g7-sm" type="button" data-act="remove" data-id="${esc(d.id)}">${svg("x", 14)}<span>移除</span></button></div>
        <div class="g7-meta">${esc(d.label)} 那边已经解除配对，这里不能再用它的 Bot，两边的 Bot 也不能互发消息。移除后，想再连就重新配对。</div></div>`;
    }
    const menu = open ? `<div class="pop g7-row-menu">
        <button type="button" class="menu-row" data-act="rename" data-id="${esc(d.id)}">${svg("pencil")}<span>重命名</span></button>
        <button type="button" class="menu-row" data-act="ask-unpair" data-id="${esc(d.id)}">${svg("unlink")}<span>解除配对</span></button></div>` : "";
    const bots = (d.bots || []).length ? `<div class="g7-bots">${d.bots.map((b) => `<span class="g7-bot">${faceX(b, d.id)}<span>${esc(b)}</span></span>`).join("")}</div>` : "";
    const added = fmtSeen(d.added); // 今天 09:12 / 昨天 18:20 / 10月12日
    const date = !added ? "" : /^今天/.test(added) ? "今天" : added;
    const meta = `<div class="g7-meta"><span>Bot 地址</span><code class="g7-addr">名字@${esc(d.name)}</code>${date ? `<span>·</span><span>${esc(date)}添加</span>` : ""}</div>`;
    const caps = (d.bots || []).length ? capability(d) : ""; // 还没收到 Bot 列表时不给开关
    return `<div class="g7-dev-card${up && !d.online ? " is-off" : ""}">
      <div class="g7-device g7-flat"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
        <div class="g7-dev-main"><div class="g7-dev-name">${esc(d.label)}</div>${statusLine(d, up)}</div>
        <div class="g7-more-wrap"><button class="ghost${open ? " is-on" : ""}" type="button" aria-label="更多" aria-expanded="${open}" data-act="row-menu" data-id="${esc(d.id)}">${svg("ellipsis")}</button>${menu}</div></div>
      ${bots}<div class="g7-details">${meta}${caps}</div></div>`;
  }
  function viewDevices() {
    const data = S.data || { self: {}, devices: [] };
    const self = data.self || {}, devs = data.devices || [], gw = self.gateway, up = !!data.gateway_ok;
    const selfCard = `<section class="profile-section"><h3>这台电脑</h3>
      <div class="g7-device g7-self"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
        <div class="g7-dev-main"><div class="g7-dev-name">${esc(self.name)}</div><div class="g7-dev-sub">${esc([self.os, (self.bots || []).length + " 个 Bot"].filter(Boolean).join(" · "))}</div></div>
        <button class="ghost" type="button" aria-label="改名" data-act="rename-self">${svg("pencil")}</button></div></section>`;
    const banner = gw && !up ? `<div class="g7-banner"><span class="g7-banner-ico">${svg("cloudOff", 18)}</span>
        <span>${sp("连不上网关" + host(gw))}<small>所有设备暂时显示离线，消息会先排队。</small></span>
        <button class="secondary g7-sm" type="button" data-act="gw-retry">${svg("refresh", 14)}<span>重试</span></button></div>` : "";
    let list;
    if (!devs.length) {
      list = `<section class="profile-section"><div class="g7-empty"><svg class="doodle" width="220" height="110" viewBox="0 0 220 110" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><use href="#doodle-devices"/></svg><h3>还没有配对的设备</h3>
          <p>把你的另一台电脑配对进来，它上面的 Bot 会出现在侧栏里，点开就能聊天。</p></div>
        <ul class="g7-can"><li>${svg("bot", 16)}<span>在这台电脑上和另一台电脑的 Bot 聊天，和本机的一样。</span></li>
          <li>${svg("swap", 16)}<span>两边的 Bot 能互发消息。</span></li>
          <li class="no">${svg("shield", 16)}<span>记忆和文件留在各自的电脑上。</span></li></ul>
        </section>`;
    } else {
      list = `<section class="profile-section"><h3>已配对 · ${devs.length}</h3>${devs.map((d) => devCard(d, up)).join("")}
          <p class="g7-fine">${sp(fineOnline)}</p></section>`;
    }
    return `<div class="sethd"><h2>设备</h2><button class="primary" type="button" data-act="add">${svg("plus", 14)}<span>配对设备</span></button></div>
      <div class="g7-lead">${sp("配对后，在这台电脑上就能和另一台电脑上的 Bot 聊天，两边的 Bot 也能互发消息。记忆和文件留在各自的电脑上。")}</div>${banner}
      <div class="g7-body">${selfCard}${list}
        <section class="profile-section"><h3>连接</h3>${lanSection()}${gwSection(gw)}</section></div>`;
  }

  function render() {
    ensureHosts();
    const focusId = document.activeElement && document.activeElement.id;
    const sel = document.activeElement && "selectionStart" in document.activeElement ? document.activeElement.selectionStart : null;
    const selEnd = sel != null ? document.activeElement.selectionEnd : null;
    const selDir = sel != null ? document.activeElement.selectionDirection : null;
    const wasView = lastView;
    devicesEl.innerHTML = S.embedded ? viewDevices() : "";
    const V = Object.assign({ rename: viewRename, unpair: viewUnpair, "gw-confirm": viewGwConfirm }, D.views)[S.view];
    modalEl.innerHTML = V ? V() : "";
    if (V && lastView === S.view) { const card = modalEl.querySelector(".modal-card"); if (card) card.style.animation = "none"; }
    lastView = S.view || "";
    const f = focusId && document.getElementById(focusId) || (S.menuFor && devicesEl.querySelector(`[data-act="row-menu"][data-id="${CSS.escape(S.menuFor)}"]`));
    const inside = document.activeElement === document.body || (devicesEl.contains(document.activeElement) || modalEl.contains(document.activeElement));
    if (f && !f.disabled) { f.focus(); if (sel != null && f.setSelectionRange) try { f.setSelectionRange(sel, selEnd, selDir); } catch (e) {} }
    else if (V && (S.view !== wasView || !inside)) { const first = modalEl.querySelector("input:not([disabled])") || modalEl.querySelector(FOCUSABLE); if (first) first.focus(); }
  }

  // ---- 事件 ----
  async function act(name, el) {
    const data = S.data || {};
    switch (name) {
      case "close": closeModal(); break;
      case "to-drawer": if (D.closePair) await D.closePair(); else closeModal(); await openDevices(); break;
      case "row-menu": S.menuFor = S.menuFor === el.dataset.id ? "" : el.dataset.id; render(); break;
      case "rename": case "ask-unpair": {
        const d = (data.devices || []).find((x) => x.id === el.dataset.id);
        if (!d) return;
        S.menuFor = ""; S.target = d; S.fieldErr = ""; S.view = name === "rename" ? "rename" : "unpair"; render(); break;
      }
      case "rename-self": S.target = { self: true, label: data.self && data.self.name, os: data.self && data.self.os }; S.fieldErr = ""; S.view = "rename"; render(); break;
      case "save-name": {
        const v = (document.getElementById("g7-name").value || "").trim();
        if (!v) { S.fieldErr = "名字不能为空。"; render(); return; } // 空名字服务端也拒：本地先说
        try {
          if (S.target.self) await api("PATCH", "/api/devices/self", { name: v });
          else await api("PATCH", "/api/devices/" + encodeURIComponent(S.target.id), { label: v });
        } catch (e) { S.fieldErr = e.message; render(); return; }
        S.view = null; await refresh(); render(); break;
      }
      case "unpair":
        try { await api("DELETE", "/api/devices/" + encodeURIComponent(S.target.id)); }
        catch (e) { S.fieldErr = e.message; render(); return; }
        S.view = null; await refresh(); render(); break; // 返回只有 ok，不知道通知送到没有：不弹 crop 21 的提示
      case "remove":
        try { await api("DELETE", "/api/devices/" + encodeURIComponent(el.dataset.id)); }
        catch (e) { S.bmErr = el.dataset.id; S.bmErrMsg = "没能移除：" + e.message; render(); return; }
        await refresh(); render(); break;
      case "bm-toggle": {
        // 后端在另一个 PR 里加 bot_messaging；没这个字段时按开着算。
        const d = (data.devices || []).find((x) => x.id === el.dataset.id);
        if (!d) return;
        const on = d.bot_messaging === false, old = d.bot_messaging; S.bmErrMsg = ""; // undefined 当开着
        d.bot_messaging = on; S.bmErr = ""; render();
        try { await api("PATCH", "/api/devices/" + encodeURIComponent(d.id), { bot_messaging: on }); }
        catch (e) { d.bot_messaging = old; S.bmErr = d.id; S.bmErrMsg = "没改成功：" + e.message + "。开关已经改回去了。"; }
        render(); break;
      }
      case "self-gw-edit": S.selfGwEdit = true; S.selfGwErr = ""; S.gwVal = (data.self && data.self.gateway) || ""; render(); { const g = document.getElementById("g7-self-gw"); if (g) g.focus(); } break;
      case "self-gw-cancel": S.selfGwEdit = false; S.selfGwErr = ""; render(); break;
      case "self-gw-official": S.gwVal = official; return act("self-gw-save", el);
      case "self-gw-save": {
        const bad = gwCheck(S.gwVal, data.self && data.self.gateway);
        if (bad) { S.selfGwErr = bad; render(); return; }
        // Paired devices would need re-pairing: ask first unless the address is unchanged.
        const gwNow = normGw(data.self && data.self.gateway), gwNew = normGw(S.gwVal);
        if ((data.devices || []).filter((d) => !d.unpaired).length && gwNew !== gwNow) { S.selfGwErr = ""; S.view = "gw-confirm"; render(); break; }
        return act("gw-confirm-save", el);
      }
      case "gw-confirm-cancel": S.view = null; render(); break; // back to the drawer, editor open with the typed value
      case "gw-confirm-save":
        S.view = null; S.selfGwEdit = true; S.selfGwErr = ""; S.checking = true; render(); // 服务端保存前先检查新地址（502 连不上时保留旧地址）
        try { await api("PUT", "/api/devices/gateway", { gateway: S.gwVal }); S.selfGwEdit = false; await refresh(); }
        catch (e) { S.selfGwErr = e.message; }
        S.checking = false; render(); break;
      case "gw-retry":
        try { S.data = await api("GET", "/api/devices?retry=1"); } catch (e) {}
        paintSidebar(); render(); break;
      default: if (D.acts[name]) await D.acts[name](el);
    }
  }

  document.addEventListener("click", (ev) => {
    const t = ev.target.closest("[data-act]");
    if (t && (devicesEl && devicesEl.contains(t) || modalEl && modalEl.contains(t))) { ev.preventDefault(); act(t.dataset.act, t); return; }
    if (S.menuFor && devicesEl && devicesEl.contains(ev.target) && !ev.target.closest(".g7-row-menu")) { S.menuFor = ""; render(); }
  });
  document.addEventListener("input", (ev) => {
    if (ev.target.id === "g7-self-gw") {
      S.gwVal = ev.target.value;
      const save = devicesEl.querySelector('[data-act="self-gw-save"]');
      if (save && !S.checking) save.disabled = !S.gwVal.trim();
    } else if (D.onInput) D.onInput(ev.target);
  });
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && ev.target.id === "g7-self-gw") { ev.preventDefault(); act("self-gw-save", ev.target); }
    if (ev.key === "Enter" && ev.target.id === "g7-name") { ev.preventDefault(); act("save-name", ev.target); }
    if (D.onKey && D.onKey(ev)) return;
    if (devicesEl && (devicesEl.firstChild || modalEl.firstChild)) trap(ev);
  });

  // 倒计时只改文字，不重画
  setInterval(() => {
    if (!modalEl || !modalEl.firstChild) return;
    modalEl.querySelectorAll("[data-left]").forEach((el) => { el.textContent = left(el.dataset.left, Number(el.dataset.minus || 0)); });
  }, 1000);

  // ---- 侧栏：其他电脑上的 Bot，按设备分组（v2 原型 side-full、side-gw-down、rail-devices）----
  // 组头（label）也是一行按钮：不做折叠，所以设备和 Bot 行都放进 #roster（本机行下面），app.js 重画本机行时不会冲掉；
  // 网关不通时只在分组上方放一条提示、整体变淡，不给每台标离线；离线的设备变淡，但行照样能点开远端对话。
  const initials = (n) => { const c = Array.from(String(n || "?")); return /[A-Za-z]/.test(c[0]) ? (c[0].toUpperCase() + (c[1] || "").toLowerCase()) : c[0]; };
  // 远端 Bot 的人像按那台设备给的 bot_avatars 画：上传过图片的画几何图形（图片不过网关），旧版本的设备没给 seed 就按名字。
  const seedOf = (n, id) => {
    const d = ((S.data || {}).devices || []).find((x) => x.id === id);
    const a = (d && d.bot_avatars && d.bot_avatars[n]) || {};
    return !a.custom && a.avatar_seed ? a.avatar_seed : "glyph"; // 没给 seed 或上传过图片：几何图形，不借别的 Bot 的脸
  };
  const face = (n, id, size) => (window.XBotFaces ? window.XBotFaces.html(n, seedOf(n, id), size) : `<span class="avatar"><svg viewBox="0 0 36 36" aria-hidden="true" style="--avatar-light:#d3d3d3;--avatar-dark:#3d3d3d;--avatar-letter-light:#333333;--avatar-letter-dark:#e4e4e4"><circle cx="18" cy="18" r="18" fill="var(--avatar-fill)"/><text x="18" y="23" text-anchor="middle" font-size="${initials(n).length > 1 ? 13 : 15}" fill="var(--avatar-ink)">${esc(initials(n))}</text></svg></span>`);
  // 没有 Bot 列表的刚配对设备也列出来（「等它的 Bot 列表」）；对方解除配对的隐藏。点组头折叠，和本机分组一样。
  const sideFolds = () => { try { return JSON.parse(localStorage.getItem("xbot-device-folders") || "{}") || {}; } catch (e) { return {}; } };
  let sideSig = "";
  function paintSidebar() {
    const nav = document.getElementById("remote-roster");
    if (!nav) return;
    const data = S.data || {};
    const devs = (data.devices || []).filter((d) => !d.unpaired);
    const up = !!data.gateway_ok, folds = sideFolds(), sel = D.chat && D.chat.isOpen() ? D.chat.key() : "";
    const sig = JSON.stringify([devs, up, folds, sel]);
    if (D.onPaint) D.onPaint(); // 打开着的远端对话跟着新的在线状态
    if (sig === sideSig) return;
    sideSig = sig;
    const had = document.activeElement && nav.contains(document.activeElement) ? document.activeElement.closest("[data-side]") : null;
    const key = had ? `[data-side="${had.dataset.side}"]` + (had.dataset.id ? `[data-id="${CSS.escape(had.dataset.id)}"]` : "") + (had.dataset.bot ? `[data-bot="${CSS.escape(had.dataset.bot)}"]` : "") : "";
    const status = (d) => {
      if (!(d.bots || []).length) return `<span class="g7-side-st">等它的 Bot 列表</span>`;
      if (!up) return ""; // 网关不通：上面一条提示，不逐台标离线
      if (d.online) return `<span class="g7-side-st"><i class="status-dot ok"></i>在线</span>`;
      const seen = fmtSeen(d.seen) || "很久以前";
      return `<span class="g7-side-st" title="离线 · 最后联系 ${esc(seen)}"><i class="status-dot off-ring"></i>${esc(seen)}</span>`;
    };
    const row = (d, b) => {
      const c = (d.chats || {})[b] || {};
      const body = String(c.last || "").trim(), me = c.last_role === "me" ? "你：" : "";
      const pv = c.queued ? `<span class="preview g7-side-q"><b class="g7-side-qb">◷ 排队中</b> · 你：${esc(c.queued)}</span>`
        : `<span class="preview">${body ? esc(me + body) : "还没有消息"}</span>`;
      return `<button class="item g7-remote-bot${sel === d.id + "\u0000" + b ? " active" : ""}" type="button" data-side="open" data-id="${esc(d.id)}" data-bot="${esc(b)}">
        ${c.unread > 0 ? face(b, d.id).replace(/<\/span>$/, '<i class="unread"></i></span>') : face(b, d.id)}<span class="item-body"><span class="name-line"><span class="name">${esc(b)}</span></span>${pv}</span></button>`;
    };
    nav.hidden = !devs.length;
    nav.innerHTML = !devs.length ? "" : (up ? "" : `<div class="g7-side-down"><span>连不上网关，其他电脑的 Bot 暂时用不了。</span><button class="ghost" type="button" aria-label="重试" data-side="retry">${svg("refresh")}</button></div>`)
      + devs.map((d) => {
        const bots = d.bots || [], closed = !!folds[d.id];
        const rows = bots.length ? bots.map((b) => row(d, b)).join("") : `<p class="g7-side-line">刚配对，几秒后会列出它的 Bot。</p>`;
        return `<div class="g7-side-dev${!up || (bots.length && !d.online) ? " is-off" : ""}"><button class="g7-side-head" type="button" data-side="fold" data-id="${esc(d.id)}" aria-expanded="${closed ? "false" : "true"}">
          <span class="g7-side-name">${svg("laptop", 13)}<span>${esc(d.label)}</span></span>${status(d)}</button><div class="g7-side-bots"${closed ? " hidden" : ""}>${rows}</div></div>`;
      }).join("");
    // 重画会换掉按钮，键盘焦点留在原来那一个（重试、组头或 Bot 行）。
    const back = key && nav.querySelector(key);
    if (back) back.focus();
  }
  // 远端对话打开或关闭后，侧栏的选中行要跟着变。
  D.repaint = () => { sideSig = ""; paintSidebar(); };
  document.addEventListener("click", async (ev) => {
    const t = ev.target.closest && ev.target.closest("[data-side]");
    if (!t) return;
    const side = t.dataset.side;
    if (side === "open") { if (D.chat) D.chat.open(t.dataset.id, t.dataset.bot); return; }
    if (side === "fold") {
      const f = sideFolds(); f[t.dataset.id] = !f[t.dataset.id];
      try { localStorage.setItem("xbot-device-folders", JSON.stringify(f)); } catch (e) {}
    } else {
      try { S.data = await api("GET", "/api/devices?retry=1"); } catch (e) {}
    }
    paintSidebar();
  });
  // 远端对话（remotechat.js，在其后加载）不是三个状态槽之一，单独挂，别被下面那两行清掉。
  Object.defineProperty(D, "chat", { get: () => window.xbotRemoteChat || null, set: () => false });
  setInterval(() => { if (!document.hidden) refresh(); }, 30000);

  Object.assign(D, { S, api, svg, esc, host, left, fmtSeen, modal, render, refresh, open: openDevices, mount, unmount, closeModal, sideFaces: face });
  D.views = {}; D.acts = {}; D.onInput = null; D.onKey = null; // pairing.js 接着往这三个上面挂
  refresh();
})();
