/* G7 添加设备嵌层：说明为主，本机短码为次；对方输入码后这边再确认（request）。
   挂在 devices.js 的 window.xbotDevices 上。 */
(function () {
  "use strict";
  const D = window.xbotDevices;
  if (!D) return;
  const { S, api, svg, esc, left, fmtSeen, modal, render, refresh } = D;
  const closeModal = D.closeModal;

  let pairGen = 0, cleanup = Promise.resolve(), pairClosing = false;
  async function startAdd() {
    const gen = ++pairGen;
    S.fieldErr = "";
    S.view = "add";
    S.busy = true; render();
    await cleanup;
    if (gen !== pairGen) return;
    pairClosing = false;
    try { await api("DELETE", "/api/devices/join"); }
    catch (e) { if (gen === pairGen) { S.busy = false; S.fieldErr = e.message; render(); } return; }
    if (gen === pairGen) await newCode();
  }
  let codeGen = 0, codeRun = null;
  async function newCode() {
    const gen = ++codeGen;
    S.busy = true; render();
    try {
      const p = api("POST", "/api/devices/code", {});
      codeRun = p;
      const out = await p;
      if (gen !== codeGen) return; // 取消后才返回：丢掉这个码
      S.pairing = out.pairing; S.code = S.pairing.a.code || ""; S.view = "add"; S.fieldErr = "";
      await refresh();
    } catch (e) {
      if (gen !== codeGen) return;
      S.fieldErr = e.message;
    } finally {
      if (gen === codeGen) { S.busy = false; render(); }
    }
  }
  // 立刻收起；等在途请求结束后再清理，重开时等待清理完成。
  function closePair() {
    const gen = ++pairGen; ++codeGen; pairClosing = true;
    S.code = ""; S.busy = false;
    S.pairing = { a: { phase: "none" }, b: { phase: "none" } };
    closeModal();
    document.querySelector('#settings-devices [data-act="add"]')?.focus();
    cleanup = Promise.allSettled([codeRun]).then(() => Promise.allSettled([
      api("DELETE", "/api/devices/code"), api("DELETE", "/api/devices/join"),
    ])).finally(() => { if (gen === pairGen) pairClosing = false; });
    return cleanup;
  }
  // 服务器状态 → 弹窗。添加设备只展示本机码，等对方连过来（a 侧）；
  // 本机去连对方的 join 流程已从产品面拿掉（「添加设备」唯一入口，短码次要）。
  function syncFromPairing() {
    const a = S.pairing.a || {};
    if (a.phase === "request" && S.view !== "request") { S.view = "request"; }
    if (S.view === "request" && a.phase === "done") { S.view = "success-a"; S.peer = { name: a.name }; refresh(); }
    if (S.view === "request" && a.phase === "denied") { S.view = null; }
  }

  let seen = "", pollSkipped = false;
  async function poll() {
    if (pairClosing) return;
    if (document.hidden) { pollSkipped = true; return; }
    const gen = pairGen;
    const active = S.view === "add" || S.view === "request";
    if (!active && !(S.data && S.data.self && S.data.self.gateway)) return;
    pollSkipped = false;
    try {
      // Compare with the last server state, not S.pairing: a refresh() that
      // already stored the new state must still move the modal on.
      const out = await api("GET", "/api/devices/pairing");
      if (gen !== pairGen || pairClosing) return;
      const now = JSON.stringify(out.pairing);
      if (now === seen) return;
      seen = now; S.pairing = out.pairing;
      syncFromPairing(); render();
    } catch (e) { /* 下次再试 */ }
  }
  // 窗口在后台时轮询停摆：一回到前台就马上查一次，「X 想连到这台电脑」不用等下一个间隔。
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden && pollSkipped) poll();
  });

  const allows = (them, themBots, mineBots) => `<ul class="g7-allows">
      <li>${svg("bot")}<span>这里能用 ${esc(them)} 上的 ${themBots} 个 Bot</span></li>
      <li>${svg("swap")}<span>${esc(them)} 也能用这台电脑上的 ${mineBots} 个 Bot</span></li></ul>`;
  const selfBots = () => (S.data && S.data.self && S.data.self.bots ? S.data.self.bots.length : 0);
  // 添加设备（shot 04）：说明为主；短码次要；不提供同权的本机输入对方码入口。
  function viewPair() {
    const a = S.pairing.a || {}, code = a.code || S.code;
    const exp = a.phase === "expired";
    const groups = code.replace(/-/g, "").match(/.{1,4}/g) || [];
    return modal(`<h2>添加设备</h2>
      <p class="g7-lead">到新电脑上打开 XBot，用同一账号登录，并确认「把这台电脑绑到账号」。绑定后，这里就能看到那台电脑上的 Bot。</p>
      <h3 class="g7-pair-label">也可以让对方输入下方短码（次要）：</h3>
      ${code ? `<button id="g7-copy-code" class="g7-code${exp ? " is-expired" : ""}" type="button" data-act="copy" aria-label="复制配对码 ${esc(code)}" ${exp ? "disabled" : ""}>${groups.map(g => `<span class="g7-group">${[...g].map(c => `<i>${esc(c)}</i>`).join("")}</span>`).join("")}</button>`
        : `<p class="g7-fine">${S.busy ? "正在生成配对码…" : "未能生成配对码，点右侧图标重试。"}</p>`}
      <div class="g7-code-meta">
        ${code ? `<span class="g7-timer">${svg("clock", 13)}${exp ? "配对码已过期" : `<span data-left="${esc(a.expires)}">${left(a.expires)}</span><span>后失效</span>`}</span>` : ""}
        <span class="g7-copy-status" role="status"></span><span class="spacer"></span>
        <button id="g7-renew" class="ghost" type="button" data-act="renew" title="换一个" aria-label="换一个" ${S.busy ? "disabled" : ""}>${svg("refresh", 16)}</button>
      </div>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions"><span class="spacer"></span><button class="primary" type="button" data-act="close-pair">完成</button></div>`, "g7-pair");
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
        <div class="g7-timer-row">${timer}</div>${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
        <div class="modal-actions g7-replace-actions"><button class="secondary" type="button" data-act="decide" data-d="deny">拒绝</button><span class="spacer"></span>
          <button class="secondary" type="button" data-act="decide" data-d="add">作为新设备添加</button>
          <button class="primary" type="button" data-act="decide" data-d="replace">替换旧的 ${esc(c.name)}</button></div>`, "g7-request");
    }
    return modal(`${head}
      <h3 class="g7-h3">允许后</h3>${allows(a.name, (a.bots || []).length, selfBots())}
      <p class="g7-fine">不是你自己在另一台电脑上操作，就点「拒绝」，这个配对码会立刻作废。</p>${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions approval-actions">${timer}<span class="spacer"></span>
        <button class="secondary" type="button" data-act="decide" data-d="deny">拒绝</button>
        <button class="primary" type="button" data-act="decide" data-d="allow">允许</button></div>`, "g7-request");
  }
  function viewSuccess() {
    const p = S.peer || {};
    const dev = (S.data && S.data.devices || []).find((d) => d.label === p.name);
    const n = dev ? dev.bots.length : (p.bots || []).length;
    const lead = n ? `${esc(p.name)} 上的 ${n} 个 Bot 会出现在左侧列表里。` : `${esc(p.name)} 上的 Bot 会出现在左侧列表里。`;
    return modal(`<div class="g7-done"><span class="g7-done-ico">${svg("check", 24)}</span><h2>已和 ${esc(p.name)} 绑定</h2><p class="g7-lead">${lead}</p></div>
      <div class="modal-actions g7-center"><button class="secondary" type="button" data-act="to-drawer">查看设备</button><button class="primary" type="button" data-act="close">完成</button></div>`);
  }
  Object.assign(D.views, { add: viewPair, request: viewRequest, "success-a": viewSuccess });

  async function act(name, el) {
    const data = S.data || {};
    switch (name) {
      case "add": await startAdd(); break;
      case "renew": await newCode(); break;
      case "close-pair": await closePair(); break;
      case "copy": {
        const code = (S.pairing.a && S.pairing.a.code) || S.code;
        try { await navigator.clipboard.writeText(code); } catch (e) { return; }
        el.classList.add("is-done");
        const status = el.parentElement.querySelector(".g7-copy-status");
        if (status) status.textContent = "已复制";
        setTimeout(() => { el.classList.remove("is-done"); if (status) status.textContent = ""; }, 2000);
        break;
      }
      case "decide": {
        const d = el.dataset.d;
        S.fieldErr = "";
        S.peer = { name: S.pairing.a.name, bots: S.pairing.a.bots || [] };
        el.disabled = true;
        try { const out = await api("POST", "/api/devices/request", { decision: d }); S.pairing = out.pairing; }
        catch (e) { el.disabled = false; S.fieldErr = e.message; render(); return; } // 同名满 9 台等：在卡片上说原因
        const phase = (S.pairing.a || {}).phase;
        // 过期窗口里点「允许」，服务器回 200 + denied：没配上，回去看设备列表。
        if (d === "deny" || phase === "denied") { S.view = null; S.code = ""; if (phase === "denied") refresh(); }
        else if (phase === "done") { S.view = "success-a"; S.code = ""; await refresh(); }
        else syncFromPairing();
        render(); break;
      }
    }
  }
  for (const name of ["add", "renew", "close-pair", "copy", "decide"]) {
    D.acts[name] = (el) => act(name, el);
  }
  D.closePair = closePair;
  D.onInput = null; // 对方的码不再从本机输入：join 输入框已从产品面拿掉
  D.onKey = (ev) => {
    // Escape 等同当前弹窗上可见的取消/关闭按钮。
    if (ev.key === "Escape" && Object.prototype.hasOwnProperty.call(D.views, S.view)) {
      ev.preventDefault();
      if (S.view === "request") act("decide", { dataset: { d: "deny" }, disabled: false });
      else if (S.view === "add") act("close-pair");
      else closeModal(); // success-a: nothing to cancel
      return true;
    }
    return false;
  };
  setInterval(poll, 1500);
})();
