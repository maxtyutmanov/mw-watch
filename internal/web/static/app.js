"use strict";

const MAX_POINTS = 3000; // matches store.MaxPoints
const DAY = 86400e3;
// Shortest length of each step, used to estimate how many points a range produces.
const STEP_MS = { "1m": 60e3, "5m": 300e3, "15m": 900e3, "1h": 3600e3, "1d": DAY, "1w": 7 * DAY, "1mo": 28 * DAY };
const SUB_DAY = new Set(["1m", "5m", "15m", "1h"]);
const TZ = Intl.DateTimeFormat().resolvedOptions().timeZone;
const SVG_NS = "http://www.w3.org/2000/svg";

const $ = (id) => document.getElementById(id);
const els = {
  from: $("from"), to: $("to"), gran: $("granularity"), chart: $("chart"), tooltip: $("tooltip"),
  error: $("error"), active: $("total-active"), screen: $("total-screen"), share: $("share"),
};

let state = { from: null, to: null, gran: "15m", preset: "today" };
let points = [];

// ---- dates ----

const dayStart = (d) => new Date(d.getFullYear(), d.getMonth(), d.getDate());
const addDays = (d, n) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);
const pad = (n) => String(n).padStart(2, "0");
const toInput = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
function fromInput(s) {
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y, m - 1, d);
}
// The selected "to" day is inclusive, so the range ends at the next midnight.
const rangeEnd = () => addDays(state.to, 1);

const PRESETS = {
  today: () => { const t = dayStart(new Date()); return [t, t, "15m"]; },
  yesterday: () => { const t = addDays(dayStart(new Date()), -1); return [t, t, "15m"]; },
  "7d": () => { const t = dayStart(new Date()); return [addDays(t, -6), t, "1h"]; },
  "30d": () => { const t = dayStart(new Date()); return [addDays(t, -29), t, "1d"]; },
  "12mo": () => { const t = dayStart(new Date()); return [new Date(t.getFullYear(), t.getMonth() - 11, 1), t, "1mo"]; },
};

// ---- formatting ----

function fmtDuration(min) {
  if (min < 60) return `${min} min`;
  const h = Math.floor(min / 60), m = min % 60;
  return m ? `${h} h ${m} min` : `${h} h`;
}

const fmtTime = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" });
const fmtDay = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" });
const fmtDayLong = new Intl.DateTimeFormat(undefined, { weekday: "short", month: "short", day: "numeric", year: "numeric" });
const fmtMonth = new Intl.DateTimeFormat(undefined, { month: "short", year: "numeric" });

function axisLabel(d, gran, multiDay) {
  if (gran === "1mo") return fmtMonth.format(d);
  if (!SUB_DAY.has(gran)) return fmtDay.format(d);
  if (multiDay && d.getHours() === 0 && d.getMinutes() === 0) return fmtDay.format(d);
  return fmtTime.format(d);
}

function pointTitle(i) {
  const start = points[i].start;
  const end = i + 1 < points.length ? points[i + 1].start : rangeEnd();
  if (state.gran === "1mo") return fmtMonth.format(start);
  if (state.gran === "1d") return fmtDayLong.format(start);
  if (state.gran === "1w") return `${fmtDay.format(start)} – ${fmtDay.format(addDays(start, 6))}`;
  return `${fmtDayLong.format(start)}, ${fmtTime.format(start)} – ${fmtTime.format(end)}`;
}

// ---- controls ----

function allowed(gran) {
  return (rangeEnd() - state.from) / STEP_MS[gran] <= MAX_POINTS;
}

function syncControls() {
  els.from.value = toInput(state.from);
  els.to.value = toInput(state.to);
  for (const opt of els.gran.options) opt.disabled = !allowed(opt.value);
  if (!allowed(state.gran)) state.gran = [...els.gran.options].find((o) => !o.disabled).value;
  els.gran.value = state.gran;
  for (const b of document.querySelectorAll("[data-preset]")) {
    b.setAttribute("aria-pressed", String(b.dataset.preset === state.preset));
  }
}

function applyPreset(name) {
  const [from, to, gran] = PRESETS[name]();
  state = { from, to, gran, preset: name };
}

function save() {
  try { localStorage.setItem("mw-watch", JSON.stringify({ preset: state.preset, gran: state.gran })); } catch {}
}

function restore() {
  try {
    const saved = JSON.parse(localStorage.getItem("mw-watch") || "null");
    if (saved && PRESETS[saved.preset]) {
      applyPreset(saved.preset);
      if (STEP_MS[saved.gran]) state.gran = saved.gran;
      return;
    }
  } catch {}
  applyPreset("today");
}

// ---- data ----

async function load() {
  syncControls();
  save();
  const params = new URLSearchParams({
    from: state.from.toISOString(), to: rangeEnd().toISOString(), granularity: state.gran, tz: TZ,
  });
  try {
    const res = await fetch(`/api/stats?${params}`);
    if (!res.ok) throw new Error((await res.text()).trim());
    const body = await res.json();
    points = body.points.map((p) => ({ start: new Date(p.start), active: p.activeMin, screen: p.screenMin }));
    els.error.hidden = true;
  } catch (err) {
    points = [];
    els.error.textContent = `Could not load stats: ${err.message}`;
    els.error.hidden = false;
  }
  renderTotals();
  renderChart();
}

function renderTotals() {
  const active = points.reduce((s, p) => s + p.active, 0);
  const screen = points.reduce((s, p) => s + p.screen, 0);
  els.active.textContent = fmtDuration(active);
  els.screen.textContent = fmtDuration(screen);
  els.share.textContent = screen ? `${Math.round((active / screen) * 100)}%` : "–";
}

// ---- chart ----

function niceStep(max, ticks) {
  const raw = max / ticks;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const norm = raw / mag;
  return (norm <= 1 ? 1 : norm <= 2 ? 2 : norm <= 5 ? 5 : 10) * mag;
}

function el(name, attrs, parent) {
  const node = document.createElementNS(SVG_NS, name);
  for (const [k, v] of Object.entries(attrs)) node.setAttribute(k, v);
  if (parent) parent.appendChild(node);
  return node;
}

function renderChart() {
  els.chart.replaceChildren();
  els.tooltip.hidden = true;
  if (!points.length) return;

  // clientWidth is 0 while the page is hidden; draw at a minimum width instead.
  const width = Math.max(els.chart.clientWidth, 240), height = 340;
  const m = { top: 12, right: 8, bottom: 26, left: 52 };
  const plotW = width - m.left - m.right, plotH = height - m.top - m.bottom;

  const maxMin = Math.max(1, ...points.map((p) => Math.max(p.active, p.screen)));
  const useHours = maxMin >= 120;
  const unit = useHours ? 60 : 1;
  const step = niceStep(maxMin / unit, 4);
  const yMax = Math.ceil(maxMin / unit / step) * step * unit;
  const y = (v) => m.top + plotH - (v / yMax) * plotH;

  const svg = el("svg", { viewBox: `0 0 ${width} ${height}` }, els.chart);

  for (let v = 0; v <= yMax + 1e-9; v += step * unit) {
    el("line", { class: v ? "grid" : "axis", x1: m.left, x2: width - m.right, y1: y(v), y2: y(v) }, svg);
    const label = el("text", { x: m.left - 8, y: y(v) + 4, "text-anchor": "end" }, svg);
    label.textContent = `${+(v / unit).toFixed(2)}${useHours ? " h" : " min"}`;
  }

  const bw = plotW / points.length;
  const gap = bw >= 4 ? Math.min(2, bw * 0.15) : 0;
  const bars = el("g", { "shape-rendering": bw < 3 ? "crispEdges" : "auto" }, svg);
  points.forEach((p, i) => {
    const x = m.left + i * bw;
    // Screen-on is drawn full width behind; active sits on top, narrower, so both stay readable.
    if (p.screen) el("rect", { class: "bar-screen", x: x + gap / 2, width: Math.max(bw - gap, 0.5), y: y(p.screen), height: y(0) - y(p.screen) }, bars);
    if (p.active) {
      const aw = bw >= 6 ? (bw - gap) * 0.55 : Math.max(bw - gap, 0.5);
      el("rect", { class: "bar-active", x: x + (bw - aw) / 2, width: aw, y: y(p.active), height: y(0) - y(p.active) }, bars);
    }
  });

  const multiDay = rangeEnd() - state.from > DAY;
  const every = Math.max(1, Math.ceil(points.length / Math.max(2, Math.floor(plotW / 90))));
  for (let i = 0; i < points.length; i += every) {
    const t = el("text", { x: m.left + i * bw + bw / 2, y: height - 8, "text-anchor": "middle" }, svg);
    t.textContent = axisLabel(points[i].start, state.gran, multiDay);
  }

  const hover = el("rect", { class: "hover", x: 0, y: m.top, width: Math.max(bw, 1), height: plotH, visibility: "hidden" }, svg);
  const overlay = el("rect", { x: m.left, y: m.top, width: plotW, height: plotH, fill: "transparent" }, svg);
  overlay.addEventListener("mousemove", (e) => {
    const box = svg.getBoundingClientRect();
    const px = ((e.clientX - box.left) / box.width) * width;
    const i = Math.min(points.length - 1, Math.max(0, Math.floor((px - m.left) / bw)));
    hover.setAttribute("x", m.left + i * bw);
    hover.setAttribute("visibility", "visible");
    showTooltip(i, e);
  });
  overlay.addEventListener("mouseleave", () => {
    hover.setAttribute("visibility", "hidden");
    els.tooltip.hidden = true;
  });
}

function showTooltip(i, e) {
  const p = points[i];
  const tip = els.tooltip;
  tip.replaceChildren();
  const when = document.createElement("div");
  when.className = "when";
  when.textContent = pointTitle(i);
  tip.appendChild(when);
  for (const [cls, label, value] of [["swatch-active", "Active", p.active], ["swatch-screen", "Screen on", p.screen]]) {
    const row = document.createElement("div");
    row.className = "row";
    const sw = document.createElement("span");
    sw.className = `swatch ${cls}`;
    row.append(sw, `${label}: ${fmtDuration(value)}`);
    tip.appendChild(row);
  }
  tip.hidden = false;
  const card = tip.parentElement.getBoundingClientRect();
  let left = e.clientX - card.left + 14;
  if (left + tip.offsetWidth > card.width - 8) left = e.clientX - card.left - tip.offsetWidth - 14;
  tip.style.left = `${Math.max(8, left)}px`;
  tip.style.top = `${Math.max(8, e.clientY - card.top - tip.offsetHeight - 10)}px`;
}

// ---- wiring ----

for (const b of document.querySelectorAll("[data-preset]")) {
  b.addEventListener("click", () => { applyPreset(b.dataset.preset); load(); });
}
els.from.addEventListener("change", () => {
  if (!els.from.value) return;
  state.from = fromInput(els.from.value);
  if (state.to < state.from) state.to = state.from;
  state.preset = null;
  load();
});
els.to.addEventListener("change", () => {
  if (!els.to.value) return;
  state.to = fromInput(els.to.value);
  if (state.to < state.from) state.from = state.to;
  state.preset = null;
  load();
});
els.gran.addEventListener("change", () => { state.gran = els.gran.value; load(); });

let resizeTimer;
window.addEventListener("resize", () => {
  clearTimeout(resizeTimer);
  resizeTimer = setTimeout(renderChart, 100);
});

// Refresh every minute. Presets roll over at midnight; custom ranges stay as
// chosen and only refresh while they include the present.
setInterval(() => {
  if (document.hidden) return;
  if (state.preset) {
    const gran = state.gran;
    applyPreset(state.preset);
    state.gran = gran;
  } else if (rangeEnd() <= new Date()) {
    return;
  }
  load();
}, 60e3);

restore();
load();
