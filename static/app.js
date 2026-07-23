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
  if (resp.status === 401) return "需要访问令牌，请先登录";
  if (resp.status === 403) return "CSRF 校验失败，请刷新页面重试";
  if (resp.status === 429) return "请求过于频繁，请稍后重试";
  if (resp.status >= 500) return "服务暂时不可用（HTTP " + resp.status + "）";
  return "请求失败（HTTP " + resp.status + "）";
}

// ===== 鉴权 =====

function showLogin(show) {
  const overlay = document.getElementById("login-overlay");
  if (!overlay) return;
  overlay.hidden = !show;
  overlay.setAttribute("aria-hidden", show ? "false" : "true");
  const app = document.getElementById("app-main");
  if (app) app.setAttribute("aria-hidden", show ? "true" : "false");
  if (show) {
    const input = document.getElementById("login-token");
    if (input) {
      input.value = "";
      setTimeout(function () {
        input.focus();
      }, 50);
    }
  }
}

function updateAuthUI() {
  const logoutBtn = document.getElementById("btn-logout");
  if (logoutBtn) {
    logoutBtn.hidden = !(authRequired && authenticated);
  }
}

function checkAuth() {
  return apiFetch("/api/auth/status", { method: "GET" })
    .then(function (resp) {
      return resp.json().then(function (data) {
        return { resp: resp, data: data };
      });
    })
    .then(function (result) {
      if (!result.resp.ok) {
        throw new Error("无法获取鉴权状态");
      }
      authRequired = !!result.data.auth_required;
      authenticated = !!result.data.authenticated;
      updateAuthUI();
      if (authRequired && !authenticated) {
        showLogin(true);
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
        showLogin(true);
        const listEl = document.getElementById("file-list");
        if (listEl) listEl.replaceChildren();
        showToast("已退出登录");
      }
    });
}

// ===== 路径导航与面包屑 =====

function renderBreadcrumbs() {
  const container = document.getElementById("breadcrumbs");
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
          authenticated = false;
          updateAuthUI();
          showLogin(true);
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
    dlBtn.addEventListener("click", function (e) {
      e.stopPropagation();
      downloadFile(filePath, fileName);
    });
    actionsEl.appendChild(dlBtn);

    item.appendChild(actionsEl);
    item.addEventListener("dblclick", function () {
      downloadFile(filePath, fileName);
    });
  }

  return item;
}

// ===== 文件下载 =====

function triggerDownloadNavigation(downloadUrl) {
  // 优先新标签，避免丢失当前目录浏览状态；被拦截时回退当前页跳转。
  const win = window.open(downloadUrl, "_blank", "noopener,noreferrer");
  if (!win) {
    window.location.assign(downloadUrl);
  }
}

function downloadFile(filePath, fileName) {
  if (!filePath) {
    showToast("⚠️ 路径解析失败");
    return;
  }

  showToast("⏳ 正在生成安全直链…");

  requestShortLink(filePath)
    .then(function (downloadUrl) {
      triggerDownloadNavigation(downloadUrl);
      showToast("✅ 唤起下载：" + fileName);
    })
    .catch(function (err) {
      showToast("❌ 下载失败：" + err.message);
      console.warn("[网盘] 直链解析失败: " + err.message);
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
          authenticated = false;
          updateAuthUI();
          showLogin(true);
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
  const loginForm = document.getElementById("login-form");
  if (loginForm) {
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
        })
        .finally(function () {
          if (btn) btn.disabled = false;
        });
    });
  }

  const logoutBtn = document.getElementById("btn-logout");
  if (logoutBtn) {
    logoutBtn.addEventListener("click", function () {
      doLogout();
    });
  }

  document.getElementById("btn-refresh").addEventListener("click", function () {
    loadFiles(currentDir);
  });

  checkAuth()
    .then(function (ok) {
      if (ok) return loadFiles(currentDir);
    })
    .catch(function (err) {
      showToast("❌ " + (err.message || "初始化失败"));
      // 若鉴权状态接口失败，仍尝试加载（兼容旧部署）
      loadFiles(currentDir);
    });
});
