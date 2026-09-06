// 即刻轻盘前端逻辑（纯白极简 + 文件夹浏览 + 访问验证）
// 全部使用 DOM API 操作，防止 XSS（禁止使用 innerHTML）
// 图标统一用内联 Lucide SVG，界面不使用 emoji

// ===== 全局状态 =====
const DIR_STORAGE_KEY = "jikeqingpan_current_dir";
const SORT_STORAGE_KEY = "jikeqingpan_sort";
const THEME_STORAGE_KEY = "jikeqingpan_theme";
let currentDir = sessionStorage.getItem(DIR_STORAGE_KEY) || "/";
let authRequired = false;
let authenticated = false;
let toastContainer = null;
let uiConfig = { showReadme: true, showReadmeOverview: true };
// 当前目录的文件数据与截断信息：排序切换时本地重渲染，不重新请求
let currentFiles = [];
let lastTruncated = false;
let lastPageLimit = 1500;
let sortState = { key: "name", dir: "asc" };
// 当前目录 README 的取回缓存，避免排序切换重渲染时重复请求
let readmeCache = null;

// ===== 主题（手动深/浅切换，未选择时跟随系统） =====

function applyTheme(theme) {
  document.documentElement.setAttribute("data-theme", theme === "dark" ? "dark" : "light");
  const btn = document.getElementById("btn-theme");
  if (btn) {
    const iconWrap = btn.querySelector(".btn-icon");
    if (iconWrap) {
      iconWrap.replaceChildren(makeIcon(theme === "dark" ? "sun" : "moon"));
      // 标记已挂载，防止 mountIcons 再追加一份导致双图标
      iconWrap.dataset.iconMounted = "1";
    }
    btn.setAttribute("aria-label", theme === "dark" ? "切换到浅色模式" : "切换到深色模式");
  }
}

function currentTheme() {
  return document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light";
}

function initTheme() {
  let stored = null;
  try { stored = localStorage.getItem(THEME_STORAGE_KEY); } catch (e) { /* ignore */ }
  if (stored !== "light" && stored !== "dark") {
    stored = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  applyTheme(stored);
}

function toggleTheme() {
  const next = currentTheme() === "dark" ? "light" : "dark";
  applyTheme(next);
  try { localStorage.setItem(THEME_STORAGE_KEY, next); } catch (e) { /* ignore */ }
}

// ===== 排序状态（字段 + 方向，持久化到 localStorage） =====

function loadSortState() {
  try {
    const parsed = JSON.parse(localStorage.getItem(SORT_STORAGE_KEY) || "null");
    if (parsed && ["name", "size", "time"].indexOf(parsed.key) !== -1 &&
      ["asc", "desc"].indexOf(parsed.dir) !== -1) {
      sortState = { key: parsed.key, dir: parsed.dir };
    }
  } catch (e) { /* ignore */ }
  syncSortControls();
}

function saveSortState() {
  try { localStorage.setItem(SORT_STORAGE_KEY, JSON.stringify(sortState)); } catch (e) { /* ignore */ }
}

const SORT_FIELD_LABELS = { name: "名称", size: "大小", time: "时间" };

function syncSortControls() {
  const trigger = document.getElementById("btn-sort-field");
  const dirBtn = document.getElementById("btn-sort-dir");
  const menu = document.getElementById("sort-menu");
  const label = document.getElementById("sort-field-label");
  if (label) label.textContent = SORT_FIELD_LABELS[sortState.key] || "名称";
  if (menu) {
    const options = menu.querySelectorAll(".sort-option");
    for (let i = 0; i < options.length; i++) {
      const opt = options[i];
      const active = opt.dataset.key === sortState.key;
      opt.classList.toggle("is-active", active);
      opt.setAttribute("aria-selected", active ? "true" : "false");
    }
  }
  if (dirBtn) {
    const iconWrap = dirBtn.querySelector(".btn-icon");
    if (iconWrap) {
      iconWrap.replaceChildren(makeIcon(sortState.dir === "asc" ? "arrow-up" : "arrow-down"));
      // 标记已挂载，防止 mountIcons 再追加一份导致双图标
      iconWrap.dataset.iconMounted = "1";
    }
    dirBtn.setAttribute("aria-label", sortState.dir === "asc" ? "当前升序，点击切换为降序" : "当前降序，点击切换为升序");
  }
}

function setSortMenuOpen(open) {
  const menu = document.getElementById("sort-menu");
  const trigger = document.getElementById("btn-sort-field");
  if (!menu || !trigger) return;
  menu.hidden = !open;
  trigger.setAttribute("aria-expanded", open ? "true" : "false");
}

// bindSortControls 绑定排序下拉（自绘菜单，替代原生 select 的系统样式）
// 与升降序切换按钮；点击外部或 Escape 关闭菜单。
function bindSortControls() {
  const trigger = document.getElementById("btn-sort-field");
  const menu = document.getElementById("sort-menu");
  const dirBtn = document.getElementById("btn-sort-dir");

  if (trigger && menu && !trigger.dataset.bound) {
    trigger.dataset.bound = "1";
    trigger.addEventListener("click", function (e) {
      e.stopPropagation();
      setSortMenuOpen(menu.hidden);
    });
    menu.addEventListener("click", function (e) {
      e.stopPropagation();
    });
    const options = menu.querySelectorAll(".sort-option");
    for (let i = 0; i < options.length; i++) {
      options[i].addEventListener("click", function () {
        if (sortState.key !== options[i].dataset.key) {
          sortState.key = options[i].dataset.key;
          saveSortState();
          syncSortControls();
          renderCurrentList(currentDir);
        }
        setSortMenuOpen(false);
      });
    }
    document.addEventListener("click", function () {
      setSortMenuOpen(false);
    });
  }

  if (dirBtn && !dirBtn.dataset.bound) {
    dirBtn.dataset.bound = "1";
    dirBtn.addEventListener("click", function () {
      sortState.dir = sortState.dir === "asc" ? "desc" : "asc";
      saveSortState();
      syncSortControls();
      renderCurrentList(currentDir);
    });
  }
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

function isPreviewableImage(name) {
  const ext = (name || "").split(".").pop().toLowerCase();
  return ["jpg", "jpeg", "png", "gif", "webp", "svg", "bmp", "ico", "heic"].indexOf(ext) !== -1;
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

function setCurrentDir(dir, opts) {
  currentDir = dir || "/";
  try {
    sessionStorage.setItem(DIR_STORAGE_KEY, currentDir);
  } catch (e) {
    /* ignore */
  }
  syncUrlDir(currentDir, opts);
}

// syncUrlDir 把当前目录同步到地址栏（?dir=...），支持浏览器前进/后退。
// mode: "push"（默认，用户主动进入目录）| "replace"（初始加载）| "none"（popstate 还原）。
function syncUrlDir(dir, opts) {
  const mode = opts && opts.mode ? opts.mode : "push";
  if (mode === "none" || !window.history || !window.history.pushState) return;
  const url = dir === "/" ? location.pathname : location.pathname + "?dir=" + encodeURIComponent(dir);
  try {
    if (mode === "replace") history.replaceState({ dir: dir }, "", url);
    else history.pushState({ dir: dir }, "", url);
  } catch (e) {
    /* file:// 等环境不支持，忽略 */
  }
}

function dirFromLocation() {
  try {
    const dir = new URLSearchParams(window.location.search).get("dir");
    // 只接受形如 /xxx 的路径，且拒绝包含 .. 的可疑值
    if (dir && dir.charAt(0) === "/" && dir.indexOf("..") === -1) return dir;
  } catch (e) {
    /* ignore */
  }
  return null;
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

let lastFocusedBeforeModal = null;

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
    // 记住触发焦点，关闭后还原（模态无障碍）
    if (document.activeElement && document.activeElement !== document.body) {
      lastFocusedBeforeModal = document.activeElement;
    }
    const input = document.getElementById("login-token");
    if (input) {
      // 不强制清空，方便输错后重试
      setTimeout(function () {
        input.focus();
        input.select();
      }, 50);
    }
  } else if (lastFocusedBeforeModal && lastFocusedBeforeModal.focus) {
    const el = lastFocusedBeforeModal;
    lastFocusedBeforeModal = null;
    try {
      el.focus();
    } catch (e) {
      /* ignore */
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
      uiConfig.showReadme = result.data.show_readme !== false;
      uiConfig.showReadmeOverview = result.data.show_readme_overview !== false;
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

  if (currentDir === "/") {
    const rootSpan = document.createElement("span");
    rootSpan.className = "breadcrumb-current";
    rootSpan.textContent = "根目录";
    container.appendChild(rootSpan);
    return;
  }

  const rootBtn = document.createElement("button");
  rootBtn.type = "button";
  rootBtn.className = "breadcrumb-item";
  rootBtn.textContent = "根目录";
  rootBtn.addEventListener("click", function () {
    enterDir("/");
  });
  container.appendChild(rootBtn);

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
    if (index === parts.length - 1) {
      const cur = document.createElement("span");
      cur.className = "breadcrumb-current";
      cur.textContent = part;
      container.appendChild(cur);
      return;
    }
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "breadcrumb-item";
    btn.textContent = part;
    const targetPath = accPath;
    btn.addEventListener("click", function () {
      enterDir(targetPath);
    });
    container.appendChild(btn);
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
  const readmePanel = document.getElementById("readme-panel");

  renderBreadcrumbs();
  listEl.replaceChildren();
  if (bannerEl) {
    bannerEl.hidden = true;
    bannerEl.replaceChildren();
  }
  if (readmePanel) {
    readmePanel.hidden = true;
    readmePanel.replaceChildren();
  }
  readmeCache = null;
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
          const message = parseAPIError(resp, text);
          requireLogin(message);
          throw new Error(message);
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

      currentFiles = Array.isArray(data.list) ? data.list : [];
      lastTruncated = !!data.truncated;
      lastPageLimit = data.list_page_limit || 1500;
      renderCurrentList(targetDir);
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

// renderCurrentList 依据 sortState 渲染当前目录文件列表。
// 排序切换只重渲染本地数据（currentFiles），不重新请求。
function renderCurrentList(dir) {
  const listEl = document.getElementById("file-list");
  const countEl = document.getElementById("file-count");
  const bannerEl = document.getElementById("list-banner");
  const files = currentFiles.slice();
  sortFiles(files);
  resetFileSearch();

  if (countEl) {
    countEl.textContent = lastTruncated
      ? "已显示前 " + files.length + " 项"
      : "共 " + files.length + " 项";
  }
  if (lastTruncated) {
    renderBanner(
      "warning",
      "文件较多，当前仅显示前 " + lastPageLimit + " 项。进入子文件夹可查看完整内容。"
    );
  } else if (bannerEl) {
    bannerEl.hidden = true;
    bannerEl.replaceChildren();
  }

  listEl.replaceChildren();
  // 用 DocumentFragment 批量插入，避免逐行 appendChild 触发多次回流
  const frag = document.createDocumentFragment();
  if (dir !== "/") {
    frag.appendChild(buildParentItem());
  }

  if (!files.length) {
    renderStatusState({
      icon: "folder-open",
      title: "这里还没有文件",
      desc: dir === "/" ? "上传文件到网盘后，就会显示在这里" : "该文件夹是空的"
    });
    if (frag.childNodes.length) listEl.appendChild(frag);
    return;
  }

  for (let i = 0; i < files.length; i++) {
    frag.appendChild(buildFileItem(files[i]));
  }
  listEl.appendChild(frag);
  const readmeFile = findReadmeFile(files);
  if (uiConfig.showReadme && readmeFile) loadReadme(readmeFile.path || "", files, dir);
}

function findReadmeFile(files) {
  const priority = { "readme.md": 1, "readme.markdown": 2, "readme.txt": 3, "readme": 4 };
  let match = null;
  let rank = 100;
  for (let i = 0; i < files.length; i++) {
    const file = files[i];
    if (Number(file.isdir) === 1) continue;
    const name = (file.server_filename || file.filename || "").toLowerCase();
    if (priority[name] && priority[name] < rank) {
      match = file;
      rank = priority[name];
    }
  }
  return match;
}

function loadReadme(path, files, dir) {
  const panel = document.getElementById("readme-panel");
  if (!panel) return;
  // 同目录重渲染（排序切换）时直接复用，不重复请求
  if (readmeCache && readmeCache.dir === dir && readmeCache.path === path) {
    renderReadmePanel(readmeCache.data, files, dir);
    return;
  }
  apiFetch("/api/readme", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path: path })
  })
    .then(function (resp) {
      return resp.text().then(function (text) {
        if (!resp.ok) throw new Error(parseAPIError(resp, text));
        return JSON.parse(text);
      });
    })
    .then(function (data) {
      if (!data.found) return;
      readmeCache = { dir: dir, path: path, data: data };
      renderReadmePanel(data, files, dir);
    })
    .catch(function (err) {
      console.warn("[网盘] README 加载失败", err);
    });
}

function renderReadmePanel(data, files, dir) {
  const panel = document.getElementById("readme-panel");
  if (!panel) return;
  const heading = document.createElement("div");
  heading.className = "readme-heading";
  const readmeIcon = document.createElement("div");
  readmeIcon.className = "readme-icon file-icon";
  readmeIcon.appendChild(makeIcon("file-text"));
  heading.appendChild(readmeIcon);
  const titleWrap = document.createElement("div");
  const title = document.createElement("h2");
  title.id = "readme-title";
  title.textContent = data.name || "README";
  const source = document.createElement("p");
  source.id = "readme-source";
  source.textContent = "当前目录说明";
  titleWrap.appendChild(title);
  titleWrap.appendChild(source);
  heading.appendChild(titleWrap);
  const layout = document.createElement("div");
  layout.className = "readme-layout";
  const main = document.createElement("div");
  main.className = "readme-main";
  const content = document.createElement("div");
  content.id = "readme-content";
  content.className = "md-content";
  renderMarkdown(content, data.content || "");
  main.appendChild(heading);
  main.appendChild(content);
  layout.appendChild(main);
  if (uiConfig.showReadmeOverview) {
    layout.appendChild(buildReadmeOverview(files || [], dir || "/"));
  }
  panel.replaceChildren(layout);
  panel.hidden = false;
}

function buildReadmeOverview(files, dir) {
  const overview = document.createElement("aside");
  overview.className = "readme-overview";
  overview.setAttribute("aria-label", "目录概览");

  const title = document.createElement("h3");
  title.textContent = "目录概览";
  overview.appendChild(title);

  let fileCount = 0;
  let folderCount = 0;
  let totalSize = 0;
  let latest = 0;
  for (let i = 0; i < files.length; i++) {
    const file = files[i];
    if (Number(file.isdir) === 1) folderCount++;
    else {
      fileCount++;
      totalSize += Number(file.size) || 0;
    }
    latest = Math.max(latest, Number(file.server_mtime || file.server_ctime) || 0);
  }

  appendOverviewItem(overview, "文件", String(fileCount));
  appendOverviewItem(overview, "文件夹", String(folderCount));
  appendOverviewItem(overview, "大小", formatSize(totalSize));
  appendOverviewItem(overview, "位置", dir === "/" ? "根目录" : dir);
  if (latest) appendOverviewItem(overview, "最近更新", formatTime(latest));
  return overview;
}

function appendOverviewItem(root, label, value) {
  const item = document.createElement("div");
  item.className = "overview-item";
  const labelEl = document.createElement("span");
  labelEl.className = "overview-label";
  labelEl.textContent = label;
  const valueEl = document.createElement("strong");
  valueEl.className = "overview-value";
  valueEl.textContent = value;
  item.appendChild(labelEl);
  item.appendChild(valueEl);
  root.appendChild(item);
}

function resetFileSearch() {
  const input = document.getElementById("file-search");
  if (input) input.value = "";
}

function filterCurrentFiles(query) {
  const normalized = (query || "").trim().toLocaleLowerCase();
  const items = document.querySelectorAll("#file-list .file-item");
  const countEl = document.getElementById("file-count");
  const statusEl = document.getElementById("status");
  let matched = 0;
  let searchable = 0;

  for (let i = 0; i < items.length; i++) {
    const item = items[i];
    if (!item.dataset.filename) {
      item.hidden = false;
      continue;
    }
    searchable++;
    const matchedItem = !normalized || item.dataset.filename.toLocaleLowerCase().indexOf(normalized) !== -1;
    item.hidden = !matchedItem;
    if (matchedItem) matched++;
  }

  if (!normalized) {
    if (statusEl && statusEl.dataset.searchState === "1") {
      statusEl.style.display = "none";
      statusEl.dataset.searchState = "0";
    }
    return;
  }
  if (countEl) countEl.textContent = "找到 " + matched + " / " + searchable + " 项";
  if (matched === 0 && statusEl) {
    statusEl.dataset.searchState = "1";
    renderStatusState({ icon: "search", title: "没有匹配的文件", desc: "换一个关键词试试" });
  } else if (statusEl && statusEl.dataset.searchState === "1") {
    statusEl.style.display = "none";
    statusEl.dataset.searchState = "0";
  }
}

function sortFiles(files) {
  const dirMul = sortState.dir === "desc" ? -1 : 1;
  const key = sortState.key;
  files.sort(function (a, b) {
    const aDir = Number(a.isdir) === 1 ? 0 : 1;
    const bDir = Number(b.isdir) === 1 ? 0 : 1;
    if (aDir !== bDir) return aDir - bDir; // 文件夹恒在前
    let cmp = 0;
    if (key === "size") {
      cmp = (Number(a.size) || 0) - (Number(b.size) || 0);
    } else if (key === "time") {
      cmp =
        (Number(a.server_mtime || a.server_ctime) || 0) -
        (Number(b.server_mtime || b.server_ctime) || 0);
    } else {
      // zh collation 让中文按拼音序，numeric 让 "2" 排在 "10" 前
      const an = a.server_filename || a.filename || a.path || "";
      const bn = b.server_filename || b.filename || b.path || "";
      cmp = an.localeCompare(bn, "zh-Hans-CN", { numeric: true });
    }
    return cmp * dirMul;
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
  item.dataset.filename = fileName;

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

    // 图片/文本/音视频均可在线预览（preview.js 按类型分发）
    if (typeof previewKind === "function" && previewKind(fileName)) {
      const previewBtn = document.createElement("button");
      previewBtn.className = "file-action";
      previewBtn.setAttribute("type", "button");
      previewBtn.appendChild(makeIcon("eye"));
      const previewLabel = document.createElement("span");
      previewLabel.textContent = "预览";
      previewBtn.appendChild(previewLabel);
      previewBtn.setAttribute("aria-label", "预览 " + fileName);
      previewBtn.addEventListener("click", function (e) {
        e.stopPropagation();
        openPreview(filePath, fileName);
      });
      actionsEl.appendChild(previewBtn);
    }

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

// trapFocusWithin 把 Tab 焦点圈定在容器内（模态无障碍）。
function trapFocusWithin(event, container) {
  const focusables = container.querySelectorAll(
    'button:not([hidden]):not([disabled]), input:not([hidden]):not([disabled]), select, textarea, a[href], [tabindex]:not([tabindex="-1"])'
  );
  if (!focusables.length) return;
  const first = focusables[0];
  const last = focusables[focusables.length - 1];
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
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
  // 主题先于图标挂载，避免按钮图标闪烁
  initTheme();
  // 填充静态标记中的 Lucide 图标占位符（header/按钮/登录卡/页脚/加载器）
  mountIcons(document);

  const searchBox = document.getElementById("file-search");
  document.addEventListener("keydown", function (event) {
    // "/" 聚焦搜索框（正在输入时让位）；Escape/方向键由 preview.js 处理
    if (event.key !== "/" || event.ctrlKey || event.metaKey || event.altKey) return;
    const active = document.activeElement;
    if (active &&
      (active.tagName === "INPUT" || active.tagName === "TEXTAREA" ||
        active.tagName === "SELECT" || active.isContentEditable)) {
      return;
    }
    const target = searchBox || document.getElementById("file-search");
    if (target) {
      event.preventDefault();
      target.focus();
      target.select();
    }
  });

  // 确保登录层与表单事件就绪（兼容旧 HTML 缓存）
  ensureLoginUI();
  bindLoginForm(document.getElementById("login-form"));
  const loginOverlay = document.getElementById("login-overlay");
  if (loginOverlay && !loginOverlay.dataset.trapBound) {
    loginOverlay.dataset.trapBound = "1";
    loginOverlay.addEventListener("keydown", function (event) {
      if (event.key === "Tab") trapFocusWithin(event, loginOverlay);
    });
  }
  // 动态兜底创建的登录层可能带新占位符，再挂一次（幂等）
  mountIcons(document);

  const themeBtn = document.getElementById("btn-theme");
  if (themeBtn) themeBtn.addEventListener("click", toggleTheme);

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

  // 排序：字段下拉（自绘菜单）+ 升降序切换；只重渲染本地数据
  loadSortState();
  bindSortControls();

  // 搜索：输入防抖 + Escape 清空
  if (searchBox) {
    let searchDebounce = null;
    searchBox.addEventListener("input", function () {
      if (searchDebounce) clearTimeout(searchDebounce);
      searchDebounce = setTimeout(function () {
        searchDebounce = null;
        filterCurrentFiles(searchBox.value);
      }, 150);
    });
    searchBox.addEventListener("keydown", function (event) {
      if (event.key === "Escape") {
        event.stopPropagation();
        searchBox.value = "";
        filterCurrentFiles("");
        searchBox.blur();
      }
    });
  }

  // 浏览器前进/后退：还原目录并重新加载
  window.addEventListener("popstate", function (event) {
    const dir = (event.state && event.state.dir) || dirFromLocation() || "/";
    setCurrentDir(dir, { mode: "none" });
    if (!authRequired || authenticated) {
      loadFiles(dir);
    } else {
      renderBreadcrumbs();
    }
  });

  // 初始目录：优先取地址栏 ?dir=，否则用 sessionStorage 记住的目录，并回写 URL
  const initialDir = dirFromLocation() || currentDir;
  setCurrentDir(initialDir, { mode: "replace" });

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
