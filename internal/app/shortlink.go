package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sync"
	"time"
)

var shortLinkTokenRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

type shortLink struct {
	filePath  string
	expiresAt time.Time
	createdAt time.Time
	maxUses   int // 0 = unlimited
	uses      int
}

type shortLinkStore struct {
	mu         sync.Mutex
	links      map[string]shortLink
	ttl        time.Duration
	maxEntries int
	maxUses    int
}

const maxShortLinks = 10000

func newShortLinkStore(ttl time.Duration, maxUses int) *shortLinkStore {
	return newShortLinkStoreWithLimits(ttl, maxShortLinks, maxUses)
}

func newShortLinkStoreWithLimits(ttl time.Duration, maxEntries, maxUses int) *shortLinkStore {
	if ttl <= 0 {
		ttl = time.Hour
	}
	if maxEntries < 1 {
		maxEntries = maxShortLinks
	}
	if maxUses < 0 {
		maxUses = 0
	}
	return &shortLinkStore{
		links:      make(map[string]shortLink),
		ttl:        ttl,
		maxEntries: maxEntries,
		maxUses:    maxUses,
	}
}

func (s *shortLinkStore) create(filePath string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, link := range s.links {
		if !now.Before(link.expiresAt) {
			delete(s.links, token)
		}
	}
	for len(s.links) >= s.maxEntries {
		evicted := evictOldestSampled(s.links, func(link shortLink) time.Time {
			return link.createdAt
		}, evictionSampleSize)
		if !evicted {
			break
		}
	}

	for i := 0; i < 3; i++ {
		buf := make([]byte, 16)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("生成短链接令牌失败: %w", err)
		}
		token := hex.EncodeToString(buf)
		if _, exists := s.links[token]; exists {
			continue
		}
		s.links[token] = shortLink{
			filePath:  filePath,
			expiresAt: now.Add(s.ttl),
			createdAt: now,
			maxUses:   s.maxUses,
		}
		return token, nil
	}
	return "", fmt.Errorf("生成短链接令牌失败: 随机令牌冲突")
}

// resolve 校验并返回文件路径。consume=true 时计入使用次数。
func (s *shortLinkStore) resolve(token string, consume bool) (string, bool) {
	if !shortLinkTokenRe.MatchString(token) {
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
	if link.maxUses > 0 && link.uses >= link.maxUses {
		delete(s.links, token)
		return "", false
	}
	if consume {
		link.uses++
		if link.maxUses > 0 && link.uses >= link.maxUses {
			delete(s.links, token)
		} else {
			s.links[token] = link
		}
	}
	return link.filePath, true
}

// snapshot 导出未过期且仍有剩余次数的条目用于落盘。
func (s *shortLinkStore) snapshot() []persistedLink {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	out := make([]persistedLink, 0, len(s.links))
	for token, link := range s.links {
		if !now.Before(link.expiresAt) {
			continue
		}
		if link.maxUses > 0 && link.uses >= link.maxUses {
			continue
		}
		out = append(out, persistedLink{
			Token:     token,
			Path:      link.filePath,
			ExpiresAt: link.expiresAt.UnixNano(),
			CreatedAt: link.createdAt.UnixNano(),
			Uses:      link.uses,
		})
	}
	return out
}

// restore 从落盘快照恢复；过期、超次或格式非法的条目直接丢弃。
// maxUses 取当前配置值（次数上限随部署策略走，不随快照）。
func (s *shortLinkStore) restore(links []persistedLink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, l := range links {
		if !shortLinkTokenRe.MatchString(l.Token) {
			continue
		}
		expiresAt := time.Unix(0, l.ExpiresAt)
		if !now.Before(expiresAt) {
			continue
		}
		if s.maxUses > 0 && l.Uses >= s.maxUses {
			continue
		}
		s.links[l.Token] = shortLink{
			filePath:  l.Path,
			expiresAt: expiresAt,
			createdAt: time.Unix(0, l.CreatedAt),
			maxUses:   s.maxUses,
			uses:      l.Uses,
		}
	}
}
