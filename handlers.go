package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
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
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求体格式错误")
		return
	}
	token := strings.TrimSpace(request.Token)
	if subtleCompareToken(token, s.cfg.AccessToken) {
		s.setAccessCookie(w, r, s.cfg.AccessToken)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSONError(w, http.StatusUnauthorized, "invalid_token", "访问令牌错误")
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
			writeJSONError(w, http.StatusBadRequest, "bad_request", "请求体格式错误")
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
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "非法路径")
		return
	}

	result, err := s.fetchFileList(dir)
	if err != nil {
		log.Printf("[ERROR] 获取文件列表失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "baidu_list_failed", "获取文件列表失败，请检查 Cookie 是否有效")
		return
	}

	// 直链只缓存于服务端，返回给前端前必须移除 dlink 字段。
	s.cache.update(result.Body)
	publicBody, err := stripDownloadLinks(result.Body)
	if err != nil {
		log.Printf("[ERROR] 清理文件列表直链失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "list_process_failed", "处理文件列表失败")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(publicBody)
}

// handleDownload 仅创建不透明短链接；真实百度直链在 /d/{token} 时再解析。
func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 8*1024)
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "请求体格式错误")
		return
	}
	filePath := request.Path
	if filePath == "" {
		writeJSONError(w, http.StatusBadRequest, "missing_path", "缺少 path 参数")
		return
	}
	if !isValidBaiduPath(filePath) {
		log.Printf("[WARN] 非法路径请求被拒绝: %q", filePath)
		writeJSONError(w, http.StatusBadRequest, "invalid_path", "非法路径")
		return
	}

	// 创建短链前确认文件存在且非目录
	if _, err := s.ensureFileMeta(filePath); err != nil {
		log.Printf("[WARN] 创建短链前校验失败: %v", err)
		writeJSONError(w, http.StatusNotFound, "file_not_found", "文件不存在或不可下载")
		return
	}

	token, err := s.shortLinks.create(filePath)
	if err != nil {
		log.Printf("[ERROR] 创建短链接失败: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "shortlink_failed", "创建短链接失败")
		return
	}
	respJSON, err := downloadJSONResponse("/d/" + token)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "response_failed", "生成直链响应失败")
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
		writeJSONError(w, http.StatusNotFound, "shortlink_not_found", "短链不存在或已过期")
		return
	}
	s.redirectToBaiduDownload(w, r, filePath)
}

func (s *Server) redirectToBaiduDownload(w http.ResponseWriter, r *http.Request, filePath string) {
	dlink, err := s.getBaiduDLink(filePath, r.Header.Get("User-Agent"))
	if err != nil {
		log.Printf("[ERROR] 获取百度直链失败: %v", err)
		writeJSONError(w, http.StatusBadGateway, "dlink_failed", "获取直链失败，请稍后重试或检查 Cookie")
		return
	}
	if !isAllowedBaiduDownloadURL(dlink) {
		log.Printf("[ERROR] 拒绝非白名单下载地址: %q", dlink)
		writeJSONError(w, http.StatusBadGateway, "dlink_rejected", "获取直链失败")
		return
	}
	http.Redirect(w, r, dlink, http.StatusFound)
}
