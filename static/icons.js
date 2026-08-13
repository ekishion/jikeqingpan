// 即刻轻盘图标模块：统一使用内联 Lucide SVG，不依赖第三方运行时。

const SVG_NS = "http://www.w3.org/2000/svg";

// 每个图标是若干 [标签, 属性] 元素规格，取自 Lucide（ISC 许可）路径数据。
const ICONS = {
  cloud: [["path", { d: "M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z" }]],
  "log-in": [["path", { d: "M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4" }], ["polyline", { points: "10 17 15 12 10 7" }], ["line", { x1: "15", x2: "3", y1: "12", y2: "12" }]],
  "log-out": [["path", { d: "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" }], ["polyline", { points: "16 17 21 12 16 7" }], ["line", { x1: "21", x2: "9", y1: "12", y2: "12" }]],
  "refresh-cw": [["path", { d: "M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" }], ["path", { d: "M21 3v5h-5" }], ["path", { d: "M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" }], ["path", { d: "M8 16H3v5" }]],
  search: [["circle", { cx: "11", cy: "11", r: "8" }], ["path", { d: "m21 21-4.35-4.35" }]],
  eye: [["path", { d: "M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0" }], ["circle", { cx: "12", cy: "12", r: "3" }]],
  x: [["path", { d: "M18 6 6 18" }], ["path", { d: "m6 6 12 12" }]],
  "shield-lock": [["circle", { cx: "12", cy: "16", r: "1" }], ["rect", { width: "18", height: "12", x: "3", y: "10", rx: "2" }], ["path", { d: "M7 10V7a5 5 0 0 1 10 0v3" }]],
  "key-round": [["path", { d: "M2 18v3c0 .6.4 1 1 1h4v-3h3v-3h2l1.4-1.4a6.5 6.5 0 1 0-4-4Z" }], ["circle", { cx: "16.5", cy: "7.5", r: ".5", fill: "currentColor" }]],
  "chevron-right": [["path", { d: "m9 18 6-6-6-6" }]],
  "corner-left-up": [["polyline", { points: "14 9 9 4 4 9" }], ["path", { d: "M20 20h-7a4 4 0 0 1-4-4V4" }]],
  github: [["path", { d: "M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5.08-1.25-.27-2.48-1-3.5.28-1.15.28-2.35 0-3.5 0 0-1 0-3 1.5-2.64-.5-5.36-.5-8 0C6 2 5 2 5 2c-.3 1.15-.3 2.35 0 3.5A5.403 5.403 0 0 0 4 9c0 3.5 2 5.5 6 5.5-.39.49-.68 1.05-.85 1.65-.17.6-.22 1.23-.15 1.85v4" }], ["path", { d: "M9 18c-4.51 2-5-2-7-2" }]],
  sparkles: [["path", { d: "M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z" }], ["path", { d: "M20 3v4" }], ["path", { d: "M22 5h-4" }], ["path", { d: "M4 17v2" }], ["path", { d: "M5 18H3" }]],
  loader: [["path", { d: "M21 12a9 9 0 1 1-6.219-8.56" }]],
  folder: [["path", { d: "M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z" }]],
  "folder-open": [["path", { d: "m6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2" }]],
  file: [["path", { d: "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" }], ["path", { d: "M14 2v4a2 2 0 0 0 2 2h4" }]],
  image: [["rect", { width: "18", height: "18", x: "3", y: "3", rx: "2" }], ["circle", { cx: "9", cy: "9", r: "2" }], ["path", { d: "m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21" }]],
  film: [["rect", { width: "18", height: "18", x: "3", y: "3", rx: "2" }], ["path", { d: "M7 3v18" }], ["path", { d: "M3 7.5h4" }], ["path", { d: "M3 12h18" }], ["path", { d: "M3 16.5h4" }], ["path", { d: "M17 3v18" }], ["path", { d: "M17 7.5h4" }], ["path", { d: "M17 16.5h4" }]],
  music: [["path", { d: "M9 18V5l12-2v13" }], ["circle", { cx: "6", cy: "18", r: "3" }], ["circle", { cx: "18", cy: "16", r: "3" }]],
  "file-text": [["path", { d: "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" }], ["path", { d: "M14 2v4a2 2 0 0 0 2 2h4" }], ["path", { d: "M16 13H8" }], ["path", { d: "M16 17H8" }], ["path", { d: "M10 9H8" }]],
  "file-spreadsheet": [["path", { d: "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" }], ["path", { d: "M14 2v4a2 2 0 0 0 2 2h4" }], ["path", { d: "M8 13h2" }], ["path", { d: "M14 13h2" }], ["path", { d: "M8 17h2" }], ["path", { d: "M14 17h2" }]],
  archive: [["rect", { width: "20", height: "5", x: "2", y: "3", rx: "1" }], ["path", { d: "M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8" }], ["path", { d: "M10 12h4" }]],
  "file-code": [["path", { d: "M10 12.5 8 15l2 2.5" }], ["path", { d: "m14 12.5 2 2.5-2 2.5" }], ["path", { d: "M14 2v4a2 2 0 0 0 2 2h4" }], ["path", { d: "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" }]],
  package: [["path", { d: "m7.5 4.27 9 5.15" }], ["path", { d: "M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z" }], ["path", { d: "m3.3 7 8.7 5 8.7-5" }], ["path", { d: "M12 22V12" }]],
  smartphone: [["rect", { width: "14", height: "20", x: "5", y: "2", rx: "2", ry: "2" }], ["path", { d: "M12 18h.01" }]],
  link: [["path", { d: "M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" }], ["path", { d: "M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" }]],
  download: [["path", { d: "M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" }], ["polyline", { points: "7 10 12 15 17 10" }], ["line", { x1: "12", x2: "12", y1: "15", y2: "3" }]],
  "circle-check": [["circle", { cx: "12", cy: "12", r: "10" }], ["path", { d: "m9 12 2 2 4-4" }]],
  "circle-alert": [["circle", { cx: "12", cy: "12", r: "10" }], ["line", { x1: "12", x2: "12", y1: "8", y2: "12" }], ["line", { x1: "12", x2: "12.01", y1: "16", y2: "16" }]],
  "triangle-alert": [["path", { d: "m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" }], ["path", { d: "M12 9v4" }], ["path", { d: "M12 17h.01" }]],
  info: [["circle", { cx: "12", cy: "12", r: "10" }], ["path", { d: "M12 16v-4" }], ["path", { d: "M12 8h.01" }]]
};

const ICON_TEMPLATES = Object.create(null);

function buildIconTemplate(name) {
  const spec = ICONS[name] || ICONS.file;
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("fill", "none");
  svg.setAttribute("stroke", "currentColor");
  svg.setAttribute("stroke-width", "2");
  svg.setAttribute("stroke-linecap", "round");
  svg.setAttribute("stroke-linejoin", "round");
  svg.setAttribute("class", "icon");
  svg.setAttribute("aria-hidden", "true");
  svg.setAttribute("focusable", "false");
  for (let i = 0; i < spec.length; i++) {
    const el = document.createElementNS(SVG_NS, spec[i][0]);
    const attrs = spec[i][1];
    for (const k in attrs) el.setAttribute(k, attrs[k]);
    svg.appendChild(el);
  }
  return svg;
}

function makeIcon(name) {
  const key = ICONS[name] ? name : "file";
  let tpl = ICON_TEMPLATES[key];
  if (!tpl) {
    tpl = buildIconTemplate(key);
    ICON_TEMPLATES[key] = tpl;
  }
  return tpl.cloneNode(true);
}

function mountIcons(root) {
  const scope = root || document;
  const nodes = scope.querySelectorAll("[data-icon]");
  for (let i = 0; i < nodes.length; i++) {
    const el = nodes[i];
    if (el.dataset.iconMounted === "1") continue;
    const name = el.getAttribute("data-icon");
    if (name) {
      el.appendChild(makeIcon(name));
      el.dataset.iconMounted = "1";
    }
  }
}
