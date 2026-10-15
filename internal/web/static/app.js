(function () {
  const $ = (id) => document.getElementById(id);
  const login = $("login");
  const shell = $("shell");
  const sidebar = $("sidebar");
  const backdrop = $("backdrop");
  const thread = $("thread");
  const notesEl = $("notes");
  const input = $("input");
  const titleEl = $("title");
  const subtitle = $("subtitle");
  const bellDot = $("bell-dot");
  const panel = $("panel");
  const panelBody = $("panel-body");
  const modal = $("modal");
  const modalFields = $("modal-fields");
  const modalError = $("modal-error");

  const state = {
    bots: [],
    groups: [],
    notes: [],
    sel: null,
    sig: "",
    streaming: false,
    knownNotes: null,
  };

  function applyTheme(theme) {
    const dark = theme === "dark";
    document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
    const meta = $("theme-color");
    if (meta) meta.setAttribute("content", dark ? "#161618" : "#ffffff");
    try { localStorage.setItem("xbot-theme", dark ? "dark" : "light"); } catch (e) {}
  }

  function initTheme() {
    let theme = "light";
    try { theme = localStorage.getItem("xbot-theme") || ""; } catch (e) {}
    if (theme !== "light" && theme !== "dark") {
      theme = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    }
    applyTheme(theme);
  }

  async function api(path, opt) {
    opt = opt || {};
    const res = await fetch(path, {
      method: opt.method || "GET",
      credentials: "same-origin",
      headers: opt.body ? { "Content-Type": "application/json" } : undefined,
      body: opt.body ? JSON.stringify(opt.body) : undefined,
    });
    const text = await res.text();
    let data = {};
    if (text) {
      try { data = JSON.parse(text); } catch (e) { data = { error: text }; }
    }
    if (res.status === 401) {
      showLogin();
      const err = new Error(data.error || "unauthorized");
      err.status = 401;
      throw err;
    }
    if (!res.ok) {
      const err = new Error(data.error || "request failed");
      err.status = res.status;
      throw err;
    }
    return data;
  }

  function showLogin() {
    shell.hidden = true;
    login.hidden = false;
  }

  function showApp() {
    login.hidden = true;
    shell.hidden = false;
  }

  function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text != null) node.textContent = text;
    return node;
  }

  function when(t) {
    if (!t) return "";
    const d = new Date(t);
    if (Number.isNaN(d.getTime())) return "";
    return d.toLocaleString("zh-CN", { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" });
  }

  function copyText(text, btn) {
    const done = () => {
      if (!btn) return;
      const prev = btn.textContent;
      btn.textContent = "已复制";
      setTimeout(() => { btn.textContent = prev; }, 1200);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done).catch(() => fallback());
      return;
    }
    fallback();
    function fallback() {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.left = "-9999px";
      document.body.append(ta);
      ta.select();
      try { document.execCommand("copy"); done(); } catch (e) {}
      ta.remove();
    }
  }

  function unwrap(content) {
    const text = content || "";
    const m = text.match(/^<untrusted_event\s+([^>]*)>\n([\s\S]*)\n<\/untrusted_event>$/);
    if (!m) return { text: text, untrusted: false, source: "" };
    const source = /source="([^"]*)"/.exec(m[1]);
    return { text: m[2], untrusted: true, source: source ? source[1] : "" };
  }

  function appendInline(parent, text) {
    const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\n]+\*)|(\[[^\]]+\]\(https?:\/\/[^\s)]+\))/g;
    let last = 0;
    let match;
    while ((match = re.exec(text))) {
      if (match.index > last) parent.append(document.createTextNode(text.slice(last, match.index)));
      const tok = match[0];
      if (tok.charAt(0) === "`") {
        parent.append(el("code", "", tok.slice(1, -1)));
      } else if (tok.startsWith("**")) {
        parent.append(el("strong", "", tok.slice(2, -2)));
      } else if (tok.charAt(0) === "*") {
        parent.append(el("em", "", tok.slice(1, -1)));
      } else {
        const lm = /^\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)$/.exec(tok);
        if (lm && (lm[2].startsWith("http://") || lm[2].startsWith("https://"))) {
          const a = document.createElement("a");
          a.textContent = lm[1];
          a.href = lm[2];
          a.rel = "noopener noreferrer";
          a.target = "_blank";
          parent.append(a);
        } else {
          parent.append(document.createTextNode(tok));
        }
      }
      last = match.index + tok.length;
    }
    if (last < text.length) parent.append(document.createTextNode(text.slice(last)));
  }

  function appendBlocks(parent, text) {
    const lines = String(text || "").replace(/\r\n/g, "\n").split("\n");
    let para = [];
    let list = null;
    const flushPara = () => {
      if (!para.length) return;
      const p = document.createElement("p");
      appendInline(p, para.join("\n"));
      parent.append(p);
      para = [];
    };
    const flushList = () => {
      if (!list) return;
      parent.append(list);
      list = null;
    };
    for (const line of lines) {
      const h = /^(#{1,3})\s+(.*)$/.exec(line);
      if (h) {
        flushPara();
        flushList();
        const p = document.createElement("p");
        p.style.fontWeight = "600";
        appendInline(p, h[2]);
        parent.append(p);
        continue;
      }
      const li = /^[-*]\s+(.*)$/.exec(line);
      if (li) {
        flushPara();
        if (!list) list = document.createElement("div");
        const row = document.createElement("div");
        row.append(document.createTextNode("· "));
        appendInline(row, li[1]);
        list.append(row);
        continue;
      }
      if (line.trim() === "") {
        flushPara();
        flushList();
        continue;
      }
      flushList();
      para.push(line);
    }
    flushPara();
    flushList();
  }

  function highlight(codeEl, text, lang) {
    const use = /go|js|ts|java|py/.test(lang || "");
    if (!use) {
      codeEl.textContent = text;
      return;
    }
    const kw = new Set(["func", "return", "if", "else", "package", "import", "const", "let", "var", "for", "type", "string", "error"]);
    let i = 0;
    while (i < text.length) {
      const ch = text.charAt(i);
      if (ch === '"' || ch === "'" || ch === "`") {
        let j = i + 1;
        while (j < text.length && text.charAt(j) !== ch) {
          if (text.charAt(j) === "\\") j++;
          j++;
        }
        if (j < text.length) j++;
        codeEl.append(el("span", "str", text.slice(i, j)));
        i = j;
        continue;
      }
      if (text.startsWith("//", i) || (ch === "#" && (lang || "").indexOf("py") >= 0)) {
        let j = text.indexOf("\n", i);
        if (j < 0) j = text.length;
        codeEl.append(el("span", "cm", text.slice(i, j)));
        i = j;
        continue;
      }
      if (/[A-Za-z_]/.test(ch)) {
        let j = i + 1;
        while (j < text.length && /[A-Za-z0-9_]/.test(text.charAt(j))) j++;
        const word = text.slice(i, j);
        if (kw.has(word)) codeEl.append(el("span", "kw", word));
        else codeEl.append(document.createTextNode(word));
        i = j;
        continue;
      }
      codeEl.append(document.createTextNode(ch));
      i++;
    }
  }

  function codeBlock(lang, code) {
    const wrap = el("div", "code-block");
    const head = el("div", "code-head");
    head.append(el("span", "", (lang || "text").trim() || "text"));
    const btn = el("button", "copy", "复制");
    btn.type = "button";
    btn.addEventListener("click", () => copyText(code, btn));
    head.append(btn);
    const pre = document.createElement("pre");
    const codeEl = document.createElement("code");
    highlight(codeEl, code.replace(/\n$/, ""), (lang || "").trim());
    pre.append(codeEl);
    wrap.append(head, pre);
    return wrap;
  }

  function appendMarkdown(parent, text) {
    const src = String(text || "");
    const re = /```([^\n`]*)\n?([\s\S]*?)```/g;
    let last = 0;
    let match;
    while ((match = re.exec(src))) {
      if (match.index > last) appendBlocks(parent, src.slice(last, match.index));
      parent.append(codeBlock(match[1], match[2]));
      last = match.index + match[0].length;
    }
    if (last < src.length) appendBlocks(parent, src.slice(last));
  }

  function heat() {
    const wrap = el("div", "heat");
    for (let i = 0; i < 80; i++) {
      const cell = document.createElement("i");
      const n = ((i * 17) ^ (i >> 1)) % 7;
      if (n > 4) cell.className = "on3";
      else if (n > 3) cell.className = "on2";
      else if (n > 1) cell.className = "on1";
      wrap.append(cell);
    }
    return wrap;
  }

  function emptyState(heading, line) {
    const box = el("div", "empty");
    box.append(el("h2", "", heading), el("p", "", line), heat());
    return box;
  }

  function avatar(name) {
    const av = el("span", "avatar", (name || "?").slice(0, 1).toUpperCase());
    return av;
  }

  function listButton(name, active, unread) {
    const btn = el("button", "item" + (active ? " active" : ""));
    btn.type = "button";
    const av = avatar(name);
    if (unread) av.append(el("i", "unread"));
    btn.append(av, el("span", "name", name));
    return btn;
  }

  function renderLists() {
    const bots = $("bot-list");
    const groups = $("group-list");
    bots.replaceChildren();
    groups.replaceChildren();
    for (const b of state.bots) {
      const btn = listButton(b.name, state.sel && state.sel.type === "bot" && state.sel.id === b.name, !!b.unread);
      btn.addEventListener("click", () => selectBot(b.name));
      bots.append(btn);
    }
    for (const g of state.groups) {
      const btn = listButton(g.name || g.id, state.sel && state.sel.type === "group" && state.sel.id === g.id, false);
      btn.addEventListener("click", () => selectGroup(g.id));
      groups.append(btn);
    }
  }

  function renderNotes() {
    bellDot.hidden = state.notes.length === 0;
    if (!state.notes.length) {
      notesEl.hidden = true;
      notesEl.replaceChildren();
      return;
    }
    notesEl.hidden = false;
    notesEl.replaceChildren();
    for (const n of state.notes) {
      const btn = el("button", "note");
      btn.type = "button";
      const left = document.createElement("span");
      left.append(el("b", "", n.bot), document.createTextNode("  " + (n.text || "")));
      btn.append(left, el("span", "meta", when(n.time)));
      btn.addEventListener("click", () => openNote(n));
      notesEl.append(btn);
    }
  }

  async function openNote(n) {
    try {
      await api("/api/notes/read", { method: "POST", body: { bot: n.bot, ids: [n.id] } });
    } catch (e) {}
    await refreshNotes();
    await refreshLists();
    if (n.bot) await selectBot(n.bot);
  }

  function toolSummary(call) {
    let args = {};
    try { args = JSON.parse((call.function && call.function.arguments) || "{}"); } catch (e) {}
    const name = (call.function && call.function.name) || "tool";
    const bits = [];
    for (const key of Object.keys(args)) {
      const v = args[key];
      if (typeof v === "string" && v) bits.push(v.length > 80 ? v.slice(0, 80) + "…" : v);
    }
    return { name: name, args: args, line: bits.join(" · ") };
  }

  function toolCard(call, result) {
    const info = toolSummary(call);
    const card = document.createElement("details");
    card.className = "card";
    const sum = document.createElement("summary");
    sum.className = "card-head";
    const text = document.createElement("span");
    text.append(el("div", "card-title", info.name));
    if (info.line) text.append(el("div", "card-sub", info.line));
    sum.append(text);
    card.append(sum);
    if (result && result.content) {
      const pre = document.createElement("pre");
      pre.textContent = result.content;
      card.append(pre);
    }
    return card;
  }

  function optionCards(info, picked) {
    const box = el("div", "options");
    const opts = Array.isArray(info.args.options) ? info.args.options : [];
    for (const raw of opts) {
      let label = String(raw);
      let recommend = false;
      if (label.startsWith("* ")) {
        recommend = true;
        label = label.slice(2);
      }
      const btn = el("button", "option" + (recommend ? " recommend" : ""), label);
      btn.type = "button";
      if (picked != null) {
        btn.disabled = true;
        if (picked === label || picked === String(raw)) btn.classList.add("picked");
      } else {
        btn.addEventListener("click", () => sendText(label));
      }
      box.append(btn);
    }
    return box;
  }

  function laterUser(messages, index) {
    for (let i = index + 1; i < messages.length; i++) {
      if (messages[i].role === "user") return unwrap(messages[i].content || "").text.trim();
    }
    return null;
  }

  function userBubble(m) {
    const row = el("div", "msg user");
    const wrap = document.createElement("div");
    const parsed = unwrap(m.content || "");
    const bubble = el("div", "bubble");
    bubble.textContent = parsed.text;
    wrap.append(bubble);
    const meta = el("div", "meta", when(m.time));
    if (parsed.untrusted) {
      const tag = el("span", "tag warn", parsed.source ? "不可信 · " + parsed.source : "不可信");
      const line = el("div", "meta");
      line.append(tag, document.createTextNode("  " + when(m.time)));
      wrap.append(line);
    } else {
      wrap.append(meta);
    }
    row.append(wrap);
    return row;
  }

  function botBubble(m, results, messages, index, who) {
    const row = el("div", "msg bot");
    row.append(avatar(who));
    const body = el("div", "body");
    const head = el("div", "who", who);
    body.append(head);
    const prose = el("div", "prose");
    if (m.content) appendMarkdown(prose, m.content);
    body.append(prose);
    const calls = m.tool_calls || [];
    for (const call of calls) {
      const info = toolSummary(call);
      if (info.name === "ask_user") {
        if (info.args.question && !(m.content || "").includes(info.args.question)) {
          const q = document.createElement("p");
          q.textContent = info.args.question;
          prose.append(q);
        }
        body.append(optionCards(info, laterUser(messages, index)));
        continue;
      }
      body.append(toolCard(call, results[call.id]));
    }
    body.append(el("div", "meta", when(m.time)));
    row.append(body);
    return row;
  }

  function renderMessages(messages, who) {
    const inner = el("div", "thread-inner");
    if (!messages || !messages.length) {
      inner.append(emptyState(who || "XBot", "这条对话还是空的。"));
      thread.replaceChildren(inner);
      return;
    }
    const results = {};
    for (const m of messages) {
      if (m.role === "tool" && m.tool_call_id) results[m.tool_call_id] = m;
    }
    let any = false;
    messages.forEach((m, i) => {
      if (m.role === "tool" || m.role === "system") return;
      if (m.role === "user" && (m.content || "").startsWith("<environment_")) return;
      if (m.role === "user" && (m.content || "").startsWith("<memory>")) return;
      any = true;
      if (m.role === "user") inner.append(userBubble(m));
      else inner.append(botBubble(m, results, messages, i, who));
    });
    if (!any) inner.append(emptyState(who || "XBot", "这条对话还是空的。"));
    thread.replaceChildren(inner);
    thread.scrollTop = thread.scrollHeight;
  }

  function renderGroup(messages, who) {
    const inner = el("div", "thread-inner");
    if (!messages || !messages.length) {
      inner.append(emptyState(who || "群", "这个群还没有消息。"));
      thread.replaceChildren(inner);
      return;
    }
    for (const m of messages) {
      const mine = m.kind === "person";
      const row = el("div", "msg " + (mine ? "user" : "bot"));
      if (!mine) row.append(avatar(m.sender || "B"));
      const body = mine ? document.createElement("div") : el("div", "body");
      if (!mine) body.append(el("div", "who", m.sender || "bot"));
      const bubble = mine ? el("div", "bubble") : el("div", "prose");
      if (mine) bubble.textContent = m.text || "";
      else appendMarkdown(bubble, m.text || "");
      body.append(bubble, el("div", "meta", when(m.time)));
      row.append(body);
      inner.append(row);
    }
    thread.replaceChildren(inner);
    thread.scrollTop = thread.scrollHeight;
  }

  function setHeader(name, sub) {
    titleEl.textContent = name || "选择一个 Bot";
    subtitle.textContent = sub || "";
    document.title = name ? name + " · XBot" : "XBot";
  }

  async function selectBot(name) {
    state.sel = { type: "bot", id: name };
    state.sig = "";
    closeDrawer();
    const bot = state.bots.find((b) => b.name === name);
    setHeader(name, bot && bot.persona ? bot.persona : "");
    input.placeholder = "写给 " + name;
    renderLists();
    await loadThread();
    if (panel.classList.contains("open")) await loadPanel();
  }

  async function selectGroup(id) {
    state.sel = { type: "group", id: id };
    state.sig = "";
    closeDrawer();
    const g = state.groups.find((x) => x.id === id);
    setHeader(g ? g.name : id, g ? (g.members || []).join(" · ") : "");
    input.placeholder = "写到这个群";
    renderLists();
    await loadThread();
    if (panel.classList.contains("open")) renderGroupPanel(g);
  }

  async function loadThread() {
    if (!state.sel || state.streaming) return;
    if (state.sel.type === "bot") {
      const data = await api("/api/bots/" + encodeURIComponent(state.sel.id) + "/transcript");
      const sig = JSON.stringify(data.messages || []);
      if (sig === state.sig) return;
      state.sig = sig;
      renderMessages(data.messages || [], state.sel.id);
      await refreshLists();
      return;
    }
    const data = await api("/api/groups/" + encodeURIComponent(state.sel.id));
    const sig = JSON.stringify(data.messages || []);
    if (sig === state.sig) return;
    state.sig = sig;
    renderGroup(data.messages || [], data.group && data.group.name);
  }

  function section(title) {
    const box = el("div", "section");
    box.append(el("h3", "", title));
    return box;
  }

  function renderGroupPanel(g) {
    panelBody.replaceChildren();
    if (!g) return;
    const s = section("群");
    s.append(el("p", "", g.name || g.id));
    s.append(el("p", "meta", (g.members || []).join("、")));
    const note = el("p", "meta", "成员只看到群里的消息，看不到彼此的私人记忆。");
    panelBody.append(s, note);
  }

  async function loadPanel() {
    if (!state.sel) return;
    if (state.sel.type === "group") {
      renderGroupPanel(state.groups.find((g) => g.id === state.sel.id));
      return;
    }
    const data = await api("/api/bots/" + encodeURIComponent(state.sel.id));
    panelBody.replaceChildren();
    const persona = section("Persona");
    persona.append(el("p", "", data.persona || ""));
    const bind = section("模型");
    const provider = data.provider || "环境变量";
    const model = data.model || "默认";
    bind.append(el("p", "", provider + " / " + model));
    const mem = section("记忆");
    const facts = data.memory || [];
    if (!facts.length) mem.append(el("p", "meta", "还没有 profile。"));
    for (const f of facts) {
      const row = el("div", "fact");
      row.append(el("span", "tag info", f.scope || ""), document.createTextNode(" " + (f.text || "")));
      mem.append(row);
    }
    const routines = section("Routines");
    const list = data.routines || [];
    if (!list.length) routines.append(el("p", "meta", "没有 routine。"));
    for (const rt of list) {
      const row = el("div", "fact");
      row.append(el("span", "", rt.name || rt.id), el("div", "meta", (rt.cron || "") + (rt.enabled ? "" : " · 停用")));
      routines.append(row);
    }
    const agents = section("子代理");
    const running = data.agents || [];
    if (!running.length) agents.append(el("p", "meta", "没有在跑的子代理。"));
    for (const ag of running) {
      const card = el("div", "agent");
      const head = document.createElement("div");
      const tagClass = ag.status === "done" ? "tag ok" : ag.status === "failed" ? "tag warn" : "tag info";
      head.append(el("span", tagClass, ag.status || "running"), document.createTextNode("  " + (ag.task || ag.id)));
      card.append(head);
      const row = el("div", "row");
      const field = document.createElement("input");
      field.placeholder = "steer";
      field.maxLength = 4000;
      const go = el("button", "secondary", "Steer");
      go.type = "button";
      go.addEventListener("click", () => controlAgent(ag.id, "steer", field.value));
      const stop = el("button", "secondary", "Cancel");
      stop.type = "button";
      stop.addEventListener("click", () => controlAgent(ag.id, "cancel", ""));
      row.append(field, go, stop);
      card.append(row);
      agents.append(card);
    }
    panelBody.append(persona, bind, mem, routines, agents);
  }

  async function controlAgent(id, kind, text) {
    if (!state.sel || state.sel.type !== "bot") return;
    const path = "/api/bots/" + encodeURIComponent(state.sel.id) + "/agents/" + encodeURIComponent(id) + "/" + kind;
    try {
      if (kind === "cancel") await api(path, { method: "POST" });
      else await api(path, { method: "POST", body: { text: text } });
      subtitle.textContent = kind === "cancel" ? "已取消" : "已发送 steer";
      await loadPanel();
    } catch (e) {
      subtitle.textContent = e.message || "失败";
    }
  }

  function closeDrawer() {
    sidebar.classList.remove("open");
    if (window.innerWidth >= 1100) backdrop.hidden = true;
    else if (!panel.classList.contains("open")) backdrop.hidden = true;
  }

  function syncBackdrop() {
    const mobile = window.innerWidth < 1100;
    const open = sidebar.classList.contains("open") || (mobile && panel.classList.contains("open"));
    backdrop.hidden = !open;
  }

  async function refreshLists() {
    const data = await api("/api/bots");
    const groups = await api("/api/groups");
    state.bots = data.bots || [];
    state.groups = groups.groups || [];
    renderLists();
  }

  async function refreshNotes() {
    const data = await api("/api/notes");
    const notes = data.notes || [];
    if (state.knownNotes) {
      const seen = new Set(state.knownNotes);
      for (const n of notes) {
        if (!seen.has(n.id)) pushNote(n.bot || "XBot", n.text || "", n.id);
      }
    }
    state.knownNotes = notes.map((n) => n.id);
    state.notes = notes;
    renderNotes();
  }

  function pushNote(title, body, tag) {
    if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
    try {
      if (navigator.serviceWorker && navigator.serviceWorker.controller) {
        navigator.serviceWorker.controller.postMessage({ type: "notify", title: title, body: body, tag: tag });
        return;
      }
      new Notification(title, { body: body, tag: tag });
    } catch (e) {}
  }

  function liveBubble() {
    let row = thread.querySelector(".msg.live");
    if (row) return row.querySelector(".prose");
    const inner = thread.querySelector(".thread-inner") || thread;
    if (inner.querySelector(".empty")) inner.replaceChildren();
    row = el("div", "msg bot live");
    const who = state.sel ? state.sel.id : "Bot";
    row.append(avatar(who));
    const body = el("div", "body");
    body.append(el("div", "who", who));
    const prose = el("div", "prose");
    body.append(prose);
    row.append(body);
    inner.append(row);
    thread.scrollTop = thread.scrollHeight;
    return prose;
  }

  async function readSSE(res) {
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
    let acc = "";
    const prose = liveBubble();
    while (true) {
      const step = await reader.read();
      if (step.done) break;
      buf += dec.decode(step.value, { stream: true });
      let cut;
      while ((cut = buf.indexOf("\n\n")) >= 0) {
        const block = buf.slice(0, cut);
        buf = buf.slice(cut + 2);
        let event = "message";
        let data = "";
        for (const line of block.split("\n")) {
          if (line.startsWith("event:")) event = line.slice(6).trim();
          else if (line.startsWith("data:")) data += line.slice(5).trim();
        }
        if (!data) continue;
        let payload = {};
        try { payload = JSON.parse(data); } catch (e) { continue; }
        if (event === "delta" && payload.text) {
          acc += payload.text;
          prose.replaceChildren();
          appendMarkdown(prose, acc);
          thread.scrollTop = thread.scrollHeight;
        } else if (event === "tool") {
          const card = el("div", "card");
          card.append(el("div", "card-title", payload.name || "tool"));
          if (payload.summary) card.append(el("div", "card-sub", payload.summary));
          prose.parentElement.append(card);
        } else if (event === "error") {
          subtitle.textContent = payload.error || "出错了";
        }
      }
    }
  }

  async function sendText(text) {
    text = (text || "").trim();
    if (!text || !state.sel || state.streaming) return;
    state.streaming = true;
    $("send").disabled = true;
    subtitle.textContent = "正在回复";
    try {
      if (state.sel.type === "group") {
        await api("/api/groups/" + encodeURIComponent(state.sel.id) + "/messages", { method: "POST", body: { text: text } });
        state.sig = "";
        await loadThread();
        subtitle.textContent = "已送到成员的 inbox";
        return;
      }
      const res = await fetch("/api/bots/" + encodeURIComponent(state.sel.id) + "/messages", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text: text }),
      });
      const ct = res.headers.get("content-type") || "";
      if (ct.includes("application/json")) {
        const data = await res.json().catch(() => ({}));
        if (res.status === 401) { showLogin(); return; }
        if (!res.ok) throw new Error(data.error || "发送失败");
        subtitle.textContent = data.status === "queued" ? "已排队，终端正在处理" : "";
      } else if (!res.ok) {
        throw new Error("发送失败");
      } else {
        await readSSE(res);
      }
      state.sig = "";
      await loadThread();
      if (subtitle.textContent === "正在回复") subtitle.textContent = "";
    } catch (e) {
      subtitle.textContent = e.message || "发送失败";
    } finally {
      state.streaming = false;
      $("send").disabled = false;
    }
  }

  function closeModal() {
    modal.hidden = true;
    modalError.hidden = true;
    modalError.textContent = "";
  }

  function openModal(title, fields, onOk) {
    $("modal-title").textContent = title;
    modalFields.replaceChildren();
    modalError.hidden = true;
    const inputs = {};
    for (const f of fields) {
      const label = el("label", "field");
      label.append(el("span", "", f.label));
      if (f.type === "checks") {
        const box = el("div", "checks");
        inputs[f.name] = [];
        for (const opt of f.options || []) {
          const row = el("label", "");
          const cb = document.createElement("input");
          cb.type = "checkbox";
          cb.value = opt;
          cb.addEventListener("change", () => {
            inputs[f.name] = Array.from(box.querySelectorAll("input:checked")).map((n) => n.value);
          });
          row.append(cb, document.createTextNode(" " + opt));
          box.append(row);
        }
        label.append(box);
      } else if (f.multiline) {
        const ta = document.createElement("textarea");
        ta.rows = 3;
        ta.placeholder = f.placeholder || "";
        ta.maxLength = 4000;
        inputs[f.name] = ta;
        label.append(ta);
      } else {
        const inp = document.createElement("input");
        inp.placeholder = f.placeholder || "";
        inp.maxLength = 64;
        inp.autocomplete = "off";
        inputs[f.name] = inp;
        label.append(inp);
      }
      modalFields.append(label);
    }
    modal.hidden = false;
    modal.onsubmit = async (ev) => {
      ev.preventDefault();
      const values = {};
      for (const key of Object.keys(inputs)) {
        const node = inputs[key];
        values[key] = Array.isArray(node) ? node.slice() : node.value;
      }
      try {
        await onOk(values);
        closeModal();
      } catch (e) {
        modalError.hidden = false;
        modalError.textContent = e.message || "失败";
      }
    };
  }

  $("login-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const err = $("login-error");
    err.hidden = true;
    try {
      const res = await fetch("/api/login", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: $("token").value.trim() }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error === "unauthorized" ? "token 不对" : (data.error || "登录失败"));
      showApp();
      await boot();
      startPoll();
    } catch (e) {
      err.hidden = false;
      err.textContent = e.message || "登录失败";
    }
  });

  $("logout").addEventListener("click", async () => {
    try { await api("/api/logout", { method: "POST" }); } catch (e) {}
    state.sel = null;
    state.knownNotes = null;
    showLogin();
  });

  $("theme").addEventListener("click", () => {
    const dark = document.documentElement.getAttribute("data-theme") === "dark";
    applyTheme(dark ? "light" : "dark");
  });

  $("collapse").addEventListener("click", () => {
    if (window.innerWidth < 768) {
      closeDrawer();
      return;
    }
    sidebar.classList.toggle("collapsed");
  });

  $("menu").addEventListener("click", () => {
    sidebar.classList.add("open");
    syncBackdrop();
  });

  backdrop.addEventListener("click", () => {
    sidebar.classList.remove("open");
    panel.classList.remove("open");
    backdrop.hidden = true;
  });

  $("info").addEventListener("click", async () => {
    panel.classList.toggle("open");
    syncBackdrop();
    if (panel.classList.contains("open")) {
      try { await loadPanel(); } catch (e) { subtitle.textContent = e.message || ""; }
    }
  });

  $("panel-close").addEventListener("click", () => {
    panel.classList.remove("open");
    syncBackdrop();
  });

  $("bell").addEventListener("click", async () => {
    if (typeof Notification !== "undefined" && Notification.permission === "default") {
      try { await Notification.requestPermission(); } catch (e) {}
    }
    notesEl.hidden = state.notes.length === 0;
    if (!notesEl.hidden) notesEl.scrollIntoView({ block: "nearest" });
  });

  $("new-bot").addEventListener("click", () => {
    openModal("新建 Bot", [
      { name: "name", label: "名字", placeholder: "ada" },
      { name: "persona", label: "Persona", placeholder: "你是一个简短的助手。", multiline: true },
    ], async (v) => {
      const name = (v.name || "").trim();
      if (!name) throw new Error("需要一个名字");
      await api("/api/bots", { method: "POST", body: { name: name, persona: (v.persona || "").trim() } });
      await refreshLists();
      await selectBot(name);
    });
  });

  $("new-group").addEventListener("click", () => {
    openModal("新建群", [
      { name: "name", label: "名字", placeholder: "晨会" },
      { name: "members", label: "成员", type: "checks", options: state.bots.map((b) => b.name) },
    ], async (v) => {
      const members = v.members || [];
      if (members.length < 2) throw new Error("至少选两个 Bot");
      const g = await api("/api/groups", { method: "POST", body: { name: (v.name || "").trim(), members: members } });
      await refreshLists();
      if (g.id) await selectGroup(g.id);
    });
  });

  $("modal-cancel").addEventListener("click", closeModal);

  $("composer").addEventListener("submit", (ev) => {
    ev.preventDefault();
    const text = input.value;
    input.value = "";
    input.style.height = "auto";
    sendText(text);
  });

  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && !ev.shiftKey) {
      ev.preventDefault();
      $("composer").requestSubmit();
    }
  });

  input.addEventListener("input", () => {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 160) + "px";
  });

  async function boot() {
    await refreshLists();
    await refreshNotes();
    if (!state.sel && state.bots[0]) await selectBot(state.bots[0].name);
    else if (!state.sel) {
      thread.replaceChildren(el("div", "thread-inner"));
      thread.querySelector(".thread-inner").append(emptyState("XBot", "新建一个 Bot，对话会一直留在这里。"));
    }
  }

  initTheme();
  thread.replaceChildren();
  const hold = el("div", "thread-inner");
  hold.append(emptyState("XBot", "登录之后，对话在这里继续。"));
  thread.append(hold);

  if ("serviceWorker" in navigator) {
    navigator.serviceWorker.register("/sw.js").catch(() => {});
  }

  function startPoll() {
    if (startPoll.started) return;
    startPoll.started = true;
    setInterval(() => {
      if (shell.hidden || state.streaming) return;
      refreshNotes().catch(() => {});
      refreshLists().catch(() => {});
      loadThread().catch(() => {});
    }, 3000);
  }

  api("/api/session").then(async () => {
    showApp();
    await boot();
    startPoll();
  }).catch(() => showLogin());
})();
