package main

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"
)

const (
	fileListPageSize = 100
	// 默认的单次列表请求翻页上限，可用 list_max_pages 配置。
	defaultFileListMaxPages = 15
)

// listFetchResult 目录列表拉取结果（含截断信息）
type listFetchResult struct {
	Body      []byte
	Truncated bool
	Pages     int
	Items     int
}

// fetchFileList 拉取百度网盘文件列表（自动翻页）。下载直链只在服务端按需生成。
func (s *Server) fetchFileList(dir string) (*listFetchResult, error) {
	if dir == "" {
		dir = "/"
	}
	maxPages := defaultFileListMaxPages
	if s.cfg != nil && s.cfg.listMaxPages() > 0 {
		maxPages = s.cfg.listMaxPages()
	}

	var (
		mergedList []json.RawMessage
		baseResp   map[string]json.RawMessage
		truncated  bool
		pages      int
	)

	for page := 1; page <= maxPages; page++ {
		apiURL := fmt.Sprintf(
			s.baiduBaseURL+"/youth/api/list?clienttype=0&app_id=%s&web=1&order=time&desc=1&num=%d&page=%d&dlink=1&dir=%s",
			url.QueryEscape(s.cfg.BaiduAppID),
			fileListPageSize,
			page,
			url.QueryEscape(dir),
		)
		body, err := s.baiduGet(apiURL, "")
		if err != nil {
			return nil, err
		}

		var resp map[string]json.RawMessage
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("解析文件列表失败: %w", err)
		}
		if page == 1 {
			baseResp = resp
		}
		pages = page

		// 百度业务错误（errno!=0）直接返回原始响应，由上层/缓存逻辑处理。
		if errnoRaw, ok := resp["errno"]; ok {
			var errno int
			if json.Unmarshal(errnoRaw, &errno) == nil && errno != 0 {
				if page == 1 {
					return &listFetchResult{Body: body, Pages: 1}, nil
				}
				break
			}
		}

		listRaw, ok := resp["list"]
		if !ok {
			if page == 1 {
				return &listFetchResult{Body: body, Pages: 1}, nil
			}
			break
		}
		var pageList []json.RawMessage
		if err := json.Unmarshal(listRaw, &pageList); err != nil {
			return nil, fmt.Errorf("解析文件列表 list 失败: %w", err)
		}
		mergedList = append(mergedList, pageList...)
		if len(pageList) < fileListPageSize {
			break
		}
		if page == maxPages {
			truncated = true
			log.Printf("[WARN] 目录 %q 达到翻页上限 %d 页（每页 %d），可能仍有未加载文件", dir, maxPages, fileListPageSize)
		}
	}

	if baseResp == nil {
		return nil, fmt.Errorf("文件列表响应为空")
	}
	cleanList, err := json.Marshal(mergedList)
	if err != nil {
		return nil, err
	}
	baseResp["list"] = cleanList
	// 附加服务端元信息，前端可用于提示截断。
	baseResp["truncated"] = json.RawMessage(strconv.FormatBool(truncated))
	baseResp["list_pages"] = json.RawMessage(strconv.Itoa(pages))
	baseResp["list_items"] = json.RawMessage(strconv.Itoa(len(mergedList)))
	baseResp["list_page_limit"] = json.RawMessage(strconv.Itoa(maxPages * fileListPageSize))

	out, err := json.Marshal(baseResp)
	if err != nil {
		return nil, err
	}
	return &listFetchResult{
		Body:      out,
		Truncated: truncated,
		Pages:     pages,
		Items:     len(mergedList),
	}, nil
}

func stripDownloadLinks(body []byte) ([]byte, error) {
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
	for _, item := range list {
		delete(item, "dlink")
	}
	cleanList, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	response["list"] = cleanList
	return json.Marshal(response)
}

// fetchUserSession 从百度接口获取 uk 与 sk
func (s *Server) fetchUserSession() (int64, string, error) {
	// 1. 尝试从 /youth/api/user/getinfo 获取 uk/sk
	apiURL := fmt.Sprintf(
		s.baiduBaseURL+"/youth/api/user/getinfo?app_id=%s&clienttype=0&web=1&need_selfinfo=1",
		url.QueryEscape(s.cfg.BaiduAppID),
	)
	body, err := s.baiduGet(apiURL, "")
	var uk int64
	var sk string
	if err == nil {
		var resp struct {
			Errno   int `json:"errno"`
			Records []struct {
				Uk int64  `json:"uk"`
				Sk string `json:"sk"`
			} `json:"records"`
		}
		if json.Unmarshal(body, &resp) == nil && len(resp.Records) > 0 {
			uk = resp.Records[0].Uk
			sk = resp.Records[0].Sk
		}
	}

	// 2. 如果不完整，从 /api/gettemplatevariable 获取
	if uk == 0 || sk == "" {
		fallbackURL := s.baiduBaseURL + `/api/gettemplatevariable?fields=["bdstoken","uk","sk"]`
		body2, err2 := s.baiduGet(fallbackURL, "")
		if err2 == nil {
			var resp2 struct {
				Result struct {
					Uk int64  `json:"uk"`
					Sk string `json:"sk"`
				} `json:"result"`
			}
			if json.Unmarshal(body2, &resp2) == nil {
				if uk == 0 {
					uk = resp2.Result.Uk
				}
				if sk == "" {
					sk = resp2.Result.Sk
				}
			}
		}
	}

	// 3. 如果还是没有 sk，从 /youth/api/report/user 获取
	if sk == "" && uk != 0 {
		skURL := fmt.Sprintf(
			s.baiduBaseURL+"/youth/api/report/user?app_id=%s&clienttype=0&web=1&action=sapi_auth&timestamp=%d",
			url.QueryEscape(s.cfg.BaiduAppID),
			time.Now().UnixMilli(),
		)
		bodySK, errSK := s.baiduGet(skURL, "")
		if errSK == nil {
			var respSK struct {
				Uinfo string `json:"uinfo"`
			}
			if json.Unmarshal(bodySK, &respSK) == nil && respSK.Uinfo != "" {
				sk = respSK.Uinfo
			}
		}
	}

	if uk == 0 || sk == "" {
		return 0, "", fmt.Errorf("无法从百度网盘获取完整的 Session uk=%d，sk 缺失或无效", uk)
	}

	return uk, sk, nil
}

// getSession 线程安全地获取或刷新 uk/sk（含 TTL）
func (s *Server) getSession() (int64, string, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	ttl := s.cfg.sessionTTL()
	if s.uk != 0 && s.sk != "" && !s.sessionAt.IsZero() && time.Since(s.sessionAt) < ttl {
		return s.uk, s.sk, nil
	}
	uk, sk, err := s.fetchUserSession()
	if err != nil {
		return 0, "", err
	}
	s.uk = uk
	s.sk = sk
	s.sessionAt = time.Now()
	log.Printf("[Session] 获取成功, uk: %d", uk)
	return uk, sk, nil
}

func (s *Server) clearSession() {
	s.sessionMu.Lock()
	s.sk = ""
	s.uk = 0
	s.sessionAt = time.Time{}
	s.sessionMu.Unlock()
}

// locatedownloadRand 使用 SHA-1 计算位于下载的 rand 参数
func locatedownloadRand(uk int64, sk string, nowMilli int64) string {
	data := fmt.Sprintf("%d%s%d0", uk, sk, nowMilli)
	h := sha1.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// locatedownloadSign 使用 MD5 计算位于下载的 sign 参数
func locatedownloadSign(fileMD5 string, fileID string, uk int64, nowMilli int64) string {
	data := fmt.Sprintf("%s_%d_%s_%d", fileMD5, uk, fileID, nowMilli)
	h := md5.New()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// ensureFileMeta 确保路径对应的文件元数据在缓存中（非目录）。
func (s *Server) ensureFileMeta(filePath string) (fileMeta, error) {
	meta, ok := s.cache.getFileMeta(filePath)
	if !ok {
		parentDir := path.Dir(filePath)
		log.Printf("[缓存] 未命中 %q，重新拉取父目录 %q 的文件列表", filePath, parentDir)
		result, err := s.fetchFileList(parentDir)
		if err != nil {
			return fileMeta{}, fmt.Errorf("重新拉取文件列表失败: %w", err)
		}
		s.cache.update(result.Body)
		meta, ok = s.cache.getFileMeta(filePath)
	}
	if !ok {
		return fileMeta{}, fmt.Errorf("找不到文件: %s", filePath)
	}
	if meta.IsDir == 1 {
		return fileMeta{}, fmt.Errorf("路径是目录，不能下载: %s", filePath)
	}
	return meta, nil
}

// getBaiduDLink 计算签名并向百度 locatedownload 接口获取直链（包含 sk 过期自动重试）
func (s *Server) getBaiduDLink(filePath string, ua string) (string, error) {
	meta, err := s.ensureFileMeta(filePath)
	if err != nil {
		return "", err
	}
	ua = normalizeDownloadUA(ua)
	if cachedDLink, ok := s.cache.getCachedDLink(filePath, ua); ok {
		return withPrivateCacheControl(cachedDLink), nil
	}

	uk, sk, err := s.getSession()
	if err != nil {
		return "", fmt.Errorf("获取百度Session失败: %w", err)
	}

	// 百度有时会用 HTTP 200 返回 errno 错误，因此响应解析失败也必须进入一次 Session 刷新重试。
	body, err := s.requestDLink(filePath, meta, uk, sk, ua)
	dlink, parseErr := parseDLinkResponse(body)
	if err != nil || parseErr != nil {
		if err != nil {
			log.Printf("[WARN] 首次 locatedownload 失败: %v，尝试清除 sk 并重试", err)
		} else {
			log.Printf("[WARN] 首次 locatedownload 返回错误: %v，尝试清除 sk 并重试", parseErr)
		}
		s.clearSession()

		uk, sk, err = s.getSession()
		if err != nil {
			return "", fmt.Errorf("重新获取Session失败: %w", err)
		}
		body, err = s.requestDLink(filePath, meta, uk, sk, ua)
		if err != nil {
			return "", fmt.Errorf("重试 locatedownload 失败: %w", err)
		}
		dlink, err = parseDLinkResponse(body)
		if err != nil {
			return "", fmt.Errorf("重试 locatedownload 失败: %w", err)
		}
	}

	dlink = withPrivateCacheControl(dlink)
	s.cache.setDLink(filePath, dlink, ua)
	return dlink, nil
}

func (s *Server) requestDLink(filePath string, meta fileMeta, uk int64, sk, ua string) ([]byte, error) {
	nowMilli := time.Now().UnixMilli()
	randVal := locatedownloadRand(uk, sk, nowMilli)
	signVal := locatedownloadSign(meta.MD5, strconv.FormatInt(meta.FsID, 10), uk, nowMilli)
	locateURL := fmt.Sprintf(
		s.baiduBaseURL+"/youth/api/locatedownload?app_id=%s&clienttype=0&web=1&devuid=0&dp-logid=%d&path=%s&rand=%s&sign=%s&time=%d",
		url.QueryEscape(s.cfg.BaiduAppID),
		time.Now().UnixNano(),
		url.QueryEscape(filePath),
		randVal,
		signVal,
		nowMilli,
	)
	return s.baiduGet(locateURL, ua)
}

func parseDLinkResponse(body []byte) (string, error) {
	if len(body) == 0 {
		return "", fmt.Errorf("locatedownload 响应为空")
	}
	var resp struct {
		Errno   int    `json:"errno"`
		ShowMsg string `json:"show_msg"`
		URL     string `json:"url"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("解析 locatedownload 响应失败: %w", err)
	}
	if resp.Errno != 0 || resp.URL == "" {
		return "", fmt.Errorf("百度 locatedownload 返回错误 errno=%d, msg=%q", resp.Errno, resp.ShowMsg)
	}
	if !isAllowedBaiduDownloadURL(resp.URL) {
		return "", fmt.Errorf("百度 locatedownload 返回了非白名单下载地址")
	}
	return resp.URL, nil
}

// baiduGet 向百度网盘API发起GET请求，Cookie由服务端注入
func (s *Server) baiduGet(apiURL string, ua string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// Cookie只在服务端注入，绝不返回给客户端
	req.Header.Set("Cookie", s.cfg.BaiduCookie)
	req.Header.Set("User-Agent", normalizeDownloadUA(ua))
	req.Header.Set("Referer", "https://pan.baidu.com/")

	client := s.httpClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求百度网盘API失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("百度网盘API返回非200状态: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	return body, nil
}

func downloadJSONResponse(dlink string) ([]byte, error) {
	return json.Marshal(struct {
		URLs []struct {
			URL string `json:"url"`
		} `json:"urls"`
	}{URLs: []struct {
		URL string `json:"url"`
	}{{URL: dlink}}})
}

// probeBaiduSession 探测 Cookie/Session 是否可用（用于 readiness）。
func (s *Server) probeBaiduSession() error {
	_, _, err := s.getSession()
	return err
}
