package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
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
		"auth_required": enabled,
		"authenticated": authenticated,
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

	// 直链只缓存于服务端，返回给前端前必须移除 dlink 字段。
	s.cache.update(result.Body)
	publicBody, err := stripDownloadLinks(result.Body)
	if err != nil {
		log.Printf("[ERROR] 清理文件列表直链失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "list_process_failed", "文件列表处理失败，请稍后重试")
		return
	}
	publicBody, err = s.filterFileList(publicBody)
	if err != nil {
		log.Printf("[ERROR] 过滤共享范围失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "list_process_failed", "文件列表处理失败")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(publicBody)
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

func (s *Server) handleShortDownload(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/d/")
	token = strings.Trim(token, "/")
	filePath, ok := s.shortLinks.resolve(token, true)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "shortlink_not_found", "下载链接已失效，请重新获取")
		return
	}
	if !s.pathAllowed(filePath) {
		writeJSONError(w, http.StatusForbidden, "path_not_allowed", "该文件不在共享范围内")
		return
	}
	s.audit(r, "shortlink_accessed", filePath)
	s.redirectToBaiduDownload(w, r, filePath)
}

func (s *Server) audit(r *http.Request, event, filePath string) {
	if s.cfg.AuditLogPath == "" {
		return
	}
	record := map[string]any{"at": time.Now().UTC().Format(time.RFC3339), "event": event, "path": filePath, "ip": s.requestClientIP(r), "user_agent": r.UserAgent()}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	f, err := os.OpenFile(s.cfg.AuditLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("[audit] open failed: %v", err)
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

func (s *Server) redirectToBaiduDownload(w http.ResponseWriter, r *http.Request, filePath string) {
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
	http.Redirect(w, r, dlink, http.StatusFound)
}
