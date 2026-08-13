package main

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

// validPathRe 只允许以 / 开头的合法路径，防止路径穿越
var validPathRe = regexp.MustCompile(`^/[^\x00-\x1f]*$`)

func isValidBaiduPath(p string) bool {
	if !validPathRe.MatchString(p) {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	// 拒绝会被规范化的路径，避免路径语义在校验后发生变化。
	cleaned := path.Clean(p)
	return cleaned == p && strings.HasPrefix(cleaned, "/")
}

// pathAllowed reports whether a path is inside one of the configured sharing roots.
// An empty list disables the allowlist for backwards compatibility.
func (s *Server) pathAllowed(p string) bool {
	if s == nil || s.cfg == nil || len(s.cfg.AllowedPaths) == 0 {
		return true
	}
	for _, root := range s.cfg.AllowedPaths {
		if p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/") {
			return true
		}
	}
	return false
}

// pathNavigable permits listing an ancestor only when it leads to an allowed root.
// The handler still filters the listing, so this does not expose sibling content.
func (s *Server) pathNavigable(p string) bool {
	if s == nil || s.cfg == nil || len(s.cfg.AllowedPaths) == 0 {
		return true
	}
	if s.pathAllowed(p) {
		return true
	}
	prefix := strings.TrimSuffix(p, "/") + "/"
	for _, root := range s.cfg.AllowedPaths {
		if strings.HasPrefix(root, prefix) {
			return true
		}
	}
	return false
}

func (s *Server) pathVisible(p string) bool {
	if s == nil || s.cfg == nil || len(s.cfg.AllowedPaths) == 0 {
		return true
	}
	if s.pathAllowed(p) {
		return true
	}
	prefix := strings.TrimSuffix(p, "/") + "/"
	for _, root := range s.cfg.AllowedPaths {
		if strings.HasPrefix(root, prefix) {
			return true
		}
	}
	return false
}

// isAllowedBaiduDownloadURL 校验直链是否指向百度下载相关域名，避免开放重定向。
func isAllowedBaiduDownloadURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "baidu.com" || host == "baidupcs.com" || host == "bdstatic.com" {
		return true
	}
	for _, suffix := range []string{".baidu.com", ".baidupcs.com", ".bdstatic.com"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

func withPrivateCacheControl(dlink string) string {
	if strings.Contains(dlink, "response-cache-control=") {
		return dlink
	}
	sep := "&"
	if !strings.Contains(dlink, "?") {
		sep = "?"
	}
	return dlink + sep + "response-cache-control=private"
}
