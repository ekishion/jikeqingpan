package app

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
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

// dirCacheEntry 缓存目录列表响应
type dirCacheEntry struct {
	body     []byte
	cachedAt time.Time
}

// readmeCacheEntry 缓存已读取的 README 内容
type readmeCacheEntry struct {
	name     string
	content  string
	cachedAt time.Time
}

// fileListCache 缓存文件列表中每个文件的元数据与目录列表，按路径索引
type fileListCache struct {
	mu           sync.RWMutex
	filesByPath  map[string]fileMeta         // 文件路径 -> 元数据
	dirsByPath   map[string]dirCacheEntry    // 目录路径 -> 目录列表 JSON
	readmeByPath map[string]readmeCacheEntry // 文件路径 -> README
	updatedAt    time.Time
	maxEntries   int
	maxDirs      int
	ttl          time.Duration
	dlinkTTL     time.Duration
}

const cachedDLinkTTL = 5 * time.Minute
const fileMetaTTL = 15 * time.Minute
const maxCachedFiles = 10000
const maxCachedDirs = 500
const maxCachedReadmes = 200

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
		filesByPath:  make(map[string]fileMeta),
		dirsByPath:   make(map[string]dirCacheEntry),
		readmeByPath: make(map[string]readmeCacheEntry),
		maxEntries:   maxEntries,
		maxDirs:      maxCachedDirs,
		ttl:          ttl,
		dlinkTTL:     dlinkTTL,
	}
}

// update 解析 Baidu 列表响应并更新文件元数据索引
func (c *fileListCache) update(listJSON []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updateLocked(listJSON, time.Now())
}

func (c *fileListCache) updateLocked(listJSON []byte, now time.Time) {
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
	if c.filesByPath == nil {
		c.filesByPath = make(map[string]fileMeta)
	}
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

// getDirList 根据目录路径获取缓存的列表响应
func (c *fileListCache) getDirList(dir string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.dirsByPath[dir]
	if !ok {
		return nil, false
	}
	if !entry.cachedAt.IsZero() && time.Since(entry.cachedAt) >= c.ttl {
		delete(c.dirsByPath, dir)
		return nil, false
	}
	return entry.body, true
}

// setDirList 缓存目录列表响应
func (c *fileListCache) setDirList(dir string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirsByPath == nil {
		c.dirsByPath = make(map[string]dirCacheEntry)
	}
	c.dirsByPath[dir] = dirCacheEntry{
		body:     bytes.Clone(body),
		cachedAt: time.Now(),
	}
	for len(c.dirsByPath) > c.maxDirs {
		evicted := evictOldestSampled(c.dirsByPath, func(e dirCacheEntry) time.Time {
			return e.cachedAt
		}, evictionSampleSize)
		if !evicted {
			break
		}
	}
}

// invalidateDir 删除指定目录的列表缓存及相关的 README 缓存
func (c *fileListCache) invalidateDir(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.dirsByPath, dir)
	cleanDir := strings.TrimSuffix(dir, "/")
	for p := range c.readmeByPath {
		if p == cleanDir || strings.HasPrefix(p, cleanDir+"/") {
			delete(c.readmeByPath, p)
		}
	}
}

// getReadme 获取缓存的 README 内容
func (c *fileListCache) getReadme(path string) (string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.readmeByPath[path]
	if !ok {
		return "", "", false
	}
	if !entry.cachedAt.IsZero() && time.Since(entry.cachedAt) >= c.ttl {
		delete(c.readmeByPath, path)
		return "", "", false
	}
	return entry.name, entry.content, true
}

// setReadme 缓存 README 内容
func (c *fileListCache) setReadme(path, name, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readmeByPath == nil {
		c.readmeByPath = make(map[string]readmeCacheEntry)
	}
	c.readmeByPath[path] = readmeCacheEntry{
		name:     name,
		content:  content,
		cachedAt: time.Now(),
	}
	for len(c.readmeByPath) > maxCachedReadmes {
		evicted := evictOldestSampled(c.readmeByPath, func(e readmeCacheEntry) time.Time {
			return e.cachedAt
		}, evictionSampleSize)
		if !evicted {
			break
		}
	}
}

// exportDirCache 导出有效的目录缓存用于持久化
func (c *fileListCache) exportDirCache() []PersistedDirCache {
	c.mu.RLock()
	defer c.mu.RUnlock()
	now := time.Now()
	var list []PersistedDirCache
	for dir, entry := range c.dirsByPath {
		if !entry.cachedAt.IsZero() && now.Sub(entry.cachedAt) >= c.ttl {
			continue
		}
		list = append(list, PersistedDirCache{
			Dir:      dir,
			Body:     json.RawMessage(entry.body),
			CachedAt: entry.cachedAt.Unix(),
		})
	}
	return list
}

// restoreDirCache 从持久化状态恢复目录缓存并预热文件元数据
func (c *fileListCache) restoreDirCache(entries []PersistedDirCache) {
	if len(entries) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirsByPath == nil {
		c.dirsByPath = make(map[string]dirCacheEntry)
	}
	now := time.Now()
	for _, entry := range entries {
		if entry.Dir == "" || len(entry.Body) == 0 {
			continue
		}
		cachedAt := time.Unix(entry.CachedAt, 0)
		if entry.CachedAt > 0 && now.Sub(cachedAt) >= c.ttl {
			continue
		}
		if entry.CachedAt == 0 {
			cachedAt = now
		}
		c.dirsByPath[entry.Dir] = dirCacheEntry{
			body:     bytes.Clone(entry.Body),
			cachedAt: cachedAt,
		}
		c.updateLocked(entry.Body, cachedAt)
	}
}

func (c *fileListCache) cleanupExpiredLocked(now time.Time) {
	for filePath, meta := range c.filesByPath {
		if !meta.CachedAt.IsZero() && now.Sub(meta.CachedAt) >= c.ttl {
			delete(c.filesByPath, filePath)
		}
	}
	for dir, entry := range c.dirsByPath {
		if !entry.cachedAt.IsZero() && now.Sub(entry.cachedAt) >= c.ttl {
			delete(c.dirsByPath, dir)
		}
	}
	for p, entry := range c.readmeByPath {
		if !entry.cachedAt.IsZero() && now.Sub(entry.cachedAt) >= c.ttl {
			delete(c.readmeByPath, p)
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
