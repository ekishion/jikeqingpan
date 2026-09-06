package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sync"
	"time"
)

// dirLinkStore 目录短链存储：token -> 网盘目录。
// 与下载短链（shortlink.go）的差异：无使用次数/不消耗——目录是反复导航的对象，
// 令牌语义是书签/分享；失效只靠 TTL 与进程重启（内存模型，见 docs/security.md）。
type dirLink struct {
	dir       string
	expiresAt time.Time
	createdAt time.Time
}

type dirLinkStore struct {
	mu         sync.Mutex
	links      map[string]dirLink
	ttl        time.Duration
	maxEntries int
}

var dirLinkTokenRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

const maxDirLinks = 10000

func newDirLinkStore(ttl time.Duration) *dirLinkStore {
	return newDirLinkStoreWithLimits(ttl, maxDirLinks)
}

func newDirLinkStoreWithLimits(ttl time.Duration, maxEntries int) *dirLinkStore {
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	if maxEntries < 1 {
		maxEntries = maxDirLinks
	}
	return &dirLinkStore{
		links:      make(map[string]dirLink),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

// create 为目录生成短链令牌；同目录重复创建会产生新令牌（旧令牌到期自然失效）。
func (s *dirLinkStore) create(dir string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, link := range s.links {
		if !now.Before(link.expiresAt) {
			delete(s.links, token)
		}
	}
	for len(s.links) >= s.maxEntries {
		evicted := evictOldestSampled(s.links, func(link dirLink) time.Time {
			return link.createdAt
		}, evictionSampleSize)
		if !evicted {
			break
		}
	}

	for i := 0; i < 3; i++ {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("生成目录短链令牌失败: %w", err)
		}
		token := hex.EncodeToString(buf)
		if _, exists := s.links[token]; exists {
			continue
		}
		s.links[token] = dirLink{
			dir:       dir,
			expiresAt: now.Add(s.ttl),
			createdAt: now,
		}
		return token, nil
	}
	return "", fmt.Errorf("生成目录短链令牌失败: 随机令牌冲突")
}

// resolve 返回令牌对应的目录；不校验路径权限（由 /api/files 的
// pathNavigable/filterFileList 在使用时重新校验）。
func (s *dirLinkStore) resolve(token string) (string, bool) {
	if !dirLinkTokenRe.MatchString(token) {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.links[token]
	if !ok {
		return "", false
	}
	if !time.Now().Before(link.expiresAt) {
		delete(s.links, token)
		return "", false
	}
	return link.dir, true
}
