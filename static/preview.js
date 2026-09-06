// 即刻轻盘预览模块：图片 / 文本 / 音视频灯箱。
// 依赖 app.js 的全局：apiFetch、parseAPIError、showToast、requestShortLink、
// isPreviewableImage、currentFiles、trapFocusWithin，以及 markdown.js 的 renderMarkdown。

// 与后端 handlers.go 的 textPreviewExts 白名单保持同步
const TEXT_PREVIEW_EXTS = [
  "txt", "md", "markdown", "json", "xml", "yaml", "yml", "csv", "log", "ini", "conf",
  "sh", "bat", "ps1", "py", "js", "ts", "go", "c", "h", "cpp", "hpp", "java", "rs",
  "sql", "toml", "html", "css"
];
const VIDEO_PREVIEW_EXTS = ["mp4", "m4v", "webm", "mov"];
const AUDIO_PREVIEW_EXTS = ["mp3", "flac", "wav", "aac", "ogg", "m4a"];

function fileExtOf(name) {
  return (name || "").split(".").pop().toLowerCase();
}

// previewKind 返回可预览类型：image / text / video / audio；不可预览返回 null
function previewKind(name) {
  if (isPreviewableImage(name)) return "image";
  const ext = fileExtOf(name);
  if (TEXT_PREVIEW_EXTS.indexOf(ext) !== -1) return "text";
  if (VIDEO_PREVIEW_EXTS.indexOf(ext) !== -1) return "video";
  if (AUDIO_PREVIEW_EXTS.indexOf(ext) !== -1) return "audio";
  return null;
}

const previewState = {
  kind: null,
  path: null,
  name: "",
  objectUrl: null,
  seq: 0 // 自增序号：快速切换预览时丢弃过期响应
};

let previewNavList = [];
let previewNavIndex = -1;
let lastFocusedBeforeLightbox = null;

// ===== 入口 =====

function openPreview(filePath, fileName) {
  if (!filePath) {
    showToast("文件路径无效，请刷新后重试", "warning");
    return;
  }
  const kind = previewKind(fileName);
  if (!kind) {
    showToast("该文件类型暂不支持在线预览", "warning");
    return;
  }
  buildPreviewNavList(kind, filePath);
  loadPreviewContent(kind, filePath, fileName);
}

// buildPreviewNavList 收集当前目录同类可预览文件，供图片上一张/下一张导航
function buildPreviewNavList(kind, path) {
  previewNavList = [];
  previewNavIndex = -1;
  if (kind !== "image") return;
  for (let i = 0; i < currentFiles.length; i++) {
    const f = currentFiles[i];
    if (Number(f.isdir) === 1) continue;
    const name = f.server_filename || f.filename || (f.path || "").split("/").pop() || "";
    if (previewKind(name) !== "image") continue;
    previewNavList.push({ path: f.path || "", name: name });
  }
  for (let i = 0; i < previewNavList.length; i++) {
    if (previewNavList[i].path === path) {
      previewNavIndex = i;
      break;
    }
  }
  if (previewNavIndex === -1 && previewNavList.length) previewNavIndex = 0;
}

function navigatePreview(delta) {
  const target = previewNavIndex + delta;
  if (target < 0 || target >= previewNavList.length) return;
  previewNavIndex = target;
  const item = previewNavList[target];
  loadPreviewContent("image", item.path, item.name);
}

// ===== 内容加载 =====

function loadPreviewContent(kind, filePath, fileName) {
  previewState.seq++;
  const seq = previewState.seq;
  previewState.kind = kind;
  previewState.path = filePath;
  previewState.name = fileName;
  resetPreviewPanes();
  openLightboxShell();
  setLightboxCaption(fileName, false);
  updateLightboxNav();

  if (kind === "image") loadImagePreview(filePath, seq);
  else if (kind === "text") loadTextPreview(filePath, fileName, seq);
  else loadMediaPreview(kind, filePath, seq);
}

function loadImagePreview(filePath, seq) {
  apiFetch("/api/preview", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path: filePath })
  })
    .then(function (resp) {
      if (!resp.ok) {
        return resp.text().then(function (text) { throw new Error(parseAPIError(resp, text)); });
      }
      return resp.blob();
    })
    .then(function (blob) {
      if (seq !== previewState.seq) return;
      const url = URL.createObjectURL(blob);
      const image = document.getElementById("lightbox-image");
      if (!image) return;
      image.onload = function () {
        if (previewState.objectUrl) URL.revokeObjectURL(previewState.objectUrl);
        previewState.objectUrl = url;
      };
      image.src = url;
      image.alt = previewState.name;
      image.hidden = false;
    })
    .catch(function (err) {
      if (seq === previewState.seq) closeLightbox();
      showToast("预览失败：" + err.message, "error");
    });
}

function loadTextPreview(filePath, fileName, seq) {
  apiFetch("/api/text", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ path: filePath })
  })
    .then(function (resp) {
      return resp.text().then(function (text) {
        if (!resp.ok) throw new Error(parseAPIError(resp, text));
        return JSON.parse(text);
      });
    })
    .then(function (data) {
      if (seq !== previewState.seq) return;
      const pre = document.getElementById("lightbox-text");
      const mdBox = document.getElementById("lightbox-markdown");
      if (!pre || !mdBox) return;
      const ext = fileExtOf(fileName);
      if (ext === "md" || ext === "markdown") {
        pre.hidden = true;
        pre.replaceChildren();
        mdBox.replaceChildren();
        renderMarkdown(mdBox, data.content || "");
        mdBox.hidden = false;
      } else {
        mdBox.hidden = true;
        mdBox.replaceChildren();
        pre.textContent = data.content || "";
        pre.hidden = false;
      }
      setLightboxCaption(fileName, !!data.truncated);
    })
    .catch(function (err) {
      if (seq === previewState.seq) closeLightbox();
      showToast("预览失败：" + err.message, "error");
    });
}

function loadMediaPreview(kind, filePath, seq) {
  // 媒体走 /d/{token} 短链：302 到百度直链，浏览器自动透传 Range，
  // 不经 VPS 中转流量（CSP media-src 已放行百度直链域）。
  requestShortLink(filePath)
    .then(function (url) {
      if (seq !== previewState.seq) return;
      const isVideo = kind === "video";
      const video = document.getElementById("lightbox-video");
      const audio = document.getElementById("lightbox-audio");
      const mediaBox = document.getElementById("lightbox-media");
      const disc = document.getElementById("media-disc");
      const fsBtn = document.getElementById("media-fs");
      const el = isVideo ? video : audio;
      if (!el || !mediaBox) return;
      mediaBox.hidden = false;
      if (disc) disc.hidden = isVideo;
      if (fsBtn) fsBtn.hidden = !isVideo;
      el.hidden = false;
      el.muted = false;
      syncMuteIcon(el);
      el.src = url;
      updateMediaUI();
      updateMediaPlayState();
      if (isVideo) {
        const play = el.play();
        if (play && play.catch) play.catch(function () { /* 自动播放被拦截时保持静默 */ });
      }
    })
    .catch(function (err) {
      if (seq === previewState.seq) closeLightbox();
      showToast("无法打开媒体：" + err.message, "error");
    });
}

// ===== 自绘媒体播放器（替代原生 controls；灯箱恒为深底，用白色基元） =====

function activeMediaEl() {
  if (previewState.kind === "video") return document.getElementById("lightbox-video");
  if (previewState.kind === "audio") return document.getElementById("lightbox-audio");
  return null;
}

function formatMediaTime(sec) {
  if (!isFinite(sec) || sec < 0) return "0:00";
  const s = Math.floor(sec % 60);
  const m = Math.floor(sec / 60) % 60;
  const h = Math.floor(sec / 3600);
  const mm = h ? String(m).padStart(2, "0") : String(m);
  return (h ? h + ":" : "") + mm + ":" + String(s).padStart(2, "0");
}

function updateMediaUI() {
  const el = activeMediaEl();
  if (!el) return;
  const fill = document.getElementById("media-fill");
  const thumb = document.getElementById("media-thumb");
  const time = document.getElementById("media-time");
  const pct = isFinite(el.duration) && el.duration > 0
    ? Math.min(100, (el.currentTime / el.duration) * 100)
    : 0;
  if (fill) fill.style.width = pct + "%";
  if (thumb) thumb.style.left = pct + "%";
  if (time) {
    time.textContent = formatMediaTime(el.currentTime) + " / " + formatMediaTime(el.duration);
  }
}

function updateMediaPlayState() {
  const el = activeMediaEl();
  const playBtn = document.getElementById("media-play");
  if (!playBtn) return;
  const playing = !!(el && !el.paused && !el.ended);
  const iconWrap = playBtn.querySelector(".btn-icon");
  if (iconWrap) {
    iconWrap.replaceChildren(makeIcon(playing ? "pause" : "play"));
    iconWrap.dataset.iconMounted = "1";
  }
  playBtn.setAttribute("aria-label", playing ? "暂停" : "播放");
  const disc = document.getElementById("media-disc");
  if (disc) disc.classList.toggle("is-playing", playing);
}

function syncMuteIcon(el) {
  const muteBtn = document.getElementById("media-mute");
  if (!muteBtn) return;
  const iconWrap = muteBtn.querySelector(".btn-icon");
  if (iconWrap) {
    iconWrap.replaceChildren(makeIcon(el && el.muted ? "volume-x" : "volume-2"));
    iconWrap.dataset.iconMounted = "1";
  }
  muteBtn.setAttribute("aria-label", el && el.muted ? "取消静音" : "静音");
}

function toggleMediaPlay() {
  const el = activeMediaEl();
  if (!el) return;
  if (el.paused || el.ended) {
    const p = el.play();
    if (p && p.catch) p.catch(function () { /* 保持静默 */ });
  } else {
    el.pause();
  }
}

function seekMediaBy(delta) {
  const el = activeMediaEl();
  if (!el || !isFinite(el.duration)) return;
  el.currentTime = Math.min(el.duration, Math.max(0, el.currentTime + delta));
  updateMediaUI();
}

function seekFromEvent(event) {
  const el = activeMediaEl();
  const seek = document.getElementById("media-seek");
  if (!el || !seek || !isFinite(el.duration) || el.duration <= 0) return;
  const rect = seek.getBoundingClientRect();
  if (!rect.width) return;
  const ratio = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width));
  el.currentTime = ratio * el.duration;
  updateMediaUI();
}

function toggleMediaMute() {
  const el = activeMediaEl();
  if (!el) return;
  el.muted = !el.muted;
  syncMuteIcon(el);
}

function toggleMediaFullscreen() {
  const el = activeMediaEl();
  if (!el || !el.requestFullscreen) return;
  if (document.fullscreenElement) {
    document.exitFullscreen();
  } else {
    const p = el.requestFullscreen();
    if (p && p.catch) p.catch(function () { /* 保持静默 */ });
  }
}

let mediaSeeking = false;

// bindMediaPlayer 绑定自绘播放器控件（幂等）；媒体元素事件在绑定层统一接。
function bindMediaPlayer() {
  const playBtn = document.getElementById("media-play");
  const muteBtn = document.getElementById("media-mute");
  const fsBtn = document.getElementById("media-fs");
  const seek = document.getElementById("media-seek");
  if (!playBtn || playBtn.dataset.bound === "1") return;
  playBtn.dataset.bound = "1";
  playBtn.addEventListener("click", toggleMediaPlay);
  if (muteBtn) muteBtn.addEventListener("click", toggleMediaMute);
  if (fsBtn) fsBtn.addEventListener("click", toggleMediaFullscreen);
  if (seek) {
    seek.addEventListener("pointerdown", function (event) {
      mediaSeeking = true;
      if (seek.setPointerCapture) {
        try { seek.setPointerCapture(event.pointerId); } catch (e) { /* ignore */ }
      }
      seekFromEvent(event);
    });
    seek.addEventListener("pointermove", function (event) {
      if (mediaSeeking) seekFromEvent(event);
    });
    seek.addEventListener("pointerup", function () { mediaSeeking = false; });
    seek.addEventListener("pointercancel", function () { mediaSeeking = false; });
  }
  ["timeupdate", "play", "pause", "ended", "loadedmetadata", "error"].forEach(function (name) {
    ["lightbox-video", "lightbox-audio"].forEach(function (id) {
      const el = document.getElementById(id);
      if (!el) return;
      el.addEventListener(name, function () {
        // 只响应当前激活的媒体；resetPreviewPanes 清 src 触发的 error 不播报
        if (activeMediaEl() !== el) return;
        if (name === "error") {
          if (el.error) showToast("媒体加载失败，请稍后重试", "error");
          return;
        }
        updateMediaUI();
        updateMediaPlayState();
      });
    });
  });
  const video = document.getElementById("lightbox-video");
  if (video) video.addEventListener("click", toggleMediaPlay);
}

// ===== 灯箱外壳 =====

function resetPreviewPanes() {
  const image = document.getElementById("lightbox-image");
  const pre = document.getElementById("lightbox-text");
  const mdBox = document.getElementById("lightbox-markdown");
  const video = document.getElementById("lightbox-video");
  const audio = document.getElementById("lightbox-audio");
  const mediaBox = document.getElementById("lightbox-media");
  const disc = document.getElementById("media-disc");
  const fsBtn = document.getElementById("media-fs");
  const playBtn = document.getElementById("media-play");
  if (image) {
    image.hidden = true;
    image.removeAttribute("src");
    image.classList.remove("is-zoomed");
    image.style.transformOrigin = "";
  }
  if (pre) {
    pre.hidden = true;
    pre.replaceChildren();
  }
  if (mdBox) {
    mdBox.hidden = true;
    mdBox.replaceChildren();
  }
  if (mediaBox) mediaBox.hidden = true;
  if (disc) {
    disc.hidden = true;
    disc.classList.remove("is-playing");
  }
  if (fsBtn) fsBtn.hidden = true;
  if (playBtn) {
    const iconWrap = playBtn.querySelector(".btn-icon");
    if (iconWrap) iconWrap.replaceChildren(makeIcon("play"));
    playBtn.setAttribute("aria-label", "播放");
  }
  [video, audio].forEach(function (el) {
    if (!el) return;
    el.pause();
    el.removeAttribute("src");
    el.load();
    el.hidden = true;
  });
  if (previewState.objectUrl) {
    URL.revokeObjectURL(previewState.objectUrl);
    previewState.objectUrl = null;
  }
}

function openLightboxShell() {
  const lightbox = document.getElementById("image-lightbox");
  if (!lightbox) return;
  if (document.activeElement && document.activeElement !== document.body) {
    lastFocusedBeforeLightbox = document.activeElement;
  }
  lightbox.hidden = false;
  lightbox.setAttribute("aria-hidden", "false");
  document.body.classList.add("lightbox-open");
  const closeBtn = document.getElementById("lightbox-close");
  if (closeBtn) closeBtn.focus();
}

function closeLightbox() {
  const lightbox = document.getElementById("image-lightbox");
  if (!lightbox) return;
  const wasOpen = !lightbox.hidden;
  resetPreviewPanes();
  previewState.kind = null;
  previewState.path = null;
  previewState.name = "";
  previewState.seq++;
  lightbox.hidden = true;
  lightbox.setAttribute("aria-hidden", "true");
  document.body.classList.remove("lightbox-open");
  updateLightboxNav();
  if (wasOpen && lastFocusedBeforeLightbox && lastFocusedBeforeLightbox.focus) {
    const el = lastFocusedBeforeLightbox;
    lastFocusedBeforeLightbox = null;
    try {
      el.focus();
    } catch (e) {
      /* ignore */
    }
  }
}

function updateLightboxNav() {
  const prev = document.getElementById("lightbox-prev");
  const next = document.getElementById("lightbox-next");
  if (!prev || !next) return;
  const show = previewState.kind === "image" && previewNavList.length > 1;
  prev.hidden = !show;
  next.hidden = !show;
  if (!show) return;
  prev.disabled = previewNavIndex <= 0;
  next.disabled = previewNavIndex >= previewNavList.length - 1;
}

function setLightboxCaption(name, truncated) {
  const caption = document.getElementById("lightbox-caption");
  if (!caption) return;
  caption.textContent = truncated ? name + "（仅显示前 512 KB）" : name;
}

// ===== 事件绑定 =====

function bindLightboxEvents() {
  const lightbox = document.getElementById("image-lightbox");
  if (!lightbox) return;
  const closeBtn = document.getElementById("lightbox-close");
  const prevBtn = document.getElementById("lightbox-prev");
  const nextBtn = document.getElementById("lightbox-next");
  const image = document.getElementById("lightbox-image");

  if (closeBtn) closeBtn.addEventListener("click", closeLightbox);
  lightbox.addEventListener("click", function (event) {
    if (event.target === lightbox) closeLightbox();
  });
  if (prevBtn) prevBtn.addEventListener("click", function () { navigatePreview(-1); });
  if (nextBtn) nextBtn.addEventListener("click", function () { navigatePreview(1); });
  bindMediaPlayer();

  // 图片缩放：点击在 1x / 放大间切换，放大中心跟随点击位置
  if (image) {
    image.addEventListener("click", function (event) {
      if (image.classList.contains("is-zoomed")) {
        image.classList.remove("is-zoomed");
        image.style.transformOrigin = "";
        return;
      }
      const rect = image.getBoundingClientRect();
      if (!rect.width || !rect.height) return;
      const x = ((event.clientX - rect.left) / rect.width) * 100;
      const y = ((event.clientY - rect.top) / rect.height) * 100;
      image.style.transformOrigin = x + "% " + y + "%";
      image.classList.add("is-zoomed");
    });
  }

  lightbox.addEventListener("keydown", function (event) {
    if (event.key === "Tab") {
      trapFocusWithin(event, lightbox);
      return;
    }
    const isMedia = previewState.kind === "video" || previewState.kind === "audio";
    if (isMedia && event.key === " ") {
      // 空格播放/暂停；方向键快退/快进 5 秒
      event.preventDefault();
      toggleMediaPlay();
      return;
    }
    if (isMedia && event.key === "ArrowLeft") {
      event.preventDefault();
      seekMediaBy(-5);
      return;
    }
    if (isMedia && event.key === "ArrowRight") {
      event.preventDefault();
      seekMediaBy(5);
      return;
    }
    if (previewNavList.length > 1 && event.key === "ArrowLeft") {
      event.preventDefault();
      navigatePreview(-1);
    }
    if (previewNavList.length > 1 && event.key === "ArrowRight") {
      event.preventDefault();
      navigatePreview(1);
    }
  });

  document.addEventListener("keydown", function (event) {
    if (event.key === "Escape") closeLightbox();
  });
}

document.addEventListener("DOMContentLoaded", bindLightboxEvents);
