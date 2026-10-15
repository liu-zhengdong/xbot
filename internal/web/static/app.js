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
  const modalForm = $("modal-form");
  const modalFields = $("modal-fields");
  const modalError = $("modal-error");
  const composer = $("composer");
  const lightbox = $("lightbox");

  const state = {
    bots: [],
    groups: [],
    notes: [],
    sel: null,
    sig: "",
    streaming: false,
    knownNotes: null,
    owner: "",
    preview: {},
    previewAt: {},
    listSig: "",
    tab: "details",
    panelData: null,
    panelTouched: false,
  };

  const FACES = [
    { bg: "#E4CDB8", shape: "M40 6C58 4 74 16 74 36C74 58 58 76 38 74C16 72 4 56 8 36C12 16 24 8 40 6Z", eyes: [30, 36, 50, 36] },
    { bg: "#C9D7CE", shape: "M28 8C48 2 70 14 72 34C74 56 58 74 36 76C14 78 4 60 6 40C8 18 14 12 28 8Z", eyes: [28, 34, 48, 38] },
    { bg: "#E3D0D6", shape: "M44 8C62 12 76 26 72 44C68 64 50 76 32 74C12 72 4 54 8 36C12 16 26 4 44 8Z", eyes: [32, 38, 52, 34] },
    { bg: "#D5D8E4", shape: "M36 4C54 6 72 8 76 28C80 48 66 72 44 76C22 80 6 64 6 42C6 20 18 2 36 4Z", eyes: [29, 36, 49, 36] },
    { bg: "#E6D7B8", shape: "M40 8C56 2 74 18 72 38C70 58 54 74 34 74C14 74 6 56 10 36C14 16 24 12 40 8Z", eyes: [31, 37, 51, 33] },
    { bg: "#C9D6DE", shape: "M32 6C52 4 70 10 74 30C78 52 60 76 38 74C16 72 4 54 8 34C12 14 18 8 32 6Z", eyes: [27, 35, 47, 37] },
    { bg: "#E5D3C4", shape: "M42 5C60 8 76 20 74 40C72 60 54 78 34 74C12 70 2 52 8 32C14 12 24 2 42 5Z", eyes: [30, 34, 50, 38] },
    { bg: "#D4DDD4", shape: "M38 8C54 2 72 14 74 34C76 56 58 74 38 76C16 78 4 58 8 38C12 16 22 12 38 8Z", eyes: [29, 38, 49, 34] },
  ];

  const THEME_MOON = '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>';
  const THEME_SUN = '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>';

  function applyTheme(theme) {
    const dark = theme === "dark";
    document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
    const meta = $("theme-color");
    if (meta) meta.setAttribute("content", dark ? "#161618" : "#ffffff");
    const icon = $("theme-icon");
    if (icon) icon.innerHTML = dark ? THEME_SUN : THEME_MOON;
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

  function clip(s, n) {
    s = String(s || "").replace(/\s+/g, " ").trim();
    if (s.length <= n) return s;
    return s.slice(0, n) + "…";
  }

  function when(t) {
    if (!t) return "";
    const d = new Date(t);
    if (Number.isNaN(d.getTime())) return "";
    const hm = String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
    const now = new Date();
    const start = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
    const days = Math.round((start(now) - start(d)) / 86400000);
    if (days === 0) return hm;
    if (days === 1) return "昨天 " + hm;
    return (d.getMonth() + 1) + "月" + d.getDate() + "日";
  }

  function cronText(cron, enabled) {
    const raw = String(cron || "").trim();
    const parts = raw.split(/\s+/);
    let label = raw;
    if (parts.length >= 5) {
      const min = parts[0];
      const hour = parts[1];
      const dom = parts[2];
      const mon = parts[3];
      const dow = parts[4];
      const week = { 0: "周日", 7: "周日", 1: "周一", 2: "周二", 3: "周三", 4: "周四", 5: "周五", 6: "周六" };
      if (/^\d+$/.test(min) && /^\d+$/.test(hour)) {
        const hm = String(hour).padStart(2, "0") + ":" + String(min).padStart(2, "0");
        if (mon === "*" && dom === "*" && dow === "*") label = "每天 " + hm;
        else if (mon === "*" && dom === "*" && (dow === "1-5" || dow === "1,2,3,4,5")) label = "工作日 " + hm;
        else if (mon === "*" && dom === "*" && week[dow]) label = "每" + week[dow] + " " + hm;
        else if (mon === "*" && dow === "*" && /^\d+$/.test(dom)) label = "每月" + Number(dom) + "日 " + hm;
      }
    }
    if (enabled === false) label += (label ? " · " : "") + "停用";
    return label;
  }

  function hashName(name) {
    let h = 2166136261;
    const s = String(name || "?");
    for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
    return h >>> 0;
  }

  const ICONS = {
    terminal: '<polyline points="4 17 10 11 4 5"/><path d="M12 19h8"/>',
    copy: '<rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
    chevron: '<path d="m9 18 6-6-6-6"/>',
    wrench: '<path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
  };

  function icon(name, extra) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("aria-hidden", "true");
    svg.classList.add("icon");
    if (extra) svg.classList.add(extra);
    svg.innerHTML = ICONS[name] || ICONS.wrench;
    return svg;
  }

  function faceFor(name, paint) {
    const face = FACES[hashName(name) % FACES.length];
    const bg = (paint && paint.bg) || face.bg;
    const eye = (paint && paint.ink) || "#3A342F";
    const e = face.eyes;
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 80 80");
    svg.setAttribute("aria-hidden", "true");
    const blob = document.createElementNS("http://www.w3.org/2000/svg", "path");
    blob.setAttribute("d", face.shape);
    blob.setAttribute("fill", bg);
    const a = document.createElementNS("http://www.w3.org/2000/svg", "ellipse");
    a.setAttribute("cx", e[0]); a.setAttribute("cy", e[1]); a.setAttribute("rx", "3.5"); a.setAttribute("ry", "4.1"); a.setAttribute("fill", eye);
    const b = document.createElementNS("http://www.w3.org/2000/svg", "ellipse");
    b.setAttribute("cx", e[2]); b.setAttribute("cy", e[3]); b.setAttribute("rx", "3.5"); b.setAttribute("ry", "4.1"); b.setAttribute("fill", eye);
    const mouth = document.createElementNS("http://www.w3.org/2000/svg", "path");
    const my = 50 + (hashName(name) % 3);
    mouth.setAttribute("d", "M" + (e[0] + 2) + " " + my + " Q40 " + (my + 5) + " " + (e[2] - 2) + " " + my);
    mouth.setAttribute("fill", "none");
    mouth.setAttribute("stroke", eye);
    mouth.setAttribute("stroke-width", "2");
    mouth.setAttribute("stroke-linecap", "round");
    svg.append(blob, a, b, mouth);
    return svg;
  }

  function avatar(name, paint, size) {
    paint = paint || {};
    const av = el("span", "avatar" + (size ? " " + size : ""));
    av.append(faceFor(name, paint));
    if (paint.url) {
      const img = document.createElement("img");
      img.alt = "";
      img.src = paint.url;
      img.addEventListener("load", () => av.classList.add("has-photo"));
      img.addEventListener("error", () => img.remove());
      av.append(img);
    }
    return av;
  }

  function paintOf(name) {
    const b = (state.bots || []).find((x) => x.name === name);
    if (!b) return null;
    return { bg: b.avatar_bg, ink: b.avatar_ink, url: b.avatar_url };
  }

  function copyText(text, btn) {
    const done = () => {
      if (!btn) return;
      btn.setAttribute("aria-label", "已复制");
      setTimeout(() => btn.setAttribute("aria-label", "复制"), 1200);
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
    if (!m) return { text: text, untrusted: false, source: "", sender: "" };
    const source = /source="([^"]*)"/.exec(m[1]);
    const sender = /sender="([^"]*)"/.exec(m[1]);
    return {
      text: m[2],
      untrusted: true,
      source: source ? source[1] : "",
      sender: sender ? sender[1] : "",
    };
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
        if (lm) {
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
        flushPara(); flushList();
        const p = document.createElement("p");
        p.style.fontWeight = "600";
        appendInline(p, h[2]);
        parent.append(p);
        continue;
      }
      const li = /^[-*]\s+(.*)$/.exec(line);
      if (li) {
        flushPara();
        if (!list) list = document.createElement("ul");
        const row = document.createElement("li");
        appendInline(row, li[1]);
        list.append(row);
        continue;
      }
      if (line.trim() === "") { flushPara(); flushList(); continue; }
      flushList();
      para.push(line);
    }
    flushPara();
    flushList();
  }

  function highlight(codeEl, text, lang) {
    const use = /go|js|ts|java|py/.test(lang || "");
    if (!use) { codeEl.textContent = text; return; }
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

  function safeImage(url) {
    if (/^data:image\/(png|jpeg|jpg|gif|webp);base64,[a-z0-9+/=\s]+$/i.test(url)) return true;
    try {
      const u = new URL(url, location.origin);
      return u.protocol === "https:" || u.protocol === "http:";
    } catch (e) { return false; }
  }

  function pullImages(text) {
    const images = [];
    let rest = String(text || "").replace(/!\[([^\]]*)\]\(([^)\s]+)\)/g, (all, alt, url) => {
      if (safeImage(url)) { images.push(url); return ""; }
      return all;
    });
    rest = rest.replace(/(https?:\/\/[^\s)]+?\.(?:png|jpe?g|gif|webp))(?:\?[^\s)]*)?/gi, (url) => {
      if (safeImage(url)) { images.push(url); return ""; }
      return url;
    });
    return { text: rest.replace(/\n{3,}/g, "\n\n").trim(), images: images };
  }

  function gallery(urls) {
    const box = el("div", "gallery");
    const extra = urls.length > 3 ? urls.length - 2 : 0;
    const show = extra ? urls.slice(0, 3) : urls;
    show.forEach((url, i) => {
      const cell = el("button", "thumb");
      cell.type = "button";
      const img = document.createElement("img");
      img.alt = "";
      img.src = url;
      img.addEventListener("error", () => { img.remove(); cell.classList.add("broken"); });
      cell.append(img);
      if (extra && i === show.length - 1) {
        cell.classList.add("more");
        cell.append(el("span", "plus", "+" + extra));
      }
      cell.addEventListener("click", () => openShots(urls));
      box.append(cell);
    });
    return box;
  }

  function openShots(urls) {
    const grid = el("div", "grid");
    for (const url of urls) {
      const img = document.createElement("img");
      img.alt = "";
      img.src = url;
      grid.append(img);
    }
    lightbox.replaceChildren(grid);
    lightbox.hidden = false;
  }

  function closeShots() {
    lightbox.hidden = true;
    lightbox.replaceChildren();
  }

  const HEAT = [
    "010010100010010010",
    "021302110231021102",
    "132021302110320211",
    "213021132021103201",
    "102310211320211032",
    "021102130210211021",
    "010010010100010010",
  ];

  function heat() {
    const card = el("div", "heat-card");
    card.append(el("div", "heat-title", "活跃对话"));
    const months = el("div", "heat-months");
    months.append(el("span", "", "8月"), el("span", "", "9月"), el("span", "", "10月"));
    const wrap = el("div", "heat");
    wrap.setAttribute("aria-hidden", "true");
    const levels = ["", "l1", "l2", "l3"];
    for (const row of HEAT) {
      for (const ch of row) {
        const cell = document.createElement("i");
        const level = levels[Number(ch)] || "";
        if (level) cell.className = level;
        wrap.append(cell);
      }
    }
    card.append(months, wrap);
    return card;
  }

  function emptyState(heading, line, who) {
    const box = el("div", "empty");
    if (who) box.append(avatar(who, paintOf(who), "lg"));
    box.append(el("h2", "", heading), el("p", "", line), heat());
    return box;
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

  function peerPhrase(name, args, who) {
    const text = clip(args.text || args.prompt || "", 72);
    if (name === "message_bot") {
      const to = args.bot || args.to || "";
      if (!to) return (who || "Bot") + (text ? " · " + text : " 发给了另一个 Bot");
      return (who || "Bot") + " 发给 " + to + (text ? " · " + text : "");
    }
    if (name === "post_group") {
      const g = args.name || args.id || "群";
      return (who || "Bot") + " 发到 " + g + (text ? " · " + text : "");
    }
    if (name === "create_group") return (who || "Bot") + " 建了群" + (args.name ? " · " + args.name : "");
    if (name === "fork_bot") return (who || "Bot") + " 分叉出 " + (args.name || "新 Bot");
    return "";
  }

  function toolIconName(name) {
    return /shell|bash|exec|terminal/i.test(name || "") ? "terminal" : "wrench";
  }

  function toolShell(name, line, resultText) {
    const card = document.createElement("details");
    card.className = "card";
    const sum = document.createElement("summary");
    sum.className = "card-head";
    const well = el("span", "card-icon");
    well.append(icon(toolIconName(name)));
    const text = el("span", "card-copy");
    text.append(el("div", "card-title", name || "tool"));
    if (line) text.append(el("div", "card-sub", line));
    sum.append(well, text, icon("chevron", "chev"));
    card.append(sum);
    if (resultText) {
      const pre = document.createElement("pre");
      pre.textContent = resultText;
      card.append(pre);
    }
    return card;
  }

  function copyBtn(text) {
    const btn = el("button", "copy");
    btn.type = "button";
    btn.setAttribute("aria-label", "复制");
    btn.title = "复制";
    btn.append(icon("copy"));
    btn.addEventListener("click", () => copyText(text || "", btn));
    return btn;
  }

  function systemLine(text, who) {
    const row = el("div", "sysline");
    row.title = text;
    if (who) row.append(avatar(who, paintOf(who), "xs"));
    row.append(el("span", "label", text));
    return row;
  }

  function optionCards(info, picked) {
    const box = el("div", "options");
    const opts = Array.isArray(info.args.options) ? info.args.options : [];
    for (const raw of opts) {
      let label = String(raw);
      let recommend = false;
      if (label.startsWith("* ")) { recommend = true; label = label.slice(2); }
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

  function appendRich(parent, text) {
    const src = String(text || "");
    const re = /```([^\n`]*)\n?([\s\S]*?)```/g;
    let last = 0;
    let match;
    const addText = (chunk) => {
      const pulled = pullImages(chunk);
      if (pulled.text) {
        const bubble = el("div", "bubble");
        const prose = el("div", "prose");
        appendMarkdown(prose, pulled.text);
        bubble.append(prose);
        parent.append(bubble);
      }
      if (pulled.images.length) parent.append(gallery(pulled.images));
    };
    while ((match = re.exec(src))) {
      if (match.index > last) addText(src.slice(last, match.index));
      parent.append(codeBlock(match[1], match[2]));
      last = match.index + match[0].length;
    }
    if (last < src.length) addText(src.slice(last));
  }

  function userBubble(m) {
    const parsed = unwrap(m.content || "");
    const row = el("div", "msg user");
    const wrap = el("div", "bubble-col");
    const pulled = pullImages(parsed.text);
    if (pulled.text) {
      const bubble = el("div", "bubble");
      bubble.textContent = pulled.text;
      wrap.append(bubble);
    }
    if (pulled.images.length) wrap.append(gallery(pulled.images));
    if (!pulled.text && !pulled.images.length) {
      const bubble = el("div", "bubble");
      bubble.textContent = parsed.text;
      wrap.append(bubble);
    }
    const metaRow = el("div", "meta-row");
    if (parsed.untrusted && !parsed.source.startsWith("bot:")) {
      metaRow.append(el("span", "tag warn", parsed.source ? "不可信 · " + parsed.source : "不可信"));
    }
    metaRow.append(el("span", "meta", when(m.time)), copyBtn(parsed.text));
    wrap.append(metaRow);
    row.append(wrap);
    return row;
  }

  function botBubble(m, results, messages, index, who, showName) {
    const row = el("div", "msg bot");
    row.append(avatar(who, paintOf(who)));
    const body = el("div", "body");
    if (showName) body.append(el("div", "who", who));
    if (m.content) appendRich(body, m.content);
    const calls = m.tool_calls || [];
    const peers = [];
    for (const call of calls) {
      const info = toolSummary(call);
      const phrase = peerPhrase(info.name, info.args, who);
      if (phrase) { peers.push(phrase); continue; }
      if (info.name === "ask_user") {
        if (info.args.question && !(m.content || "").includes(info.args.question)) {
          const bubble = el("div", "bubble");
          const q = document.createElement("p");
          q.textContent = info.args.question;
          bubble.append(q);
          body.append(bubble);
        }
        body.append(optionCards(info, laterUser(messages, index)));
        continue;
      }
      body.append(toolShell(info.name, info.line, results[call.id] && results[call.id].content));
    }
    const metaRow = el("div", "meta-row");
    metaRow.append(el("span", "meta", when(m.time)), copyBtn(m.content || ""));
    body.append(metaRow);
    row.append(body);
    return { row: row, peers: peers };
  }

  function skipMessage(m) {
    if (!m || m.role === "tool" || m.role === "system") return true;
    const c = m.content || "";
    if (m.role === "user" && (c.startsWith("<environment_") || c.startsWith("<memory>"))) return true;
    return false;
  }

  function renderMessages(messages, who) {
    const inner = el("div", "thread-inner");
    const prevTop = thread.scrollTop;
    const near = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (!messages || !messages.length) {
      inner.append(emptyState(who || "XBot", "还没有消息。写一句，对话会留在这里。", who));
      thread.replaceChildren(inner);
      return;
    }
    const results = {};
    for (const m of messages) {
      if (m.role === "tool" && m.tool_call_id) results[m.tool_call_id] = m;
    }
    let any = false;
    let lastBot = "";
    messages.forEach((m, i) => {
      if (skipMessage(m)) return;
      if (m.role === "user") {
        const parsed = unwrap(m.content || "");
        if (parsed.untrusted && (parsed.source.startsWith("bot:") || parsed.source.startsWith("group:"))) {
          any = true;
          lastBot = "";
          const body = clip(pullImages(parsed.text).text, 80) || "一条消息";
          if (parsed.source.startsWith("bot:")) {
            const from = parsed.sender || parsed.source.slice(4);
            inner.append(systemLine(from + " 发来 · " + body, from));
          } else {
            inner.append(systemLine("群里 · " + body));
          }
          return;
        }
        any = true;
        lastBot = "";
        inner.append(userBubble(m));
        return;
      }
      any = true;
      const showName = who !== lastBot;
      lastBot = who;
      const built = botBubble(m, results, messages, i, who, showName);
      const empty = !m.content && !(m.tool_calls || []).some((c) => !peerPhrase(toolSummary(c).name, toolSummary(c).args, who));
      if (!empty) inner.append(built.row);
      for (const line of built.peers) inner.append(systemLine(line, who));
    });
    if (!any) inner.append(emptyState(who || "XBot", "还没有消息。写一句，对话会留在这里。", who));
    thread.replaceChildren(inner);
    thread.scrollTop = near ? thread.scrollHeight : Math.min(prevTop, thread.scrollHeight);
  }

  function renderGroup(messages, who) {
    const inner = el("div", "thread-inner");
    const prevTop = thread.scrollTop;
    const near = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (!messages || !messages.length) {
      inner.append(emptyState(who || "群", "还没有消息。写一句，对话会留在这里。", who));
      thread.replaceChildren(inner);
      return;
    }
    let last = "";
    for (const m of messages) {
      const mine = m.kind === "person";
      const text = m.text || "";
      if (!mine && m.kind === "bot" && last && last !== m.sender && mine === false) {
        // Bot speaking after another bot stays a bubble; the action line is the tool call.
      }
      if (!mine && /^\[action\]/i.test(text)) {
        inner.append(systemLine(clip((m.sender || "Bot") + " · " + text.replace(/^\[action\]\s*/i, ""), 96), m.sender));
        last = m.sender || "";
        continue;
      }
      const row = el("div", "msg " + (mine ? "user" : "bot"));
      if (mine) {
        const wrap = el("div", "bubble-col");
        const pulled = pullImages(text);
        if (pulled.text) {
          const bubble = el("div", "bubble");
          bubble.textContent = pulled.text;
          wrap.append(bubble);
        }
        if (pulled.images.length) wrap.append(gallery(pulled.images));
        if (!pulled.text && !pulled.images.length) wrap.append(el("div", "bubble", text));
        const metaRow = el("div", "meta-row");
        metaRow.append(el("span", "meta", when(m.time)), copyBtn(text));
        wrap.append(metaRow);
        row.append(wrap);
      } else {
        row.append(avatar(m.sender || "B", paintOf(m.sender)));
        const body = el("div", "body");
        if (m.sender !== last) body.append(el("div", "who", m.sender || "bot"));
        appendRich(body, text);
        const metaRow = el("div", "meta-row");
        metaRow.append(el("span", "meta", when(m.time)), copyBtn(text));
        body.append(metaRow);
        row.append(body);
      }
      last = mine ? "" : (m.sender || "");
      inner.append(row);
    }
    thread.replaceChildren(inner);
    thread.scrollTop = near ? thread.scrollHeight : Math.min(prevTop, thread.scrollHeight);
  }

  function previewFromMessages(messages) {
    if (!messages) return "";
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i];
      if (skipMessage(m)) continue;
      const calls = m.tool_calls || [];
      if (calls.length) {
        const info = toolSummary(calls[0]);
        const phrase = peerPhrase(info.name, info.args, "");
        if (phrase && !(m.content || "").trim()) return phrase.replace(/^Bot /, "");
      }
      const parsed = unwrap(m.content || "");
      const text = clip(pullImages(parsed.text).text, 72);
      if (text) return text;
      if (calls.length) return toolSummary(calls[0]).name;
    }
    return "";
  }

  function previewFromGroup(messages) {
    if (!messages || !messages.length) return "";
    const m = messages[messages.length - 1];
    const who = m.kind === "person" ? "你" : (m.sender || "");
    const text = clip(m.text || "", 64);
    if (!text) return "";
    return who ? who + "：" + text : text;
  }

  function lineForBot(b) {
    const p = state.preview["b:" + b.name];
    if (p) return p;
    if (b.description) return b.description;
    if (b.persona) return b.persona;
    return "还没有消息";
  }

  function lineForGroup(g) {
    const p = state.preview["g:" + g.id];
    if (p) return p;
    return (g.members || []).join(" · ") || "还没有消息";
  }

  function listButton(name, preview, active, unread, paint, primary) {
    const btn = el("button", "item" + (active ? " active" : ""));
    btn.type = "button";
    btn.title = name + (preview ? " — " + preview : "");
    const av = avatar(name, paint);
    if (unread) av.append(el("i", "unread"));
    const body = el("span", "item-body");
    const line = el("span", "name-line");
    line.append(el("span", "name", name));
    if (primary) line.append(el("span", "badge", "主"));
    body.append(line, el("span", "preview", preview || "还没有消息"));
    btn.append(av, body);
    return btn;
  }

  function sectionLabel(text, action) {
    const row = el("div", "group-label");
    row.append(el("span", "label", text));
    if (action) row.append(action);
    return row;
  }

  function renderLists() {
    const sig = JSON.stringify({
      bots: state.bots.map((b) => [b.name, !!b.pinned, !!b.primary, !!b.unread, b.description, lineForBot(b)]),
      groups: state.groups.map((g) => [g.id, g.name, lineForGroup(g)]),
      sel: state.sel,
    });
    if (sig === state.listSig) return;
    state.listSig = sig;
    const pins = $("pin-row");
    const roster = $("roster");
    pins.replaceChildren();
    roster.replaceChildren();
    const pinned = state.bots.filter((b) => b.pinned);
    const rest = state.bots.filter((b) => !b.pinned);
    for (const b of pinned) {
      const btn = el("button", "pin" + (state.sel && state.sel.type === "bot" && state.sel.id === b.name ? " active" : ""));
      btn.type = "button";
      btn.title = b.name;
      btn.setAttribute("aria-label", b.name);
      const av = avatar(b.name, { bg: b.avatar_bg, ink: b.avatar_ink, url: b.avatar_url }, "pin");
      if (b.unread) av.append(el("i", "unread"));
      btn.append(av);
      btn.addEventListener("click", () => selectBot(b.name));
      pins.append(btn);
    }
    const add = el("button", "pin add");
    add.type = "button";
    add.setAttribute("aria-label", "新建 Bot");
    add.title = "新建 Bot";
    const face = el("span", "plus-face");
    face.append(icon("plus"));
    add.append(face);
    add.addEventListener("click", openNewBot);
    pins.append(add);

    const addRow = (b) => {
      const btn = listButton(b.name, lineForBot(b), state.sel && state.sel.type === "bot" && state.sel.id === b.name, !!b.unread, {
        bg: b.avatar_bg, ink: b.avatar_ink, url: b.avatar_url,
      }, !!b.primary);
      btn.addEventListener("click", () => selectBot(b.name));
      roster.append(btn);
    };
    if (pinned.length) {
      roster.append(sectionLabel("置顶"));
      pinned.forEach(addRow);
    }
    if (rest.length) {
      roster.append(sectionLabel("Bot"));
      rest.forEach(addRow);
    }
    const newer = el("button", "icon-btn");
    newer.type = "button";
    newer.setAttribute("aria-label", "新建群组");
    newer.title = "新建群组";
    newer.append(icon("plus"));
    newer.addEventListener("click", openNewGroup);
    roster.append(sectionLabel("群组", newer));
    for (const g of state.groups) {
      const btn = listButton(g.name || g.id, lineForGroup(g), state.sel && state.sel.type === "group" && state.sel.id === g.id, false, null, false);
      btn.addEventListener("click", () => selectGroup(g.id));
      roster.append(btn);
    }
  }

  function renderMe() {
    const slot = $("me-avatar");
    slot.replaceChildren(avatar(state.owner || "me", null, "me"));
    const name = $("me-name");
    name.textContent = "我";
    name.title = state.owner || "";
  }

  function renderNotes() {
    bellDot.hidden = state.notes.length === 0;
    if (!state.notes.length) {
      notesEl.hidden = true;
      notesEl.replaceChildren();
      return;
    }
    const open = !notesEl.hidden;
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
    notesEl.hidden = !open;
  }

  async function openNote(n) {
    notesEl.hidden = true;
    try { await api("/api/notes/read", { method: "POST", body: { bot: n.bot, ids: [n.id] } }); } catch (e) {}
    await refreshNotes();
    await refreshLists();
    if (n.bot) await selectBot(n.bot);
  }

  function setHeader(name) {
    titleEl.textContent = name || "XBot";
    document.title = name ? name + " · XBot" : "XBot";
  }

  function setStatus(text) {
    let desc = "";
    if (!text && state.sel && state.sel.type === "bot") {
      const bot = (state.bots || []).find((b) => b.name === state.sel.id);
      desc = (bot && bot.description) || "";
    }
    const show = text || desc;
    subtitle.textContent = show;
    subtitle.hidden = !show;
  }

  async function selectBot(name) {
    state.sel = { type: "bot", id: name };
    state.sig = "";
    state.tab = state.tab || "details";
    closeDrawer();
    setHeader(name);
    setStatus("");
    syncMore();
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
    setHeader(g ? g.name : id);
    setStatus("");
    syncMore();
    input.placeholder = "写到这个群";
    renderLists();
    await loadThread();
    if (panel.classList.contains("open")) await loadPanel();
  }

  async function loadThread() {
    if (!state.sel || state.streaming) return;
    if (state.sel.type === "bot") {
      const data = await api("/api/bots/" + encodeURIComponent(state.sel.id) + "/transcript");
      const messages = data.messages || [];
      const sig = JSON.stringify(messages);
      state.preview["b:" + state.sel.id] = previewFromMessages(messages);
      state.previewAt["b:" + state.sel.id] = Date.now();
      renderLists();
      if (sig === state.sig) return;
      state.sig = sig;
      renderMessages(messages, state.sel.id);
      return;
    }
    const data = await api("/api/groups/" + encodeURIComponent(state.sel.id));
    const messages = data.messages || [];
    const sig = JSON.stringify(messages);
    state.preview["g:" + state.sel.id] = previewFromGroup(messages);
    state.previewAt["g:" + state.sel.id] = Date.now();
    renderLists();
    if (sig === state.sig) return;
    state.sig = sig;
    renderGroup(messages, data.group && data.group.name);
  }

  function section(title) {
    const box = el("div", "section");
    if (title) box.append(el("h3", "", title));
    return box;
  }

  function kv(k, v) {
    const row = el("div", "kv");
    row.append(el("div", "k", k), el("div", "v", v || "—"));
    return row;
  }

  const TABS = [["details", "详情"], ["library", "资料"], ["computer", "电脑"]];

  function tabBar() {
    const bar = el("div", "tabs");
    bar.setAttribute("role", "tablist");
    for (const pair of TABS) {
      const btn = el("button", state.tab === pair[0] ? "on" : "", pair[1]);
      btn.type = "button";
      btn.setAttribute("role", "tab");
      btn.setAttribute("aria-selected", state.tab === pair[0] ? "true" : "false");
      btn.addEventListener("click", () => {
        state.tab = pair[0];
        paintPanel();
      });
      bar.append(btn);
    }
    return bar;
  }

  function hero(name, desc, paint) {
    const box = el("div", "hero");
    box.append(avatar(name, paint, "lg"));
    box.append(el("h2", "", name || "Bot"));
    if (desc) box.append(el("p", "desc", desc));
    return box;
  }

  function paintPanel() {
    panelBody.replaceChildren();
    if (!state.sel) {
      panelBody.append(el("p", "hint", "先选一个 Bot。"));
      return;
    }
    if (state.sel.type === "group") {
      const g = state.groups.find((x) => x.id === state.sel.id) || { name: state.sel.id, members: [] };
      panelBody.append(hero(g.name || g.id, (g.members || []).join(" · "), null), tabBar());
      const body = el("div", "tab-body");
      if (state.tab === "library") {
        body.append(el("p", "hint", "群没有单独的资料。每人只看到群里的消息。"));
      } else if (state.tab === "computer") {
        body.append(el("p", "hint", "群没有自己的任务。任务在各个 Bot 的电脑页。"));
      } else {
        const s = section("成员");
        s.append(el("p", "", (g.members || []).join("、") || "还没有成员"));
        s.append(el("p", "hint", "成员只看到群里的消息，看不到彼此的私人记忆。"));
        body.append(s);
      }
      panelBody.append(body);
      return;
    }
    const data = state.panelData || { name: state.sel.id };
    const desc = data.description || "";
    panelBody.append(hero(data.name || state.sel.id, desc, {
      bg: data.avatar_bg, ink: data.avatar_ink, url: data.avatar_url,
    }), tabBar());
    const body = el("div", "tab-body");
    if (state.tab === "library") body.append(libraryView(data));
    else if (state.tab === "computer") body.append(computerView(data));
    else body.append(detailsView(data));
    panelBody.append(body);
  }

  function detailsView(data) {
    const wrap = document.createDocumentFragment();
    const style = data.style || {};
    const bits = [];
    if (style.tone) bits.push(style.tone);
    if (style.length) bits.push(style.length === "short" ? "短" : style.length === "long" ? "长" : style.length === "medium" ? "中" : style.length);
    if (style.language) bits.push(style.language);
    const pro = { quiet: "安静", normal: "适中", active: "积极" }[data.proactivity] || "";
    if (pro) bits.push(pro);
    if (data.primary) bits.push("主 Bot");
    const about = section("");
    if (data.persona) {
      const block = el("div", "kv");
      block.append(el("div", "k", "人格"), el("p", "clamp v", data.persona));
      about.append(block);
    }
    if (bits.length) about.append(kv("风格", bits.join(" · ")));
    about.append(kv("模型", (data.provider || "环境变量") + " / " + (data.model || "默认")));
    if (data.quiet_start || data.quiet_end) about.append(kv("免打扰", (data.quiet_start || "") + " – " + (data.quiet_end || "")));
    const actions = el("div", "actions");
    const pin = el("button", "secondary", data.pinned ? "取消置顶" : "置顶");
    pin.type = "button";
    pin.addEventListener("click", () => togglePin(state.sel.id, !data.pinned));
    const forkBtn = el("button", "secondary", "分叉");
    forkBtn.type = "button";
    forkBtn.addEventListener("click", () => openFork(state.sel.id));
    const exp = el("button", "secondary", "导出对话");
    exp.type = "button";
    exp.addEventListener("click", openExport);
    actions.append(pin, forkBtn, exp);
    about.append(actions);
    const edit = document.createElement("details");
    edit.className = "edit";
    const sum = document.createElement("summary");
    sum.textContent = "编辑档案";
    edit.append(sum, profileForm(data));
    about.append(edit);
    wrap.append(about);
    return wrap;
  }

  function scopeLabel(scope) {
    if (scope === "person") return "关于你";
    if (scope === "bot") return "关于 Bot";
    if (scope === "bot-person") return "你们之间";
    return scope || "资料";
  }

  function libraryView(data) {
    const wrap = document.createDocumentFragment();
    const facts = data.memory || [];
    if (!facts.length) wrap.append(el("p", "hint", "还没有记下的资料。"));
    for (const f of facts) {
      const card = el("div", "lib-card");
      card.append(el("div", "k", scopeLabel(f.scope)), el("div", "v", f.text || ""));
      wrap.append(card);
    }
    wrap.append(el("p", "hint", "本机文件还没有列表。Bot 直接读工作目录里的笔记。"));
    return wrap;
  }

  function computerView(data) {
    const wrap = document.createDocumentFragment();
    const tasks = section("任务");
    const running = data.agents || [];
    if (!running.length) tasks.append(el("p", "hint", "没有在跑的任务。"));
    for (const ag of running) {
      const card = el("div", "task");
      const top = el("div", "task-top");
      const labels = { running: "进行中", done: "完成", failed: "失败" };
      const tagClass = ag.status === "done" ? "tag ok" : ag.status === "failed" ? "tag warn" : "tag info";
      top.append(el("span", tagClass, labels[ag.status] || ag.status || "进行中"));
      card.append(top, el("p", "task-text", ag.task || ag.id));
      const row = el("div", "row");
      const field = document.createElement("input");
      field.placeholder = "插一句";
      field.maxLength = 4000;
      const go = el("button", "secondary", "引导");
      go.type = "button";
      go.addEventListener("click", () => controlAgent(ag.id, "steer", field.value));
      const stop = el("button", "text-btn danger", "停止");
      stop.type = "button";
      stop.addEventListener("click", () => controlAgent(ag.id, "cancel", ""));
      row.append(field, go, stop);
      card.append(row);
      tasks.append(card);
    }
    const routines = section("例程");
    const list = data.routines || [];
    if (!list.length) routines.append(el("p", "hint", "没有例程。"));
    for (const rt of list) {
      const card = el("div", "routine");
      card.append(el("span", "", rt.name || rt.id), el("span", "meta", cronText(rt.cron, rt.enabled)));
      routines.append(card);
    }
    wrap.append(tasks, routines);
    return wrap;
  }

  async function loadPanel() {
    if (!state.sel) { paintPanel(); return; }
    if (state.sel.type === "group") { state.panelData = null; paintPanel(); return; }
    state.panelData = await api("/api/bots/" + encodeURIComponent(state.sel.id));
    paintPanel();
  }

  function labeled(label, node) {
    const box = el("label", "field", label);
    box.append(node);
    return box;
  }

  function textInput(value, max) {
    const node = document.createElement("input");
    node.value = value || "";
    if (max) node.maxLength = max;
    return node;
  }

  function choice(options, value) {
    const node = document.createElement("select");
    for (const pair of options) {
      const o = document.createElement("option");
      o.value = pair[0];
      o.textContent = pair[1];
      if (pair[0] === (value || "")) o.selected = true;
      node.append(o);
    }
    return node;
  }

  function profileForm(data) {
    const wrap = document.createElement("div");
    const head = el("div", "profile-head");
    const mark = avatar(data.name, { bg: data.avatar_bg, ink: data.avatar_ink, url: data.avatar_url }, "lg");
    const file = document.createElement("input");
    file.type = "file";
    file.accept = "image/png,image/jpeg,image/gif,image/webp";
    file.hidden = true;
    const pick = el("button", "secondary", "上传头像");
    pick.type = "button";
    const clear = el("button", "secondary", "清除头像");
    clear.type = "button";
    const stack = el("div", "stack");
    stack.append(pick, clear, file);
    head.append(mark, stack);
    const style = data.style || {};
    const name = textInput(data.name, 64);
    const description = textInput(data.description, 160);
    const tone = textInput(style.tone, 80);
    const length = choice([["", "未设置"], ["short", "短"], ["medium", "中"], ["long", "长"]], style.length);
    const emoji = choice([["", "未设置"], ["none", "不用"], ["spare", "少量"], ["often", "常用"]], style.emoji);
    const language = textInput(style.language, 40);
    const pro = choice([["", "未设置"], ["quiet", "安静"], ["normal", "适中"], ["active", "积极"]], data.proactivity);
    const quietStart = textInput(data.quiet_start, 5);
    quietStart.type = "time";
    const quietEnd = textInput(data.quiet_end, 5);
    quietEnd.type = "time";
    const persona = document.createElement("textarea");
    persona.value = data.persona || "";
    persona.maxLength = 32000;
    const err = el("p", "error", "");
    err.hidden = true;
    const save = el("button", "secondary", "保存");
    save.type = "button";
    const exp = el("button", "secondary", "导出模板");
    exp.type = "button";
    const imp = el("button", "secondary", "导入模板");
    imp.type = "button";
    const fileIn = document.createElement("input");
    fileIn.type = "file";
    fileIn.accept = "application/json,.json";
    fileIn.hidden = true;
    const actions = el("div", "actions");
    actions.append(save, exp, imp, fileIn);
    wrap.append(
      head,
      labeled("名字", name),
      labeled("简介", description),
      labeled("语气", tone),
      labeled("回复长度", length),
      labeled("表情", emoji),
      labeled("语言", language),
      labeled("主动程度", pro),
      labeled("免打扰开始", quietStart),
      labeled("免打扰结束", quietEnd),
      labeled("人格", persona),
      err, actions,
    );
    const showErr = (e, fallback) => { err.hidden = false; err.textContent = (e && e.message) || fallback; };
    pick.addEventListener("click", () => file.click());
    file.addEventListener("change", async () => {
      if (!file.files || !file.files[0]) return;
      try {
        await uploadAvatar(data.name, file.files[0]);
        await refreshLists();
        await loadPanel();
      } catch (e) { showErr(e, "头像上传失败"); }
    });
    clear.addEventListener("click", async () => {
      try {
        await api("/api/bots/" + encodeURIComponent(data.name) + "/avatar", { method: "DELETE" });
        await refreshLists();
        await loadPanel();
      } catch (e) { showErr(e, "清除失败"); }
    });
    save.addEventListener("click", async () => {
      err.hidden = true;
      const body = {
        name: name.value.trim(),
        description: description.value.trim(),
        tone: tone.value.trim(),
        length: length.value,
        emoji: emoji.value,
        language: language.value.trim(),
        proactivity: pro.value,
        quiet_start: quietStart.value,
        quiet_end: quietEnd.value,
        persona: persona.value.trim(),
      };
      if ((body.quiet_start === "") !== (body.quiet_end === "")) {
        err.hidden = false;
        err.textContent = "免打扰需要开始和结束，或都留空。";
        return;
      }
      try {
        const res = await api("/api/bots/" + encodeURIComponent(data.name), { method: "PATCH", body: body });
        await refreshLists();
        const next = (res && res.name) || body.name || data.name;
        if (state.sel && next !== state.sel.id) await selectBot(next);
        else await loadPanel();
      } catch (e) { showErr(e, "保存失败"); }
    });
    exp.addEventListener("click", async () => {
      err.hidden = true;
      try { await downloadTemplate(data.name); }
      catch (e) { showErr(e, "导出失败"); }
    });
    imp.addEventListener("click", () => fileIn.click());
    fileIn.addEventListener("change", async () => {
      const f = fileIn.files && fileIn.files[0];
      fileIn.value = "";
      if (!f) return;
      err.hidden = true;
      try {
        const tpl = JSON.parse(await f.text());
        const res = await api("/api/bots/import", { method: "POST", body: tpl });
        await refreshLists();
        if (res && res.name) await selectBot(res.name);
      } catch (e) { showErr(e, "导入失败"); }
    });
    return wrap;
  }

  async function uploadAvatar(name, file) {
    const body = new FormData();
    body.append("file", file);
    const res = await fetch("/api/bots/" + encodeURIComponent(name) + "/avatar", {
      method: "POST", credentials: "same-origin", body: body,
    });
    const text = await res.text();
    let payload = {};
    if (text) { try { payload = JSON.parse(text); } catch (e) { payload = { error: text }; } }
    if (!res.ok) throw new Error(payload.error || "头像上传失败");
    return payload;
  }

  async function downloadTemplate(name) {
    const res = await fetch("/api/bots/" + encodeURIComponent(name) + "/template", { credentials: "same-origin" });
    if (!res.ok) {
      let msg = "导出失败";
      try { const data = await res.json(); if (data.error) msg = data.error; } catch (e) {}
      throw new Error(msg);
    }
    const blob = await res.blob();
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name + ".json";
    a.click();
    URL.revokeObjectURL(a.href);
  }

  function openFork(src) {
    openModal("分叉 Bot", [
      { name: "name", label: "新名字", placeholder: "reviewer" },
      { name: "role", label: "角色", placeholder: "负责看这个项目的 pull request。", multiline: true },
      { name: "group", label: "群（可选）", placeholder: "留空则不建群" },
    ], async (v) => {
      const name = (v.name || "").trim();
      const role = (v.role || "").trim();
      if (!name) throw new Error("需要一个名字");
      if (!role) throw new Error("需要一个角色");
      const body = { name: name, role: role };
      const group = (v.group || "").trim();
      if (group) body.group = group;
      await api("/api/bots/" + encodeURIComponent(src) + "/fork", { method: "POST", body: body });
      await refreshLists();
      await selectBot(name);
    });
  }

  async function togglePin(name, pinned) {
    try {
      await api("/api/bots/" + encodeURIComponent(name) + "/pin", { method: "POST", body: { pinned: pinned } });
      await refreshLists();
      if (state.sel && state.sel.type === "bot" && state.sel.id === name && panel.classList.contains("open")) await loadPanel();
    } catch (e) {
      setStatus(e.message || "失败");
    }
  }

  async function controlAgent(id, kind, text) {
    if (!state.sel || state.sel.type !== "bot") return;
    const path = "/api/bots/" + encodeURIComponent(state.sel.id) + "/agents/" + encodeURIComponent(id) + "/" + kind;
    try {
      if (kind === "cancel") await api(path, { method: "POST" });
      else await api(path, { method: "POST", body: { text: text } });
      setStatus(kind === "cancel" ? "已取消" : "已记下");
      await loadPanel();
    } catch (e) {
      setStatus(e.message || "失败");
    }
  }

  function closeDrawer() {
    sidebar.classList.remove("open");
    syncBackdrop();
  }

  function syncBackdrop() {
    const w = window.innerWidth;
    const drawer = w < 768 && sidebar.classList.contains("open");
    const sheet = w < 1100 && panel.classList.contains("open");
    backdrop.hidden = !(drawer || sheet);
  }

  async function refreshLists() {
    const data = await api("/api/bots");
    const groups = await api("/api/groups");
    state.bots = data.bots || [];
    state.groups = groups.groups || [];
    renderLists();
    warmOne().catch(() => {});
  }

  let warming = false;
  async function warmOne() {
    if (warming || state.streaming) return;
    const now = Date.now();
    const jobs = [];
    for (const b of state.bots) jobs.push({ key: "b:" + b.name, kind: "bot", id: b.name });
    for (const g of state.groups) jobs.push({ key: "g:" + g.id, kind: "group", id: g.id });
    const job = jobs.find((j) => state.preview[j.key] == null || now - (state.previewAt[j.key] || 0) > 20000);
    if (!job) return;
    if (state.sel && ((job.kind === "bot" && state.sel.type === "bot" && state.sel.id === job.id) || (job.kind === "group" && state.sel.type === "group" && state.sel.id === job.id))) {
      state.previewAt[job.key] = now;
      return;
    }
    warming = true;
    try {
      if (job.kind === "bot") {
        const data = await api("/api/bots/" + encodeURIComponent(job.id) + "/transcript");
        state.preview[job.key] = previewFromMessages(data.messages || []);
      } else {
        const data = await api("/api/groups/" + encodeURIComponent(job.id));
        state.preview[job.key] = previewFromGroup(data.messages || []);
      }
      state.previewAt[job.key] = Date.now();
      renderLists();
    } catch (e) {
      if (state.preview[job.key] == null) state.preview[job.key] = "";
      state.previewAt[job.key] = Date.now();
    } finally {
      warming = false;
    }
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
    const empty = inner.querySelector(".empty");
    if (empty) empty.remove();
    row = el("div", "msg bot live");
    const who = state.sel ? state.sel.id : "Bot";
    row.append(avatar(who, paintOf(who)));
    const body = el("div", "body");
    const bubble = el("div", "bubble");
    const prose = el("div", "prose");
    bubble.append(prose);
    body.append(bubble, el("div", "meta live-status", "正在回复"));
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
        const host = prose.closest(".body") || prose.parentElement;
        if (event === "delta" && payload.text) {
          const status = host && host.querySelector(".live-status");
          if (status) status.remove();
          acc += payload.text;
          prose.replaceChildren();
          appendMarkdown(prose, acc);
          thread.scrollTop = thread.scrollHeight;
        } else if (event === "tool") {
          const status = host && host.querySelector(".live-status");
          if (status) status.remove();
          const phrase = peerPhrase(payload.name || "", { text: payload.summary || "" }, state.sel && state.sel.id);
          if (phrase && host) {
            host.parentElement.insertAdjacentElement("afterend", systemLine(phrase, state.sel && state.sel.id));
          } else if (host) {
            host.append(toolShell(payload.name || "tool", payload.summary || "", ""));
          }
          thread.scrollTop = thread.scrollHeight;
        } else if (event === "error") {
          setStatus(payload.error || "出错了");
        }
      }
    }
  }

  async function sendText(text) {
    text = (text || "").trim();
    if (!text || !state.sel || state.streaming) return;
    state.streaming = true;
    $("send").disabled = true;
    setStatus("正在回复");
    try {
      if (state.sel.type === "group") {
        await api("/api/groups/" + encodeURIComponent(state.sel.id) + "/messages", { method: "POST", body: { text: text } });
        state.sig = "";
        await loadThread();
        setStatus("已送到成员的 inbox");
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
        setStatus(data.status === "queued" ? "已排队，终端正在处理" : "");
      } else if (!res.ok) {
        throw new Error("发送失败");
      } else {
        await readSSE(res);
      }
      state.sig = "";
      await loadThread();
      if (subtitle.textContent === "正在回复") setStatus("");
    } catch (e) {
      setStatus(e.message || "发送失败");
      if (!input.value) input.value = text;
      syncComposer();
    } finally {
      state.streaming = false;
      $("send").disabled = false;
    }
  }

  function closeModal() {
    modal.hidden = true;
    modalError.hidden = true;
    modalError.textContent = "";
    modalForm.onsubmit = null;
  }

  function openModal(title, fields, onOk, okLabel) {
    $("modal-title").textContent = title;
    $("modal-ok").textContent = okLabel || "创建";
    $("modal-ok").disabled = false;
    modalFields.replaceChildren();
    modalError.hidden = true;
    const inputs = {};
    for (const f of fields) {
      if (f.type === "check") {
        const row = el("label", "check-row");
        const cb = document.createElement("input");
        cb.type = "checkbox";
        cb.checked = !!f.checked;
        inputs[f.name] = cb;
        row.append(cb, el("span", "", f.label));
        modalFields.append(row);
        continue;
      }
      const label = el("label", "field");
      label.append(el("span", "", f.label));
      if (f.type === "choice") {
        const box = el("div", "choice");
        box.setAttribute("role", "radiogroup");
        const group = "choice-" + f.name;
        const current = f.value || (f.options && f.options[0] && f.options[0][0]) || "";
        for (const pair of f.options || []) {
          const opt = el("label", "choice-opt");
          const radio = document.createElement("input");
          radio.type = "radio";
          radio.name = group;
          radio.value = pair[0];
          radio.checked = pair[0] === current;
          opt.append(radio, document.createTextNode(pair[1]));
          box.append(opt);
        }
        inputs[f.name] = box;
        label.append(box);
      } else if (f.type === "checks") {
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
    const first = modalFields.querySelector("input, textarea");
    if (first) first.focus();
    modalForm.onsubmit = async (ev) => {
      ev.preventDefault();
      const values = {};
      for (const key of Object.keys(inputs)) {
        const node = inputs[key];
        if (Array.isArray(node)) values[key] = node.slice();
        else if (node instanceof HTMLInputElement && node.type === "checkbox") values[key] = node.checked;
        else if (node instanceof HTMLElement && node.classList.contains("choice")) {
          const picked = node.querySelector("input:checked");
          values[key] = picked ? picked.value : "";
        } else values[key] = node.value;
      }
      const ok = $("modal-ok");
      ok.disabled = true;
      try {
        await onOk(values);
        closeModal();
      } catch (e) {
        modalError.hidden = false;
        modalError.textContent = e.message || "失败";
      } finally {
        ok.disabled = false;
      }
    };
  }

  function exportName(res, name, format) {
    const disp = res.headers.get("content-disposition") || "";
    const star = /filename\*=UTF-8''([^;]+)/i.exec(disp);
    if (star) {
      try { return decodeURIComponent(star[1].trim()); } catch (e) {}
    }
    const plain = /filename="([^"]+)"/i.exec(disp) || /filename=([^;]+)/i.exec(disp);
    if (plain) return plain[1].trim();
    return name + (format === "jsonl" ? ".jsonl" : ".md");
  }

  async function downloadExport(name, format, tools) {
    const q = "format=" + encodeURIComponent(format) + "&tools=" + (tools ? "1" : "0");
    const res = await fetch("/api/bots/" + encodeURIComponent(name) + "/export?" + q, {
      credentials: "same-origin",
    });
    if (res.status === 401) {
      showLogin();
      throw new Error("需要重新登录");
    }
    if (!res.ok) {
      let msg = "";
      const text = await res.text().catch(() => "");
      if (text) {
        try { msg = (JSON.parse(text).error || "").trim(); } catch (e) { msg = text.trim().slice(0, 160); }
      }
      if (res.status === 404 || res.status === 501) throw new Error("导出还没接上");
      throw new Error(msg || "导出失败");
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = exportName(res, name, format);
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1500);
  }

  function openExport() {
    if (!state.sel || state.sel.type !== "bot") return;
    const name = state.sel.id;
    closePops();
    openModal("导出对话", [
      { name: "format", label: "格式", type: "choice", value: "md", options: [["md", "Markdown"], ["jsonl", "JSONL"]] },
      { name: "tools", label: "包含工具细节", type: "check" },
    ], async (v) => {
      const format = v.format === "jsonl" ? "jsonl" : "md";
      await downloadExport(name, format, !!v.tools);
    }, "下载");
  }

  function syncMore() {
    const bot = !!(state.sel && state.sel.type === "bot");
    $("more").hidden = !bot;
    if (!bot) {
      $("more-menu").hidden = true;
      $("more").setAttribute("aria-expanded", "false");
    }
  }

  function openNewBot() {
    openModal("新建 Bot", [
      { name: "name", label: "名字", placeholder: "ada" },
      { name: "persona", label: "人设", placeholder: "你是一个简短的助手。", multiline: true },
    ], async (v) => {
      const name = (v.name || "").trim();
      if (!name) throw new Error("需要一个名字");
      await api("/api/bots", { method: "POST", body: { name: name, persona: (v.persona || "").trim() } });
      await refreshLists();
      await selectBot(name);
    });
  }

  function openNewGroup() {
    openModal("新建群组", [
      { name: "name", label: "名字", placeholder: "晨会" },
      { name: "members", label: "成员", type: "checks", options: state.bots.map((b) => b.name) },
    ], async (v) => {
      const members = v.members || [];
      if (members.length < 2) throw new Error("至少选两个 Bot");
      const g = await api("/api/groups", { method: "POST", body: { name: (v.name || "").trim(), members: members } });
      await refreshLists();
      if (g.id) await selectGroup(g.id);
    });
  }

  function syncComposer() {
    composer.classList.toggle("has-text", input.value.trim().length > 0);
  }

  function closePops() {
    $("connect-pop").hidden = true;
    $("connect").setAttribute("aria-expanded", "false");
    $("plus-menu").hidden = true;
    $("plus").setAttribute("aria-expanded", "false");
    $("more-menu").hidden = true;
    $("more").setAttribute("aria-expanded", "false");
    notesEl.hidden = true;
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
      state.owner = data.owner || "";
      renderMe();
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
    state.listSig = "";
    showLogin();
  });

  $("theme").addEventListener("click", () => {
    const dark = document.documentElement.getAttribute("data-theme") === "dark";
    applyTheme(dark ? "light" : "dark");
  });

  $("collapse").addEventListener("click", () => {
    if (window.innerWidth < 768) { closeDrawer(); return; }
    sidebar.classList.toggle("collapsed");
    $("collapse").textContent = sidebar.classList.contains("collapsed") ? "展开列表" : "收起列表";
  });

  $("menu").addEventListener("click", () => {
    sidebar.classList.add("open");
    if (window.innerWidth < 768) panel.classList.remove("open");
    $("info").setAttribute("aria-expanded", "false");
    syncBackdrop();
  });

  backdrop.addEventListener("click", () => {
    state.panelTouched = true;
    sidebar.classList.remove("open");
    panel.classList.remove("open");
    $("info").setAttribute("aria-expanded", "false");
    backdrop.hidden = true;
  });

  $("info").addEventListener("click", async () => {
    state.panelTouched = true;
    const open = panel.classList.toggle("open");
    $("info").setAttribute("aria-expanded", open ? "true" : "false");
    if (open && window.innerWidth < 768) sidebar.classList.remove("open");
    syncBackdrop();
    if (open) {
      try { await loadPanel(); } catch (e) { setStatus(e.message || ""); }
    }
  });

  $("panel-close").addEventListener("click", () => {
    state.panelTouched = true;
    panel.classList.remove("open");
    $("info").setAttribute("aria-expanded", "false");
    syncBackdrop();
  });

  $("bell").addEventListener("click", async (ev) => {
    ev.stopPropagation();
    if (typeof Notification !== "undefined" && Notification.permission === "default") {
      try { await Notification.requestPermission(); } catch (e) {}
    }
    if (!state.notes.length) { setStatus("没有新通知"); return; }
    notesEl.hidden = !notesEl.hidden;
  });

  $("connect").addEventListener("click", (ev) => {
    ev.stopPropagation();
    const pop = $("connect-pop");
    pop.hidden = !pop.hidden;
    $("connect").setAttribute("aria-expanded", pop.hidden ? "false" : "true");
  });

  $("plus").addEventListener("click", (ev) => {
    ev.stopPropagation();
    const menu = $("plus-menu");
    menu.hidden = !menu.hidden;
    $("plus").setAttribute("aria-expanded", menu.hidden ? "false" : "true");
  });

  $("more").addEventListener("click", (ev) => {
    ev.stopPropagation();
    const menu = $("more-menu");
    menu.hidden = !menu.hidden;
    $("more").setAttribute("aria-expanded", menu.hidden ? "false" : "true");
  });

  $("export-chat").addEventListener("click", () => openExport());

  document.addEventListener("click", (ev) => {
    if (!ev.target.closest(".connect-wrap")) {
      $("connect-pop").hidden = true;
      $("connect").setAttribute("aria-expanded", "false");
    }
    if (!ev.target.closest(".plus-wrap")) {
      $("plus-menu").hidden = true;
      $("plus").setAttribute("aria-expanded", "false");
    }
    if (!ev.target.closest(".more-wrap")) {
      $("more-menu").hidden = true;
      $("more").setAttribute("aria-expanded", "false");
    }
    if (!ev.target.closest("#notes") && !ev.target.closest("#bell")) notesEl.hidden = true;
  });

  document.addEventListener("keydown", (ev) => {
    if (ev.key !== "Escape") return;
    if (!lightbox.hidden) { closeShots(); return; }
    if (!modal.hidden) { closeModal(); return; }
    closePops();
    if (panel.classList.contains("open") && window.innerWidth < 1100) {
      panel.classList.remove("open");
      $("info").setAttribute("aria-expanded", "false");
    }
    sidebar.classList.remove("open");
    syncBackdrop();
  });

  modal.addEventListener("click", (ev) => { if (ev.target === modal) closeModal(); });
  $("modal-cancel").addEventListener("click", closeModal);
  lightbox.addEventListener("click", (ev) => { if (ev.target === lightbox) closeShots(); });

  composer.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const text = input.value;
    input.value = "";
    input.style.height = "auto";
    syncComposer();
    sendText(text);
  });

  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && !ev.shiftKey) {
      ev.preventDefault();
      composer.requestSubmit();
    }
  });

  input.addEventListener("input", () => {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 140) + "px";
    syncComposer();
  });

  window.addEventListener("resize", () => {
    if (window.innerWidth >= 768) sidebar.classList.remove("open");
    syncPanelDefault();
    syncBackdrop();
  });

  async function boot() {
    await refreshLists();
    await refreshNotes();
    renderMe();
    const home = state.bots.find((b) => b.primary) || state.bots[0];
    if (!state.sel && home) await selectBot(home.name);
    else if (!state.sel) {
      const inner = el("div", "thread-inner");
      inner.append(emptyState("选一个对话", "左边是你的 Bot。"));
      thread.replaceChildren(inner);
    }
    for (let i = 0; i < state.bots.length + state.groups.length; i++) await warmOne();
    syncPanelDefault();
  }

  function syncPanelDefault() {
    if (state.panelTouched) return;
    const wide = window.innerWidth >= 1280;
    const was = panel.classList.contains("open");
    panel.classList.toggle("open", wide);
    $("info").setAttribute("aria-expanded", wide ? "true" : "false");
    syncBackdrop();
    if (wide && !was) loadPanel().catch(() => {});
  }

  initTheme();
  syncComposer();
  const hold = el("div", "thread-inner");
  hold.append(emptyState("XBot", "登录之后，对话在这里继续。"));
  thread.replaceChildren(hold);

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

  api("/api/session").then(async (data) => {
    state.owner = (data && data.owner) || "";
    renderMe();
    showApp();
    await boot();
    startPoll();
  }).catch(() => showLogin());
})();
