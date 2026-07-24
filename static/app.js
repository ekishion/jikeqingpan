// 临时盘前端逻辑 (极简明亮风格 + 文件夹支持 + 访问令牌)
// 全部使用 DOM API 操作，防止 XSS（禁止使用 innerHTML）

// ===== 全局状态 =====
const DIR_STORAGE_KEY = "jikeqingpan_current_dir";
let currentDir = sessionStorage.getItem(DIR_STORAGE_KEY) || "/";
let authRequired = false;
let authenticated = false;
let toastContainer = null;

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

function fileIcon(name, isDir) {
  if (isDir) return "📁";
  const ext = (name || "").split(".").pop().toLowerCase();
  const map = {
    jpg: "🖼️", jpeg: "🖼️", png: "🖼️", gif: "🖼️", webp: "🖼️", svg: "🖼️",
    mp4: "🎬", mov: "🎬", avi: "🎬", mkv: "🎬", flv: "🎬",
    mp3: "🎵", flac: "🎵", wav: "🎵", aac: "🎵",
    pdf: "📄", doc: "📝", docx: "📝", xls: "📊", xlsx: "📊", ppt: "📋", pptx: "📋",
    zip: "📦", rar: "📦", "7z": "📦", tar: "📦", gz: "📦",
    exe: "⚙️", msi: "⚙️", dmg: "⚙️", apk: "📱",
    txt: "📃", md: "📃", json: "🔧", xml: "🔧", yaml: "🔧"
  };
  return map[ext] || "📄";
}

function showToast(msg) {
  if (!toastContainer) {
    toastContainer = document.createElement("div");
    toastContainer.id = "toast-container";
    toastContainer.setAttribute("aria-live", "polite");
    toastContainer.setAttribute("aria-atomic", "false");
    document.body.appendChild(toastContainer);
  }

  const toast = document.createElement("div");
  toast.className = "toast";
  toast.setAttribute("role", "status");
  toast.textContent = msg;
  toastContainer.appendChild(toast);

  requestAnimationFrame(function () {
    toast.classList.add("show");
  });

  const dismiss = function () {
    if (toast.dataset.dismissed === "true") return;
    toast.dataset.dismissed = "true";
    toast.classList.remove("show");
    setTimeout(function () {
      toast.remove();
    }, 220);
  };

  toast.addEventListener("click", dismiss);
  setTimeout(dismiss, 3000);
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
  if (resp.status === 401) return "未登录或访问令牌无效，请先输入 access_token 登录";
  if (resp.status === 403) return "CSRF 校验失败，请刷新页面重试";
  if (resp.status === 429) return "请求过于频繁，请稍后重试";
  if (resp.status >= 500) return "服务暂时不可用（HTTP " + resp.status + "）";
  return "请求失败（HTTP " + resp.status + "）";
}

// ===== 鉴权 =====

function injectLoginStyles() {
  if (document.getElementById("login-overlay-style")) return;
  const style = document.createElement("style");
  style.id = "login-overlay-style";
  style.textContent = [
    "#login-overlay{position:fixed;inset:0;z-index:2000;display:flex;align-items:center;justify-content:center;padding:20px;background:rgba(15,23,42,.45);backdrop-filter:blur(4px)}",
    "#login-overlay[hidden]{display:none!important}",
    ".login-card{width:100%;max-width:400px;background:#fff;border-radius:16px;box-shadow:0 20px 50px rgba(15,23,42,.18);padding:28px 24px 22px}",
    ".login-card h2{margin:0 0 8px;font-size:1.25rem;color:#0f172a}",
    ".login-card p{margin:0 0 18px;color:#64748b;font-size:.92rem;line-height:1.5}",
    ".login-card label{display:block;font-size:.85rem;color:#334155;margin-bottom:6px}",
    ".login-card input[type=password],.login-card input[type=text]{width:100%;box-sizing:border-box;border:1px solid #cbd5e1;border-radius:10px;padding:10px 12px;font-size:1rem;outline:none}",
    ".login-card input:focus{border-color:#3b82f6;box-shadow:0 0 0 3px rgba(59,130,246,.15)}",
    ".login-actions{margin-top:16px;display:flex;gap:10px}",
    ".login-actions button{flex:1;border:none;border-radius:10px;padding:10px 14px;font-size:.95rem;cursor:pointer;background:linear-gradient(135deg,#3b82f6,#2563eb);color:#fff;font-weight:600}",
    ".login-actions button:disabled{opacity:.6;cursor:not-allowed}",
    ".login-error{margin-top:12px;color:#dc2626;font-size:.88rem}",
    ".header-actions{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-shrink:0;margin-top:4px}",
    ".btn-logout{border:1px solid #e2e8f0;background:#fff;color:#475569;border-radius:8px;padding:6px 10px;font-size:.85rem;cursor:pointer;white-space:nowrap}"
  ].join("");
  document.head.appendChild(style);
}

function ensureLoginOpenButton() {
  let loginBtn = document.getElementById("btn-login-open");
  if (loginBtn) return loginBtn;

  const header = document.querySelector(".header-actions") || document.querySelector("header") || document.body;
  loginBtn = document.createElement("button");
  loginBtn.type = "button";
  loginBtn.className = "btn-logout";
  loginBtn.id = "btn-login-open";
  loginBtn.textContent = "登录";
  loginBtn.hidden = true;
  header.appendChild(loginBtn);
  loginBtn.addEventListener("click", function () {
    requireLogin("请输入访问令牌");
  });
  return loginBtn;
}

function ensureLoginUI() {
  injectLoginStyles();
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

  const h2 = document.createElement("h2");
  h2.id = "login-title";
  h2.textContent = "访问验证";

  const p = document.createElement("p");
  p.textContent = "此实例已启用访问令牌。请输入管理员提供的令牌后继续浏览与下载。";

  const form = document.createElement("form");
  form.id = "login-form";

  const label = document.createElement("label");
  label.setAttribute("for", "login-token");
  label.textContent = "访问令牌";

  const input = document.createElement("input");
  input.id = "login-token";
  input.name = "access_token";
  input.type = "password";
  input.autocomplete = "current-password";
  input.required = true;
  input.placeholder = "粘贴 access_token";

  const actions = document.createElement("div");
  actions.className = "login-actions";
  const btn = document.createElement("button");
  btn.type = "submit";
  btn.id = "btn-login";
  btn.textContent = "进入网盘";
  actions.appendChild(btn);

  const err = document.createElement("div");
  err.id = "login-error";
  err.className = "login-error";
  err.hidden = true;

  form.appendChild(label);
  form.appendChild(input);
  form.appendChild(actions);
  form.appendChild(err);
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

function requireLogin(message) {
  authRequired = true;
  authenticated = false;
  updateAuthUI();
  showLogin(true);

  const statusEl = document.getElementById("status");
  if (statusEl) {
    statusEl.style.display = "block";
    statusEl.replaceChildren();
    const tip = document.createElement("div");
    tip.className = "status-tip status-error";
    tip.textContent = message || "请先登录";
    statusEl.appendChild(tip);
    const openBtn = document.createElement("button");
    openBtn.type = "button";
    openBtn.className = "btn-refresh";
    openBtn.style.marginTop = "12px";
    openBtn.textContent = "输入访问令牌";
    openBtn.addEventListener("click", function () {
      showLogin(true);
      const input = document.getElementById("login-token");
      if (input) input.focus();
    });
    statusEl.appendChild(openBtn);
  }

  const bannerEl = document.getElementById("list-banner");
  if (bannerEl) {
    bannerEl.hidden = false;
    bannerEl.replaceChildren();
    const box = document.createElement("div");
    box.className = "banner-warning";
    box.textContent = message || "需要登录后才能查看文件列表";
    bannerEl.appendChild(box);
  }

  if (message) {
    const errEl = document.getElementById("login-error");
    if (errEl) {
      errEl.hidden = false;
      errEl.textContent = message;
    }
    showToast("🔑 " + message);
  }
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
        throw new Error("无法获取鉴权状态（HTTP " + result.resp.status + "）");
      }
      authRequired = !!result.data.auth_required;
      authenticated = !!result.data.authenticated;
      updateAuthUI();
      if (authRequired && !authenticated) {
        requireLogin("请输入访问令牌后继续");
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
      showToast("✅ 登录成功");
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
      if (authRequired) {
        requireLogin("已退出登录");
        const listEl = document.getElementById("file-list");
        if (listEl) listEl.replaceChildren();
        const statusEl = document.getElementById("status");
        if (statusEl) {
          statusEl.style.display = "block";
          statusEl.replaceChildren();
          const tip = document.createElement("div");
          tip.className = "status-tip";
          tip.textContent = "请先登录";
          statusEl.appendChild(tip);
        }
      } else {
        showLogin(false);
        showToast("已退出");
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
      return;
    }
    const btn = document.getElementById("btn-login");
    if (btn) btn.disabled = true;
    doLogin(token)
      .catch(function (err) {
        if (errEl) {
          errEl.hidden = false;
          errEl.textContent = err.message || "登录失败";
        }
        showToast("❌ " + (err.message || "登录失败"));
        showLogin(true);
      })
      .finally(function () {
        if (btn) btn.disabled = false;
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
    sep.textContent = " / ";
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
        throw new Error("百度返回错误 errno=" + data.errno + (data.show_msg ? " " + data.show_msg : ""));
      }

      const files = Array.isArray(data.list) ? data.list : [];
      sortFiles(files);

      if (countEl) {
        let label = files.length + " 项";
        if (data.truncated) {
          label += "（可能未完整）";
        }
        countEl.textContent = label;
      }

      if (data.truncated && bannerEl) {
        bannerEl.hidden = false;
        const msg = document.createElement("div");
        msg.className = "banner-warning";
        const limit = data.list_page_limit || 1500;
        msg.textContent =
          "⚠️ 当前目录条目较多，仅加载了前约 " +
          limit +
          " 项，可能仍有未显示文件。可进入子目录缩小范围。";
        bannerEl.appendChild(msg);
      }

      if (targetDir !== "/") {
        listEl.appendChild(buildParentItem());
      }

      if (!files.length) {
        statusEl.style.display = "block";
        statusEl.replaceChildren();
        const empty = document.createElement("div");
        empty.className = "status-tip";
        empty.textContent = "此目录为空";
        statusEl.appendChild(empty);
        return;
      }

      files.forEach(function (file) {
        listEl.appendChild(buildFileItem(file));
      });
    })
    .catch(function (err) {
      if (refreshBtn) refreshBtn.disabled = false;
      statusEl.style.display = "block";
      statusEl.replaceChildren();
      const errEl = document.createElement("div");
      errEl.className = "status-error";
      errEl.textContent = "❌ " + (err && err.message ? err.message : "加载失败");
      statusEl.appendChild(errEl);
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
  item.className = "file-item";
  item.setAttribute("role", "button");
  item.tabIndex = 0;

  const icon = document.createElement("div");
  icon.className = "file-icon";
  icon.textContent = "⬆️";

  const info = document.createElement("div");
  info.className = "file-info";
  const name = document.createElement("div");
  name.className = "file-name";
  name.textContent = "..（返回上级）";
  const meta = document.createElement("div");
  meta.className = "file-meta";
  meta.textContent = "目录";
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
  item.className = "file-item";

  const icon = document.createElement("div");
  icon.className = "file-icon";
  icon.textContent = fileIcon(fileName, isDir);

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
    copyBtn.textContent = "复制链接 🔗";
    copyBtn.setAttribute("aria-label", "复制链接 " + fileName);
    copyBtn.addEventListener("click", function (e) {
      e.stopPropagation();
      copyLink(filePath);
    });
    actionsEl.appendChild(copyBtn);

    const dlBtn = document.createElement("button");
    dlBtn.className = "file-action";
    dlBtn.setAttribute("type", "button");
    dlBtn.textContent = "下载 ⬇";
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
    showToast("⚠️ 路径解析失败");
    return;
  }

  if (downloadInflight[filePath]) {
    showToast("⏳ 已在准备下载，请稍候…");
    return;
  }

  downloadInflight[filePath] = true;
  showToast("⏳ 正在生成安全直链…");

  requestShortLink(filePath)
    .then(function (downloadUrl) {
      triggerDownloadNavigation(downloadUrl);
      showToast("✅ 唤起下载：" + fileName);
    })
    .catch(function (err) {
      showToast("❌ 下载失败：" + err.message);
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
      showToast("✅ 短链接已复制（需已登录才能访问）");
    })
    .catch(function (err) {
      showToast("❌ 复制失败：" + (err.message || "未知错误"));
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
        throw new Error("短链接为空");
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
        throw new Error("浏览器拒绝复制");
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
  // 确保登录层与表单事件就绪（兼容旧 HTML 缓存）
  ensureLoginUI();
  bindLoginForm(document.getElementById("login-form"));

  const logoutBtn = document.getElementById("btn-logout");
  if (logoutBtn) {
    logoutBtn.addEventListener("click", function () {
      doLogout();
    });
  }

  const loginOpenBtn = document.getElementById("btn-login-open");
  if (loginOpenBtn) {
    loginOpenBtn.addEventListener("click", function () {
      requireLogin("请输入访问令牌");
    });
  }

  const refreshBtn = document.getElementById("btn-refresh");
  if (refreshBtn) {
    refreshBtn.addEventListener("click", function () {
      if (authRequired && !authenticated) {
        requireLogin("请先登录后再刷新列表");
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
      requireLogin((err && err.message) || "鉴权初始化失败，请尝试输入访问令牌");
    });
});
