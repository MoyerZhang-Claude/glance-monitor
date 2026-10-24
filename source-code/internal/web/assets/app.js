"use strict";
/* Glance settings page. Talks to the local Glance process over /api/*. */

const TOKEN = new URLSearchParams(location.search).get("t") || "";
const S = { meta: null, cfg: null, snap: null, app: null, cat: {}, scene: "night", section: "overlay", syncers: [], live: [] };

/* ---------------- utilities ---------------- */

async function api(path, body) {
  const opt = { headers: { "X-Glance-Token": TOKEN } };
  if (body !== undefined) {
    opt.method = "POST";
    opt.headers["Content-Type"] = "application/json";
    opt.body = JSON.stringify(body);
  }
  const r = await fetch(path, opt);
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
}

function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  if (props) {
    for (const [k, v] of Object.entries(props)) {
      if (v === undefined || v === null || v === false) continue;
      if (k === "class") el.className = v;
      else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
      else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
      else if (k === "html") el.innerHTML = v;
      else el.setAttribute(k, v === true ? "" : v);
    }
  }
  for (const k of kids.flat()) {
    if (k === null || k === undefined || k === false) continue;
    el.append(k instanceof Node ? k : document.createTextNode(String(k)));
  }
  return el;
}

const $ = (sel, root = document) => root.querySelector(sel);

function get(path) { return path.split(".").reduce((o, k) => (o == null ? o : o[k]), S.cfg); }
function set(path, value) {
  const keys = path.split(".");
  let o = S.cfg;
  for (const k of keys.slice(0, -1)) o = o[k];
  o[keys[keys.length - 1]] = value;
  commit();
}

let toastTimer;
function toast(msg) {
  const t = $("#toast");
  t.textContent = msg;
  t.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.remove("show"), 2200);
}

function hexToRgb(hex) {
  const n = parseInt(hex.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}
function rgba(hex, a) { const [r, g, b] = hexToRgb(hex); return `rgba(${r},${g},${b},${a})`; }

/* ---------------- saving ---------------- */

let saveTimer = null, saving = false, dirty = false;
function commit() {
  dirty = true;
  renderPreview();
  clearTimeout(saveTimer);
  saveTimer = setTimeout(flush, 160);
}
async function flush() {
  if (saving) { saveTimer = setTimeout(flush, 120); return; }
  saving = true;
  dirty = false;
  try {
    const c = await api("/api/config", S.cfg);
    if (!dirty) { S.cfg = c; syncAll(); }
  } catch (e) {
    toast("保存失败：" + e.message);
  } finally {
    saving = false;
  }
}
function syncAll() {
  for (const fn of S.syncers) fn();
  renderPreview();
}
async function action(name, arg) {
  try { return await api("/api/action", { action: name, arg: arg || "" }); }
  catch (e) { toast("操作失败：" + e.message); throw e; }
}

/* ---------------- metric model (mirror of internal/layout) ---------------- */

const KIND = { pct: 0, temp: 1, power: 2, clock: 3, fps: 4, ms: 5, gb: 6, time: 7 };
const OPTIONAL = new Set(["cpu.vrm", "cpu.tempmax", "gpu.hotspot", "gpu.memtemp", "gpu.fan", "gpu.memclock"]);

function info(id) { return S.cat[id]; }
function unitFor(id, u) {
  const mi = info(id);
  if (!mi) return "";
  if (u && mi.units && mi.units.includes(u)) return u;
  return mi.unit;
}
function fmt(id, v, unit) {
  const mi = info(id);
  switch (mi && mi.kind) {
    case KIND.clock: return unit === "GHz" ? (v / 1000).toFixed(2) : String(Math.round(v));
    case KIND.power: return v < 9.95 ? v.toFixed(1) : String(Math.round(v));
    case KIND.ms: return v >= 99.5 ? String(Math.round(v)) : v.toFixed(1);
    case KIND.gb: return v.toFixed(1);
    case KIND.time: return new Date().toTimeString().slice(0, 5);
    default: return String(Math.round(v));
  }
}
function template(id, unit) {
  const mi = info(id);
  switch (mi && mi.kind) {
    case KIND.clock: return unit === "GHz" ? "8.88" : "8888";
    case KIND.power: case KIND.fps: return "888";
    case KIND.ms: case KIND.gb: return "88.8";
    case KIND.time: return "88:88";
    default: return "100";
  }
}
function value(id) {
  const s = S.snap;
  if (!s) return undefined;
  if (id === "time.clock") return 0;
  if (id.startsWith("fps.") && !s.fpsActive) return undefined;
  return s.values[id];
}
function thresholdColor(item, v, base) {
  const mi = info(item.metric);
  const o = S.cfg.overlay;
  if (!mi) return base;
  if (mi.highBad) {
    if (item.crit > 0 && v >= item.crit) return o.critColor;
    if (item.warn > 0 && v >= item.warn) return o.warnColor;
  } else {
    if (item.crit > 0 && v <= item.crit) return o.critColor;
    if (item.warn > 0 && v <= item.warn) return o.warnColor;
  }
  return base;
}

function buildModel(editing) {
  const cfg = S.cfg, o = cfg.overlay, snap = S.snap || { values: {} };
  const rows = [];
  for (const g of cfg.groups) {
    if (!g.enabled) continue;
    if (g.id === "fps" && !snap.fpsActive && o.hideIdleFps && !editing) continue;
    let cells = [];
    if (g.id === "gpu" && snap.gpuAsleep) {
      cells.push({ value: "休眠", dim: true, color: o.unitColor });
    } else {
      for (const it of g.items) {
        if (!it.enabled) continue;
        const v = value(it.metric);
        const ok = v !== undefined;
        if (!ok && (OPTIONAL.has(it.metric) || (snap.unsupported || {})[it.metric])) continue;
        const unit = unitFor(it.metric, it.unit);
        cells.push({
          metric: it.metric, prefix: it.prefix, unit: o.showUnits ? unit : "",
          template: template(it.metric, unit),
          value: ok ? fmt(it.metric, v, unit) : "--",
          dim: !ok,
          color: ok ? thresholdColor(it, v, o.valueColor) : o.unitColor,
        });
      }
      if (g.id === "fps" && o.showProcess && snap.fpsActive && snap.fpsProcess) {
        cells.push({ value: snap.fpsProcess.replace(/\.exe$/i, ""), dim: true, color: o.unitColor });
      }
    }
    if (!cells.length) continue;
    if (o.layout === "list") {
      cells.forEach((c, i) => {
        const mi = info(c.metric);
        const row = { cells: [] };
        if (o.showLabels) row.cells.push({ label: true, value: i === 0 ? g.label : "", color: g.color });
        c.prefix = c.metric === "fps.fps" ? "帧率" : (mi ? mi.short : "");
        row.cells.push(c);
        rows.push(row);
      });
    } else {
      const row = { cells: [] };
      if (o.showLabels && g.label) row.cells.push({ label: true, value: g.label, color: g.color });
      row.cells.push(...cells);
      rows.push(row);
    }
  }
  const model = { rows, layout: o.layout };
  if (o.layout === "line" && rows.length > 1) model.rows = [{ cells: rows.flatMap(r => r.cells) }];
  const fpsOn = cfg.groups.some(g => g.id === "fps" && g.enabled && g.items.some(i => i.metric === "fps.fps" && i.enabled));
  model.showGraph = o.graph && fpsOn;
  return model;
}

/* ---------------- HUD replica ---------------- */

const measureCtx = document.createElement("canvas").getContext("2d");
function textWidth(s, font) { measureCtx.font = font; return measureCtx.measureText(s).width; }

function hudFontStack(name) { return `"${name}", "Microsoft YaHei UI", "Segoe UI", sans-serif`; }

function renderHud(container) {
  const o = S.cfg.overlay;
  const model = buildModel(true);
  container.textContent = "";
  if (!S.cfg.overlay.visible) {
    container.append(h("div", { class: "hud-empty" }, "悬浮窗已隐藏"));
  }
  const fontCss = `${o.bold ? 700 : 400} ${o.fontSize}px ${hudFontStack(o.font)}`;
  const hud = h("div", { class: `hud fx-${o.effect}` + (model.layout === "line" ? " line" : "") });
  const st = hud.style;
  st.font = fontCss;
  st.color = o.valueColor;
  st.setProperty("--colgap", o.colGap + "px");
  st.setProperty("--rowgap", o.rowGap + "px");
  st.setProperty("--pad", o.padding + "px");
  st.setProperty("--radius", o.radius + "px");
  st.setProperty("--unitsize", (o.unitScale * 100).toFixed(0) + "%");
  st.setProperty("--unitcolor", o.unitColor);
  st.background = o.bgOpacity > 0 ? rgba(o.bgColor, o.bgOpacity / 100) : "transparent";
  st.opacity = o.opacity / 100;
  if (!o.visible) st.opacity = 0.35;
  let cols = 1;
  for (const r of model.rows) cols = Math.max(cols, r.cells.length);
  if (model.layout !== "line") st.gridTemplateColumns = `repeat(${cols}, auto)`;

  const cellEl = c => {
    if (c.label) return h("span", { class: "cell lbl", style: { color: c.color } }, c.value);
    const tw = c.template && !c.dim ? textWidth(c.template, fontCss) : 0;
    return h("span", { class: "cell" + (c.dim ? " dim" : "") },
      c.prefix ? h("span", { class: "pre" }, c.prefix) : null,
      h("span", { class: "val", style: { color: c.color, minWidth: tw ? tw + "px" : null } }, c.value),
      c.unit ? h("span", { class: "unit" }, c.unit) : null);
  };
  for (const r of model.rows) {
    r.cells.forEach(c => hud.append(cellEl(c)));
    if (model.layout !== "line") for (let i = r.cells.length; i < cols; i++) hud.append(h("span"));
  }
  if (model.showGraph) {
    const cv = h("canvas", { class: "graph" });
    hud.append(cv);
    requestAnimationFrame(() => drawGraph(cv, o, hud));
  }
  if (!model.rows.length && !model.showGraph) hud.append(h("span", { class: "hud-empty" }, "没有选择任何显示项目"));
  container.append(hud);
  return hud;
}

function drawGraph(cv, o, hud) {
  const w = Math.max(150, hud.clientWidth - 2 * o.padding);
  const hgt = o.graphHeight;
  const dpr = window.devicePixelRatio || 1;
  cv.width = w * dpr; cv.height = hgt * dpr;
  cv.style.width = w + "px"; cv.style.height = hgt + "px";
  const ctx = cv.getContext("2d");
  ctx.scale(dpr, dpr);
  ctx.fillStyle = "rgba(255,255,255,0.11)";
  ctx.fillRect(0, 0, w, hgt);
  let ft = (S.snap && S.snap.fpsActive && S.snap.frameTimes) || [];
  if (!ft.length) { // illustrative curve when no game is running
    ft = Array.from({ length: 300 }, (_, i) => 7 + Math.sin(i / 9) * 0.8 + (i % 97 === 0 ? 14 : 0));
  }
  const sorted = [...ft].sort((a, b) => a - b);
  let ymax = 33.4;
  const p99 = sorted[Math.floor((sorted.length - 1) * 0.99)] || 0;
  ymax = Math.max(ymax, Math.min(250, Math.ceil(p99 * 1.25 / 10) * 10));
  ctx.setLineDash([3, 3]);
  ctx.strokeStyle = "rgba(255,255,255,0.24)";
  for (const ms of [16.7, 33.3]) if (ms < ymax) { const y = hgt - ms / ymax * hgt; ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke(); }
  ctx.setLineDash([]);
  const total = ft.reduce((a, b) => a + b, 0);
  const span = Math.max(total, o.graphSecs * 1000);
  let t = span - total;
  const pts = [];
  for (const v of ft) { t += v; pts.push([t / span * (w - 1), hgt - Math.min(v, ymax) / ymax * hgt]); }
  ctx.beginPath();
  ctx.moveTo(pts[0][0], hgt);
  pts.forEach(p => ctx.lineTo(p[0], p[1]));
  ctx.lineTo(pts[pts.length - 1][0], hgt);
  ctx.fillStyle = rgba(o.graphColor, 0.24);
  ctx.fill();
  ctx.beginPath();
  pts.forEach((p, i) => (i ? ctx.lineTo(p[0], p[1]) : ctx.moveTo(p[0], p[1])));
  ctx.strokeStyle = o.graphColor;
  ctx.lineWidth = 1.25;
  ctx.stroke();
}

/* ---------------- preview stage ---------------- */

let stage, stageHud, stageNote;
function buildStage() {
  stage = h("div", { class: "stage", "data-scene": S.scene });
  stageHud = h("div", { class: "stage-hud" });
  stageNote = h("div", { class: "stage-note" });
  const tools = h("div", { class: "stage-tools", role: "group", "aria-label": "预览背景" });
  for (const [k, label] of [["night", "暗场景"], ["snow", "亮场景"], ["neon", "霓虹"]]) {
    tools.append(h("button", {
      "aria-pressed": String(S.scene === k), title: "在" + label + "上检查可读性",
      onclick: e => {
        S.scene = k; stage.dataset.scene = k;
        tools.querySelectorAll("button").forEach(b => b.setAttribute("aria-pressed", String(b === e.currentTarget)));
      },
    }, label));
  }
  stage.append(stageHud, stageNote, tools);
}
function renderPreview() {
  if (!stage || !S.cfg) return;
  const hud = renderHud(stageHud);
  const s = S.snap;
  stageNote.textContent = s && s.fpsActive ? `正在统计：${s.fpsProcess.replace(/\.exe$/i, "")}${s.frameGen ? "（已识别帧生成）" : ""}` :
    (S.cfg.overlay.hideIdleFps ? "预览 · 未运行游戏时帧率行会自动隐藏" : "预览");
  updateGhost(hud);
}

/* ---------------- controls ---------------- */

// liveText is a text node that re-renders whenever the config changes.
function liveText(fn) {
  const t = document.createTextNode(fn());
  S.syncers.push(() => { t.textContent = fn(); });
  return t;
}

function row(label, desc, ...ctl) {
  return h("div", { class: "row" },
    h("div", null, h("div", { class: "label" }, label), desc ? h("div", { class: "desc" }, desc) : null),
    h("div", { class: "ctl" }, ...ctl));
}

function toggle(path, opts = {}) {
  const input = h("input", { type: "checkbox", role: "switch", "aria-label": opts.aria || path });
  const read = () => (opts.invert ? !get(path) : !!get(path));
  input.checked = read();
  input.addEventListener("change", () => set(path, opts.invert ? !input.checked : input.checked));
  S.syncers.push(() => { input.checked = read(); });
  return h("label", { class: "switch" }, input, h("span"));
}

function seg(path, options, onChange) {
  const box = h("div", { class: "seg", role: "group" });
  const btns = options.map(([v, label]) => h("button", {
    type: "button", onclick: () => { set(path, v); paint(); onChange && onChange(v); },
  }, label));
  const paint = () => btns.forEach((b, i) => b.setAttribute("aria-pressed", String(get(path) === options[i][0])));
  paint();
  S.syncers.push(paint);
  box.append(...btns);
  return box;
}

function range(path, min, max, step, suffix = "", fmtFn) {
  const out = h("span", { class: "num-out" });
  const input = h("input", { type: "range", min, max, step, "aria-label": path });
  const show = () => { out.textContent = (fmtFn ? fmtFn(get(path)) : get(path)) + suffix; };
  input.value = get(path);
  show();
  input.addEventListener("input", () => { set(path, Number(input.value)); show(); });
  S.syncers.push(() => { if (document.activeElement !== input) input.value = get(path); show(); });
  return [input, out];
}

function color(path) {
  const input = h("input", { type: "color", "aria-label": path });
  input.value = get(path).toLowerCase();
  input.addEventListener("input", () => set(path, input.value.toUpperCase()));
  S.syncers.push(() => { if (document.activeElement !== input) input.value = get(path).toLowerCase(); });
  return input;
}

function select(path, options, onChange) {
  const sel = h("select", { "aria-label": path });
  const fill = () => {
    sel.textContent = "";
    for (const o of options()) {
      if (o.group) {
        const og = h("optgroup", { label: o.group });
        o.items.forEach(([v, l]) => og.append(h("option", { value: v }, l)));
        sel.append(og);
      } else sel.append(h("option", { value: o[0] }, o[1]));
    }
    sel.value = get(path);
  };
  fill();
  sel.addEventListener("change", () => { set(path, sel.value); onChange && onChange(sel.value); });
  S.syncers.push(() => { if (document.activeElement !== sel) sel.value = get(path); });
  return sel;
}

function number(path, min, max) {
  const input = h("input", { type: "number", min, max, step: 1, "aria-label": path });
  input.value = get(path);
  input.addEventListener("change", () => {
    let v = Number(input.value);
    if (Number.isNaN(v)) v = 0;
    set(path, Math.max(min, Math.min(max, Math.round(v))));
  });
  S.syncers.push(() => { if (document.activeElement !== input) input.value = get(path); });
  return input;
}

const KEYMAP = { Space: "Space", PageUp: "PageUp", PageDown: "PageDown", End: "End", Home: "Home", ArrowLeft: "Left", ArrowUp: "Up", ArrowRight: "Right", ArrowDown: "Down", Insert: "Insert", Delete: "Delete", Pause: "Pause", ScrollLock: "ScrollLock", Backquote: "`", Minus: "-", Equal: "=", BracketLeft: "[", BracketRight: "]", Semicolon: ";", Quote: "'", Comma: ",", Period: ".", Slash: "/", Backslash: "\\" };
function keyName(e) {
  const c = e.code;
  if (/^Key[A-Z]$/.test(c)) return c.slice(3);
  if (/^Digit\d$/.test(c)) return c.slice(5);
  if (/^Numpad\d$/.test(c)) return "Num" + c.slice(6);
  if (/^F\d{1,2}$/.test(c)) return c;
  return KEYMAP[c] || "";
}
function hotkey(path, errKey) {
  const cap = h("button", { class: "keycap", type: "button" });
  const err = h("div", { class: "err" });
  let listening = false;
  const paint = () => {
    cap.classList.toggle("listening", listening);
    cap.textContent = listening ? "请按下组合键…（Esc 取消，Backspace 清除）" : (get(path) || "未设置");
    const e = S.app && S.app.hotkeyErrors && S.app.hotkeyErrors[errKey];
    err.textContent = e ? "⚠ " + e : "";
  };
  cap.addEventListener("click", () => { listening = true; paint(); });
  cap.addEventListener("blur", () => { listening = false; paint(); });
  cap.addEventListener("keydown", e => {
    if (!listening) return;
    e.preventDefault();
    if (e.code === "Escape") { listening = false; paint(); return; }
    if (e.code === "Backspace") { set(path, ""); listening = false; paint(); return; }
    const k = keyName(e);
    if (!k) return;
    const mods = [e.ctrlKey && "Ctrl", e.altKey && "Alt", e.shiftKey && "Shift", e.metaKey && "Win"].filter(Boolean);
    if (!mods.length && !/^F\d+$/.test(k)) { toast("请同时按住 Ctrl / Alt / Shift 中的至少一个"); return; }
    set(path, [...mods, k].join("+"));
    listening = false;
    paint();
  });
  paint();
  S.syncers.push(paint);
  S.live.push(paint);
  return h("div", { style: { display: "flex", flexDirection: "column", alignItems: "flex-end", gap: "4px" } }, cap, err);
}

/* ---------------- sections ---------------- */

const ICONS = {
  overlay: '<rect x="3" y="4" width="18" height="13" rx="2"/><path d="M8 21h8M12 17v4"/><rect x="5.5" y="6.5" width="6" height="4" rx="1"/>',
  metrics: '<path d="M4 6h10M4 12h16M4 18h7"/><circle cx="18" cy="6" r="2"/><circle cx="15" cy="18" r="2"/>',
  style: '<path d="M4 20h4L19 9l-4-4L4 16v4z"/><path d="M13 7l4 4"/>',
  tray: '<rect x="3" y="14" width="18" height="6" rx="1.5"/><path d="M15 17h.01M18 17h.01"/><path d="M12 4v6M9 7l3 3 3-3"/>',
  general: '<circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M4.2 4.2l2.1 2.1M17.7 17.7l2.1 2.1M2 12h3M19 12h3M4.2 19.8l2.1-2.1M17.7 6.3l2.1-2.1"/>',
  sensors: '<path d="M3 12h4l3-7 4 14 3-7h4"/>',
  about: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7.5v.01"/>',
};
const SECTIONS = [
  ["overlay", "悬浮窗"], ["metrics", "显示内容"], ["style", "外观"], ["tray", "托盘图标"],
  ["general", "常规"], ["sensors", "传感器状态"], ["about", "关于"],
];

function buildNav() {
  const nav = $("#nav");
  nav.textContent = "";
  for (const [id, label] of SECTIONS) {
    const a = h("a", { href: "#" + id, "data-id": id });
    a.innerHTML = `<svg viewBox="0 0 24 24" aria-hidden="true">${ICONS[id]}</svg>`;
    a.append(label);
    nav.append(a);
  }
  nav.append(h("div", { class: "spacer" }), h("div", { class: "ver" }, "版本 " + S.meta.version));
}

function route() {
  let id = location.hash.slice(1) || "overlay";
  const welcome = id === "welcome";
  if (welcome) id = "overlay";
  if (!SECTIONS.some(s => s[0] === id)) id = "overlay";
  S.section = id;
  document.querySelectorAll(".section").forEach(sec => sec.classList.toggle("active", sec.id === "sec-" + id));
  document.querySelectorAll("#nav a").forEach(a => { if (a.dataset.id === id) a.setAttribute("aria-current", "page"); else a.removeAttribute("aria-current"); });
  const slot = $(`#sec-${id} .stage-slot`);
  if (slot) { slot.append(stage); renderPreview(); }
  if (welcome) $("#welcome").hidden = false;
  $("#main").scrollTop = 0;
}

function section(id, title, lead, ...content) {
  return h("section", { class: "section", id: "sec-" + id, "aria-labelledby": "t-" + id },
    h("h1", { id: "t-" + id }, title), lead ? h("p", { class: "lead" }, lead) : null, ...content);
}

/* ---- overlay ---- */
function buildOverlay() {
  const welcome = h("div", { class: "banner", id: "welcome", hidden: true },
    h("div", null,
      h("b", null, "Glance 已经在运行了"),
      h("p", null, "悬浮窗默认在屏幕左上角，数据每秒刷新一次。任务栏右下角的托盘图标会显示 CPU 温度，鼠标放上去可以看完整数据。下面可以调整显示位置、内容和样式，修改会立即生效。")),
    h("button", { class: "btn", onclick: () => { $("#welcome").hidden = true; history.replaceState(null, "", "#overlay"); } }, "知道了"));

  const screen = h("div", { class: "screen", role: "group", "aria-label": "屏幕位置" });
  const anchors = ["tl", "tc", "tr", "ml", "mc", "mr", "bl", "bc", "br"];
  const names = { tl: "左上", tc: "顶部居中", tr: "右上", ml: "左侧居中", mc: "屏幕中央", mr: "右侧居中", bl: "左下", bc: "底部居中", br: "右下" };
  const btns = anchors.map(a => h("button", {
    type: "button", title: names[a], "aria-label": names[a],
    onclick: () => {
      S.cfg.overlay.anchor = a;
      const d = S.meta.defaults.overlay;
      S.cfg.overlay.offsetX = a[1] === "c" ? 0 : d.offsetX;
      S.cfg.overlay.offsetY = a[0] === "m" ? 0 : d.offsetY;
      paintAnchor(); commit(); syncAll();
    },
  }));
  const ghost = h("div", { class: "ghost" });
  screen.append(...btns, ghost);
  const paintAnchor = () => btns.forEach((b, i) => b.setAttribute("aria-pressed", String(S.cfg.overlay.anchor === anchors[i])));
  paintAnchor();
  S.syncers.push(paintAnchor);
  S.ghost = { el: ghost, screen };

  const editBtn = h("button", { class: "btn primary", type: "button", onclick: async () => {
    await action("edit_mode", S.app && S.app.editing ? "off" : "on");
    poll();
  } }, "在屏幕上拖动");
  S.live.push(() => {
    const on = S.app && S.app.editing;
    editBtn.textContent = on ? "完成拖动并锁定" : "在屏幕上拖动";
  });

  const monitorSel = () => {
    const mons = S.meta.monitors || [];
    if (mons.length < 2) return null;
    return row("显示器", "悬浮窗显示在哪块屏幕上。", select("overlay.monitor", () => mons.map((m, i) =>
      [m.primary ? "" : m.device, `${m.primary ? "主显示器" : "显示器 " + (i + 1)}（${m.width}×${m.height}，缩放 ${m.scale}%）`])));
  };

  return section("overlay", "悬浮窗", "在游戏和桌面上都显示的数据窗口。它是一个独立的透明窗口，鼠标会直接穿透，不会干扰游戏操作。",
    welcome,
    h("div", { class: "stage-slot" }),
    h("div", { class: "rows", style: { marginTop: "18px" } },
      row("显示悬浮窗", liveText(() => `快捷键 ${S.cfg.general.hotkeyToggle || "未设置"} 可随时显示 / 隐藏。`), toggle("overlay.visible")),
      row("显示时机", "“仅全屏时”：只有游戏全屏或无边框全屏时才出现，回到桌面自动隐藏。",
        seg("overlay.showMode", [["always", "始终显示"], ["fullscreen", "仅全屏时"]])),
      row("排列方式", "分组：每类一行；单行：全部排成一行，适合贴在屏幕顶部；列表：每项一行。",
        seg("overlay.layout", [["rows", "分组"], ["line", "单行"], ["list", "列表"]])),
    ),
    h("h2", null, "位置"),
    h("div", { class: "pos" },
      screen,
      h("div", { class: "pos-side" },
        h("div", { class: "rows" },
          row("距离边缘", "水平 / 垂直方向的偏移，单位为像素（按 100% 缩放计算）。",
            h("span", { class: "hint" }, "X"), number("overlay.offsetX", -2000, 8000),
            h("span", { class: "hint" }, "Y"), number("overlay.offsetY", -2000, 8000)),
          monitorSel(),
        ),
        h("div", { class: "btn-row" }, editBtn,
          h("button", { class: "btn", type: "button", onclick: () => action("reset_position").then(reloadConfig) }, "恢复默认位置")),
        h("p", { class: "hint" }, liveText(() => `拖动时会自动吸附到屏幕边缘和中线；双击悬浮窗或按 ${S.cfg.general.hotkeyEdit || "快捷键"} 锁定。`)),
      )),
    h("div", { class: "rows", style: { marginTop: "18px" } },
      row("没有游戏时隐藏帧率行", "回到桌面或游戏暂停渲染时，帧率这一行会自动收起。", toggle("overlay.hideIdleFps")),
      row("录屏 / 截图时隐藏", "开启后悬浮窗不会出现在 OBS、NVIDIA 录制和截图中（需要 Windows 10 2004 以上）。", toggle("overlay.hideCapture")),
    ));
}

function updateGhost(hud) {
  if (!S.ghost || !hud) return;
  const mons = S.meta.monitors || [];
  const m = mons.find(x => (S.cfg.overlay.monitor ? x.device === S.cfg.overlay.monitor : x.primary)) || mons[0] || { width: 2560, height: 1600, scale: 150 };
  const lw = m.width * 100 / m.scale, lh = m.height * 100 / m.scale;
  const sw = S.ghost.screen.clientWidth || 224, sh = S.ghost.screen.clientHeight || 140;
  const k = sw / lw;
  const w = Math.max(6, hud.offsetWidth * k), hh = Math.max(4, hud.offsetHeight * k);
  const o = S.cfg.overlay;
  let x, y;
  if (o.anchor[1] === "l") x = o.offsetX * k; else if (o.anchor[1] === "r") x = sw - w - o.offsetX * k; else x = (sw - w) / 2 + o.offsetX * k;
  if (o.anchor[0] === "t") y = o.offsetY * k; else if (o.anchor[0] === "b") y = sh - hh - o.offsetY * k; else y = (sh - hh) / 2 + o.offsetY * k;
  Object.assign(S.ghost.el.style, { left: x + "px", top: y + "px", width: w + "px", height: hh + "px" });
}

/* ---- metrics ---- */
function buildMetrics() {
  const list = h("div", { class: "groups" });
  S.groupList = list;
  renderGroups();
  return section("metrics", "显示内容", "选择悬浮窗显示哪些数据，以及它们的顺序。超过阈值时数值会变成橙色或红色。",
    h("div", { class: "stage-slot" }),
    h("h2", null, "分组与项目"),
    list);
}

function moveIn(arr, i, d) {
  const j = i + d;
  if (j < 0 || j >= arr.length) return;
  [arr[i], arr[j]] = [arr[j], arr[i]];
}

function renderGroups() {
  const list = S.groupList;
  list.textContent = "";
  const groupsMeta = Object.fromEntries(S.meta.groups.map(g => [g.id, g]));
  S.cfg.groups.forEach((g, gi) => {
    const gm = groupsMeta[g.id];
    const head = h("div", { class: "group-head" },
      h("span", { class: "swatch", style: { background: g.color } }),
      h("span", { class: "gname" }, gm.name),
      h("label", { class: "switch", title: "显示这个分组" },
        Object.assign(h("input", { type: "checkbox", role: "switch", "aria-label": "显示" + gm.name, onchange: e => { g.enabled = e.target.checked; commit(); renderGroups(); } }), { checked: g.enabled }),
        h("span")),
      h("span", { class: "hint" }, "标签"),
      Object.assign(h("input", { type: "text", maxlength: 12, "aria-label": gm.name + "标签", onchange: e => { g.label = e.target.value.trim(); commit(); } }), { value: g.label }),
      Object.assign(h("input", { type: "color", "aria-label": gm.name + "颜色", oninput: e => { g.color = e.target.value.toUpperCase(); head.querySelector(".swatch").style.background = g.color; commit(); } }), { value: g.color.toLowerCase() }),
      h("span", { class: "grow" }),
      h("button", { class: "btn mv", type: "button", title: "上移", "aria-label": "上移" + gm.name, disabled: gi === 0, onclick: () => { moveIn(S.cfg.groups, gi, -1); commit(); renderGroups(); } }, "↑"),
      h("button", { class: "btn mv", type: "button", title: "下移", "aria-label": "下移" + gm.name, disabled: gi === S.cfg.groups.length - 1, onclick: () => { moveIn(S.cfg.groups, gi, 1); commit(); renderGroups(); } }, "↓"));
    const items = h("div", { class: "items" });
    g.items.forEach((it, ii) => {
      const mi = info(it.metric);
      if (!mi) return;
      const live = h("div", { class: "live", "data-live": it.metric });
      const cb = h("input", { type: "checkbox", "aria-label": mi.name, onchange: e => { it.enabled = e.target.checked; commit(); } });
      cb.checked = it.enabled;
      let unitSel = null;
      if (mi.units && mi.units.length > 1) {
        unitSel = h("select", { "aria-label": mi.name + "单位", onchange: e => { it.unit = e.target.value; commit(); } },
          ...mi.units.map(u => h("option", { value: u }, u)));
        unitSel.value = unitFor(it.metric, it.unit);
      }
      const thrOk = mi.kind !== KIND.time;
      const thr = thrOk ? h("div", { class: "thr" },
        h("span", { class: "w" }, mi.highBad ? "警告 ≥" : "警告 ≤"),
        Object.assign(h("input", { type: "number", step: "any", "aria-label": mi.name + "警告阈值", placeholder: "关", onchange: e => { it.warn = Number(e.target.value) || 0; commit(); } }), { value: it.warn || "" }),
        h("span", { class: "c" }, mi.highBad ? "严重 ≥" : "严重 ≤"),
        Object.assign(h("input", { type: "number", step: "any", "aria-label": mi.name + "严重阈值", placeholder: "关", onchange: e => { it.crit = Number(e.target.value) || 0; commit(); } }), { value: it.crit || "" }),
      ) : h("div");
      items.append(h("div", { class: "item" },
        cb,
        h("div", { class: "iname" }, h("span", null, mi.name), mi.hint ? h("small", null, mi.hint) : null),
        live,
        unitSel || h("span"),
        thr,
        h("div", { class: "tools" },
          h("button", { class: "btn", type: "button", title: "上移", "aria-label": "上移" + mi.name, disabled: ii === 0, onclick: () => { moveIn(g.items, ii, -1); commit(); renderGroups(); } }, "↑"),
          h("button", { class: "btn", type: "button", title: "下移", "aria-label": "下移" + mi.name, disabled: ii === g.items.length - 1, onclick: () => { moveIn(g.items, ii, 1); commit(); renderGroups(); } }, "↓"))));
    });
    list.append(h("div", { class: "group" + (g.enabled ? "" : " off") }, head, items));
  });
  updateLive();
}

function updateLive() {
  document.querySelectorAll("[data-live]").forEach(el => {
    const id = el.dataset.live;
    const s = S.snap;
    el.classList.remove("na");
    if (!s) { el.textContent = ""; return; }
    if (id.startsWith("gpu.") && s.gpuAsleep) { el.textContent = "休眠中"; el.classList.add("na"); return; }
    const v = value(id);
    if (v === undefined) {
      el.textContent = id.startsWith("fps.") ? "无游戏" : ((s.unsupported || {})[id] ? "本机不支持" : "未检测到");
      el.classList.add("na");
      return;
    }
    const it = S.cfg.groups.flatMap(g => g.items).find(i => i.metric === id) || {};
    const unit = unitFor(id, it.unit);
    el.textContent = fmt(id, v, unit);
    if (unit) el.append(h("span", { class: "u" }, unit));
  });
}

/* ---- style ---- */
const PRESETS = [
  { name: "默认", desc: "半透明深色底", sample: { bg: "rgba(11,15,20,.55)", color: "#fff", shadow: true },
    o: { bgColor: "#0B0F14", bgOpacity: 55, effect: "shadow", valueColor: "#FFFFFF", unitColor: "#AEB6C2", radius: 8, padding: 8, layout: "rows" }, colors: null },
  { name: "无背景描边", desc: "只有文字，黑色描边", sample: { bg: "transparent", color: "#fff", outline: true },
    o: { bgOpacity: 0, effect: "outline", valueColor: "#FFFFFF", unitColor: "#D5DBE3" }, colors: null },
  { name: "经典橙色", desc: "类似 RTSS 的配色", sample: { bg: "transparent", color: "#FF9F1C", shadow: true },
    o: { bgOpacity: 0, effect: "outline", valueColor: "#FFFFFF", unitColor: "#FFC870" },
    colors: { cpu: "#FF9F1C", gpu: "#FF9F1C", fps: "#FF9F1C", ram: "#FF9F1C", time: "#FF9F1C" } },
  { name: "顶部单行", desc: "细长一条，贴屏幕顶部", sample: { bg: "rgba(11,15,20,.6)", color: "#fff", line: true },
    o: { layout: "line", anchor: "tc", offsetX: 0, offsetY: 0, radius: 0, padding: 5, fontSize: 13, bgOpacity: 60, effect: "none" }, colors: null },
];
function applyPreset(p) {
  Object.assign(S.cfg.overlay, p.o);
  if (p.colors) S.cfg.groups.forEach(g => { if (p.colors[g.id]) g.color = p.colors[g.id]; });
  else {
    const def = Object.fromEntries(S.meta.groups.map(g => [g.id, g.color]));
    S.cfg.groups.forEach(g => { g.color = def[g.id]; });
  }
  commit(); syncAll(); renderGroups();
  toast("已应用“" + p.name + "”风格");
}

function fontOptions() {
  const all = S.meta.fonts || [];
  const have = new Set(all);
  const rec = ["Segoe UI Semibold", "Segoe UI", "Bahnschrift SemiBold", "Bahnschrift", "Consolas", "Microsoft YaHei UI", "Arial", "Verdana"].filter(f => have.has(f) || !all.length);
  const cur = get("overlay.font");
  const groups = [{ group: "推荐", items: rec.map(f => [f, f]) }];
  if (cur && !rec.includes(cur) && !have.has(cur)) groups[0].items.push([cur, cur + "（未安装）"]);
  if (all.length) groups.push({ group: "全部字体", items: all.filter(f => !rec.includes(f)).map(f => [f, f]) });
  return groups;
}

function buildStyle() {
  const presets = h("div", { class: "presets" }, ...PRESETS.map(p => {
    const sw = h("div", { class: "sw" }, h("span", {
      style: {
        background: p.sample.bg, color: p.sample.color,
        textShadow: p.sample.shadow ? "1px 1px 0 rgba(0,0,0,.6)" : "none",
        webkitTextStroke: p.sample.outline ? "2px rgba(0,0,0,.85)" : "", paintOrder: "stroke fill",
      },
    }, p.sample.line ? "CPU 45% GPU 98% FPS 120" : "CPU 45%  72°C"));
    return h("button", { class: "preset", type: "button", onclick: () => applyPreset(p) }, sw, h("b", null, p.name), h("small", null, p.desc));
  }));
  const fontPreview = h("span", { class: "hint" });
  const paintFont = () => { fontPreview.style.fontFamily = hudFontStack(get("overlay.font")); fontPreview.textContent = "72°C 144"; };
  paintFont();
  S.syncers.push(paintFont);

  return section("style", "外观", "字体、颜色和背景。用预览右下角的按钮切换亮 / 暗场景，检查在不同游戏画面上是否看得清。",
    h("div", { class: "stage-slot" }),
    h("h2", null, "快速风格"),
    presets,
    h("h2", null, "文字"),
    h("div", { class: "rows" },
      row("字体", "数字推荐使用 Segoe UI Semibold 或 Bahnschrift，中文会自动使用微软雅黑。", fontPreview, select("overlay.font", fontOptions)),
      row("字号", null, ...range("overlay.fontSize", 10, 32, 1, " px")),
      row("粗体", null, toggle("overlay.bold")),
      row("文字效果", "阴影和描边能让文字在亮色画面上也清晰可见。", seg("overlay.effect", [["none", "无"], ["shadow", "阴影"], ["outline", "描边"]])),
      row("数值颜色", null, color("overlay.valueColor")),
      row("单位与小字", "单位（°C、W、MHz）和“1%”等前缀的颜色与大小。", color("overlay.unitColor"), ...range("overlay.unitScale", 0.5, 1, 0.02, "", v => Math.round(v * 100) + "%")),
      row("显示单位", null, toggle("overlay.showUnits")),
      row("显示分组标签", "每行开头的 CPU / GPU / FPS 字样。", toggle("overlay.showLabels")),
      row("警告 / 严重颜色", "数值超过“显示内容”里设置的阈值时使用。", color("overlay.warnColor"), color("overlay.critColor")),
    ),
    h("h2", null, "背景与间距"),
    h("div", { class: "rows" },
      row("背景颜色", null, color("overlay.bgColor")),
      row("背景不透明度", "设为 0% 就只有文字。", ...range("overlay.bgOpacity", 0, 100, 1, "%")),
      row("整体不透明度", "整个悬浮窗（文字 + 背景）的透明程度。", ...range("overlay.opacity", 20, 100, 1, "%")),
      row("圆角", null, ...range("overlay.radius", 0, 24, 1, " px")),
      row("内边距", null, ...range("overlay.padding", 0, 32, 1, " px")),
      row("列间距", null, ...range("overlay.colGap", 2, 48, 1, " px")),
      row("行间距", null, ...range("overlay.rowGap", 0, 24, 1, " px")),
    ),
    h("h2", null, "帧时间曲线"),
    h("div", { class: "rows" },
      row("显示帧时间曲线", "在帧率下方画出最近每一帧的耗时，尖峰就是卡顿。", toggle("overlay.graph")),
      row("曲线高度", null, ...range("overlay.graphHeight", 16, 160, 1, " px")),
      row("时间跨度", null, ...range("overlay.graphSecs", 2, 60, 1, " 秒")),
      row("曲线颜色", null, color("overlay.graphColor")),
      row("显示游戏进程名", "在帧率行末尾显示正在统计的程序名称。", toggle("overlay.showProcess")),
    ));
}

/* ---- tray ---- */
function buildTray() {
  const cv = h("canvas", { width: 48, height: 48 });
  const cvLight = h("canvas", { width: 48, height: 48 });
  const clock = () => { const d = new Date(); return [d.toTimeString().slice(0, 5), d.toLocaleDateString("zh-CN")]; };
  const tbDark = h("div", { class: "taskbar" }, h("span", null, "︿"), cv, h("span", { class: "clock" }));
  const tbLight = h("div", { class: "taskbar light" }, h("span", null, "︿"), cvLight, h("span", { class: "clock" }));
  const paint = () => {
    for (const [c, light, tb] of [[cv, false, tbDark], [cvLight, true, tbLight]]) {
      drawTrayIcon(c, light);
      const [t, d] = clock();
      tb.querySelector(".clock").innerHTML = `${t}<br>${d}`;
    }
  };
  S.live.push(paint);
  S.syncers.push(paint);

  const metricOpts = () => {
    const ids = ["cpu.temp", "cpu.tempmax", "cpu.usage", "cpu.power", "gpu.temp", "gpu.hotspot", "gpu.usage", "gpu.power", "fps.fps", "ram.usage"];
    return [["logo", "程序图标"], ...ids.map(id => [id, info(id).name])];
  };
  const colorRow = () => {
    const auto = h("input", { type: "checkbox", "aria-label": "自动颜色" });
    const pick = h("input", { type: "color", "aria-label": "图标颜色" });
    const syncC = () => {
      const v = get("tray.iconColor");
      auto.checked = v === "auto";
      pick.disabled = v === "auto";
      pick.value = v !== "auto" ? v.toLowerCase() : "#ffffff";
    };
    auto.addEventListener("change", () => { set("tray.iconColor", auto.checked ? "auto" : (pick.value || "#ffffff").toUpperCase()); syncC(); paint(); });
    pick.addEventListener("input", () => { set("tray.iconColor", pick.value.toUpperCase()); paint(); });
    syncC();
    S.syncers.push(syncC);
    return row("数字颜色", "“自动”会根据任务栏深浅色切换黑白；超过阈值时变为橙色或红色。",
      h("label", { class: "hint", style: { display: "flex", gap: "6px", alignItems: "center" } }, auto, "自动"), pick);
  };
  return section("tray", "托盘图标", "任务栏右下角的图标可以直接显示一个数字。鼠标停在图标上会弹出完整数据卡片，左键打开设置，右键打开菜单。",
    h("div", { class: "tray-demo" }, tbDark, tbLight),
    h("div", { class: "rows", style: { marginTop: "14px" } },
      row("图标显示", null, select("tray.iconMetric", metricOpts, paint)),
      colorRow(),
      row("悬停显示数据卡片", "关闭后改为显示系统自带的文字提示。", toggle("tray.hoverCard")),
    ),
    h("p", { class: "hint", style: { marginTop: "14px" } },
      "图标被收进了“︿”里？打开 Windows 设置 → 个性化 → 任务栏 → 其他系统托盘图标，把 Glance 打开，就能一直显示在任务栏上。"));
}

function drawTrayIcon(c, light) {
  const ctx = c.getContext("2d");
  const W = c.width;
  ctx.clearRect(0, 0, W, W);
  const m = get("tray.iconMetric");
  if (m === "logo") {
    const img = drawTrayIcon.img || (drawTrayIcon.img = Object.assign(new Image(), { src: "icon.svg", onload: () => S.live.forEach(f => f()) }));
    if (img.complete) ctx.drawImage(img, 4, 4, W - 8, W - 8);
    return;
  }
  const v = value(m);
  let text = "--", col = null;
  if (m.startsWith("gpu.") && S.snap && S.snap.gpuAsleep) text = "zz";
  else if (v !== undefined) {
    text = String(Math.round(v));
    const it = S.cfg.groups.flatMap(g => g.items).find(i => i.metric === m);
    if (it) {
      const c2 = thresholdColor(it, v, null);
      if (c2) col = light ? (c2 === S.cfg.overlay.warnColor ? "#B86E00" : "#C42B1C") : c2;
    }
  }
  const ic = get("tray.iconColor");
  if (!col) col = ic !== "auto" ? ic : (light ? "#1B1B1B" : "#FFFFFF");
  ctx.fillStyle = col;
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  let size = 44;
  ctx.font = `700 ${size}px "Segoe UI", sans-serif`;
  const w = ctx.measureText(text).width;
  const sx = Math.min(1, (W * 0.98) / w);
  ctx.save();
  ctx.translate(W / 2, W / 2 + 2);
  ctx.scale(sx, 1);
  ctx.fillText(text, 0, 0);
  ctx.restore();
}

/* ---- general ---- */
function buildGeneral() {
  const auto = h("input", { type: "checkbox", role: "switch", "aria-label": "开机自动启动" });
  auto.addEventListener("change", async () => {
    auto.disabled = true;
    try {
      await action("autostart", auto.checked ? "on" : "off");
      toast(auto.checked ? "已设置开机自动启动" : "已取消开机自动启动");
    } finally { auto.disabled = false; poll(); }
  });
  S.live.push(() => { if (document.activeElement !== auto && S.app) auto.checked = !!S.app.autostart; });

  return section("general", "常规", null,
    h("div", { class: "rows" },
      row("开机自动启动", "登录 Windows 后自动在后台运行。使用系统计划任务实现，开机不会弹出管理员确认窗口。",
        h("label", { class: "switch" }, auto, h("span"))),
      row("启动时自动获取管理员权限", "读取 CPU 温度、功耗和统计帧率都需要管理员权限。关闭后只能显示占用率、频率和显卡数据。",
        toggle("general.autoElevate")),
      row("刷新间隔", "数值越小越实时，1 秒是兼顾流畅和省电的选择。",
        seg("general.intervalMs", [[500, "0.5 秒"], [1000, "1 秒"], [2000, "2 秒"]])),
    ),
    h("h2", null, "快捷键"),
    h("div", { class: "rows" },
      row("显示 / 隐藏悬浮窗", null, hotkey("general.hotkeyToggle", "toggle")),
      row("调整悬浮窗位置", "按一次进入拖动模式，再按一次锁定。", hotkey("general.hotkeyEdit", "edit")),
    ),
    h("h2", null, "帧率统计"),
    h("div", { class: "rows" },
      row("统计游戏帧率", "通过 Windows 自带的图形事件统计，不注入游戏，对帧率没有影响。", toggle("general.fpsCapture")),
      row("1% Low 统计时长", "用最近这段时间内的帧计算 1% / 0.1% Low。进出菜单、加载、切出游戏导致帧率大幅变化时，会从新场景重新统计，所以数值反映的是当前画面的流畅度。",
        ...range("general.lowWindowSec", 5, 60, 1, " 秒")),
      row("1% Low 计算方式", "平均值：最慢 1% 帧的平均帧率（常见评测口径）；百分位：第 99 百分位帧时间对应的帧率。",
        seg("general.lowMethod", [["avg", "平均值"], ["pct", "百分位"]])),
    ),
    excludeEditor(),
    h("h2", null, "传感器"),
    h("div", { class: "rows" },
      row("CPU 频率显示", "最高：所有核心里最快的那个；平均：全部核心平均；仅 P 核：只看性能核心的最高频率。",
        seg("general.cpuClockMode", [["max", "最高"], ["avg", "平均"], ["pmax", "仅 P 核"]])),
      row("独显休眠时不读取", "笔记本独显空闲时会断电省电。开启后 Glance 不会去唤醒它，悬浮窗显示“休眠”。建议保持开启。",
        toggle("general.wakeGpu", { invert: true })),
    ));
}

function excludeEditor() {
  const chips = h("div", { class: "chips" });
  const sugg = h("div", { class: "chips" });
  const input = h("input", { type: "text", placeholder: "程序名，例如 obs64.exe", "aria-label": "添加不统计帧率的程序" });
  const list = () => S.cfg.general.fpsExclude || (S.cfg.general.fpsExclude = []);
  const add = name => {
    name = (name || "").trim().toLowerCase();
    if (!name) return;
    if (!name.includes(".")) name += ".exe";
    if (!list().includes(name)) { list().push(name); commit(); }
    paint();
  };
  const paint = () => {
    chips.textContent = "";
    for (const n of list()) {
      chips.append(h("span", { class: "chip" }, n, h("button", { type: "button", title: "移除", "aria-label": "移除 " + n,
        onclick: () => { S.cfg.general.fpsExclude = list().filter(x => x !== n); commit(); paint(); } }, "×")));
    }
    if (!list().length) chips.append(h("span", { class: "hint" }, "列表为空：所有正在渲染的前台程序都会统计帧率。"));
    // processes currently rendering that are not excluded yet
    sugg.textContent = "";
    const rd = (S.snap && S.snap.readings) || [];
    const cur = ((S.snap && S.snap.fpsProcess) || "").toLowerCase();
    const names = rd.filter(r => r.group === "帧率").map(r => r.name.split(" (")[0].toLowerCase()).filter(n => n && n !== cur && !list().includes(n));
    if (names.length) {
      sugg.append(h("span", { class: "hint", style: { alignSelf: "center" } }, "正在渲染："));
      names.forEach(n => sugg.append(h("button", { class: "chip suggest", type: "button", title: "加入排除列表", onclick: () => add(n) }, "+ " + n)));
    }
  };
  input.addEventListener("keydown", e => { if (e.key === "Enter") { add(input.value); input.value = ""; } });
  paint();
  S.syncers.push(paint);
  S.live.push(() => { if (document.activeElement !== input) paint(); });
  return h("div", null,
    h("h2", null, "不统计帧率的程序"),
    h("p", { class: "hint", style: { marginTop: 0 } }, "浏览器、聊天软件这类程序在前台时也会一直刷新画面。把它们放进这个列表，回到桌面时帧率行就不会显示这些程序的数据。"),
    chips,
    h("div", { class: "add-row" }, input,
      h("button", { class: "btn", type: "button", onclick: () => { add(input.value); input.value = ""; } }, "添加"),
      h("button", { class: "btn", type: "button", onclick: () => { S.cfg.general.fpsExclude = [...S.meta.defaults.general.fpsExclude]; commit(); paint(); } }, "恢复默认列表")),
    sugg);
}

/* ---- sensors ---- */
function buildSensors() {
  const checks = h("div", { class: "checks" });
  const procs = h("div");
  const tbody = h("tbody");
  S.live.push(() => {
    const s = S.snap; if (!s) return;
    const st = s.status || {};
    const items = [];
    items.push(st.elevated ? ["ok", "管理员权限", "已获得，所有传感器都可以读取。"] :
      ["bad", "没有管理员权限", "CPU 温度、功耗和帧率统计需要管理员权限。", h("button", { class: "btn primary", onclick: () => action("restart_admin") }, "以管理员身份重启")]);
    const pawn = {
      ok: ["ok", "CPU 温度与功耗", `通过 PawnIO 驱动读取（版本 ${st.pawnioVersion || "未知"}）。`],
      denied: ["warn", "CPU 温度与功耗", "需要管理员权限才能读取。"],
      missing: ["bad", "CPU 温度与功耗", "没有找到 PawnIO 驱动。请从 pawnio.eu 安装后重启 Glance。"],
      failed: ["bad", "CPU 温度与功耗", `PawnIO 驱动加载失败（已安装版本 ${st.pawnioVersion || "未知"}）。请更新到最新版 PawnIO 后重试，详情见日志。`],
    }[st.pawnio] || ["warn", "CPU 温度与功耗", "状态未知。"];
    items.push(pawn);
    if (st.cpuTempSource === "acpi") items.push(["warn", "CPU 温度来源", "正在使用主板 ACPI 温度区，可能不够准确。"]);
    const gpu = s.gpuAsleep ? ["ok", "显卡", "独显正在休眠（省电中），开始游戏后会自动读取数据。"] :
      st.nvml === "ok" ? ["ok", "显卡", `通过 NVIDIA 官方接口读取：${s.gpuName}`] :
      st.nvml === "failed" ? ["warn", "显卡", "NVIDIA 接口加载失败，改用 Windows 计数器（只有占用率和显存）。"] :
      st.gpuSource === "pdh" ? ["warn", "显卡", "未检测到 NVIDIA 显卡，使用 Windows 计数器读取占用率和显存。"] : ["warn", "显卡", "正在检测…"];
    items.push(gpu);
    if ((s.unsupported || {})["gpu.power"]) items.push(["warn", "GPU 功耗", "这块显卡的驱动不提供功耗读数（NVIDIA 对部分笔记本显卡关闭了这项数据，NVIDIA 自带的统计浮窗同样显示不了）。悬浮窗里的 GPU 功耗会自动隐藏。"]);
    const etw = { ok: ["ok", "帧率统计", "运行中。"], denied: ["bad", "帧率统计", "需要管理员权限。"], failed: ["bad", "帧率统计", "启动失败，详情见日志。"], off: ["warn", "帧率统计", "已在“常规”中关闭。"] }[st.etw] || ["warn", "帧率统计", "状态未知"];
    items.push(etw);
    items.push(st.vrm === "ok" ? ["ok", "供电温度", "已检测到。"] : ["warn", "供电温度 (VRM)", "这台电脑没有开放主板供电温度传感器（笔记本通常如此），这一项会自动隐藏。可以改用“CPU 最热核心”或“GPU 热点温度”。"]);
    checks.textContent = "";
    for (const [cls, t, d, extra] of items) {
      checks.append(h("div", { class: "check " + cls }, h("span", { class: "dot" }), h("div", null, h("div", { class: "t" }, t), h("div", { class: "d" }, d)), extra || h("span")));
    }
    const rd = s.readings || [];
    const fpsRows = rd.filter(r => r.group === "帧率");
    procs.textContent = "";
    procs.append(h("p", { class: "hint" }, s.fpsActive ? `正在统计：${s.fpsProcess}（${s.fpsApi}${s.frameGen ? "，已识别帧生成，1% Low 按实际显示节奏计算" : ""}）` : "当前没有统计帧率的程序。前台程序在渲染画面时会自动开始统计。"));
    if (fpsRows.length) procs.append(h("p", { class: "hint" }, "正在渲染的程序：" + fpsRows.map(r => `${r.name} ${Math.round(r.value)} FPS`).join("，")));
    tbody.textContent = "";
    for (const r of rd) {
      if (r.group === "帧率") continue;
      const v = Math.abs(r.value) >= 100 ? Math.round(r.value) : r.value.toFixed(1);
      tbody.append(h("tr", null, h("td", { class: "g" }, r.group), h("td", null, r.name), h("td", { class: "v" }, `${v} ${r.unit}`)));
    }
  });
  return section("sensors", "传感器状态", "各个数据来源是否正常工作。如果某项数据显示“--”，可以在这里找到原因。",
    checks,
    h("h2", null, "帧率"), procs,
    h("h2", null, "全部读数"),
    h("table", { class: "readings" }, h("thead", null, h("tr", null, h("th", null, "类别"), h("th", null, "传感器"), h("th", { style: { textAlign: "right" } }, "当前值"))), tbody),
    h("div", { class: "btn-row" },
      h("button", { class: "btn", onclick: async () => {
        const txt = await action("diagnostics");
        try { await navigator.clipboard.writeText(txt); toast("诊断信息已复制"); } catch { toast("诊断信息已写入日志"); }
      } }, "复制诊断信息"),
      h("button", { class: "btn", onclick: () => action("open_log") }, "打开日志"),
      h("button", { class: "btn", onclick: () => action("open_folder") }, "打开所在文件夹")));
}

function selfUsage() {
  const el = h("span", { class: "num-out", style: { minWidth: "200px" } });
  S.live.push(() => {
    const v = (S.snap && S.snap.values) || {};
    if (v["self.cpu"] === undefined) { el.textContent = "统计中…"; return; }
    el.textContent = `CPU ${v["self.cpu"] < 0.1 ? "<0.1" : v["self.cpu"].toFixed(1)}%  ·  内存 ${Math.round(v["self.ws"] || v["self.mem"])} MB`;
  });
  return el;
}

/* ---- about ---- */
function buildAbout() {
  return section("about", "关于 Glance", null,
    h("div", { class: "rows" },
      row("版本", null, h("span", { class: "num-out" }, S.meta.version)),
      row("Glance 自身占用", "整台电脑的 CPU 占用百分比，以及 Glance 占用的内存。", selfUsage()),
      row("程序位置", null, h("span", { class: "paths" }, S.meta.exe)),
      row("配置文件", "所有设置都保存在这里，可以复制到别的电脑使用。", h("span", { class: "paths" }, S.meta.configPath)),
    ),
    S.meta.configError ? h("p", { class: "err" }, S.meta.configError) : null,
    h("div", { class: "btn-row" },
      h("button", { class: "btn danger", onclick: async () => {
        if (!confirm("确定要把所有设置恢复为默认值吗？")) return;
        S.cfg = await action("reset_config");
        syncAll(); renderGroups(); toast("已恢复默认设置");
      } }, "恢复默认设置"),
      h("button", { class: "btn danger", onclick: async () => {
        if (!confirm("退出 Glance？悬浮窗和托盘图标都会关闭。")) return;
        await action("quit");
        document.body.innerHTML = '<p style="padding:40px">Glance 已退出，可以关闭这个窗口了。</p>';
        setTimeout(() => window.close(), 1500);
      } }, "退出 Glance")),
    h("p", { class: "hint", style: { marginTop: "22px" } },
      "CPU 传感器通过 PawnIO 驱动读取（模块来自 namazso/PawnIO.Modules，LGPL-2.1）。帧率统计的事件定义参考了 Intel PresentMon（MIT）。"));
}

/* ---------------- status pills ---------------- */
function paintUpdate() {
  let bar = $("#update-bar");
  const ready = S.app && S.app.updateReady;
  if (!ready) { if (bar) bar.remove(); return; }
  if (bar) return;
  bar = h("div", { class: "banner", id: "update-bar", style: { margin: "14px 28px 0", maxWidth: "864px" } },
    h("div", null, h("b", null, "新版本已经准备好"), h("p", null, "安装后 Glance 会自动重启，设置不会丢失。")),
    h("button", { class: "btn primary", type: "button", onclick: async e => {
      e.currentTarget.disabled = true;
      e.currentTarget.textContent = "正在重启…";
      await action("apply_update");
      document.body.innerHTML = '<p style="padding:40px;font:15px sans-serif">Glance 正在更新并重启，新的设置窗口会自动打开。</p>';
      setTimeout(() => window.close(), 800);
    } }, "安装并重启"));
  $("#main").prepend(bar);
}

function paintStatus() {
  paintUpdate();
  const s = S.snap; if (!s) return;
  const st = s.status || {};
  const pills = [
    [st.elevated ? "ok" : "bad", st.elevated ? "管理员" : "无管理员权限"],
    [st.pawnio === "ok" ? "ok" : (st.pawnio === "denied" ? "warn" : "bad"), "CPU 传感器"],
    [s.gpuAsleep ? "ok" : (st.nvml === "ok" ? "ok" : "warn"), s.gpuAsleep ? "独显休眠" : "显卡"],
    [st.etw === "ok" ? "ok" : (st.etw === "off" ? "warn" : "bad"), s.fpsActive ? `帧率 · ${Math.round(s.values["fps.fps"] || 0)}` : "帧率"],
  ];
  const box = $("#status");
  box.textContent = "";
  for (const [cls, label] of pills) box.append(h("button", { class: "pill " + cls, type: "button", onclick: () => { location.hash = "#sensors"; } }, label));
}

/* ---------------- polling ---------------- */
async function poll() {
  try {
    const r = await api("/api/state");
    S.snap = r.snapshot;
    S.app = r.app;
    paintStatus();
    updateLive();
    S.live.forEach(f => f());
    renderPreview();
  } catch (e) {
    $("#status").innerHTML = '<span class="pill bad">与 Glance 的连接已断开</span>';
  }
}

async function reloadConfig() {
  S.cfg = await api("/api/config");
  syncAll();
  renderGroups();
}

/* ---------------- boot ---------------- */
async function boot() {
  try {
    [S.meta, S.cfg] = await Promise.all([api("/api/meta"), api("/api/config")]);
  } catch (e) {
    $("#loading").textContent = "无法连接到 Glance。请从托盘图标重新打开设置。（" + e.message + "）";
    return;
  }
  for (const m of S.meta.catalog) S.cat[m.id] = m;
  buildNav();
  buildStage();
  const main = $("#main");
  main.textContent = "";
  main.append(buildOverlay(), buildMetrics(), buildStyle(), buildTray(), buildGeneral(), buildSensors(), buildAbout());
  window.addEventListener("hashchange", route);
  await poll();
  route();
  setInterval(poll, 1000);
}

boot();
