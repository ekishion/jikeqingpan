package main

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// fileMeta 文件元数据，包含下载签名计算所需的 id 和 md5
type fileMeta struct {
	FsID          int64     `json:"fs_id"`
	MD5           string    `json:"md5"`
	IsDir         int       `json:"isdir"`
	DLink         string    `json:"dlink"`
	DLinkUA       string    `json:"-"` // 直链与申请时的 UA 绑定，避免跨 UA 复用失效
	DLinkCachedAt time.Time `json:"-"`
	CachedAt      time.Time `json:"-"`
	LastAccessAt  time.Time `json:"-"`
}

// fileListCache 缓存文件列表中每个文件的元数据，按路径索引
type fileListCache struct {
	mu          sync.RWMutex
	filesByPath map[string]fileMeta // 文件路径 -> 元数据
	updatedAt   time.Time
	maxEntries  int
	ttl         time.Duration
	dlinkTTL    time.Duration
}

const cachedDLinkTTL = 5 * time.Minute
const fileMetaTTL = 15 * time.Minute
const maxCachedFiles = 10000

func newFileListCache() *fileListCache {
	return newFileListCacheWithLimits(maxCachedFiles, fileMetaTTL, cachedDLinkTTL)
}

func newFileListCacheWithLimits(maxEntries int, ttl, dlinkTTL time.Duration) *fileListCache {
	if maxEntries < 1 {
		maxEntries = maxCachedFiles
	}
	if ttl <= 0 {
		ttl = fileMetaTTL
	}
	if dlinkTTL <= 0 {
		dlinkTTL = cachedDLinkTTL
	}
	return &fileListCache{
		filesByPath: make(map[string]fileMeta),
		maxEntries:  maxEntries,
		ttl:         ttl,
		dlinkTTL:    dlinkTTL,
	}
}

// update 解析 Baidu 列表响应并更新索引
func (c *fileListCache) update(listJSON []byte) {
	var resp struct {
		Errno int `json:"errno"`
		List  []struct {
			Path  string `json:"path"`
			FsID  int64  `json:"fs_id"`
			MD5   string `json:"md5"`
			IsDir int    `json:"isdir"`
			DLink string `json:"dlink"`
		} `json:"list"`
	}
	if err := json.Unmarshal(listJSON, &resp); err != nil {
		log.Printf("[WARN] 解析文件列表失败: %v", err)
		return
	}
	if resp.Errno != 0 {
		log.Printf("[WARN] 百度API返回错误 errno=%d", resp.Errno)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.filesByPath == nil {
		c.filesByPath = make(map[string]fileMeta)
	}
	now := time.Now()
	c.cleanupExpiredLocked(now)
	for _, f := range resp.List {
		if f.Path != "" && f.FsID != 0 {
			// 列表接口用默认 UA 拉取；仅当后续请求 UA 一致时才复用该 dlink。
			dlink := f.DLink
			dlinkUA := ""
			dlinkCachedAt := time.Time{}
			if dlink != "" {
				dlinkUA = defaultBaiduUA
				dlinkCachedAt = now
			}
			c.filesByPath[f.Path] = fileMeta{
				FsID:          f.FsID,
				MD5:           f.MD5,
				IsDir:         f.IsDir,
				DLink:         dlink,
				DLinkUA:       dlinkUA,
				DLinkCachedAt: dlinkCachedAt,
				CachedAt:      now,
				LastAccessAt:  now,
			}
		}
	}
	c.enforceLimitLocked()
	c.updatedAt = time.Now()
	log.Printf("[缓存] 更新完成：共 %d 个文件，当前总共已缓存 %d 个元数据", len(resp.List), len(c.filesByPath))
}

// getFileMeta 根据路径获取文件元数据
func (c *fileListCache) getFileMeta(path string) (fileMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.filesByPath[path]
	if ok && !meta.CachedAt.IsZero() && time.Since(meta.CachedAt) >= c.ttl {
		delete(c.filesByPath, path)
		return fileMeta{}, false
	}
	if ok {
		meta.LastAccessAt = time.Now()
		c.filesByPath[path] = meta
	}
	return meta, ok
}

func (c *fileListCache) getCachedDLink(filePath, ua string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.filesByPath[filePath]
	if ok && !meta.CachedAt.IsZero() && time.Since(meta.CachedAt) >= c.ttl {
		delete(c.filesByPath, filePath)
		return "", false
	}
	ua = normalizeDownloadUA(ua)
	if !ok || meta.DLink == "" || meta.DLinkUA == "" || meta.DLinkUA != ua ||
		meta.DLinkCachedAt.IsZero() || time.Since(meta.DLinkCachedAt) >= c.dlinkTTL {
		return "", false
	}
	meta.LastAccessAt = time.Now()
	c.filesByPath[filePath] = meta
	return meta.DLink, true
}

func (c *fileListCache) setDLink(filePath, dlink, ua string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.filesByPath[filePath]
	if !ok {
		return
	}
	meta.DLink = dlink
	meta.DLinkUA = normalizeDownloadUA(ua)
	meta.DLinkCachedAt = time.Now()
	meta.LastAccessAt = meta.DLinkCachedAt
	c.filesByPath[filePath] = meta
}

func (c *fileListCache) cleanupExpiredLocked(now time.Time) {
	for filePath, meta := range c.filesByPath {
		if !meta.CachedAt.IsZero() && now.Sub(meta.CachedAt) >= c.ttl {
			delete(c.filesByPath, filePath)
		}
	}
}

func (c *fileListCache) enforceLimitLocked() {
	for len(c.filesByPath) > c.maxEntries {
		evicted := evictOldestSampled(c.filesByPath, func(meta fileMeta) time.Time {
			if meta.LastAccessAt.IsZero() {
				return meta.CachedAt
			}
			return meta.LastAccessAt
		}, evictionSampleSize)
		if !evicted {
			return
		}
	}
}
