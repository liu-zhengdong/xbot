/* G7 配对码弹窗：A「添加设备」显示码并确认「X 想连到这台电脑」，B「加入另一台设备」输入码。
   挂在 devices.js 的 window.xbotDevices 上。校验位和 internal/remote 同一套（Luhn mod 30），打错不连网关。 */
(function () {
  "use strict";
  const D = window.xbotDevices;
  if (!D) return;
  const { S, api, svg, esc, host, left, fmtSeen, modal, render, refresh } = D;
  const closeModal = D.closeModal, openDrawer = D.open;
  const ALPHA = "23456789ABCDEFGHJKMNPQRSTVWXYZ";
  // ---- 配对码 ----
  function checkChar(data) {
    let sum = 0, factor = 2; const n = ALPHA.length;
    for (let i = data.length - 1; i >= 0; i--) {
      const v = ALPHA.indexOf(data[i]) * factor;
      sum += Math.floor(v / n) + (v % n);
      factor = 3 - factor;
    }
    return ALPHA[(n - (sum % n)) % n];
  }
  // 粘贴整段话时取第一段合法的 12 位；否则只留字符表里的字符。
  function cleanCode(raw) {
    const up = String(raw || "").toUpperCase();
    let run = "";
    for (const ch of up) {
      if (ALPHA.includes(ch)) { run += ch; if (run.length === 12) return run; }
      else if (/[\p{L}\p{N}]/u.test(ch)) run = "";
    }
    const kept = [...up].filter((c) => ALPHA.includes(c)).join("");
    return kept.slice(0, 12);
  }
  const groupCode = (c) => c.replace(/(.{4})(?=.)/g, "$1-");
  const codeOK = (c) => c.length === 12 && checkChar(c.slice(0, 11)) === c[11];

  async function startAdd() {
    S.fieldErr = "";
    if (!S.data || !S.data.self || !S.data.self.gateway) { S.view = "add-gw"; S.gwVal = ""; render(); return; }
    await newCode();
  }
  async function newCode(gateway) {
    S.busy = true; render();
    try {
      const out = await api("POST", "/api/devices/code", gateway ? { gateway } : {});
      S.pairing = out.pairing; S.code = S.pairing.a.code || ""; S.view = "add"; S.fieldErr = "";
      if (gateway) await refresh();
    } catch (e) {
      S.fieldErr = e.message;
      if (!S.view) S.view = "add-gw";
    }
    S.busy = false; render();
  }
  function startJoin() {
    S.view = "join"; S.joinVal = ""; S.fieldErr = "";
    S.gwOpen = !(S.data && S.data.self && S.data.self.gateway); S.gwVal = "";
    api("DELETE", "/api/devices/join").catch(() => {});
    S.pairing.b = { phase: "none" };
    render();
  }
  async function submitJoin() {
    const code = cleanCode(S.joinVal);
    if (code.length !== 12) return;
    if (!codeOK(code)) { S.pairing.b = { phase: "typo" }; render(); return; } // 本机判断，不连网关
    const body = { code };
    if (S.gwOpen) { if (!S.gwVal.trim()) return; body.gateway = S.gwVal.trim(); }
    S.pairing.b = { phase: "checking" }; render();
    try {
      const out = await api("POST", "/api/devices/join", body);
      S.pairing = out.pairing;
      if (body.gateway && S.pairing.b.phase !== "gateway_unreachable") { S.gwOpen = false; refresh(); }
    } catch (e) {
      S.pairing.b = { phase: "gateway_unreachable", gateway: body.gateway || (S.data && S.data.self.gateway) };
    }
    syncFromPairing(); render();
  }

  // 服务器状态 → 弹窗
  function syncFromPairing() {
    const a = S.pairing.a || {}, b = S.pairing.b || {};
    if (a.phase === "request" && S.view !== "request") { S.view = "request"; }
    if (S.view === "request" && a.phase === "done") { S.view = "success-a"; S.peer = { name: a.name }; refresh(); }
    if (S.view === "request" && a.phase === "denied") { S.view = null; }
    if (S.view === "add" && a.phase === "none") { S.view = null; }
    if (["join", "confirm", "waiting"].includes(S.view)) {
      if (b.phase === "confirm") { S.view = "confirm"; S.peer = { name: b.name, os: b.os, bots: b.bots || [] }; }
      else if (b.phase === "waiting") { S.view = "waiting"; }
      else if (b.phase === "denied" && S.view === "waiting") { S.view = "denied"; }
      else if (b.phase === "done") { S.view = "success-b"; S.peer = Object.assign({}, S.peer, { name: b.name || (S.peer && S.peer.name) }); refresh(); }
      else if (S.view !== "join" && b.phase !== "waiting") { S.view = "join"; }
    }
  }

  let seen = "";
  async function poll() {
    if (document.hidden) return;
    const active = S.view === "add" || S.view === "join" || S.view === "confirm" || S.view === "waiting" || S.view === "request";
    if (!active && !(S.data && S.data.self && S.data.self.gateway)) return;
    try {
      // Compare with the last server state, not S.pairing: a refresh() that
      // already stored the new state must still move the modal on.
      const out = await api("GET", "/api/devices/pairing");
      const now = JSON.stringify(out.pairing);
      if (now === seen) return;
      const local = S.pairing.b || {};
      seen = now; S.pairing = out.pairing;
      if (local.phase === "typo" && (S.pairing.b || {}).phase === "none") S.pairing.b = local; // checked here only
      syncFromPairing(); render();
    } catch (e) { /* 下次再试 */ }
  }

  const allows = (them, themBots, mineBots) => `<ul class="g7-allows">
      <li>${svg("bot")}<span>这里能用 ${esc(them)} 上的 ${themBots} 个 Bot</span></li>
      <li>${svg("swap")}<span>${esc(them)} 也能用这台电脑上的 ${mineBots} 个 Bot</span></li></ul>`;
  const spin = (t) => `<span class="g7-wait">${svg("spin")}<span>${t}</span></span>`;
  const selfBots = () => (S.data && S.data.self && S.data.self.bots ? S.data.self.bots.length : 0);
  function deviceCard(p, extra) {
    const bots = (p.bots || []);
    return `<div class="g7-device"><span class="g7-dev-ico">${svg("laptop", 20)}</span><div class="g7-dev-main"><div class="g7-dev-name">${esc(p.name)}</div>
      <div class="g7-dev-sub">${esc(p.os || "")}${p.os ? " · " : ""}${bots.length} 个 Bot${bots.length ? "：" + bots.map(esc).join("、") : ""}</div></div>${extra || ""}</div>`;
  }
  function viewAdd() {
    const a = S.pairing.a || {};
    const code = a.code || S.code;
    const exp = a.phase === "expired";
    const groups = (code || "").split("-").map((g) => `<span class="g7-group">${g.split("").map((c) => `<i>${esc(c)}</i>`).join("")}</span>`).join("");
    return modal(`<h2>添加设备</h2>
      <p class="g7-lead">在另一台电脑的 XBot 里打开「设备」，选「加入另一台设备」，输入下面的配对码。</p>
      <div class="g7-code${exp ? " is-expired" : ""}" aria-label="配对码 ${esc(code)}">${groups}</div>
      <div class="g7-code-meta">
        ${exp ? `<span class="g7-timer">${svg("clock", 13)}<span>配对码已过期</span></span>`
              : `<span class="g7-timer">${svg("clock", 13)}<span data-left="${esc(a.expires)}">${left(a.expires)}</span><span>后失效</span></span>`}
        <span class="spacer"></span>
        ${exp ? "" : `<button class="secondary g7-sm" type="button" data-act="copy">${svg("copy")}<span>复制</span></button>`}
        <button class="${exp ? "primary" : "secondary"} g7-sm" type="button" data-act="renew" ${S.busy ? "disabled" : ""}>${svg("refresh")}<span>${exp ? "重新生成" : "换一个"}</span></button>
      </div>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="g7-allow-note">${svg("shield")}<p>配对后，两台电脑能互相使用对方的 Bot。对方输入配对码后，这台电脑还会请你确认。</p></div>
      <div class="modal-actions">${exp ? "" : spin("等另一台电脑输入")}<span class="spacer"></span>
        <button class="secondary" type="button" data-act="cancel-code">取消</button></div>`);
  }
  function viewAddGw() {
    return modal(`<h2>添加设备</h2>
      <p class="g7-lead">配对要经过你自己的网关。这台电脑还没设网关地址，填好后才能生成配对码。</p>
      <label class="field g7-gw-field"><span>网关地址</span><input id="g7-gw" type="url" placeholder="https://" spellcheck="false" value="${esc(S.gwVal)}"></label>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : `<p class="g7-fine">两台电脑要填同一个网关地址。</p>`}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close">取消</button>
        <button class="primary" type="button" data-act="save-gw-code" ${S.gwVal.trim() && !S.busy ? "" : "disabled"}>保存并生成配对码</button></div>`);
  }
  function viewJoin() {
    const b = S.pairing.b || {};
    const v = groupCode(cleanCode(S.joinVal));
    const n = v.replace(/-/g, "").length;
    let ph = b.phase && b.phase !== "none" ? b.phase : (n ? "typing" : "empty");
    if (ph === "wrong") ph = "wrong_or_used";
    const gw = b.gateway || (S.data && S.data.self && S.data.self.gateway) || S.gwVal;
    const peerName = b.name || "另一台电脑";
    const msg = {
      empty: `<p class="g7-hint">12 位，字母不分大小写，可以直接粘贴。</p>`,
      typing: `<p class="g7-hint">${n < 12 ? `还差 ${12 - n} 位` : ""}</p>`,
      checking: `<p class="g7-hint">${spin("正在找这台电脑")}</p>`,
      typo: `<p class="error g7-msg">这个配对码有一位不对。核对另一台电脑上显示的码，再输一次。</p>`,
      wrong_or_used: `<p class="error g7-msg">配对码不对或已用过。核对一下，或在另一台电脑上点「换一个」。</p>`,
      expired: `<p class="error g7-msg">这个配对码已经过期，在另一台电脑上点「重新生成」。</p>`,
      already: `<p class="g7-hint g7-info">${svg("check", 14)}<span>这台电脑已经和「${esc(peerName)}」配对了，不用再配。</span></p>`,
      gateway_unreachable: `<p class="error g7-msg">连不上网关 ${esc(host(gw))}。检查一下网络，或者改一下网关地址。</p>`,
      offline: `<p class="error g7-msg">另一台电脑没有回应。确认它开着 XBot，再试一次。</p>`,
    }[ph] || "";
    const bad = ph === "typo" || ph === "expired" || ph === "wrong_or_used";
    const retry = ph === "gateway_unreachable" || ph === "offline";
    let actions;
    if (ph === "already") actions = `<button class="secondary" type="button" data-act="close">关闭</button><button class="primary" type="button" data-act="to-drawer">查看设备</button>`;
    else if (retry) actions = `${ph === "gateway_unreachable" ? `<button class="text-btn g7-gw-change" type="button" data-act="gw-open">改网关地址</button>` : ""}<span class="spacer"></span><button class="secondary" type="button" data-act="close-join">取消</button><button class="primary" type="button" data-act="retry">${svg("refresh")}<span>重试</span></button>`;
    else actions = `<button class="secondary" type="button" data-act="close-join">取消</button><button class="primary" type="button" data-act="join" ${n === 12 && !bad && ph !== "checking" && (!S.gwOpen || S.gwVal.trim()) ? "" : "disabled"}>下一步</button>`;
    const gwBlock = retry && !S.gwOpen ? "" : S.gwOpen
      ? `<label class="field g7-gw-field"><span>网关地址</span><input id="g7-gw" type="url" placeholder="https://" spellcheck="false" value="${esc(S.gwVal)}"></label>
         ${S.data && S.data.self && S.data.self.gateway ? "" : `<p class="g7-fine g7-gw-why">还没设网关。两台电脑要通过同一个网关找到对方，填和另一台电脑一样的地址。</p>`}`
      : `<div class="g7-gw">${svg("server", 14)}<span>网关 ${esc(host(gw))}</span><button class="text-btn g7-gw-edit" type="button" data-act="gw-open">改</button></div>`;
    return modal(`<h2>加入另一台设备</h2>
      <p class="g7-lead">在另一台电脑的 XBot 里打开「设备」，选「添加设备」，把显示的配对码输到这里。</p>
      <label class="field g7-code-field${bad ? " is-bad" : ""}"><span class="g7-sr">配对码</span>
        <input id="g7-code" type="text" inputmode="text" autocomplete="one-time-code" spellcheck="false" autocapitalize="characters" maxlength="200"
          placeholder="XXXX-XXXX-XXXX" value="${esc(v)}" ${ph === "checking" ? "disabled" : ""}></label>
      ${msg}${gwBlock}
      <div class="modal-actions">${actions}</div>`);
  }
  function viewConfirm() {
    const p = S.peer || {};
    return modal(`<h2>连接到 ${esc(p.name)}？</h2>
      <p class="g7-lead">配对码对应的是这台电脑。确认是你自己的电脑，再连接。</p>
      ${deviceCard(p)}
      <h3 class="g7-h3">连接后</h3>${allows(p.name, (p.bots || []).length, selfBots())}
      <p class="g7-fine">${esc(p.name)} 那边还要点一次「允许」。不认识这台电脑就点「取消」。</p>
      <div class="modal-actions"><button class="secondary" type="button" data-act="close-join">取消</button><button class="primary" type="button" data-act="confirm" ${S.busy ? "disabled" : ""}>连接</button></div>`);
  }
  function viewWaiting() {
    const p = S.peer || {}, b = S.pairing.b || {};
    return modal(`<h2>等 ${esc(p.name)} 允许</h2>
      <p class="g7-lead">请到 ${esc(p.name)} 上点「允许」。</p>
      ${deviceCard(p, `<span class="g7-dev-side">${svg("spin", 18)}</span>`)}
      <div class="modal-actions"><span class="g7-timer">${svg("clock", 13)}<span data-left="${esc(b.expires)}" data-minus="15000">${left(b.expires, 15000)}</span><span>后自动取消</span></span>
        <span class="spacer"></span><button class="secondary" type="button" data-act="close-join">取消</button></div>`);
  }
  function viewDenied() {
    const p = S.peer || {};
    return modal(`<h2>${esc(p.name)} 没有允许</h2>
      <p class="g7-lead">这次没连上，两边什么都没共享。要再试，请在 ${esc(p.name)} 上重新「添加设备」，拿一个新的配对码。</p>
      <div class="modal-actions"><span class="spacer"></span><button class="primary" type="button" data-act="close-join">知道了</button></div>`);
  }
  function viewRequest() {
    const a = S.pairing.a || {};
    const head = `<div class="approval-head g7-req-head"><span class="approval-ico">${svg("laptop", 16)}</span><div class="approval-main">
        <div class="approval-title">${esc(a.name)} 想连到这台电脑</div>
        <div class="approval-detail">${esc(a.os || "")}${a.os ? " · " : ""}刚刚输入了配对码${S.code ? " " + esc(S.code) : ""}</div></div></div>`;
    const timer = `<span class="approval-timer">${svg("clock", 13)}<span><span data-left="${esc(a.expires)}">${left(a.expires)}</span> 后自动拒绝</span></span>`;
    const c = a.collision;
    if (c) {
      const old = (S.data && S.data.devices || []).find((d) => d.label === c.name);
      const status = old && old.online ? "在线" : "离线" + (c.seen && fmtSeen(c.seen) ? " · 最后在线 " + fmtSeen(c.seen) : "");
      return modal(`${head}
        <div class="g7-device g7-old"><span class="g7-dev-ico">${svg("laptop", 20)}</span><div class="g7-dev-main"><div class="g7-dev-name">已经有一台 ${esc(c.name)}</div>
          <div class="g7-dev-sub"><i class="status-dot ${old && old.online ? "ok" : "off-ring"}"></i>${esc(status)} · ${c.bots} 个 Bot</div></div></div>
        <p class="g7-fine">如果是同一台电脑重装了 XBot，就替换旧的：侧栏里的分组和你起的名字都保留。不是你自己在操作，就点「拒绝」。</p>
        <div class="g7-timer-row">${timer}</div>
        <div class="modal-actions g7-replace-actions"><button class="secondary" type="button" data-act="decide" data-d="deny">拒绝</button><span class="spacer"></span>
          <button class="secondary" type="button" data-act="decide" data-d="add">作为新设备添加</button>
          <button class="primary" type="button" data-act="decide" data-d="replace">替换旧的 ${esc(c.name)}</button></div>`, "g7-request");
    }
    return modal(`${head}
      <h3 class="g7-h3">允许后</h3>${allows(a.name, (a.bots || []).length, selfBots())}
      <p class="g7-fine">不是你自己在另一台电脑上操作，就点「拒绝」，这个配对码会立刻作废。</p>
      <div class="modal-actions approval-actions">${timer}<span class="spacer"></span>
        <button class="secondary" type="button" data-act="decide" data-d="deny">拒绝</button>
        <button class="primary" type="button" data-act="decide" data-d="allow">允许</button></div>`, "g7-request");
  }
  function viewSuccess(side) {
    const p = S.peer || {};
    const dev = (S.data && S.data.devices || []).find((d) => d.label === p.name);
    const n = dev ? dev.bots.length : (p.bots || []).length;
    const lead = n ? `${esc(p.name)} 上的 ${n} 个 Bot 会出现在左侧列表里。` : `${esc(p.name)} 上的 Bot 会出现在左侧列表里。`;
    return modal(`<div class="g7-done"><span class="g7-done-ico">${svg("check", 24)}</span><h2>已和 ${esc(p.name)} 配对</h2><p class="g7-lead">${lead}</p></div>
      <div class="modal-actions g7-center"><button class="secondary" type="button" data-act="to-drawer">查看设备</button><button class="primary" type="button" data-act="${side === "b" ? "close-join" : "close"}">完成</button></div>`);
  }
  Object.assign(D.views, { add: viewAdd, "add-gw": viewAddGw, join: viewJoin, confirm: viewConfirm, waiting: viewWaiting, denied: viewDenied,
    request: viewRequest, "success-a": () => viewSuccess("a"), "success-b": () => viewSuccess("b") });

  async function act(name, el) {
    const data = S.data || {};
    switch (name) {
      case "add": await startAdd(); break;
      case "join-open": startJoin(); break;
      case "renew": await newCode(); break;
      case "cancel-code": api("DELETE", "/api/devices/code").catch(() => {}); S.code = ""; closeModal(); break;
      case "copy": {
        const code = (S.pairing.a && S.pairing.a.code) || S.code;
        try { await navigator.clipboard.writeText(code); } catch (e) { return; }
        el.classList.add("is-done"); el.innerHTML = svg("check") + "<span>已复制</span>";
        setTimeout(() => { if (el.isConnected) { el.classList.remove("is-done"); el.innerHTML = svg("copy") + "<span>复制</span>"; } }, 2000);
        break;
      }
      case "save-gw-code": S.fieldErr = ""; await newCode(S.gwVal.trim()); break;
      case "join": case "retry":
        if (S.pairing.b) S.pairing.b = { phase: "none" };
        await submitJoin(); break;
      case "gw-open":
        S.gwOpen = true; S.gwVal = S.gwVal || (data.self && data.self.gateway) || "";
        if (S.pairing.b && S.pairing.b.phase === "gateway_unreachable") S.pairing.b = { phase: "none" };
        render(); { const g = document.getElementById("g7-gw"); if (g) g.focus(); } break;
      case "close-join": api("DELETE", "/api/devices/join").catch(() => {}); S.pairing.b = { phase: "none" }; closeModal(); break;
      case "confirm":
        S.busy = true; render();
        try { const out = await api("POST", "/api/devices/join/confirm"); S.pairing = out.pairing; S.view = "waiting"; }
        catch (e) { S.pairing.b = { phase: "none" }; S.view = "join"; }
        S.busy = false; syncFromPairing(); render(); break;
      case "decide": {
        const d = el.dataset.d;
        S.peer = { name: S.pairing.a.name, bots: S.pairing.a.bots || [] };
        el.disabled = true;
        try { const out = await api("POST", "/api/devices/request", { decision: d }); S.pairing = out.pairing; }
        catch (e) { el.disabled = false; return; }
        if (d === "deny") { S.view = null; S.code = ""; } else { S.view = "success-a"; S.code = ""; await refresh(); }
        render(); break;
      }
    }
  }
  for (const name of ["add", "join-open", "renew", "cancel-code", "copy", "save-gw-code", "join", "retry", "gw-open", "close-join", "confirm", "decide"]) {
    D.acts[name] = (el) => act(name, el);
  }
  D.onInput = (t) => {
    const modalEl = document.getElementById("devices-modal");
    if (t.id === "g7-code") {
      const c = cleanCode(t.value);
      S.joinVal = c; t.value = groupCode(c);
      const b = S.pairing.b || {};
      if (b.phase && b.phase !== "none" && b.phase !== "checking") S.pairing.b = { phase: "none" };
      if (c.length === 12 && !codeOK(c)) S.pairing.b = { phase: "typo" }; // 本机判断，不等网关地址
      else if (c.length === 12 && (!S.gwOpen || S.gwVal.trim())) { submitJoin(); return; } // 输满自动查找
      render();
    } else if (t.id === "g7-gw") {
      S.gwVal = t.value;
      const btn = modalEl && modalEl.querySelector(S.view === "add-gw" ? '[data-act="save-gw-code"]' : '[data-act="join"]');
      if (btn) btn.disabled = !t.value.trim() || (S.view === "join" && cleanCode(S.joinVal).length !== 12);
    }
  };
  D.onKey = (ev) => {
    if (ev.key === "Enter" && ev.target.id === "g7-gw" && S.view === "add-gw" && S.gwVal.trim()) { ev.preventDefault(); act("save-gw-code", ev.target); return true; }
    if (ev.key === "Escape" && S.view === "add-gw") { closeModal(); return true; }
    return false;
  };
  setInterval(poll, 1500);
})();
