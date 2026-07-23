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
