// README 的零依赖 Markdown 安全渲染器。
// 只创建 DOM 节点，不使用 innerHTML，因此原始 HTML 和脚本不会被执行。

function renderMarkdown(root, source) {
  root.replaceChildren();
  const lines = String(source || "").replace(/\r\n?/g, "\n").split("\n");
  let index = 0;

  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) {
      index++;
      continue;
    }

    const fence = line.match(/^\s*```\s*([^`]*)$/);
    if (fence) {
      const code = [];
      index++;
      while (index < lines.length && !/^\s*```\s*$/.test(lines[index])) {
        code.push(lines[index++]);
      }
      if (index < lines.length) index++;
      const pre = document.createElement("pre");
      pre.className = "readme-code";
      const codeEl = document.createElement("code");
      if (fence[1].trim()) codeEl.dataset.language = fence[1].trim();
      codeEl.textContent = code.join("\n");
      pre.appendChild(codeEl);
      root.appendChild(pre);
      continue;
    }

    const heading = line.match(/^\s{0,3}(#{1,6})\s+(.+?)\s*#*\s*$/);
    if (heading) {
      const el = document.createElement("h" + heading[1].length);
      appendInlineMarkdown(el, heading[2]);
      root.appendChild(el);
      index++;
      continue;
    }

    if (/^\s*(\*{3,}|-{3,}|_{3,})\s*$/.test(line)) {
      root.appendChild(document.createElement("hr"));
      index++;
      continue;
    }

    const quote = line.match(/^\s*>\s?(.*)$/);
    if (quote) {
      const blockquote = document.createElement("blockquote");
      appendInlineMarkdown(blockquote, quote[1]);
      root.appendChild(blockquote);
      index++;
      continue;
    }

    const list = line.match(/^\s*([-*+]\s+|\d+[.]\s+)(.+)$/);
    if (list) {
      const ordered = /^\d/.test(list[1]);
      const listEl = document.createElement(ordered ? "ol" : "ul");
      while (index < lines.length) {
        const item = lines[index].match(/^\s*([-*+]\s+|\d+[.]\s+)(.+)$/);
        if (!item || /^\d/.test(item[1]) !== ordered) break;
        const li = document.createElement("li");
        appendInlineMarkdown(li, item[2]);
        listEl.appendChild(li);
        index++;
      }
      root.appendChild(listEl);
      continue;
    }

    const paragraph = [line.trim()];
    index++;
    while (index < lines.length && lines[index].trim() &&
      !/^\s*```/.test(lines[index]) &&
      !/^\s{0,3}#{1,6}\s+/.test(lines[index]) &&
      !/^\s*(?:[-*+]\s+|\d+[.]\s+|>\s?)/.test(lines[index])) {
      paragraph.push(lines[index].trim());
      index++;
    }
    const p = document.createElement("p");
    appendInlineMarkdown(p, paragraph.join("\n"));
    root.appendChild(p);
  }
}

function appendInlineMarkdown(root, source) {
  const pattern = /(`[^`]+`|\[([^\]]+)\]\(([^)]+)\)|\*\*([^*]+)\*\*|__([^_]+)__|\*([^*]+)\*|_([^_]+)_)/g;
  let last = 0;
  let match;
  while ((match = pattern.exec(source))) {
    if (match.index > last) root.appendChild(document.createTextNode(source.slice(last, match.index)));
    if (match[1][0] === "`") {
      const code = document.createElement("code");
      code.textContent = match[1].slice(1, -1);
      root.appendChild(code);
    } else if (match[2] && safeMarkdownURL(match[3])) {
      const link = document.createElement("a");
      link.href = match[3];
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      link.textContent = match[2];
      root.appendChild(link);
    } else if (match[2]) {
      root.appendChild(document.createTextNode(match[0]));
    } else {
      const strong = match[4] || match[5];
      const em = match[6] || match[7];
      const el = document.createElement(strong ? "strong" : "em");
      el.textContent = strong || em;
      root.appendChild(el);
    }
    last = pattern.lastIndex;
  }
  if (last < source.length) root.appendChild(document.createTextNode(source.slice(last)));
}

function safeMarkdownURL(value) {
  try {
    const url = new URL(value, window.location.origin);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch (_) {
    return false;
  }
}
