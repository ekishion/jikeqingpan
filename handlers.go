package main

import (
	"encoding/json"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.probeBaiduSession(); err != nil {
		log.Printf("[readyz] baidu session probe failed: %v", err)
		writeJSONError(w, http.StatusServiceUnavailable, "not_ready", "百度会话不可用，请检查 baidu_cookie")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	enabled := s.cfg.authEnabled()
	authenticated := !enabled || s.hasValidAccess(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_required":        enabled,
		"authenticated":        authenticated,
		"show_readme":          s.cfg.showReadme(),
		"show_readme_overview": s.cfg.showReadmeOverview(),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.authEnabled() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "auth_required": false})
		return
	}
	// 登录失败指数退避：锁定期内直接拒绝，不比对令牌。
	ip := s.requestClientIP(r)
	now := time.Now()
	if ok, retryAfter := s.loginGuard.allow(ip, now); !ok {
		secs := int(retryAfter.Seconds())
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeJSONError(w, http.StatusTooManyRequests, "login_locked", "验证过于频繁，请稍后再试")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	token := strings.TrimSpace(request.Token)
	if subtleCompareToken(token, s.cfg.AccessToken) {
		s.loginGuard.recordSuccess(ip)
		if err := s.issueSession(w, r); err != nil {
			log.Printf("[ERROR] 签发会话令牌失败: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "session_issue_failed", "验证失败，请稍后重试")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	s.loginGuard.recordFailure(ip, now)
	writeJSONError(w, http.StatusUnauthorized, "invalid_token", "访问令牌不正确，请检查后重试")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearAccessCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleFiles 获取文件列表，代理百度网盘 list API
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	var dir string
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
		var request struct {
			Dir string `json:"dir"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
			return
		}
		dir = request.Dir
	} else {
		dir = r.URL.Query().Get("dir")
	}
	if dir == "" {
		dir = "/"
	}
	if !isValidBaiduPath(dir) {
		log.Printf("[WARN] 获取文件列表的非法路径被拒绝: %q", dir)
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !s.pathNavigable(dir) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该目录不在共享范围内")
		return
	}

	result, err := s.fetchFileList(dir)
	if err != nil {
		log.Printf("[ERROR] 获取文件列表失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "baidu_list_failed", "无法获取文件列表，请稍后重试")
		return
	}

	// 先按共享范围过滤再写缓存：越界路径不占用缓存空间。
	// 缓存需要 dlink 字段计算签名，因此先 update 缓存、再剥离 dlink 返回给前端。
	filteredBody, err := s.filterFileList(result.Body)
	if err != nil {
		log.Printf("[ERROR] 过滤共享范围失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "list_process_failed", "文件列表处理失败")
		return
	}
	s.cache.update(filteredBody)
	publicBody, err := stripDownloadLinks(filteredBody)
	if err != nil {
		log.Printf("[ERROR] 清理文件列表直链失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "list_process_failed", "文件列表处理失败，请稍后重试")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(publicBody)
}

// handleReadme 读取当前目录中的 README 文件，并返回给前端安全渲染。
func (s *Server) handleReadme(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	readmePath := request.Path
	if !isValidBaiduPath(readmePath) {
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !isReadmeFileName(readmeName(readmePath)) {
		writeJSONError(w, http.StatusNotFound, "readme_not_found", "README 不存在")
		return
	}
	if !s.pathAllowed(readmePath) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该目录不在共享范围内")
		return
	}

	dlink, err := s.getBaiduDLink(readmePath, r.Header.Get("User-Agent"))
	if err != nil || !isAllowedBaiduDownloadURL(dlink) {
		writeJSONError(w, http.StatusBadGateway, "readme_link_failed", "无法准备 README")
		return
	}
	upstream, err := s.httpClient.Get(dlink)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "readme_fetch_failed", "无法读取 README")
		return
	}
	defer upstream.Body.Close()
	if upstream.StatusCode < http.StatusOK || upstream.StatusCode >= http.StatusMultipleChoices {
		writeJSONError(w, http.StatusBadGateway, "readme_fetch_failed", "README 暂时不可用")
		return
	}
	maxReadmeBytes := s.cfg.readmeMaxBytes()
	body, err := io.ReadAll(io.LimitReader(upstream.Body, int64(maxReadmeBytes)+1))
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "readme_fetch_failed", "无法读取 README")
		return
	}
	if len(body) > maxReadmeBytes {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "readme_too_large", "README 超过大小上限，无法展示")
		return
	}
	if !utf8.Valid(body) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "readme_not_text", "README 不是可展示的文本文件")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"found":   true,
		"name":    readmeName(readmePath),
		"content": string(body),
	})
}

func readmeName(filePath string) string {
	if index := strings.LastIndex(filePath, "/"); index >= 0 {
		return filePath[index+1:]
	}
	return filePath
}

// textPreviewExts 允许文本预览的扩展名白名单（与 static/preview.js 的 TEXT_PREVIEW_EXTS 保持同步）。
var textPreviewExts = map[string]struct{}{
	"txt": {}, "md": {}, "markdown": {}, "json": {}, "xml": {}, "yaml": {}, "yml": {},
	"csv": {}, "log": {}, "ini": {}, "conf": {}, "sh": {}, "bat": {}, "ps1": {},
	"py": {}, "js": {}, "ts": {}, "go": {}, "c": {}, "h": {}, "cpp": {}, "hpp": {},
	"java": {}, "rs": {}, "sql": {}, "toml": {}, "html": {}, "css": {},
}

func isTextPreviewableExt(name string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	_, ok := textPreviewExts[ext]
	return ok
}

// handleTextPreview 返回文本/代码文件内容，供前端在灯箱中安全渲染。
// 内容以 JSON 字符串回传，前端用 textContent/安全渲染器展示，无注入面。
func (s *Server) handleTextPreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	if !isValidBaiduPath(request.Path) {
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !s.pathAllowed(request.Path) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}
	if !isTextPreviewableExt(request.Path) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "text_not_allowed", "该文件类型不支持文本预览")
		return
	}
	if meta, err := s.ensureFileMeta(request.Path); err != nil || meta.IsDir == 1 {
		writeJSONError(w, http.StatusNotFound, "file_not_found", "文件不存在或已被移动")
		return
	}
	dlink, err := s.getBaiduDLink(request.Path, r.Header.Get("User-Agent"))
	if err != nil || !isAllowedBaiduDownloadURL(dlink) {
		writeJSONError(w, http.StatusBadGateway, "text_link_failed", "无法准备文本预览")
		return
	}
	upstream, err := s.httpClient.Get(dlink)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "text_fetch_failed", "无法读取文本内容")
		return
	}
	defer upstream.Body.Close()
	if upstream.StatusCode < http.StatusOK || upstream.StatusCode >= http.StatusMultipleChoices {
		writeJSONError(w, http.StatusBadGateway, "text_fetch_failed", "文本内容暂时不可用")
		return
	}
	const maxTextBytes = 512 * 1024
	body, err := io.ReadAll(io.LimitReader(upstream.Body, maxTextBytes+1))
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "text_fetch_failed", "无法读取文本内容")
		return
	}
	truncated := false
	if len(body) > maxTextBytes {
		body = body[:maxTextBytes]
		truncated = true
	}
	if truncated {
		// 截断点可能落在多字节字符中间，最多回退 UTF-8 单字符的最大字节数
		for i := 0; i < utf8.UTFMax && len(body) > 0 && !utf8.Valid(body); i++ {
			body = body[:len(body)-1]
		}
	}
	if !utf8.Valid(body) {
		writeJSONError(w, http.StatusUnsupportedMediaType, "text_not_text", "该文件不是文本内容")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"found":     true,
		"name":      readmeName(request.Path),
		"content":   string(body),
		"truncated": truncated,
	})
}

func isReadmeFileName(name string) bool {
	switch strings.ToLower(name) {
	case "readme.md", "readme.markdown", "readme.txt", "readme":
		return true
	default:
		return false
	}
}

func (s *Server) filterFileList(body []byte) ([]byte, error) {
	if s == nil || s.cfg == nil || len(s.cfg.AllowedPaths) == 0 {
		return body, nil
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	listRaw, ok := response["list"]
	if !ok {
		return body, nil
	}
	var list []map[string]json.RawMessage
	if err := json.Unmarshal(listRaw, &list); err != nil {
		return nil, err
	}
	filtered := make([]map[string]json.RawMessage, 0, len(list))
	for _, item := range list {
		var itemPath string
		if err := json.Unmarshal(item["path"], &itemPath); err != nil || !isValidBaiduPath(itemPath) {
			continue
		}
		if s.pathVisible(itemPath) {
			filtered = append(filtered, item)
		}
	}
	cleanList, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	response["list"] = cleanList
	response["list_items"] = json.RawMessage(strconv.Itoa(len(filtered)))
	return json.Marshal(response)
}

// handleDownload 仅创建不透明短链接；真实百度直链在 /d/{token} 时再解析。
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	filePath := request.Path
	if filePath == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_path", "请选择要下载的文件")
		return
	}
	if !isValidBaiduPath(filePath) {
		log.Printf("[WARN] 非法路径请求被拒绝: %q", filePath)
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !s.pathAllowed(filePath) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}

	// 创建短链前确认文件存在且非目录
	if _, err := s.ensureFileMeta(filePath); err != nil {
		log.Printf("[WARN] 创建短链前校验失败: %v", err)
		writeJSONError(w, http.StatusNotFound, "file_not_found", "文件不存在或已被移动")
		return
	}

	token, err := s.shortLinks.create(filePath)
	if err != nil {
		log.Printf("[ERROR] 创建短链接失败: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "shortlink_failed", "生成下载链接失败，请重试")
		return
	}
	s.audit(r, "shortlink_issued", filePath)
	respJSON, err := downloadJSONResponse("/d/" + token)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "response_failed", "生成下载链接失败，请重试")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(respJSON)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求内容有误，请刷新页面重试")
		return
	}
	if !isValidBaiduPath(request.Path) {
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "文件路径无效")
		return
	}
	if !s.pathAllowed(request.Path) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}
	meta, err := s.ensureFileMeta(request.Path)
	if err != nil || meta.IsDir == 1 {
		writeJSONError(w, http.StatusNotFound, "file_not_found", "图片不存在或已被移动")
		return
	}
	dlink, err := s.getBaiduDLink(request.Path, r.Header.Get("User-Agent"))
	if err != nil || !isAllowedBaiduDownloadURL(dlink) {
		writeJSONError(w, http.StatusBadGateway, "preview_link_failed", "无法准备图片预览")
		return
	}
	upstream, err := s.httpClient.Get(dlink)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "preview_fetch_failed", "无法读取图片预览")
		return
	}
	defer upstream.Body.Close()
	if upstream.StatusCode < http.StatusOK || upstream.StatusCode >= http.StatusMultipleChoices {
		writeJSONError(w, http.StatusBadGateway, "preview_fetch_failed", "图片预览暂时不可用")
		return
	}
	maxPreviewBytes := s.cfg.previewMaxBytes()
	// 上游明确声明超限时直接拒绝，避免白拉整份流量。
	if upstream.ContentLength > int64(maxPreviewBytes) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "preview_too_large", "图片超过可预览的大小上限")
		return
	}
	// 只缓冲嗅探所需的前 512 字节，其余流式转发，避免大图整块驻留内存。
	sniff := make([]byte, 512)
	n, _ := io.ReadFull(upstream.Body, sniff)
	contentType := http.DetectContentType(sniff[:n])
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(request.Path)))
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		writeJSONError(w, http.StatusUnsupportedMediaType, "preview_not_image", "该文件不是可预览的图片")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, no-store")
	if upstream.ContentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(upstream.ContentLength, 10))
	}
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(sniff[:n]); err != nil {
		return
	}
	// 无 Content-Length 的上游靠限读兜底；超限截断的图片无法渲染，属可接受的降级。
	_, _ = io.Copy(w, io.LimitReader(upstream.Body, int64(maxPreviewBytes)-int64(n)))
}

func (s *Server) handleShortDownload(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/d/")
	token = strings.Trim(token, "/")
	// 先探查不计入使用次数，直链解析成功后才消耗，避免解析失败白耗短链。
	filePath, ok := s.shortLinks.resolve(token, false)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "shortlink_not_found", "下载链接已失效，请重新获取")
		return
	}
	if !s.pathAllowed(filePath) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}
	dlink, err := s.getBaiduDLink(filePath, r.Header.Get("User-Agent"))
	if err != nil {
		log.Printf("[ERROR] 获取百度直链失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "dlink_failed", "下载失败，请稍后重试")
		return
	}
	if !isAllowedBaiduDownloadURL(dlink) {
		log.Printf("[ERROR] 拒绝非白名单下载地址: %q", dlink)
		writeJSONError(w, http.StatusBadGateway, "dlink_rejected", "下载失败，请稍后重试")
		return
	}
	if _, consumed := s.shortLinks.resolve(token, true); !consumed {
		// 探查与消耗之间短链恰好过期或耗尽，按失效处理。
		writeJSONError(w, http.StatusNotFound, "shortlink_not_found", "下载链接已失效，请重新获取")
		return
	}
	s.audit(r, "shortlink_accessed", filePath)
	http.Redirect(w, r, dlink, http.StatusFound)
}

// defaultAuditMaxBytes 审计日志滚动阈值：超过后轮转为 *.old，磁盘占用有界。
const defaultAuditMaxBytes = 10 * 1024 * 1024

func (s *Server) audit(r *http.Request, event, filePath string) {
	if s.cfg.AuditLogPath == "" {
		return
	}
	record := map[string]any{"at": time.Now().UTC().Format(time.RFC3339), "event": event, "path": filePath, "ip": s.requestClientIP(r), "user_agent": r.UserAgent()}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	data = append(data, '\n')
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	if err := s.writeAuditLocked(data); err != nil {
		log.Printf("[audit] write failed: %v", err)
	}
}

// writeAuditLocked 写入审计记录，超限时滚动。调用方需持有 auditMu。
func (s *Server) writeAuditLocked(data []byte) error {
	if s.auditFile == nil {
		if err := s.openAuditFileLocked(); err != nil {
			return err
		}
	}
	if s.auditSize+int64(len(data)) > s.auditMax {
		s.rotateAuditFileLocked()
	}
	n, err := s.auditFile.Write(data)
	s.auditSize += int64(n)
	return err
}

func (s *Server) openAuditFileLocked() error {
	if dir := filepath.Dir(s.cfg.AuditLogPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(s.cfg.AuditLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	s.auditFile = f
	s.auditSize = info.Size()
	return nil
}

// rotateAuditFileLocked 关闭当前文件、轮转为 .old（覆盖上一次的 .old）后重开。
func (s *Server) rotateAuditFileLocked() {
	if s.auditFile != nil {
		_ = s.auditFile.Close()
		s.auditFile = nil
	}
	if err := os.Rename(s.cfg.AuditLogPath, s.cfg.AuditLogPath+".old"); err != nil && !os.IsNotExist(err) {
		log.Printf("[audit] rotate failed: %v", err)
	}
	if err := s.openAuditFileLocked(); err != nil {
		log.Printf("[audit] reopen after rotate failed: %v", err)
	}
}

// closeAudit 关闭审计日志句柄（优雅关闭时调用）。
func (s *Server) closeAudit() {
	if s.cfg == nil || s.cfg.AuditLogPath == "" {
		return
	}
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	if s.auditFile != nil {
		_ = s.auditFile.Close()
		s.auditFile = nil
	}
}
