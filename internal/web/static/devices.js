/* G7 设备：账户菜单第一项「设备」打开右侧抽屉，列出这台电脑和配对的设备。
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
    pencil: '<path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/>',
    unlink: '<path d="m18.84 12.25 1.72-1.71a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="m5.17 11.75-1.71 1.71a5 5 0 0 0 7.07 7.07l1.71-1.71"/><path d="M8 2v3"/><path d="M2 8h3"/><path d="M16 22v-3"/><path d="M22 16h-3"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    keyboard: '<rect width="20" height="14" x="2" y="5" rx="2"/><path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M6 13h.01M18 13h.01M9 15.5h6"/>',
    spin: '<circle cx="12" cy="12" r="8.5" opacity=".22"/><path d="M12 3.5a8.5 8.5 0 0 1 8.5 8.5"/>',
    bot: '<rect x="4" y="8" width="16" height="12" rx="3"/><path d="M12 4v4"/><path d="M9 13h.01M15 13h.01"/>',
    swap: '<path d="M16 3l4 4-4 4"/><path d="M20 7H4"/><path d="M8 21l-4-4 4-4"/><path d="M4 17h16"/>',
    shield: '<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/>',
    server: '<rect width="20" height="8" x="2" y="2" rx="2"/><rect width="20" height="8" x="2" y="14" rx="2"/><path d="M6 6h.01M6 18h.01"/>',
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
    let data = null;
    try { data = await res.json(); } catch (e) { data = null; }
    if (!res.ok) { const err = new Error((data && data.error) || "请求失败"); err.status = res.status; throw err; }
    return data;
  }
  // 登录过期：收起设备抽屉和弹窗、清掉状态和挂到 body 上的层，重新加载让 app.js 走登录流程
  let authed = false; // Before login (the login page) a 401 is normal: never reload then.
  function sessionExpired() {
    if (!authed) { const err = new Error("未登录"); err.status = 401; throw err; }
    S.drawer = false; S.view = null; S.menuFor = ""; S.target = null; S.fieldErr = ""; S.busy = false;
    if (drawerEl) { try { drawerEl.remove(); } catch (e) { drawerEl.innerHTML = ""; } }
    if (modalEl) { try { modalEl.remove(); } catch (e) { modalEl.innerHTML = ""; } }
    drawerEl = modalEl = null;
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
    drawer: false,
    code: "",              // A 最近一次显示的码，用于确认框副行
    peer: null,            // B 端最近看到的对方 {name, os, bots}
    joinVal: "", gwOpen: false, gwVal: "", fieldErr: "",
    menuFor: "", target: null, selfGwEdit: false, selfGwErr: "", busy: false,
  };
  let drawerEl = null, modalEl = null, lastView = "", opener = null;

  function ensureHosts() {
    if (drawerEl) return;
    drawerEl = document.createElement("div"); drawerEl.className = "g7-layer"; drawerEl.id = "devices-drawer";
    modalEl = document.createElement("div"); modalEl.className = "g7-layer"; modalEl.id = "devices-modal";
    document.body.append(drawerEl, modalEl);
  }

  async function refresh() {
    try { S.data = await api("GET", "/api/devices"); S.pairing = S.data.pairing; } catch (e) { /* 离线时保留旧数据 */ }
    paintCount();
    paintSidebar();
  }
  function paintCount() {
    const el = document.getElementById("account-devices-count");
    if (!el) return;
    const n = S.data && S.data.devices ? S.data.devices.length : 0;
    el.textContent = n ? String(n) : "";
  }

  // ---- 打开、关闭 ----
  async function openDrawer() {
    ensureHosts();
    if (!S.drawer) opener = document.activeElement;
    S.drawer = true; S.menuFor = ""; S.selfGwEdit = false; S.selfGwErr = "";
    await refresh();
    render();
  }
  function closeDrawer() { S.drawer = false; S.view = null; render(); }
  function closeModal() { S.view = null; S.fieldErr = ""; render(); }

  const FOCUSABLE = 'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';
  // 顶层设备层 = 有弹窗时弹窗，否则抽屉；Tab/Shift+Tab 在层内循环，Escape 关掉这一层
  function trap(ev) {
    const top = modalEl && modalEl.firstChild ? "modal" : drawerEl && drawerEl.firstChild ? "drawer" : "";
    if (!top) return false;
    if (ev.key === "Escape") {
      if (top === "modal" && S.view !== "rename" && S.view !== "unpair" && S.view !== "gw-confirm") return false; // pairing.js owns its modals' Escape (it cancels codes)
      top === "modal" ? closeModal() : closeDrawer();
      return true;
    }
    if (ev.key !== "Tab") return false;
    const layer = top === "modal" ? modalEl : drawerEl;
    const items = Array.from(layer.querySelectorAll(FOCUSABLE));
    if (!items.length) return false;
    const i = items.indexOf(ev.target);
    ev.preventDefault();
    const next = ev.shiftKey ? (i <= 0 ? items.length - 1 : i - 1) : (i < 0 || i === items.length - 1 ? 0 : i + 1);
    items[next].focus();
    return true;
  }

  function modal(inner, cls) { return `<div class="modal${cls ? " " + cls : ""}" data-g7-modal><div class="modal-card g7-card" role="dialog" aria-modal="true">${inner}</div></div>`; }

  function viewRename() {
    const t = S.target || {};
    const self = t.self;
    return modal(`<h2>${self ? "重命名这台电脑" : "重命名设备"}</h2>
      <label class="field"><span>名字</span><input id="g7-name" type="text" maxlength="40" value="${esc(t.label)}"></label>
      <p class="g7-fine">${self ? "新配对的设备上会显示这个名字。" : `只改这台电脑上显示的名字，${esc(t.label)} 那边不变。`}</p>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close">取消</button><button class="primary" type="button" data-act="save-name">保存</button></div>`, "g7-over");
  }
  function viewUnpair() {
    const t = S.target || {};
    return modal(`<h2>解除和 ${esc(t.label)} 的配对？</h2>
      <p class="g7-lead">左侧列表里不再显示 ${esc(t.label)} 的 ${(t.bots || []).length} 个 Bot，${esc(t.label)} 也用不了这台电脑上的 Bot。想再连，重新配对就行。</p>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close">取消</button><button class="primary" type="button" data-act="unpair">解除配对</button></div>`, "g7-over");
  }
  function viewGwConfirm() {
    const devs = (S.data && S.data.devices) || [];
    return modal(`<h2>改网关地址？</h2>
      <p class="g7-lead">这些设备连在原来的网关上，改了以后要在两台电脑上重新配对：</p>
      <ul class="g7-plain">${devs.map((d) => `<li>${esc(d.label)}</li>`).join("")}</ul>
      <p class="g7-fine">配对不会被删除，改回原来的地址就能继续用。</p>
      <div class="modal-actions"><button class="secondary" type="button" data-act="gw-confirm-cancel">取消</button><span class="spacer"></span><button class="primary" type="button" data-act="gw-confirm-save">改网关地址</button></div>`, "g7-over");
  }

  function devRow(d, up) {
    // 对方解除了配对：只留一个灰色标记和「解除配对」，不再显示在线状态。
    const status = d.unpaired
      ? `<span class="g7-status g7-unpaired">已解除配对</span>`
      : !up ? "" : d.online
      ? `<span class="g7-status"><i class="status-dot ok"></i>在线</span>`
      : `<span class="g7-status"><i class="status-dot off-ring"></i>离线${fmtSeen(d.seen) ? " · 最后在线 " + esc(fmtSeen(d.seen)) : ""}</span>`;
    const bots = d.unpaired ? "" : d.bots.map((b) => `<span class="g7-bot"><span>${esc(b)}</span></span>`).join("");
    const open = S.menuFor === d.id;
    const rename = d.unpaired ? "" : `<button type="button" class="menu-row" data-act="rename" data-id="${esc(d.id)}">${svg("pencil")}<span>重命名</span></button>`;
    const menu = open ? `<div class="pop g7-row-menu">${rename}
        <button type="button" class="menu-row" data-act="ask-unpair" data-id="${esc(d.id)}">${svg("unlink")}<span>解除配对</span></button></div>` : "";
    return `<div class="g7-dev-card${!d.unpaired && up && !d.online ? " is-off" : ""}">
      <div class="g7-device g7-flat"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
        <div class="g7-dev-main"><div class="g7-dev-name">${esc(d.label)}</div>${status}</div>
        <div class="g7-more-wrap"><button class="ghost${open ? " is-on" : ""}" type="button" aria-label="更多" data-act="row-menu" data-id="${esc(d.id)}">${svg("ellipsis")}</button>${menu}</div></div>
      ${bots ? `<div class="g7-bots">${bots}</div>` : ""}</div>`;
  }
  function viewDrawer() {
    const data = S.data || { self: {}, devices: [] };
    const self = data.self || {}, devs = data.devices || [];
    const gw = self.gateway;
    const up = !!data.gateway_ok;
    const gwLine = S.selfGwEdit
      ? `<div class="g7-gw-edit-row"><input id="g7-self-gw" type="url" placeholder="https://" spellcheck="false" value="${esc(S.gwVal)}">
          <button class="secondary g7-sm" type="button" data-act="self-gw-cancel">取消</button><button class="primary g7-sm" type="button" data-act="self-gw-save" ${S.busy ? "disabled" : ""}>保存</button></div>
         ${S.selfGwErr ? `<p class="error g7-inline-err">${esc(S.selfGwErr)}</p>` : ""}`
      : `<div class="g7-gw g7-self-gw">${svg("server", 14)}<span>${gw ? "网关 " + esc(host(gw)) : "还没设网关"}</span><button class="text-btn" type="button" data-act="self-gw-edit">${gw ? "改" : "设置"}</button></div>`;
    const pair = !!D.acts.add; // pairing.js adds 添加设备 and 加入另一台设备
    const body = devs.length
      ? `<section class="profile-section"><h3>已配对 · ${devs.length}</h3>
          ${gw && !up ? `<div class="g7-gw-down"><span>连不上网关，其他电脑的 Bot 暂时用不了。</span><span class="spacer"></span><button class="ghost" type="button" aria-label="重试" data-act="gw-retry">${svg("refresh")}</button></div>` : ""}
          ${devs.map((d) => devRow(d, up)).join("")}</section>`
      : `<div class="g7-empty"><span class="g7-empty-ico">${svg("devices", 28)}</span><h3>还没有配对的设备</h3><p>配对后，两台电脑能互相使用对方的 Bot。</p></div>
         ${pair ? `<div class="g7-options">
          <button class="g7-option" type="button" data-act="add"><span class="g7-opt-ico">${svg("plus", 18)}</span><span><b>添加设备</b><small>在这台电脑上生成配对码，到另一台电脑上输入</small></span></button>
          <button class="g7-option" type="button" data-act="join-open"><span class="g7-opt-ico">${svg("keyboard", 18)}</span><span><b>加入另一台设备</b><small>输入另一台电脑上显示的配对码</small></span></button></div>` : ""}`;
    const foot = devs.length && pair ? `<div class="modal-actions g7-drawer-foot"><button class="secondary" type="button" data-act="join-open">${svg("keyboard")}<span>加入另一台设备</span></button><button class="primary" type="button" data-act="add">${svg("plus")}<span>添加设备</span></button></div>` : "";
    return `<div class="modal profile-drawer g7-drawer" data-g7-drawer><div class="modal-card profile-modal" role="dialog" aria-modal="true" aria-label="设备">
      <div class="g7-drawer-head"><h2>设备</h2><button class="ghost" type="button" aria-label="关闭" data-act="close-drawer">${svg("x")}</button></div>
      <div class="g7-drawer-body">
        <section class="profile-section"><h3>这台电脑</h3>
          <div class="g7-device g7-self"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
            <div class="g7-dev-main"><div class="g7-dev-name">${esc(self.name)}</div><div class="g7-dev-sub">${esc(self.os || "")} · ${(self.bots || []).length} 个 Bot · 配对的设备上显示这个名字</div></div>
            <button class="ghost" type="button" aria-label="重命名" data-act="rename-self">${svg("pencil")}</button></div>
          ${gwLine}
        </section>${body}
      </div>${foot}</div></div>`;
  }

  function render() {
    ensureHosts();
    const focusId = document.activeElement && document.activeElement.id;
    const sel = document.activeElement && "selectionStart" in document.activeElement ? document.activeElement.selectionStart : null;
    const wasDrawer = !!drawerEl.firstChild, wasView = lastView;
    drawerEl.innerHTML = S.drawer ? viewDrawer() : "";
    if (wasDrawer && S.drawer) drawerEl.querySelector(".profile-modal").style.animation = "none";
    const V = Object.assign({ rename: viewRename, unpair: viewUnpair, "gw-confirm": viewGwConfirm }, D.views)[S.view];
    modalEl.innerHTML = V ? V() : "";
    if (V && lastView === S.view) { const card = modalEl.querySelector(".modal-card"); if (card) card.style.animation = "none"; }
    lastView = S.view || "";
    const f = focusId && document.getElementById(focusId);
    const inside = document.activeElement === document.body || (drawerEl.contains(document.activeElement) || modalEl.contains(document.activeElement));
    if (f && !f.disabled) { f.focus(); if (sel != null && f.setSelectionRange) try { f.setSelectionRange(f.value.length, f.value.length); } catch (e) {} }
    else if (V && (S.view !== wasView || !inside)) { const first = modalEl.querySelector("input:not([disabled])") || modalEl.querySelector(FOCUSABLE); if (first) first.focus(); }
    else if (!inside) { const first = (V ? modalEl : drawerEl).querySelector(V ? ".modal-card button" : ".profile-modal button"); if (first) first.focus(); }
    if (!S.drawer && opener && opener.focus && document.contains(opener)) opener.focus();
    if (!S.drawer) opener = null;
  }

  // ---- 事件 ----
  async function act(name, el) {
    const data = S.data || {};
    switch (name) {
      case "close-drawer": closeDrawer(); break;
      case "close": closeModal(); break;
      case "to-drawer": S.view = null; await openDrawer(); break;
      case "row-menu": S.menuFor = S.menuFor === el.dataset.id ? "" : el.dataset.id; render(); break;
      case "rename": case "ask-unpair": {
        const d = (data.devices || []).find((x) => x.id === el.dataset.id);
        if (!d) return;
        S.menuFor = ""; S.target = d; S.fieldErr = ""; S.view = name === "rename" ? "rename" : "unpair"; render(); break;
      }
      case "rename-self": S.target = { self: true, label: data.self && data.self.name }; S.fieldErr = ""; S.view = "rename"; render(); break;
      case "save-name": {
        const v = (document.getElementById("g7-name").value || "").trim();
        if (!v) return;
        try {
          if (S.target.self) await api("PATCH", "/api/devices/self", { name: v });
          else await api("PATCH", "/api/devices/" + encodeURIComponent(S.target.id), { label: v });
        } catch (e) { S.fieldErr = e.message; render(); return; }
        S.view = null; await refresh(); render(); break;
      }
      case "unpair":
        try { await api("DELETE", "/api/devices/" + encodeURIComponent(S.target.id)); }
        catch (e) { S.fieldErr = e.message; render(); return; }
        S.view = null; await refresh(); render(); break;
      case "self-gw-edit": S.selfGwEdit = true; S.selfGwErr = ""; S.gwVal = (data.self && data.self.gateway) || ""; render(); { const g = document.getElementById("g7-self-gw"); if (g) g.focus(); } break;
      case "self-gw-cancel": S.selfGwEdit = false; S.selfGwErr = ""; render(); break;
      case "self-gw-save": {
        // Paired devices would need re-pairing: ask first unless the address is unchanged.
        const gwNow = normGw(data.self && data.self.gateway), gwNew = normGw(S.gwVal);
        if ((data.devices || []).length && gwNew && gwNew !== gwNow) { S.fieldErr = ""; S.view = "gw-confirm"; render(); break; }
        return act("gw-confirm-save", el);
      }
      case "gw-confirm-cancel": S.view = null; render(); break; // back to the drawer, editor open with the typed value
      case "gw-confirm-save":
        S.view = null; S.busy = true; render();
        try { await api("PUT", "/api/devices/gateway", { gateway: S.gwVal }); S.selfGwEdit = false; S.selfGwErr = ""; await refresh(); }
        catch (e) { S.selfGwErr = e.message; }
        S.busy = false; render(); break;
      case "gw-retry":
        try { S.data = await api("GET", "/api/devices?retry=1"); } catch (e) {}
        paintSidebar(); render(); break;
      default: if (D.acts[name]) await D.acts[name](el);
    }
  }

  document.addEventListener("click", (ev) => {
    const t = ev.target.closest("[data-act]");
    if (t && (drawerEl && drawerEl.contains(t) || modalEl && modalEl.contains(t))) { ev.preventDefault(); act(t.dataset.act, t); return; }
    if (S.menuFor && drawerEl && drawerEl.contains(ev.target) && !ev.target.closest(".g7-row-menu")) { S.menuFor = ""; render(); }
    // 点遮罩关闭抽屉（弹窗不随手关，免得丢掉正在进行的配对）
    if (drawerEl && ev.target.matches && ev.target.matches("[data-g7-drawer]") && !S.view) closeDrawer();
  });
  document.addEventListener("input", (ev) => {
    if (ev.target.id === "g7-self-gw") S.gwVal = ev.target.value;
    else if (D.onInput) D.onInput(ev.target);
  });
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && ev.target.id === "g7-self-gw") { ev.preventDefault(); act("self-gw-save", ev.target); }
    if (ev.key === "Enter" && ev.target.id === "g7-name") { ev.preventDefault(); act("save-name", ev.target); }
    if (D.onKey && D.onKey(ev)) return;
    if (drawerEl && (drawerEl.firstChild || modalEl.firstChild)) trap(ev);
  });

  // 倒计时只改文字，不重画
  setInterval(() => {
    if (!modalEl || !modalEl.firstChild) return;
    modalEl.querySelectorAll("[data-left]").forEach((el) => { el.textContent = left(el.dataset.left, Number(el.dataset.minus || 0)); });
  }, 1000);

  function bindMenu() {
    const btn = document.getElementById("account-devices");
    if (!btn) return;
    btn.insertAdjacentHTML("afterbegin", svg("devices"));
    btn.addEventListener("click", () => {
      const menu = document.getElementById("account-menu");
      if (menu) menu.hidden = true;
      const acct = document.getElementById("account");
      if (acct) acct.setAttribute("aria-expanded", "false");
      opener = btn;
      openDrawer();
    });
    const acct = document.getElementById("account");
    if (acct) acct.addEventListener("click", () => { refresh(); });
  }
  // ---- 侧栏：其他电脑上的 Bot，按设备分组 ----
  // 网关不通时只在分组上方放一条提示、整体变淡，不给每台标离线；网关通时离线的设备变淡并标「离线」。
  const initials = (n) => { const c = Array.from(String(n || "?")); return /[A-Za-z]/.test(c[0]) ? (c[0].toUpperCase() + (c[1] || "").toLowerCase()) : c[0]; };
  const face = (n) => `<span class="avatar"><svg viewBox="0 0 36 36" aria-hidden="true" style="--avatar-light:#d3d3d3;--avatar-dark:#3d3d3d;--avatar-letter-light:#333333;--avatar-letter-dark:#e4e4e4"><circle cx="18" cy="18" r="18" fill="var(--avatar-fill)"/><text x="18" y="23" text-anchor="middle" font-size="${initials(n).length > 1 ? 13 : 15}" fill="var(--avatar-ink)">${esc(initials(n))}</text></svg></span>`;
  const sideFolds = () => { try { return JSON.parse(localStorage.getItem("xbot-device-folders") || "{}") || {}; } catch (e) { return {}; } };
  let sideSig = "";
  function paintSidebar() {
    const nav = document.getElementById("remote-roster");
    if (!nav) return;
    const data = S.data || {};
    const devs = (data.devices || []).filter((d) => d.bots && d.bots.length && !d.unpaired);
    const up = !!data.gateway_ok, folds = sideFolds();
    const sig = JSON.stringify([devs, up, folds]);
    if (sig === sideSig) return;
    sideSig = sig;
    const had = document.activeElement && nav.contains(document.activeElement) ? document.activeElement.closest("[data-side]") : null;
    const key = had ? `[data-side="${had.dataset.side}"]` + (had.dataset.id ? `[data-id="${CSS.escape(had.dataset.id)}"]` : "") : "";
    nav.hidden = !devs.length;
    nav.innerHTML = !devs.length ? "" : (up ? "" : `<div class="g7-side-down"><span>连不上网关，其他电脑的 Bot 暂时用不了。</span><button class="ghost" type="button" aria-label="重试" data-side="retry">${svg("refresh")}</button></div>`)
      + devs.map((d) => {
        const off = !up || !d.online, closed = !!folds[d.id];
        const rows = d.bots.map((b) => `<div class="item other g7-remote-bot" title="${esc(b)} · 在 ${esc(d.label)} 上">${face(b)}<span class="item-body"><span class="name-line"><span class="name">${esc(b)}</span></span>
          <span class="preview">在 ${esc(d.label)} 上</span></span></div>`).join("");
        return `<div class="folder g7-side-dev${off ? " is-off" : ""}"><div class="folder-head"><button class="folder-toggle" type="button" data-side="fold" data-id="${esc(d.id)}" aria-expanded="${closed ? "false" : "true"}">
          <span class="folder-name">${svg("laptop", 13)}<span>${esc(d.label)}</span></span>${up && !d.online ? `<span class="g7-side-off">离线</span>` : ""}<span class="folder-count">${d.bots.length}</span></button></div>
          <div class="folder-bots"${closed ? " hidden" : ""}>${rows}</div></div>`;
      }).join("");
    // 重画会换掉按钮，键盘焦点留在同一个折叠或重试按钮上。
    const back = key && nav.querySelector(key);
    if (back) back.focus();
  }
  document.addEventListener("click", async (ev) => {
    const t = ev.target.closest && ev.target.closest("[data-side]");
    if (!t) return;
    if (t.dataset.side === "fold") {
      const f = sideFolds(); f[t.dataset.id] = !f[t.dataset.id];
      try { localStorage.setItem("xbot-device-folders", JSON.stringify(f)); } catch (e) {}
    } else {
      try { S.data = await api("GET", "/api/devices?retry=1"); } catch (e) {}
    }
    paintSidebar();
  });
  setInterval(() => { if (!document.hidden) refresh(); }, 30000);

  const D = window.xbotDevices = { S, api, svg, esc, host, left, fmtSeen, modal, render, refresh, open: openDrawer, closeModal,
    views: {}, acts: {}, onInput: null, onKey: null };
  bindMenu();
  refresh();
})();
