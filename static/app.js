// 即刻轻盘前端逻辑（纯白极简 + 文件夹浏览 + 访问验证）
// 全部使用 DOM API 操作，防止 XSS（禁止使用 innerHTML）
// 图标统一用内联 Lucide SVG，界面不使用 emoji

// ===== 全局状态 =====
const DIR_STORAGE_KEY = "jikeqingpan_current_dir";
let currentDir = sessionStorage.getItem(DIR_STORAGE_KEY) || "/";
let authRequired = false;
let authenticated = false;
let toastContainer = null;

// ===== Lucide 图标（内联 SVG，禁用 emoji / innerHTML；全部经 DOM API 构建） =====
const SVG_NS = "http://www.w3.org/2000/svg";

// 每个图标是若干 [标签, 属性] 元素规格，取自 Lucide（ISC 许可）路径数据。
const ICONS = {
  cloud: [["path", { d: "M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z" }]],
  "log-in": [
    ["path", { d: "M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4" }],
    ["polyline", { points: "10 17 15 12 10 7" }],
    ["line", { x1: "15", x2: "3", y1: "12", y2: "12" }]
  ],
  "log-out": [
    ["path", { d: "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" }],
    ["polyline", { points: "16 17 21 12 16 7" }],
    ["line", { x1: "21", x2: "9", y1: "12", y2: "12" }]
  ],
  "refresh-cw": [
    ["path", { d: "M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8" }],
    ["path", { d: "M21 3v5h-5" }],
    ["path", { d: "M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16" }],
    ["path", { d: "M8 16H3v5" }]
  ],
  "shield-lock": [
    ["circle", { cx: "12", cy: "16", r: "1" }],
    ["rect", { width: "18", height: "12", x: "3", y: "10", rx: "2" }],
    ["path", { d: "M7 10V7a5 5 0 0 1 10 0v3" }]
  ],
  "key-round": [
    ["path", { d: "M2 18v3c0 .6.4 1 1 1h4v-3h3v-3h2l1.4-1.4a6.5 6.5 0 1 0-4-4Z" }],
    ["circle", { cx: "16.5", cy: "7.5", r: ".5", fill: "currentColor" }]
  ],
  "chevron-right": [["path", { d: "m9 18 6-6-6-6" }]],
  "corner-left-up": [
    ["polyline", { points: "14 9 9 4 4 9" }],
    ["path", { d: "M20 20h-7a4 4 0 0 1-4-4V4" }]
  ],
  github: [
    ["path", { d: "M15 22v-4a4.8 4.8 0 0 0-1-3.5c3 0 6-2 6-5.5.08-1.25-.27-2.48-1-3.5.28-1.15.28-2.35 0-3.5 0 0-1 0-3 1.5-2.64-.5-5.36-.5-8 0C6 2 5 2 5 2c-.3 1.15-.3 2.35 0 3.5A5.403 5.403 0 0 0 4 9c0 3.5 3 5.5 6 5.5-.39.49-.68 1.05-.85 1.65-.17.6-.22 1.23-.15 1.85v4" }],
    ["path", { d: "M9 18c-4.51 2-5-2-7-2" }]
  ],
  sparkles: [
    ["path", { d: "M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z" }],
    ["path", { d: "M20 3v4" }],
    ["path", { d: "M22 5h-4" }],
    ["path", { d: "M4 17v2" }],
    ["path", { d: "M5 18H3" }]
  ],
  loader: [["path", { d: "M21 12a9 9 0 1 1-6.219-8.56" }]],
  folder: [["path", { d: "M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z" }]],
  "folder-open": [["path", { d: "m6 14 1.5-2.9A2 2 0 0 1 9.24 10H20a2 2 0 0 1 1.94 2.5l-1.54 6a2 2 0 0 1-1.95 1.5H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h3.9a2 2 0 0 1 1.69.9l.81 1.2a2 2 0 0 0 1.67.9H18a2 2 0 0 1 2 2v2" }]],
  file: [
    ["path", { d: "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" }],
    ["path", { d: "M14 2v4a2 2 0 0 0 2 2h4" }]
  ],
  image: [
    ["rect", { width: "18", height: "18", x: "3", y: "3", rx: "2", ry: "2" }],
    ["circle", { cx: "9", cy: "9", r: "2" }],
    ["path", { d: "m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21" }]
  ],
  film: [
    ["rect", { width: "18", height: "18", x: "3", y: "3", rx: "2" }],
    ["path", { d: "M7 3v18" }],
    ["path", { d: "M3 7.5h4" }],
    ["path", { d: "M3 12h18" }],
    ["path", { d: "M3 16.5h4" }],
    ["path", { d: "M17 3v18" }],
    ["path", { d: "M17 7.5h4" }],
    ["path", { d: "M17 16.5h4" }]
  ],
  music: [
    ["path", { d: "M9 18V5l12-2v13" }],
    ["circle", { cx: "6", cy: "18", r: "3" }],
    ["circle", { cx: "18", cy: "16", r: "3" }]
  ],
  "file-text": [
    ["path", { d: "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" }],
    ["path", { d: "M14 2v4a2 2 0 0 0 2 2h4" }],
    ["path", { d: "M16 13H8" }],
    ["path", { d: "M16 17H8" }],
    ["path", { d: "M10 9H8" }]
  ],
  "file-spreadsheet": [
    ["path", { d: "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" }],
    ["path", { d: "M14 2v4a2 2 0 0 0 2 2h4" }],
    ["path", { d: "M8 13h2" }],
    ["path", { d: "M14 13h2" }],
    ["path", { d: "M8 17h2" }],
    ["path", { d: "M14 17h2" }]
  ],
  archive: [
    ["rect", { width: "20", height: "5", x: "2", y: "3", rx: "1" }],
    ["path", { d: "M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8" }],
    ["path", { d: "M10 12h4" }]
  ],
  "file-code": [
    ["path", { d: "M10 12.5 8 15l2 2.5" }],
    ["path", { d: "m14 12.5 2 2.5-2 2.5" }],
    ["path", { d: "M14 2v4a2 2 0 0 0 2 2h4" }],
    ["path", { d: "M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z" }]
  ],
  package: [
    ["path", { d: "m7.5 4.27 9 5.15" }],
    ["path", { d: "M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z" }],
    ["path", { d: "m3.3 7 8.7 5 8.7-5" }],
    ["path", { d: "M12 22V12" }]
  ],
  smartphone: [
    ["rect", { width: "14", height: "20", x: "5", y: "2", rx: "2", ry: "2" }],
    ["path", { d: "M12 18h.01" }]
  ],
  link: [
    ["path", { d: "M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" }],
    ["path", { d: "M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" }]
  ],
  download: [
    ["path", { d: "M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" }],
    ["polyline", { points: "7 10 12 15 17 10" }],
    ["line", { x1: "12", x2: "12", y1: "15", y2: "3" }]
  ],
  "circle-check": [
    ["circle", { cx: "12", cy: "12", r: "10" }],
    ["path", { d: "m9 12 2 2 4-4" }]
  ],
  "circle-alert": [
    ["circle", { cx: "12", cy: "12", r: "10" }],
    ["line", { x1: "12", x2: "12", y1: "8", y2: "12" }],
    ["line", { x1: "12", x2: "12.01", y1: "16", y2: "16" }]
  ],
  "triangle-alert": [
    ["path", { d: "m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" }],
    ["path", { d: "M12 9v4" }],
    ["path", { d: "M12 17h.01" }]
  ],
  info: [
    ["circle", { cx: "12", cy: "12", r: "10" }],
    ["path", { d: "M12 16v-4" }],
    ["path", { d: "M12 8h.01" }]
  ]
};

// 每个图标模板只构建一次，之后用 cloneNode 复制，减少重复的 DOM 创建与内存分配。
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
    for (const k in attrs) {
      el.setAttribute(k, attrs[k]);
    }
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

// setButtonLoading 切换按钮的加载态：图标换成旋转指示器、文案临时替换、禁用点击。
// 还原时恢复原图标与原文案。
function setButtonLoading(btn, loading, loadingText) {
  if (!btn) return;
  const iconWrap = btn.querySelector(".btn-icon");
  const labelEl = btn.querySelector(".btn-label");
  if (loading) {
    if (btn.dataset.loading === "1") return;
    btn.dataset.loading = "1";
    btn.disabled = true;
    btn.classList.add("is-loading");
    if (labelEl && loadingText) {
      btn.dataset.prevLabel = labelEl.textContent;
      labelEl.textContent = loadingText;
    }
    if (iconWrap) {
      btn.dataset.prevIcon = iconWrap.getAttribute("data-icon") || "";
      iconWrap.replaceChildren(makeIcon("loader"));
    }
    return;
  }
  if (btn.dataset.loading !== "1") return;
  delete btn.dataset.loading;
  btn.disabled = false;
  btn.classList.remove("is-loading");
  if (labelEl && btn.dataset.prevLabel) {
    labelEl.textContent = btn.dataset.prevLabel;
    delete btn.dataset.prevLabel;
  }
  if (iconWrap) {
    const prev = btn.dataset.prevIcon;
    iconWrap.replaceChildren(makeIcon(prev || "log-in"));
    delete btn.dataset.prevIcon;
  }
}

// 把带 data-icon 属性的占位元素填充为对应 SVG（幂等）。
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

// ===== 工具函数 =====

function formatSize(bytes) {
  if (bytes === undefined || bytes === null || bytes === "") return "-";
  const v = Number(bytes);
  if (isNaN(v) || v < 0) return "-";
  if (v === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let val = v;
  while (val >= 1024 && i < units.length - 1) {
    val /= 1024;
    i++;
  }
  return val.toFixed(i === 0 ? 0 : 1) + " " + units[i];
}

function formatTime(ts) {
  if (!ts) return "-";
  const d = new Date(Number(ts) * 1000);
  return d.toLocaleString("zh-CN", { hour12: false });
}

// 依扩展名解析 { icon: Lucide 图标名, cat: 颜色分类 }
const FILE_TYPE_MAP = {
  jpg: ["image", "image"], jpeg: ["image", "image"], png: ["image", "image"],
  gif: ["image", "image"], webp: ["image", "image"], svg: ["image", "image"],
  bmp: ["image", "image"], ico: ["image", "image"], heic: ["image", "image"],
  mp4: ["film", "video"], mov: ["film", "video"], avi: ["film", "video"],
  mkv: ["film", "video"], flv: ["film", "video"], webm: ["film", "video"], wmv: ["film", "video"],
  mp3: ["music", "audio"], flac: ["music", "audio"], wav: ["music", "audio"],
  aac: ["music", "audio"], ogg: ["music", "audio"], m4a: ["music", "audio"],
  pdf: ["file-text", "doc"], doc: ["file-text", "doc"], docx: ["file-text", "doc"],
  txt: ["file-text", "doc"], md: ["file-text", "doc"], rtf: ["file-text", "doc"],
  ppt: ["file-text", "doc"], pptx: ["file-text", "doc"],
  xls: ["file-spreadsheet", "sheet"], xlsx: ["file-spreadsheet", "sheet"], csv: ["file-spreadsheet", "sheet"],
  zip: ["archive", "archive"], rar: ["archive", "archive"], "7z": ["archive", "archive"],
  tar: ["archive", "archive"], gz: ["archive", "archive"], bz2: ["archive", "archive"],
  exe: ["package", "app"], msi: ["package", "app"], dmg: ["package", "app"], apk: ["smartphone", "app"],
  json: ["file-code", "code"], xml: ["file-code", "code"], yaml: ["file-code", "code"],
  yml: ["file-code", "code"], html: ["file-code", "code"], css: ["file-code", "code"],
  js: ["file-code", "code"], ts: ["file-code", "code"], go: ["file-code", "code"], py: ["file-code", "code"]
};

function fileIconMeta(name, isDir) {
  if (isDir) return { icon: "folder", cat: "folder" };
  const ext = (name || "").split(".").pop().toLowerCase();
  const hit = FILE_TYPE_MAP[ext];
  if (hit) return { icon: hit[0], cat: hit[1] };
  return { icon: "file", cat: "default" };
}

const TOAST_ICONS = {
  success: "circle-check",
  error: "circle-alert",
  warning: "triangle-alert",
  info: "info",
  loading: "loader"
};

// 同时最多保留 3 条提示，超出时挤掉最旧的，避免连点后铺满屏幕。
const TOAST_MAX = 3;
const TOAST_DURATION = 3000;

function showToast(msg, type) {
  const kind = TOAST_ICONS[type] ? type : "info";
  if (!toastContainer) {
    toastContainer = document.createElement("div");
    toastContainer.id = "toast-container";
    toastContainer.setAttribute("aria-live", "polite");
    toastContainer.setAttribute("aria-atomic", "false");
    document.body.appendChild(toastContainer);
  }

  // 内容相同的提示不重复堆叠，只重置它的存活时间。
  const existing = toastContainer.lastElementChild;
  if (existing && existing.dataset.msg === msg && existing.dataset.dismissed !== "true") {
    if (existing._timer) clearTimeout(existing._timer);
    existing._timer = setTimeout(function () {
      if (existing._dismiss) existing._dismiss();
    }, TOAST_DURATION);
    return;
  }

  while (toastContainer.childElementCount >= TOAST_MAX) {
    toastContainer.removeChild(toastContainer.firstElementChild);
  }

  const toast = document.createElement("div");
  toast.className = "toast toast-" + kind;
  toast.setAttribute("role", "status");
  toast.dataset.msg = msg;

  const iconWrap = document.createElement("span");
  iconWrap.className = "toast-icon";
  iconWrap.appendChild(makeIcon(TOAST_ICONS[kind]));
  const label = document.createElement("span");
  label.className = "toast-text";
  label.textContent = msg;
  toast.appendChild(iconWrap);
  toast.appendChild(label);
  toastContainer.appendChild(toast);

  requestAnimationFrame(function () {
    toast.classList.add("show");
  });

  const dismiss = function () {
    if (toast.dataset.dismissed === "true") return;
    toast.dataset.dismissed = "true";
    if (toast._timer) clearTimeout(toast._timer);
    toast.classList.remove("show");
    setTimeout(function () {
      toast.remove();
    }, 220);
  };
  toast._dismiss = dismiss;

  toast.addEventListener("click", dismiss);
  toast._timer = setTimeout(dismiss, TOAST_DURATION);
}

function setCurrentDir(dir) {
  currentDir = dir || "/";
  try {
    sessionStorage.setItem(DIR_STORAGE_KEY, currentDir);
  } catch (e) {
    /* ignore */
  }
}

function parseAPIError(resp, bodyText) {
  try {
    const data = JSON.parse(bodyText);
    if (data && data.error && data.error.message) {
      return data.error.message;
    }
  } catch (e) {
    /* plain text */
  }
  // 面向使用者的兜底文案：说明发生了什么 + 下一步怎么做，不暴露内部术语。
  if (resp.status === 401) return "登录已失效，请重新验证";
  if (resp.status === 403) return "页面已过期，请刷新后重试";
  if (resp.status === 429) return "操作太频繁了，请稍后再试";
  if (resp.status >= 500) return "服务暂时不可用，请稍后重试";
  return "操作失败，请稍后重试";
}

// ===== 鉴权 =====

function ensureLoginOpenButton() {
  let loginBtn = document.getElementById("btn-login-open");
  if (loginBtn) return loginBtn;

  const header = document.querySelector(".header-actions") || document.querySelector("header") || document.body;
  loginBtn = document.createElement("button");
  loginBtn.type = "button";
  loginBtn.className = "btn btn-ghost";
  loginBtn.id = "btn-login-open";
  const icon = document.createElement("span");
  icon.className = "btn-icon";
  icon.appendChild(makeIcon("log-in"));
  const label = document.createElement("span");
  label.className = "btn-label";
  label.textContent = "登录";
  loginBtn.appendChild(icon);
  loginBtn.appendChild(label);
  loginBtn.hidden = true;
  header.appendChild(loginBtn);
  loginBtn.addEventListener("click", function () {
    requireLogin("");
  });
  return loginBtn;
}

function ensureLoginUI() {
  ensureLoginOpenButton();

  let overlay = document.getElementById("login-overlay");
  if (overlay) {
    // 旧 DOM 若缺表单，补齐最小结构
    if (!document.getElementById("login-form")) {
      overlay.replaceChildren();
    } else {
      bindLoginForm(document.getElementById("login-form"));
      return overlay;
    }
  } else {
    overlay = document.createElement("div");
    overlay.id = "login-overlay";
    document.body.appendChild(overlay);
  }

  // 旧页面/缓存未嵌入登录层时，动态创建，避免“永远没有弹窗”
  overlay.setAttribute("aria-hidden", "true");
  overlay.hidden = true;

  const card = document.createElement("div");
  card.className = "login-card";
  card.setAttribute("role", "dialog");
  card.setAttribute("aria-modal", "true");
  card.setAttribute("aria-labelledby", "login-title");

  const mark = document.createElement("span");
  mark.className = "login-mark";
  mark.appendChild(makeIcon("shield-lock"));

  const h2 = document.createElement("h2");
  h2.id = "login-title";
  h2.textContent = "验证访问权限";

  const p = document.createElement("p");
  p.textContent = "该网盘已开启访问保护，输入访问令牌即可浏览和下载文件。";

  const form = document.createElement("form");
  form.id = "login-form";

  const label = document.createElement("label");
  label.setAttribute("for", "login-token");
  label.textContent = "访问令牌";

  const inputWrap = document.createElement("div");
  inputWrap.className = "input-wrap";
  const inputIcon = document.createElement("span");
  inputIcon.className = "input-icon";
  inputIcon.appendChild(makeIcon("key-round"));
  const input = document.createElement("input");
  input.id = "login-token";
  input.name = "access_token";
  input.type = "password";
  input.autocomplete = "current-password";
  input.required = true;
  input.placeholder = "请输入访问令牌";
  inputWrap.appendChild(inputIcon);
  inputWrap.appendChild(input);

  const actions = document.createElement("div");
  actions.className = "login-actions";
  const btn = document.createElement("button");
  btn.type = "submit";
  btn.id = "btn-login";
  btn.className = "btn btn-primary";
  const btnIcon = document.createElement("span");
  btnIcon.className = "btn-icon";
  btnIcon.appendChild(makeIcon("log-in"));
  const btnLabel = document.createElement("span");
  btnLabel.className = "btn-label";
  btnLabel.textContent = "验证并进入";
  btn.appendChild(btnIcon);
  btn.appendChild(btnLabel);
  actions.appendChild(btn);

  const err = document.createElement("div");
  err.id = "login-error";
  err.className = "login-error";
  err.hidden = true;

  form.appendChild(label);
  form.appendChild(inputWrap);
  form.appendChild(actions);
  form.appendChild(err);
  card.appendChild(mark);
  card.appendChild(h2);
  card.appendChild(p);
  card.appendChild(form);
  overlay.appendChild(card);

  bindLoginForm(form);
  return overlay;
}

function showLogin(show) {
  const overlay = ensureLoginUI();
  if (show) {
    overlay.hidden = false;
    overlay.removeAttribute("hidden");
    overlay.style.display = "flex";
    overlay.setAttribute("aria-hidden", "false");
  } else {
    overlay.hidden = true;
    overlay.setAttribute("hidden", "");
    overlay.style.display = "none";
    overlay.setAttribute("aria-hidden", "true");
  }

  const app = document.getElementById("app-main");
  if (app) app.setAttribute("aria-hidden", show ? "true" : "false");

  if (show) {
    const input = document.getElementById("login-token");
    if (input) {
      // 不强制清空，方便输错后重试
      setTimeout(function () {
        input.focus();
        input.select();
      }, 50);
    }
  }
}

function updateAuthUI() {
  const logoutBtn = document.getElementById("btn-logout");
  const loginBtn = document.getElementById("btn-login-open");
  if (logoutBtn) {
    logoutBtn.hidden = !(authRequired && authenticated);
  }
  if (loginBtn) {
    // 需要鉴权且未登录时，始终提供手动入口
    loginBtn.hidden = !(authRequired && !authenticated);
  }
}

// requireLogin 进入「需要验证」状态。
// 同一条信息只报一次：具体错误显示在验证弹窗内，背后列表区只放安静的占位态，
// 不再叠加横幅与 toast（避免同一件事被播报多次）。
function requireLogin(message) {
  authRequired = true;
  authenticated = false;
  updateAuthUI();
  showLogin(true);

  renderStatusState({
    icon: "shield-lock",
    title: "需要验证后查看",
    desc: "完成验证即可浏览和下载文件",
    actionLabel: "去验证",
    actionIcon: "key-round",
    onAction: function () {
      showLogin(true);
      const input = document.getElementById("login-token");
      if (input) input.focus();
    }
  });

  const bannerEl = document.getElementById("list-banner");
  if (bannerEl) {
    bannerEl.hidden = true;
    bannerEl.replaceChildren();
  }

  // 未验证时列表不可用，计数不应停留在「加载中…」
  const countEl = document.getElementById("file-count");
  if (countEl) countEl.textContent = "未验证";

  const refreshBtn = document.getElementById("btn-refresh");
  if (refreshBtn) refreshBtn.disabled = false;

  const errEl = document.getElementById("login-error");
  if (errEl) {
    // 无具体错误时清空，避免上一次的提示残留
    errEl.hidden = !message;
    errEl.textContent = message || "";
  }
}

// renderStatusState 统一渲染列表区的空态/错误态：
// 图标 + 标题 + 说明 + 可选操作按钮（错误态必须给出重试入口）。
function renderStatusState(opts) {
  const statusEl = document.getElementById("status");
  if (!statusEl) return;
  statusEl.style.display = "block";
  statusEl.replaceChildren();

  const box = document.createElement("div");
  box.className = "state" + (opts.tone === "error" ? " state-error" : "");

  const iconWrap = document.createElement("div");
  iconWrap.className = "state-icon";
  iconWrap.appendChild(makeIcon(opts.icon || "info"));
  box.appendChild(iconWrap);

  const title = document.createElement("div");
  title.className = "state-title";
  title.textContent = opts.title;
  box.appendChild(title);

  if (opts.desc) {
    const desc = document.createElement("div");
    desc.className = "state-desc";
    desc.textContent = opts.desc;
    box.appendChild(desc);
  }

  if (opts.actionLabel && typeof opts.onAction === "function") {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "btn btn-ghost state-action";
    const ic = document.createElement("span");
    ic.className = "btn-icon";
    ic.appendChild(makeIcon(opts.actionIcon || "refresh-cw"));
    const lb = document.createElement("span");
    lb.className = "btn-label";
    lb.textContent = opts.actionLabel;
    btn.appendChild(ic);
    btn.appendChild(lb);
    btn.addEventListener("click", opts.onAction);
    box.appendChild(btn);
  }

  statusEl.appendChild(box);
}

// renderBanner 在列表横幅区渲染带图标的提示（icon 元素 + 文本 span）。
function renderBanner(kind, text) {
  const bannerEl = document.getElementById("list-banner");
  if (!bannerEl) return;
  bannerEl.hidden = false;
  bannerEl.replaceChildren();
  const box = document.createElement("div");
  box.className = "banner-warning";
  box.appendChild(makeIcon(kind === "warning" ? "triangle-alert" : "info"));
  const span = document.createElement("span");
  span.textContent = text;
  box.appendChild(span);
  bannerEl.appendChild(box);
}

function checkAuth() {
  return apiFetch("/api/auth/status", { method: "GET" })
    .then(function (resp) {
      return resp.text().then(function (text) {
        let data = null;
        try {
          data = JSON.parse(text);
        } catch (e) {
          data = null;
        }
        return { resp: resp, data: data, text: text };
      });
    })
    .then(function (result) {
      if (!result.resp.ok || !result.data) {
        throw new Error("无法确认登录状态，请刷新页面重试");
      }
      authRequired = !!result.data.auth_required;
      authenticated = !!result.data.authenticated;
      updateAuthUI();
      if (authRequired && !authenticated) {
        // 首次进入尚未发生错误，弹窗内不显示错误文案
        requireLogin("");
        return false;
      }
      showLogin(false);
      return true;
    });
}

function doLogin(token) {
  return apiFetch("/api/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ token: token })
  }).then(function (resp) {
    return resp.text().then(function (text) {
      if (!resp.ok) {
        throw new Error(parseAPIError(resp, text));
      }
      authRequired = true;
      authenticated = true;
      updateAuthUI();
      showLogin(false);
      showToast("验证成功", "success");
      return loadFiles(currentDir);
    });
  });
}

function doLogout() {
  return apiFetch("/api/logout", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: "{}"
  })
    .catch(function () {
      /* still clear UI */
    })
    .then(function () {
      authenticated = false;
      updateAuthUI();
      const listEl = document.getElementById("file-list");
      if (listEl) listEl.replaceChildren();
      if (authRequired) {
        // 主动退出是预期操作，不算错误：弹窗内不显示红色错误文案
        requireLogin("");
        showToast("已退出登录", "info");
      } else {
        showLogin(false);
        showToast("已退出登录", "info");
      }
    });
}

function bindLoginForm(loginForm) {
  if (!loginForm || loginForm.dataset.bound === "1") return;
  loginForm.dataset.bound = "1";
  loginForm.addEventListener("submit", function (e) {
    e.preventDefault();
    const input = document.getElementById("login-token");
    const token = input ? input.value.trim() : "";
    const errEl = document.getElementById("login-error");
    if (errEl) {
      errEl.hidden = true;
      errEl.textContent = "";
    }
    if (!token) {
      if (errEl) {
        errEl.hidden = false;
        errEl.textContent = "请输入访问令牌";
      }
      if (input) input.focus();
      return;
    }
    const btn = document.getElementById("btn-login");
    // 提交中：按钮进入 loading 态，避免重复提交（弹窗内已有错误提示，不再叠加 toast）
    setButtonLoading(btn, true, "验证中…");
    doLogin(token)
      .catch(function (err) {
        if (errEl) {
          errEl.hidden = false;
          errEl.textContent = err.message || "验证失败，请重试";
        }
        showLogin(true);
        if (input) {
          input.focus();
          input.select();
        }
      })
      .finally(function () {
        setButtonLoading(btn, false);
      });
  });
}

function renderBreadcrumbs() {
  const container = document.getElementById("breadcrumbs");
  if (!container) return;
  container.replaceChildren();

  const rootSpan = document.createElement("span");
  if (currentDir === "/") {
    rootSpan.className = "breadcrumb-current";
    rootSpan.textContent = "根目录";
    container.appendChild(rootSpan);
  } else {
    rootSpan.className = "breadcrumb-item";
    rootSpan.textContent = "根目录";
    rootSpan.addEventListener("click", function () {
      enterDir("/");
    });
    container.appendChild(rootSpan);
  }

  if (currentDir === "/") return;

  const parts = currentDir.split("/").filter(function (p) {
    return p !== "";
  });
  let accPath = "";
  parts.forEach(function (part, index) {
    const sep = document.createElement("span");
    sep.className = "breadcrumb-separator";
    sep.setAttribute("aria-hidden", "true");
    sep.appendChild(makeIcon("chevron-right"));
    container.appendChild(sep);

    accPath += "/" + part;
    const isLast = index === parts.length - 1;
    const span = document.createElement("span");
    if (isLast) {
      span.className = "breadcrumb-current";
      span.textContent = part;
      container.appendChild(span);
    } else {
      span.className = "breadcrumb-item";
      span.textContent = part;
      const targetPath = accPath;
      span.addEventListener("click", function () {
        enterDir(targetPath);
      });
      container.appendChild(span);
    }
  });
}

function enterDir(dir) {
  setCurrentDir(dir);
  loadFiles(dir);
}

// ===== 文件列表加载 =====

function loadFiles(dir) {
  const targetDir = dir || currentDir;
  const listEl = document.getElementById("file-list");
  const statusEl = document.getElementById("status");
  const countEl = document.getElementById("file-count");
  const refreshBtn = document.getElementById("btn-refresh");
  const bannerEl = document.getElementById("list-banner");

  renderBreadcrumbs();
  listEl.replaceChildren();
  if (bannerEl) {
    bannerEl.hidden = true;
    bannerEl.replaceChildren();
  }
  statusEl.style.display = "block";
  statusEl.replaceChildren();

  const spinner = document.createElement("div");
  spinner.className = "spinner";
  spinner.appendChild(makeIcon("loader"));
  const tip = document.createElement("div");
  tip.className = "status-tip";
  tip.textContent = "正在加载文件列表…";
  statusEl.appendChild(spinner);
  statusEl.appendChild(tip);

  if (refreshBtn) refreshBtn.disabled = true;
  if (countEl) countEl.textContent = "加载中…";

  return apiFetch("/api/files", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ dir: targetDir })
  })
    .then(function (resp) {
      return resp.text().then(function (text) {
        if (resp.status === 401) {
          requireLogin(parseAPIError(resp, text));
          throw new Error(parseAPIError(resp, text));
        }
        if (!resp.ok) {
          throw new Error(parseAPIError(resp, text));
        }
        return JSON.parse(text);
      });
    })
    .then(function (data) {
      statusEl.style.display = "none";
      if (refreshBtn) refreshBtn.disabled = false;

      if (data.errno !== undefined && data.errno !== 0) {
        // 内部错误码只写入控制台，界面上给使用者可理解的说明
        console.warn("[网盘] 百度返回错误 errno=" + data.errno, data.show_msg || "");
        throw new Error("网盘服务返回异常，请稍后重试");
      }

      const files = Array.isArray(data.list) ? data.list : [];
      sortFiles(files);

      if (countEl) {
        countEl.textContent = data.truncated
          ? "已显示前 " + files.length + " 项"
          : "共 " + files.length + " 项";
      }

      if (data.truncated) {
        const limit = data.list_page_limit || 1500;
        renderBanner(
          "warning",
          "文件较多，当前仅显示前 " + limit + " 项。进入子文件夹可查看完整内容。"
        );
      }

      // 用 DocumentFragment 批量插入，避免逐行 appendChild 触发多次回流
      const frag = document.createDocumentFragment();
      if (targetDir !== "/") {
        frag.appendChild(buildParentItem());
      }

      if (!files.length) {
        renderStatusState({
          icon: "folder-open",
          title: "这里还没有文件",
          desc: targetDir === "/" ? "上传文件到网盘后，就会显示在这里" : "该文件夹是空的"
        });
        if (frag.childNodes.length) listEl.appendChild(frag);
        return;
      }

      for (let i = 0; i < files.length; i++) {
        frag.appendChild(buildFileItem(files[i]));
      }
      listEl.appendChild(frag);
    })
    .catch(function (err) {
      if (refreshBtn) refreshBtn.disabled = false;
      // 未登录已由 requireLogin 接管界面，避免再盖一层错误态
      if (authRequired && !authenticated) return;
      renderStatusState({
        tone: "error",
        icon: "circle-alert",
        title: "文件列表加载失败",
        desc: err && err.message ? err.message : "请检查网络后重试",
        actionLabel: "重新加载",
        onAction: function () {
          loadFiles(targetDir);
        }
      });
      if (countEl) countEl.textContent = "加载失败";
      console.warn("[网盘] 列表失败", err);
    });
}

function sortFiles(files) {
  files.sort(function (a, b) {
    const aDir = Number(a.isdir) === 1 ? 0 : 1;
    const bDir = Number(b.isdir) === 1 ? 0 : 1;
    if (aDir !== bDir) return aDir - bDir;
    const an = (a.server_filename || a.filename || a.path || "").toLowerCase();
    const bn = (b.server_filename || b.filename || b.path || "").toLowerCase();
    if (an < bn) return -1;
    if (an > bn) return 1;
    return 0;
  });
}

function buildParentItem() {
  const item = document.createElement("div");
  item.className = "file-item is-dir";
  item.setAttribute("role", "button");
  item.tabIndex = 0;

  const icon = document.createElement("div");
  icon.className = "file-icon";
  icon.setAttribute("data-cat", "folder");
  icon.appendChild(makeIcon("corner-left-up"));

  const info = document.createElement("div");
  info.className = "file-info";
  const name = document.createElement("div");
  name.className = "file-name";
  name.textContent = "返回上级";
  const meta = document.createElement("div");
  meta.className = "file-meta";
  meta.textContent = "返回上一层";
  info.appendChild(name);
  info.appendChild(meta);

  item.appendChild(icon);
  item.appendChild(info);

  const parent = currentDir.replace(/\/+$/, "").split("/").slice(0, -1).join("/") || "/";
  const go = function () {
    enterDir(parent);
  };
  item.addEventListener("click", go);
  item.addEventListener("keydown", function (e) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      go();
    }
  });
  return item;
}

function buildFileItem(file) {
  const isDir = Number(file.isdir) === 1;
  const fileName = file.server_filename || file.filename || (file.path || "").split("/").pop() || "未命名";
  const filePath = file.path || "";

  const item = document.createElement("div");
  item.className = "file-item" + (isDir ? " is-dir" : "");

  const meta0 = fileIconMeta(fileName, isDir);
  const icon = document.createElement("div");
  icon.className = "file-icon";
  icon.setAttribute("data-cat", meta0.cat);
  icon.appendChild(makeIcon(meta0.icon));

  const info = document.createElement("div");
  info.className = "file-info";
  const name = document.createElement("div");
  name.className = "file-name";
  name.textContent = fileName;
  name.title = fileName;
  const meta = document.createElement("div");
  meta.className = "file-meta";
  if (isDir) {
    meta.textContent = "文件夹 · " + formatTime(file.server_mtime || file.server_ctime);
  } else {
    meta.textContent =
      formatSize(file.size) + " · " + formatTime(file.server_mtime || file.server_ctime);
  }
  info.appendChild(name);
  info.appendChild(meta);

  item.appendChild(icon);
  item.appendChild(info);

  if (isDir) {
    item.setAttribute("role", "button");
    item.tabIndex = 0;
    const openDir = function () {
      if (filePath) enterDir(filePath);
    };
    item.addEventListener("click", openDir);
    item.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        openDir();
      }
    });
  } else {
    const actionsEl = document.createElement("div");
    actionsEl.className = "file-actions";

    const copyBtn = document.createElement("button");
    copyBtn.className = "file-action";
    copyBtn.setAttribute("type", "button");
    copyBtn.appendChild(makeIcon("link"));
    const copyLabel = document.createElement("span");
    copyLabel.textContent = "复制链接";
    copyBtn.appendChild(copyLabel);
    copyBtn.setAttribute("aria-label", "复制链接 " + fileName);
    copyBtn.addEventListener("click", function (e) {
      e.stopPropagation();
      copyLink(filePath);
    });
    actionsEl.appendChild(copyBtn);

    const dlBtn = document.createElement("button");
    dlBtn.className = "file-action";
    dlBtn.setAttribute("type", "button");
    dlBtn.appendChild(makeIcon("download"));
    const dlLabel = document.createElement("span");
    dlLabel.textContent = "下载";
    dlBtn.appendChild(dlLabel);
    dlBtn.setAttribute("aria-label", "下载 " + fileName);
    const onDownload = function (e) {
      e.preventDefault();
      e.stopPropagation();
      downloadFile(filePath, fileName);
    };
    dlBtn.addEventListener("click", onDownload);
    // 防止连点按钮时：两次 click + 冒泡 dblclick 触发多次下载
    dlBtn.addEventListener("dblclick", function (e) {
      e.preventDefault();
      e.stopPropagation();
    });
    actionsEl.appendChild(dlBtn);

    item.appendChild(actionsEl);
    // 双击文件行下载；点到按钮时已 stopPropagation，不会重复
    item.addEventListener("dblclick", function (e) {
      if (e.target && e.target.closest && e.target.closest(".file-action")) return;
      downloadFile(filePath, fileName);
    });
  }

  return item;
}

// ===== 文件下载 =====

// 进行中的下载请求：同一 path 只允许一个 in-flight，避免连点打出多条直链
const downloadInflight = Object.create(null);

function triggerDownloadNavigation(downloadUrl) {
  // 注意：不要用 window.open(..., "noopener") 后再 if (!win) location.assign。
  // 现代浏览器在 noopener 时会返回 null，即使新标签已打开，从而误触发第二次导航。
  const a = document.createElement("a");
  a.href = downloadUrl;
  a.target = "_blank";
  a.rel = "noopener noreferrer";
  a.style.display = "none";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

function downloadFile(filePath, fileName) {
  if (!filePath) {
    showToast("文件路径无效，请刷新后重试", "warning");
    return;
  }

  if (downloadInflight[filePath]) {
    showToast("正在准备下载，请稍候", "loading");
    return;
  }

  downloadInflight[filePath] = true;
  showToast("正在准备下载…", "loading");

  requestShortLink(filePath)
    .then(function (downloadUrl) {
      triggerDownloadNavigation(downloadUrl);
      showToast("已开始下载 " + fileName, "success");
    })
    .catch(function (err) {
      showToast("下载失败，" + err.message, "error");
      console.warn("[网盘] 直链解析失败: " + err.message);
    })
    .finally(function () {
      setTimeout(function () {
        delete downloadInflight[filePath];
      }, 1500);
    });
}

function copyLink(filePath) {
  requestShortLink(filePath)
    .then(function (url) {
      return copyText(url);
    })
    .then(function () {
      showToast("下载链接已复制，对方需验证后才能访问", "success");
    })
    .catch(function (err) {
      showToast("复制失败，" + (err.message || "请重试"), "error");
      console.warn("复制链接失败", err);
    });
}

function requestShortLink(filePath) {
  return apiFetch("/api/download", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path: filePath })
  })
    .then(function (resp) {
      return resp.text().then(function (text) {
        if (resp.status === 401) {
          requireLogin(parseAPIError(resp, text));
        }
        if (!resp.ok) {
          throw new Error(parseAPIError(resp, text));
        }
        return JSON.parse(text);
      });
    })
    .then(function (data) {
      const urls = data.urls || [];
      if (!urls.length || !urls[0].url) {
        throw new Error("未能获取下载地址，请重试");
      }
      return new URL(urls[0].url, window.location.origin).href;
    });
}

/** 带 CSRF 的 same-origin 请求；403 时尝试刷新 cookie 并重试一次 */
function apiFetch(url, options) {
  options = options || {};
  const headers = Object.assign({}, options.headers || {});
  headers["X-CSRF-Token"] = getCookie("csrf_token") || "";

  const init = Object.assign({}, options, {
    credentials: "same-origin",
    headers: headers
  });

  return fetch(url, init).then(function (resp) {
    if (resp.status !== 403 || init._csrfRetried) {
      return resp;
    }
    return refreshCsrfCookie().then(function (ok) {
      if (!ok) return resp;
      const retryHeaders = Object.assign({}, options.headers || {});
      retryHeaders["X-CSRF-Token"] = getCookie("csrf_token") || "";
      return fetch(
        url,
        Object.assign({}, options, {
          credentials: "same-origin",
          headers: retryHeaders,
          _csrfRetried: true
        })
      );
    });
  });
}

function refreshCsrfCookie() {
  return fetch("/", { method: "GET", credentials: "same-origin", cache: "no-store" })
    .then(function (resp) {
      return resp.ok && !!getCookie("csrf_token");
    })
    .catch(function () {
      return false;
    });
}

function getCookie(name) {
  const prefix = name + "=";
  const cookies = document.cookie.split(";");
  for (let i = 0; i < cookies.length; i++) {
    const cookie = cookies[i].trim();
    if (cookie.indexOf(prefix) === 0) {
      return decodeURIComponent(cookie.slice(prefix.length));
    }
  }
  return "";
}

function copyText(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    return navigator.clipboard.writeText(text);
  }

  return new Promise(function (resolve, reject) {
    const input = document.createElement("input");
    input.value = text;
    document.body.appendChild(input);
    input.select();
    try {
      if (!document.execCommand("copy")) {
        throw new Error("请手动复制");
      }
      resolve();
    } catch (err) {
      reject(err);
    } finally {
      document.body.removeChild(input);
    }
  });
}

// ===== 初始化 =====
document.addEventListener("DOMContentLoaded", function () {
  // 填充静态标记中的 Lucide 图标占位符（header/按钮/登录卡/页脚/加载器）
  mountIcons(document);

  // 确保登录层与表单事件就绪（兼容旧 HTML 缓存）
  ensureLoginUI();
  bindLoginForm(document.getElementById("login-form"));
  // 动态兜底创建的登录层可能带新占位符，再挂一次（幂等）
  mountIcons(document);

  const logoutBtn = document.getElementById("btn-logout");
  if (logoutBtn) {
    logoutBtn.addEventListener("click", function () {
      doLogout();
    });
  }

  const loginOpenBtn = document.getElementById("btn-login-open");
  if (loginOpenBtn) {
    loginOpenBtn.addEventListener("click", function () {
      requireLogin("");
    });
  }

  const refreshBtn = document.getElementById("btn-refresh");
  if (refreshBtn) {
    refreshBtn.addEventListener("click", function () {
      if (authRequired && !authenticated) {
        requireLogin("");
        return;
      }
      loadFiles(currentDir);
    });
  }

  checkAuth()
    .then(function (ok) {
      if (ok) return loadFiles(currentDir);
      // 未登录：只弹窗，不偷偷拉列表
    })
    .catch(function (err) {
      console.warn("[鉴权] 初始化失败", err);
      // 鉴权状态接口异常时，宁可弹出登录，也不要无令牌硬拉列表导致 401 误导成 Cookie 错误
      requireLogin((err && err.message) || "无法确认登录状态，请刷新页面重试");
    });
});
